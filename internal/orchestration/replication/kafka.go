package replication

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The CLI must not bind the broker's inherited JMX port.
const kafkaQuery = `unset JMX_PORT KAFKA_JMX_OPTS
exec kafka-topics.sh "$@"`

// --all bypasses Kafka's ACL-filtered topic-list precheck and queries DescribeConfigs directly.
const transactionTopicQuery = `unset JMX_PORT KAFKA_JMX_OPTS
if output="$(kafka-configs.sh "$@" --describe --all --entity-type topics --entity-name __transaction_state 2>&1)"; then
  printf 'present\n'
elif [[ "$output" == *AuthorizationException* ]]; then
  printf 'unverified\n'
elif [[ "$output" == *UnknownTopicOrPartitionException* ]]; then
  printf 'absent\n'
else
  printf 'unverified\n'
fi`

var (
	kafkaTopicPattern     = regexp.MustCompile(`\bTopic:\s*(\S+)`)
	kafkaPartitionPattern = regexp.MustCompile(`\bPartition:\s*(\d+)\b`)
	kafkaCountPattern     = regexp.MustCompile(`\bPartitionCount:\s*(\d+)\b`)
	kafkaFieldsPattern    = regexp.MustCompile(`\b(Leader|Replicas|Isr):\s*([-\d,]+)`)
)

func (c *Checker) checkKafka(ctx context.Context, inventory inventory) Result {
	members, err := inventory.members("kafka", "kafka", "kafka")
	if err != nil {
		return result("kafka", Unknown, err.Error())
	}
	if err := expectedMembers(members); err != nil {
		return result("kafka", Degraded, err.Error())
	}
	command := []string{"bash", "-ec", kafkaQuery, "replication-check", "--bootstrap-server", c.options.KafkaBootstrapServer, "--describe"}
	if c.options.KafkaClientProperties != "" {
		command = append(command, "--command-config", c.options.KafkaClientProperties)
	}
	data, err := c.query(ctx, members[0].pod.Name, "kafka", command)
	if err != nil {
		return result("kafka", Unknown, err.Error())
	}
	report := evaluateKafka(string(data))
	if report.Status == Unknown || hasTransactionTopic(string(data)) {
		return report
	}
	probe := []string{"bash", "-ec", transactionTopicQuery, "replication-check", "--bootstrap-server", c.options.KafkaBootstrapServer}
	if c.options.KafkaClientProperties != "" {
		probe = append(probe, "--command-config", c.options.KafkaClientProperties)
	}
	presence, err := c.query(ctx, members[0].pod.Name, "kafka", probe)
	if err != nil || strings.TrimSpace(string(presence)) != "absent" {
		report.Status = Unknown
		report.Messages = append(report.Messages, "__transaction_state was not listed and its absence could not be verified; check topic permissions or retry")
		return report
	}
	report.Messages = append(report.Messages, "__transaction_state is absent; transaction-topic replication is not applicable to this observation")
	return report
}

func hasTransactionTopic(output string) bool {
	for _, match := range kafkaTopicPattern.FindAllStringSubmatch(output, -1) {
		if match[1] == "__transaction_state" {
			return true
		}
	}
	return false
}

func brokerIDs(value string) (map[int]bool, error) {
	if value == "" {
		return nil, fmt.Errorf("missing broker IDs")
	}
	ids := make(map[int]bool)
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.Atoi(part)
		if err != nil || id < 0 || ids[id] {
			return nil, fmt.Errorf("invalid or duplicate broker ID")
		}
		ids[id] = true
	}
	return ids, nil
}

func partitionProblem(line string) string {
	fields := make(map[string]string)
	for _, match := range kafkaFieldsPattern.FindAllStringSubmatch(line, -1) {
		fields[match[1]] = match[2]
	}
	replicas, err := brokerIDs(fields["Replicas"])
	if err != nil || len(replicas) < minReplicas {
		return "fewer than two distinct assigned replicas or invalid assignment"
	}
	isr, err := brokerIDs(fields["Isr"])
	if err != nil || len(isr) != len(replicas) {
		return "assigned replicas are not all in sync"
	}
	for id := range replicas {
		if !isr[id] {
			return "assigned replicas are not all in sync"
		}
	}
	leader, err := strconv.Atoi(fields["Leader"])
	if err != nil || !isr[leader] {
		return "no available in-sync leader"
	}
	return ""
}

func evaluateKafka(output string) Result {
	counts := make(map[string]int)
	partitions := make(map[string]map[int]bool)
	var problems []string
	for _, line := range strings.Split(output, "\n") {
		topicMatch := kafkaTopicPattern.FindStringSubmatch(line)
		if topicMatch == nil {
			continue
		}
		topic := topicMatch[1]
		if count := kafkaCountPattern.FindStringSubmatch(line); count != nil {
			value, err := strconv.Atoi(count[1])
			if err != nil || value < 1 || counts[topic] != 0 {
				return result("kafka", Unknown, "invalid or duplicate topic summary")
			}
			counts[topic] = value
		}
		if partition := kafkaPartitionPattern.FindStringSubmatch(line); partition != nil {
			id, err := strconv.Atoi(partition[1])
			if err != nil || partitions[topic][id] {
				return result("kafka", Unknown, "invalid or duplicate partition description")
			}
			if partitions[topic] == nil {
				partitions[topic] = make(map[int]bool)
			}
			partitions[topic][id] = true
			if problem := partitionProblem(line); problem != "" {
				problems = append(problems, fmt.Sprintf("%s partition %d: %s", topic, id, problem))
			}
		}
	}
	if err := completeKafkaEvidence(counts, partitions); err != nil {
		return result("kafka", Unknown, err.Error())
	}
	report := result("kafka", Healthy, fmt.Sprintf("all partitions of %d topics have at least two replicas, complete ISR and an in-sync leader", len(counts)))
	if len(problems) > 0 {
		report = Result{Component: "kafka", Status: Degraded, Messages: problems}
	}
	return report
}

func completeKafkaEvidence(counts map[string]int, partitions map[string]map[int]bool) error {
	for _, topic := range []string{"__consumer_offsets"} {
		if counts[topic] == 0 {
			return fmt.Errorf("required internal topic %s is absent; initialize its workload and repeat the check", topic)
		}
	}
	if len(counts) != len(partitions) {
		return fmt.Errorf("topic summaries and partition descriptions do not match")
	}
	for topic, count := range counts {
		if len(partitions[topic]) != count {
			return fmt.Errorf("incomplete partition descriptions for %s", topic)
		}
		for id := 0; id < count; id++ {
			if !partitions[topic][id] {
				return fmt.Errorf("missing partition %d for %s", id, topic)
			}
		}
	}
	return nil
}

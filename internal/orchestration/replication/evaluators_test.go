package replication

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func hdfsFixture(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	fields := map[string]any{
		"name":                  "Hadoop:service=NameNode,name=FSNamesystem",
		"UnderReplicatedBlocks": 0, "MissingBlocks": 0, "CorruptBlocks": 0, "PendingReplicationBlocks": 0, "NumLiveDataNodes": 3, "Safemode": "",
	}
	mutate(fields)
	return encode(t, map[string]any{"configuredReplication": 3, "jmx": map[string]any{"beans": []any{fields}}})
}

func TestHDFSEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		status string
	}{
		{"recovered", func(map[string]any) {}, Healthy},
		{"replicating", func(m map[string]any) { m["UnderReplicatedBlocks"] = 12 }, Degraded},
		{"missing block", func(m map[string]any) { m["MissingBlocks"] = 1 }, Degraded},
		{"corrupt block", func(m map[string]any) { m["CorruptBlocks"] = 1 }, Degraded},
		{"pending replication", func(m map[string]any) { m["PendingReplicationBlocks"] = 2 }, Degraded},
		{"missing datanode", func(m map[string]any) { m["NumLiveDataNodes"] = 2 }, Degraded},
		{"safe mode", func(m map[string]any) { m["Safemode"] = "ON" }, Degraded},
		{"missing metric", func(m map[string]any) { delete(m, "UnderReplicatedBlocks") }, Unknown},
		{"null metric", func(m map[string]any) { m["CorruptBlocks"] = nil }, Unknown},
		{"negative metric", func(m map[string]any) { m["MissingBlocks"] = -1 }, Unknown},
		{"unrelated bean", func(m map[string]any) { m["name"] = "Hadoop:service=Other" }, Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.status, evaluateHDFS(hdfsFixture(t, test.mutate), 3).Status)
		})
	}
	data := strings.Replace(string(hdfsFixture(t, func(map[string]any) {})), `"configuredReplication":3`, `"configuredReplication":1`, 1)
	assert.Equal(t, Degraded, evaluateHDFS([]byte(data), 3).Status)
	assert.Equal(t, Unknown, evaluateHDFS([]byte(`{"beans":[]}`), 3).Status)
}

func esFixture() map[string]any {
	return map[string]any{
		"status": "green", "number_of_nodes": 3, "unassigned_shards": 0, "initializing_shards": 0, "relocating_shards": 0,
		"indices": map[string]any{"events": map[string]any{"number_of_replicas": 1}},
	}
}

func TestElasticsearchEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		status string
	}{
		{"recovered", func(map[string]any) {}, Healthy},
		{"yellow", func(m map[string]any) { m["status"] = "yellow" }, Degraded},
		{"node missing", func(m map[string]any) { m["number_of_nodes"] = 2 }, Degraded},
		{"initializing", func(m map[string]any) { m["initializing_shards"] = 1 }, Degraded},
		{"relocating", func(m map[string]any) { m["relocating_shards"] = 1 }, Degraded},
		{"unassigned", func(m map[string]any) { m["unassigned_shards"] = 1 }, Degraded},
		{"green without replicas", func(m map[string]any) {
			m["indices"] = map[string]any{"events": map[string]any{"number_of_replicas": 0}}
		}, Degraded},
		{"empty cluster", func(m map[string]any) { m["indices"] = map[string]any{} }, Unknown},
		{"partial response", func(m map[string]any) { delete(m, "initializing_shards") }, Unknown},
		{"missing index replication", func(m map[string]any) { m["indices"] = map[string]any{"events": map[string]any{}} }, Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := esFixture()
			test.mutate(fields)
			assert.Equal(t, test.status, evaluateElasticsearch(encode(t, fields), 3).Status)
		})
	}
}

func kafkaFixture() string {
	var lines []string
	for _, topic := range []string{"events", "__consumer_offsets", "__transaction_state"} {
		lines = append(lines, fmt.Sprintf("Topic: %s TopicId: x PartitionCount: 1 ReplicationFactor: 2 Configs: min.insync.replicas=1", topic))
		lines = append(lines, fmt.Sprintf("  Topic: %s Partition: 0 Leader: 0 Replicas: 0,1 Isr: 1,0", topic))
	}
	return strings.Join(lines, "\n")
}

func TestKafkaEvidence(t *testing.T) {
	tests := []struct {
		name, from, to, status string
	}{
		{"recovered", "Isr: 1,0", "Isr: 1,0", Healthy},
		{"single replica transaction state", "Leader: 0 Replicas: 0,1 Isr: 1,0", "Leader: 0 Replicas: 0 Isr: 0", Degraded},
		{"missing ISR", "Isr: 1,0", "Isr: 0", Degraded},
		{"wrong ISR", "Isr: 1,0", "Isr: 2,0", Degraded},
		{"missing leader", "Leader: 0", "Leader: -1", Degraded},
		{"duplicate replica", "Replicas: 0,1", "Replicas: 0,0", Degraded},
		{"empty ISR", "Isr: 1,0", "Isr:", Degraded},
		{"unused transactions", "__transaction_state", "another-topic", Healthy},
		{"missing consumer offsets", "__consumer_offsets", "another-topic", Unknown},
		{"incomplete partitions", "PartitionCount: 1", "PartitionCount: 2", Unknown},
		{"wrong partition ID", "Partition: 0", "Partition: 5", Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := strings.ReplaceAll(kafkaFixture(), test.from, test.to)
			assert.Equal(t, test.status, evaluateKafka(output).Status)
		})
	}
	assert.Equal(t, Unknown, evaluateKafka("").Status)
	assert.Equal(t, Unknown, evaluateKafka(kafkaFixture()+"\n"+kafkaFixture()).Status)
}

func clickhouseFixture() map[string]any {
	return map[string]any{
		"database": "otel", "table": "traces", "zookeeper_path": "/clickhouse/tables/shard0/traces",
		"replica_name": "replica0", "is_readonly": 0, "is_session_expired": 0,
		"total_replicas": "2", "active_replicas": "2", "log_max_index": "100", "log_pointer": "101",
		"pending_data_tasks": "0", "absolute_delay": "0", "last_queue_update_exception": "", "zookeeper_exception": "",
	}
}

func TestClickHouseReplicaEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		status string
	}{
		{"recovered", func(map[string]any) {}, Healthy},
		{"merge backlog alone", func(m map[string]any) { m["queue_size"] = 8; m["merges_in_queue"] = 8 }, Healthy},
		{"lagging log", func(m map[string]any) { m["log_pointer"] = "100" }, Degraded},
		{"data backlog", func(m map[string]any) { m["pending_data_tasks"] = "1" }, Degraded},
		{"read only", func(m map[string]any) { m["is_readonly"] = 1 }, Degraded},
		{"expired session", func(m map[string]any) { m["is_session_expired"] = 1 }, Degraded},
		{"inactive replica", func(m map[string]any) { m["active_replicas"] = "1" }, Degraded},
		{"wrong replication", func(m map[string]any) { m["total_replicas"] = "1" }, Degraded},
		{"coordination exception", func(m map[string]any) { m["zookeeper_exception"] = "connection failed" }, Degraded},
		{"historical queue exception after recovery", func(m map[string]any) { m["last_queue_update_exception"] = "previous Keeper error" }, Healthy},
		{"queue exception with backlog", func(m map[string]any) {
			m["last_queue_update_exception"] = "Keeper error"
			m["log_pointer"] = "100"
		}, Degraded},
		{"missing backlog evidence", func(m map[string]any) { delete(m, "pending_data_tasks") }, Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := clickhouseFixture()
			test.mutate(row)
			observations, err := parseClickHouse(encode(t, map[string]any{"data": []any{row}}), "pod0", 2)
			if test.status == Unknown {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			peer := observations[0]
			peer.pod, peer.name, peer.problems = "pod1", "replica1", nil
			report := evaluateClickHouse(append(observations, peer))
			assert.Equal(t, test.status, report.Status)
			if test.name == "historical queue exception after recovery" {
				assert.Contains(t, strings.Join(report.Messages, " "), "previous queue-update exception")
			}
		})
	}
}

func TestClickHouseChecksEveryTableReplica(t *testing.T) {
	observations, err := parseClickHouse(encode(t, map[string]any{"data": []any{clickhouseFixture()}}), "pod0", 2)
	require.NoError(t, err)
	assert.Equal(t, Degraded, evaluateClickHouse(observations).Status)
	assert.Equal(t, Unknown, evaluateClickHouse(append(observations, observations...)).Status)
	_, err = parseClickHouse([]byte(`{"data":[]}`), "pod0", 2)
	require.Error(t, err)
}

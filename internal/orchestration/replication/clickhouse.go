package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

const clickhouseSQL = `SELECT database, table, zookeeper_path, replica_name,
is_readonly, is_session_expired, total_replicas, active_replicas,
log_max_index, log_pointer, absolute_delay, last_queue_update_exception, zookeeper_exception,
ifNull(pending_data_tasks, 0) AS pending_data_tasks
FROM system.replicas
LEFT JOIN (
 SELECT database, table, countIf(type != 'MERGE_PARTS') AS pending_data_tasks
 FROM system.replication_queue GROUP BY database, table
) USING (database, table)
WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
FORMAT JSON`

const clickhouseQuery = `export CLICKHOUSE_PASSWORD="${CLICKHOUSE_ADMIN_PASSWORD:?missing ClickHouse credentials}"
exec clickhouse-client --host 127.0.0.1 --port "${CLICKHOUSE_TCP_PORT:-9000}" \
 --user "${CLICKHOUSE_ADMIN_USER:?missing ClickHouse user}" --readonly 1 --query "$1"`

type replicaObservation struct {
	path     string
	name     string
	pod      string
	total    int64
	problems []string
}

func (c *Checker) checkClickHouse(ctx context.Context, inventory inventory) Result {
	members, err := inventory.members("clickhouse")
	if err != nil {
		return result("clickhouse", Unknown, err.Error())
	}
	var observations []replicaObservation
	for _, member := range members {
		if member.replicas < minReplicas {
			return result("clickhouse", Degraded, fmt.Sprintf("%s belongs to a shard with fewer than two replicas", member.pod.Name))
		}
		command := []string{"bash", "-ec", clickhouseQuery, "replication-check", clickhouseSQL}
		data, err := c.query(ctx, member.pod.Name, "clickhouse", command)
		if err != nil {
			return result("clickhouse", Unknown, err.Error())
		}
		rows, err := parseClickHouse(data, member.pod.Name, member.replicas)
		if err != nil {
			return result("clickhouse", Unknown, fmt.Sprintf("%s: %v", member.pod.Name, err))
		}
		observations = append(observations, rows...)
	}
	return evaluateClickHouse(observations)
}

func parseClickHouse(data []byte, pod string, expected int) ([]replicaObservation, error) {
	var response struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil || len(response.Data) == 0 {
		return nil, fmt.Errorf("no replicated-table evidence returned")
	}
	var observations []replicaObservation
	for _, row := range response.Data {
		observation, err := parseReplica(row, pod, expected)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func parseReplica(row map[string]json.RawMessage, pod string, expected int) (replicaObservation, error) {
	fields := make(map[string]string)
	for _, key := range []string{"database", "table", "zookeeper_path", "replica_name", "last_queue_update_exception", "zookeeper_exception"} {
		value, err := text(row, key)
		if err != nil {
			return replicaObservation{}, err
		}
		fields[key] = value
	}
	counts, err := numbers(row, "is_readonly", "is_session_expired", "total_replicas", "active_replicas",
		"log_max_index", "log_pointer", "pending_data_tasks", "absolute_delay")
	if err != nil {
		return replicaObservation{}, err
	}
	if fields["zookeeper_path"] == "" || fields["replica_name"] == "" || fields["database"] == "" || fields["table"] == "" {
		return replicaObservation{}, fmt.Errorf("missing replicated-table identity")
	}
	observation := replicaObservation{path: fields["zookeeper_path"], name: fields["replica_name"], pod: pod, total: counts["total_replicas"]}
	if observation.total < minReplicas || observation.total != int64(expected) || counts["active_replicas"] != observation.total {
		observation.problems = append(observation.problems, fmt.Sprintf("%d/%d replicas active; %d expected", counts["active_replicas"], observation.total, expected))
	}
	if counts["is_readonly"] != 0 || counts["is_session_expired"] != 0 {
		observation.problems = append(observation.problems, "replica is read-only or coordination session expired")
	}
	if counts["log_pointer"] <= counts["log_max_index"] || counts["pending_data_tasks"] != 0 {
		observation.problems = append(observation.problems, fmt.Sprintf("replication backlog: %d data tasks; reported delay %ds", counts["pending_data_tasks"], counts["absolute_delay"]))
	}
	if fields["last_queue_update_exception"] != "" || fields["zookeeper_exception"] != "" {
		observation.problems = append(observation.problems, "replication queue or coordination query reported an exception")
	}
	return observation, nil
}

func evaluateClickHouse(observations []replicaObservation) Result {
	groups := make(map[string][]replicaObservation)
	var problems []string
	for _, observation := range observations {
		groups[observation.path] = append(groups[observation.path], observation)
		for _, problem := range observation.problems {
			problems = append(problems, fmt.Sprintf("%s %s: %s", observation.pod, observation.path, problem))
		}
	}
	if len(groups) == 0 {
		return result("clickhouse", Unknown, "no replicated tables found")
	}
	for path, replicas := range groups {
		names := make(map[string]bool)
		pods := make(map[string]bool)
		for _, replica := range replicas {
			if names[replica.name] || pods[replica.pod] || replica.total != replicas[0].total {
				return result("clickhouse", Unknown, "duplicate or inconsistent replica evidence for "+path)
			}
			names[replica.name], pods[replica.pod] = true, true
		}
		if int64(len(replicas)) != replicas[0].total {
			problems = append(problems, fmt.Sprintf("%s: queried %d/%d table replicas", path, len(replicas), replicas[0].total))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return Result{Component: "clickhouse", Status: Degraded, Messages: problems}
	}
	return result("clickhouse", Healthy, fmt.Sprintf("%d replicated table groups checked on every member; no pending data replication tasks", len(groups)))
}

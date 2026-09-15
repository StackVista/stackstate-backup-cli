package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

const elasticsearchQuery = `set -- --fail --silent --show-error --max-time 20
if [ -n "${ELASTIC_PASSWORD:-}" ]; then
  set -- "$@" --user "elastic:${ELASTIC_PASSWORD}"
fi
exec curl "$@" 'http://127.0.0.1:9200/_cluster/health?level=indices'`

func (c *Checker) checkElasticsearch(ctx context.Context, inventory inventory) Result {
	members, err := inventory.members("elasticsearch", "", "elasticsearch")
	if err != nil {
		return result("elasticsearch", Unknown, err.Error())
	}
	if err := expectedMembers(members); err != nil {
		return result("elasticsearch", Degraded, err.Error())
	}
	command := []string{"bash", "-ec", elasticsearchQuery}
	data, err := c.query(ctx, members[0].pod.Name, "elasticsearch", command)
	if err != nil {
		return result("elasticsearch", Unknown, err.Error())
	}
	return evaluateElasticsearch(data, len(members))
}

func evaluateElasticsearch(data []byte, expected int) Result {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return result("elasticsearch", Unknown, "invalid cluster health response")
	}
	counts, err := numbers(fields, "number_of_nodes", "unassigned_shards", "initializing_shards", "relocating_shards")
	if err != nil {
		return result("elasticsearch", Unknown, err.Error())
	}
	status, err := text(fields, "status")
	if err != nil {
		return result("elasticsearch", Unknown, err.Error())
	}
	var indices map[string]map[string]json.RawMessage
	if err := json.Unmarshal(fields["indices"], &indices); err != nil || len(indices) == 0 {
		return result("elasticsearch", Unknown, "no index replication evidence returned")
	}
	var problems []string
	if status != "green" || counts["number_of_nodes"] != int64(expected) {
		problems = append(problems, fmt.Sprintf("cluster status %s; %d/%d expected nodes", status, counts["number_of_nodes"], expected))
	}
	for _, key := range []string{"unassigned_shards", "initializing_shards", "relocating_shards"} {
		if counts[key] > 0 {
			problems = append(problems, fmt.Sprintf("%s=%d", key, counts[key]))
		}
	}
	for name, index := range indices {
		replicas, err := number(index, "number_of_replicas")
		if err != nil {
			return result("elasticsearch", Unknown, fmt.Sprintf("index %s: %v", name, err))
		}
		if replicas < minReplicas-1 {
			problems = append(problems, fmt.Sprintf("index %s has no replica shard", name))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return Result{Component: "elasticsearch", Status: Degraded, Messages: problems}
	}
	return result("elasticsearch", Healthy, fmt.Sprintf("%d indices have replica shards; all shards allocated with no recovery or relocation", len(indices)))
}

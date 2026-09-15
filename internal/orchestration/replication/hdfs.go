package replication

import (
	"context"
	"encoding/json"
	"fmt"
)

const hdfsQuery = `unset HADOOP_OPTS
printf '{"configuredReplication":'
hdfs getconf -confKey dfs.replication
printf ',"minimumReplication":'
hdfs getconf -confKey dfs.namenode.replication.min
printf ',"jmx":'
curl --fail --silent --show-error --max-time 20 'http://127.0.0.1:50070/jmx'
printf '}'`

func (c *Checker) checkHDFS(ctx context.Context, inventory inventory) Result {
	namenodes, err := inventory.members("hbase", "hdfs-nn", "namenode")
	if err != nil {
		return result("hdfs", Unknown, err.Error())
	}
	if len(namenodes) != 1 {
		return result("hdfs", Unknown, "expected the chart's single NameNode; external or HA NameNode layouts are not supported")
	}
	datanodes, err := inventory.members("hbase", "hdfs-dn", "datanode")
	if err != nil {
		return result("hdfs", Unknown, err.Error())
	}
	if err := expectedMembers(datanodes); err != nil {
		return result("hdfs", Degraded, err.Error())
	}
	data, err := c.query(ctx, namenodes[0].pod.Name, "namenode", []string{"bash", "-ec", hdfsQuery})
	if err != nil {
		return result("hdfs", Unknown, err.Error())
	}
	return evaluateHDFS(data, len(datanodes))
}

func evaluateHDFS(data []byte, expected int) Result {
	var response struct {
		Replication *int `json:"configuredReplication"`
		Minimum     *int `json:"minimumReplication"`
		JMX         struct {
			Beans []map[string]json.RawMessage `json:"beans"`
		} `json:"jmx"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Replication == nil || response.Minimum == nil {
		return result("hdfs", Unknown, "invalid HDFS replication/JMX response")
	}
	fields := make(map[string]json.RawMessage)
	for _, bean := range response.JMX.Beans {
		name, _ := text(bean, "name")
		switch name {
		case "Hadoop:service=NameNode,name=FSNamesystem", "Hadoop:service=NameNode,name=FSNamesystemState", "Hadoop:service=NameNode,name=NameNodeInfo":
			for key, value := range bean {
				fields[key] = value
			}
		}
	}
	counts, err := numbers(fields, "UnderReplicatedBlocks", "MissingBlocks", "CorruptBlocks", "PendingReplicationBlocks", "NumLiveDataNodes")
	if err != nil {
		return result("hdfs", Unknown, err.Error())
	}
	safemode, err := text(fields, "Safemode")
	if err != nil {
		return result("hdfs", Unknown, err.Error())
	}
	var problems []string
	if *response.Replication < minReplicas {
		problems = append(problems, fmt.Sprintf("configured block replication is %d; at least %d required", *response.Replication, minReplicas))
	}
	if *response.Minimum < minReplicas {
		problems = append(problems, fmt.Sprintf("minimum write replication is %d; at least %d required", *response.Minimum, minReplicas))
	}
	if safemode != "" {
		problems = append(problems, "NameNode is in safe mode")
	}
	if counts["NumLiveDataNodes"] != int64(expected) {
		problems = append(problems, fmt.Sprintf("%d/%d expected DataNodes are live", counts["NumLiveDataNodes"], expected))
	}
	for _, key := range []string{"UnderReplicatedBlocks", "MissingBlocks", "CorruptBlocks", "PendingReplicationBlocks"} {
		if counts[key] != 0 {
			problems = append(problems, fmt.Sprintf("%s=%d", key, counts[key]))
		}
	}
	if len(problems) > 0 {
		return Result{Component: "hdfs", Status: Degraded, Messages: problems}
	}
	return result("hdfs", Healthy, fmt.Sprintf("%d DataNodes live; no missing, corrupt, under-replicated or pending-replication blocks", expected))
}

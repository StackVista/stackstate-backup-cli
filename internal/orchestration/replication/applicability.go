package replication

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
)

const (
	hdfsComponent       = "hdfs"
	clickhouseComponent = "clickhouse"
	nameNodeComponent   = "hdfs-nn"
	dataNodeComponent   = "hdfs-dn"
	monoComponent       = "stackgraph"
)

func workloadComponent(workload appsv1.StatefulSet) string {
	name := workload.Labels["app.kubernetes.io/name"]
	component := workload.Labels["app.kubernetes.io/component"]
	if name == "hbase" {
		switch component {
		case nameNodeComponent, dataNodeComponent, monoComponent:
			return hdfsComponent
		default:
			return ""
		}
	}
	switch name {
	case "elasticsearch", clickhouseComponent, "kafka", "zookeeper":
		return name
	default:
		return ""
	}
}

func databaseContainer(workload appsv1.StatefulSet) string {
	if workloadComponent(workload) == hdfsComponent {
		switch workload.Labels["app.kubernetes.io/component"] {
		case nameNodeComponent:
			return "namenode"
		case dataNodeComponent:
			return "datanode"
		case monoComponent:
			return monoComponent
		}
	}
	return workloadComponent(workload)
}

// Use desired replicas, never the number of surviving or Ready pods.
func (i inventory) applicability(component string) *Result {
	expected := make(map[string]appsv1.StatefulSet)
	for _, workload := range i.workloads {
		if workloadComponent(workload) == component {
			if _, duplicate := expected[workload.Name]; duplicate {
				return resultPointer(component, Unknown, "duplicate database workload")
			}
			expected[workload.Name] = workload
		}
	}
	if len(expected) == 0 {
		return resultPointer(component, Unknown, "no supported database StatefulSet found; select components explicitly if this database is intentionally disabled")
	}
	for _, workload := range expected {
		container := databaseContainer(workload)
		if !hasContainer(workload.Spec.Template.Spec.Containers, container) {
			return resultPointer(component, Unknown, fmt.Sprintf("%s has an unsupported container layout", workload.Name))
		}
		if _, err := i.workloadPods(workload); err != nil {
			return resultPointer(component, Unknown, err.Error())
		}
	}
	return replicationApplicability(component, expected)
}

func replicationApplicability(component string, expected map[string]appsv1.StatefulSet) *Result {
	total, single, shards := 0, 0, 0
	namenodes := 0
	for _, workload := range expected {
		if component == hdfsComponent {
			switch workload.Labels["app.kubernetes.io/component"] {
			case monoComponent:
				if len(expected) != 1 || *workload.Spec.Replicas != 1 {
					return resultPointer(component, Unknown, "unsupported HBase mono layout")
				}
				return resultPointer(component, NotApplicable, "HBase mono layout does not use distributed HDFS; availability checked; storage redundancy not checked")
			case dataNodeComponent:
			case nameNodeComponent:
				if *workload.Spec.Replicas != 1 {
					return resultPointer(component, Unknown, "multiple NameNode replicas are not supported")
				}
				namenodes++
				continue
			default:
				continue
			}
		}
		total += int(*workload.Spec.Replicas)
		shards++
		if *workload.Spec.Replicas == 1 {
			single++
		}
	}
	if component == hdfsComponent && (namenodes != 1 || shards == 0) {
		return resultPointer(component, Unknown, "distributed HDFS layout requires a NameNode and DataNodes")
	}
	if total == 1 || component == clickhouseComponent && shards == single {
		return resultPointer(component, NotApplicable, "StatefulSet configuration has one replica per database group; availability checked; application replication not applicable; storage redundancy not checked")
	}
	if component == clickhouseComponent && single > 0 {
		return resultPointer(component, Unknown, "mixed single-replica and replicated ClickHouse shards are not supported")
	}
	return nil
}

func resultPointer(component, status, message string) *Result {
	value := result(component, status, message)
	return &value
}

package replication

import (
	"context"
	"fmt"
	"slices"
)

// Summary counters include departing replicas and incomplete WAL blocks; audit per-block live replicas instead.
const hdfsAuditQuery = `unset HADOOP_OPTS
exec hdfs fsck / -files -blocks -openforwrite -includeSnapshots`

// Verify audits HDFS before success, then rechecks the selected databases.
func (c *Checker) Verify(ctx context.Context, previous Report) Report {
	if previous.Status != Healthy || !slices.ContainsFunc(previous.Checks, func(check Result) bool {
		return check.Component == hdfsComponent && check.Status == Healthy
	}) {
		return previous
	}
	report := previous
	report.Checks = slices.Clone(previous.Checks)
	before, err := c.discover(ctx)
	if err != nil {
		return replaceHDFS(report, result(hdfsComponent, Unknown, err.Error()))
	}
	if before.fingerprint(c.options.Components) != previous.topology {
		return invalidateAudit(report)
	}
	namenodes, err := before.members("hbase", nameNodeComponent, "namenode")
	if err != nil || len(namenodes) != 1 {
		return replaceHDFS(report, result(hdfsComponent, Unknown, "HDFS audit requires one available NameNode"))
	}
	audit := c.auditHDFS(ctx, namenodes[0].pod.Name)
	if audit.Status != Healthy {
		return replaceHDFS(report, audit)
	}
	current := c.Check(ctx)
	current.CheckedAt = previous.CheckedAt
	if current.topology != previous.topology {
		return invalidateAudit(current)
	}
	for n := range current.Checks {
		if current.Checks[n].Component == hdfsComponent && current.Checks[n].Status == Healthy {
			current.Checks[n].Messages = append(current.Checks[n].Messages, audit.Messages...)
		}
	}
	return current
}

func (c *Checker) auditHDFS(ctx context.Context, pod string) Result {
	ctx, cancel := context.WithTimeout(ctx, c.options.HDFSAuditTimeout)
	defer cancel()
	parser := &fsckParser{}
	err := c.kube.ExecTo(ctx, c.options.Namespace, pod, "namenode", []string{"bash", "-ec", hdfsAuditQuery}, parser)
	if ctx.Err() != nil {
		return result(hdfsComponent, Unknown, fmt.Sprintf("HDFS block audit did not complete: %v", ctx.Err()))
	}
	audit := parser.result()
	if err != nil && audit.Status != Degraded {
		failure := result(hdfsComponent, Unknown, fmt.Sprintf("HDFS block audit execution failed: %v", err))
		if audit.Status == Unknown {
			failure.Messages = append(failure.Messages, audit.Messages...)
		}
		return failure
	}
	return audit
}

func replaceHDFS(report Report, audit Result) Report {
	for n := range report.Checks {
		if report.Checks[n].Component == hdfsComponent {
			report.Checks[n] = audit
		}
	}
	report.Status = reportStatus(report.Checks)
	return report
}

func invalidateAudit(report Report) Report {
	report.Status = Unknown
	for n := range report.Checks {
		report.Checks[n].Status = Unknown
		report.Checks[n].Messages = append(report.Checks[n].Messages, "database state changed around the HDFS audit; repeat the observation")
	}
	return report
}

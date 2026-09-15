# Check database replication

`sts-backup replication check` inspects the chart-managed databases in a
namespace containing one SUSE Observability installation. It does not require
the backup ConfigMap, backup Secret, or enabled backups.

```bash
sts-backup replication check --namespace observability
```

For testing from a source checkout:

```bash
go run . replication check --namespace observability --wait
```

Replace `observability` with the installation namespace. The command uses the
current kubeconfig context; use `--kubeconfig <path>` to select another config.

The command returns exit code **0** when every selected component is `healthy`
or `not_applicable`. Any degraded, missing, inaccessible, unsupported or
incompletely described component returns exit code **1**. A missing component
is never silently skipped.

Checks follow the deployed topology: StatefulSet desired replica counts already
reflect the sizing profile and its overrides. A component configured with one
replica per database group is `not_applicable` after its Kubernetes availability
checks pass. HBase mono is also `not_applicable` for distributed HDFS checks.
This works with Helm or GitOps deployments without reading Helm release records,
Secrets or a profile name. It does not infer configuration from the number of
surviving pods.

Use JSON for automation:

```bash
sts-backup replication check \
  --namespace observability \
  --output json
```

The report identifies the namespace, observation time, selected
components, status (`healthy`, `degraded`, `unknown` or `not_applicable`) and
diagnostic messages. The status describes only the selected checks. If all
selected components are `not_applicable`, the overall status is also
`not_applicable`; exit code zero does not establish application redundancy.

## Wait for recovery

```bash
sts-backup replication check \
  --namespace observability \
  --wait --timeout 15m --interval 10s --stable-for 30s \
  --output json > replication.json
```

Wait mode requires consecutive successful observations spanning `--stable-for`,
with all applicable replication checks healthy and all selected workloads
available. Components marked `not_applicable` do not prevent success.
If every selected component is `not_applicable`, wait mode completes after the
first successful availability observation.
The default is 30 seconds, starting when the first fully healthy observation
completes. Earlier rounds with any `unknown` or `degraded` component do not
count. An unsuccessful observation resets the period.
Changes to relevant database topology, readiness or container identity also
reset the period between observations, even if each observation is healthy.
Progress explains when such a change restarts the timer.

Progress includes UTC timestamps and shows the elapsed and remaining healthy
period, resets, and successful completion. Seeing every component healthy
does not mean the stability period has already elapsed. Database queries take
time; the checker needs another completed healthy observation to confirm the
period, rather than exiting on a timer alone.
Once stable, an applicable HDFS check also needs its final block audit to pass.
Progress announces final verification before reporting completion. A failed
audit resets stability and delays the next observation by at least 30 seconds
(or `--interval`, if longer).

To finish on the first fully healthy observation, explicitly use
`--wait --stable-for 0s`.

Progress goes to stderr; stdout contains one final report. Table output includes
the report time and the start time of the last completed observation. JSON
retains its `checkedAt` timestamp. The overall deadline includes database
queries and the final audit. `--request-timeout` bounds ordinary API requests
and database probes; `--hdfs-audit-timeout` separately bounds the HDFS audit.

Ctrl+C stops further queries. Cancellation or timeout returns nonzero and sets
the overall result to `unknown`, retaining the last completed observation
instead of replacing it with errors from interrupted queries. If no observation
completed, the report says so. Healthy component results in a cancelled report
describe that previous observation; the wait did not finish successfully.

Select an explicit subset if a database is intentionally disabled:

```bash
sts-backup replication check \
  --namespace observability \
  --components hdfs,elasticsearch,kafka
```

This selection does not validate the omitted database.

## What is checked

The checker discovers StatefulSets using `app.kubernetes.io/name`
(`hbase`, `elasticsearch`, `kafka`, `clickhouse`, `zookeeper`) and
`app.kubernetes.io/component` within the namespace. The HDFS components
`hdfs-nn` and `hdfs-dn` distinguish the NameNode and DataNodes from the
SecondaryNameNode. It assumes one SUSE Observability installation per
namespace; no Helm release name is required. It verifies the desired pods
exist, belong to those StatefulSets, are Ready, and are not terminating or
undergoing a rollout.
It queries database state and compares relevant Kubernetes state before and
afterward. Changes to selected StatefulSet membership, desired replicas,
generation, rollout revisions or convergence invalidate the observation.
So do changes to their Pods' identity, ownership, node assignment, readiness
transitions or termination, and database container identity, start time or
restart count.

Resource versions, unrelated annotations and readiness heartbeat timestamps do
not invalidate observations. Workloads outside the selected checks are excluded.
These comparisons detect changes visible in the sampled state; they are not a
continuous Kubernetes event watch.

| Component | Replication evidence |
|---|---|
| HDFS | Configured default and minimum write replication are at least two; all expected DataNodes are live; the NameNode is out of safe mode; no missing, corrupt, under-replicated or pending-replication blocks are reported by JMX. Before overall success, a file/block audit verifies completed blocks against their replication targets. |
| Elasticsearch | Expected members are present; health is green; every returned index has at least one replica shard; no shards are unassigned, initializing or relocating. |
| Kafka | Every described partition has at least two distinct assigned replicas, complete ISR membership and an in-sync leader. Summaries and partition descriptions must agree. `__consumer_offsets` must exist. `__transaction_state` is validated when present; verified absence is reported without failing the check. |
| ClickHouse | Every discovered member is queried. Replicated table groups have their expected active replicas, live coordination sessions and no read-only members. Replication logs are caught up, and no replication queue tasks other than background `MERGE_PARTS` remain. Missing or duplicate table replicas and current query exceptions fail the check. |
| ZooKeeper | At least three voting members are available. Every member reports the expected voting membership; exactly one is leader and the rest are followers. The leader reports all expected followers synchronized and is checked again after sampling the ensemble. |

Kafka creates `__transaction_state` lazily when transactions are used. If it
is not listed, a separate read-only topic configuration query must confirm
absence; permission failures or ambiguous results produce `unknown`, retaining
any partition or ISR problems already found. The checker never creates topics.
It does not validate the broker defaults that would govern
a future transaction topic. A present transaction topic still needs at least two
replicas and complete ISR.

For replicated ClickHouse deployments, the check requires replicated-table
evidence and does not classify an empty result as healthy. Unreplicated tables
are outside its scope. When both log pointers are zero, the checker queries
`system.zookeeper` to distinguish a genuinely empty replication log from an
unprocessed first log entry. Query failures remain `unknown`; pending queue
tasks, inactive replicas and coordination errors still fail.

ClickHouse can retain `last_queue_update_exception` after recovery. The checker
reports that history without failing otherwise healthy replication. Current
coordination-query errors, expired sessions and replication backlog still fail.

ZooKeeper is checked by default. To inspect it alone:

```bash
sts-backup replication check -n observability --components zookeeper --wait
```

ZooKeeper's `ruok` response does not prove that the ensemble has recovered.
The checker reads `mntr` from every member instead, and requires full voting
membership to recover rather than accepting a surviving majority. Wait mode
applies the same healthy observation period as for the other databases.

## Final HDFS audit

When all selected lightweight checks pass, the checker runs a read-only HDFS
audit before returning success. With `--wait`, this happens after the stability
period, not on every poll. HDFS marked `not_applicable` does not require an audit.

The audit uses `hdfs fsck / -files -blocks -openforwrite -includeSnapshots`.
It examines NameNode file and block metadata, not the contents of HFiles or WALs.
Every audited file must have a replication target of at least two, and completed
blocks must have enough live replicas to meet that file's target. Completed
blocks in open files and snapshot references are included. Counts can include
the same underlying block referenced by multiple snapshots.

For under-construction blocks, Hadoop reports expected pipeline membership.
The checker requires that count to meet the file's target and explicitly reports
that persistence of the latest writes is not verified. It does not stop writers,
roll WALs or require every WAL to close. A healthy audit is not proof that every
active pipeline has durably replicated its latest bytes.

The default audit limit is two minutes, bounded by the remaining overall
`--timeout`. Adjust it with `--hdfs-audit-timeout` for larger namespaces:

```bash
sts-backup replication check -n observability --wait \
  --timeout 15m --hdfs-audit-timeout 5m
```

Output is parsed as a stream, with bounded line size and diagnostic storage.
The parser requires matching file/block counts and a complete final summary;
`fsck`'s `HEALTHY` line alone is insufficient. Incomplete, timed-out or unsupported
reports return `unknown`. Erasure-coded files and symlink records are unsupported.
Parser errors include a line number and a bounded, escaped excerpt of the
rejected record.
After a successful audit, all selected lightweight checks run again and relevant
Kubernetes state must still match. The audit is a sampled scan, not an atomic
filesystem snapshot; its cost depends on file/block count and NameNode load.

## Access and supported layouts

The command uses the normal kubeconfig selection or a Kubernetes service
account when running in a Pod. It requires listing Pods and StatefulSets and
creating `pods/exec` requests in the target namespace.

**`pods/exec` permission allows arbitrary commands in pods.** The checker
itself executes only fixed read-only queries. Use a dedicated identity and
grant this permission only in the installation's namespace. It does not scale
workloads, change PDBs, annotate resources, repair databases or invoke restore
operations. No backup credentials are loaded.

Queries use the database tools already present in the product containers:
`hdfs` and `curl` in the NameNode, `curl` in Elasticsearch,
`kafka-topics.sh` and `kafka-configs.sh` in Kafka, `clickhouse-client` in ClickHouse, and Bash TCP
access to ZooKeeper.
Custom images, container names, external databases, alternative NameNode
topologies and overridden database name/component labels are not supported by
these initial adapters. Failed discovery or unsupported response formats
produce `unknown`, not success.

The initial HTTP adapters use the chart's NameNode and Elasticsearch ports.
The ZooKeeper adapter requires the chart's plaintext loopback client port
and `mntr` in its four-letter-command whitelist. It supports the chart's
single ensemble of voting participants; external ensembles, observer layouts,
weighted quorums and TLS-only client listeners are outside its scope. Missing
membership or synchronization metrics produce `unknown`. No configuration
changes, HTTP AdminServer or database writes are needed.

For authenticated Kafka, supply a client properties file already mounted in
the broker and an appropriate bootstrap address:

```bash
sts-backup replication check -n observability \
  --components kafka \
  --kafka-bootstrap-server suse-observability-kafka:9092 \
  --kafka-client-properties /mounted/client.properties
```

The credentials must be able to describe all topics in the installation and
describe topic configurations for `__transaction_state` when verifying absence.
Elasticsearch uses the pod's `ELASTIC_PASSWORD` when present. For HTTPS,
use `--elasticsearch-scheme https`, `--elasticsearch-ca` with a CA path inside
the pod, and `--elasticsearch-server-name` matching the server certificate.
The server name is resolved to loopback inside that pod; certificate
verification is not disabled.

ClickHouse uses the pod's `CLICKHOUSE_ADMIN_USER`,
`CLICKHOUSE_ADMIN_PASSWORD` and `CLICKHOUSE_TCP_PORT`. Credentials stay inside
the pod. Query stderr is suppressed because database tools can echo credentials;
when a query fails, inspect the component's configuration through your normal
administrative procedure. Ordinary query output is size-limited and excess output
is treated as unverified; the final HDFS report uses the streaming parser described
above.

## Maintenance boundary

This is a sampled replication report, **not a “safe to remove this node”
certificate or a maintenance lock**. It does not prevent another process from
starting maintenance, provide an atomic database snapshot, or prove continued
health between observations.

The current StatefulSet specification is the configuration baseline. The checker
cannot distinguish an intentional replica-count override from an accidental or
temporary scale-down in that specification, recover an original sizing profile,
or detect an entirely deleted shard from surviving workloads alone. Keep the
desired topology intact during node maintenance. A missing pod or a workload
still configured for several replicas cannot become `not_applicable` just
because fewer replicas are Ready.

`not_applicable` verifies Kubernetes availability only. It does not establish
database health or storage-level redundancy for a single-replica component.

This first version does not validate Longhorn volume health or placement,
spare capacity, HBase region assignment and WAL recovery, quorum survival
after a specific node is removed, VictoriaMetrics redundancy, backup freshness,
or the consequences of removing a particular node. The HDFS audit verifies
completed blocks but does not prove persistence of the latest active WAL writes.
It is not a complete implementation
of the product's node-maintenance checklist.

Continue to serialize maintenance, preserve storage redundancy, follow the
documented recovery procedure and check these additional requirements.
Run this checker after the affected node or replacement can schedule workloads.
Only proceed when the report and the remaining maintenance checks pass.

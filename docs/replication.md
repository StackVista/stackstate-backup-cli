# Check database replication

`sts-backup replication check` inspects the chart-managed databases in a
namespace containing one SUSE Observability installation. It works from a
workstation or a Kubernetes Job and does not require the backup ConfigMap,
backup Secret, or enabled backups.

```bash
sts-backup replication check --namespace observability
```

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

Progress includes UTC timestamps and shows the elapsed and remaining healthy
period, resets, and successful completion. Seeing every component healthy
does not mean the stability period has already elapsed. Database queries take
time; the checker needs another completed healthy observation to confirm the
period, rather than exiting on a timer alone.

To finish on the first fully healthy observation, explicitly use
`--wait --stable-for 0s`.

Progress goes to stderr; stdout contains one final report. Table output includes
the report time and the start time of the last completed observation. JSON
retains its `checkedAt` timestamp. The overall deadline includes database
queries. `--request-timeout` bounds each API request or query.

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
It queries database state and invalidates the observation if Kubernetes
resource versions change during those queries.

| Component | Replication evidence |
|---|---|
| HDFS | Configured default block replication is at least two; all expected DataNodes are live; the NameNode is out of safe mode; no missing, corrupt, under-replicated or pending-replication blocks are reported by JMX. |
| Elasticsearch | Expected members are present; health is green; every returned index has at least one replica shard; no shards are unassigned, initializing or relocating. |
| Kafka | Every described partition has at least two distinct assigned replicas, complete ISR membership and an in-sync leader. Summaries and partition descriptions must agree. `__consumer_offsets` must exist. `__transaction_state` is validated when present; verified absence is reported without failing the check. |
| ClickHouse | Every discovered member is queried. Replicated table groups have their expected active replicas, live coordination sessions and no read-only members. Replication logs are caught up, and no replication queue tasks other than background `MERGE_PARTS` remain. Missing or duplicate table replicas and current query exceptions fail the check. |
| ZooKeeper | At least three voting members are available. Every member reports the expected voting membership; exactly one is leader and the rest are followers. The leader reports all expected followers synchronized and is checked again after sampling the ensemble. |

Kafka creates `__transaction_state` lazily when transactions are used. If it
is not listed, a separate read-only topic configuration query must confirm
absence; permission failures or ambiguous results produce `unknown`. The checker
never creates topics. It does not validate the broker defaults that would govern
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
administrative procedure. Query output is size-limited and excess output is
treated as unverified.

## Kubernetes Job

[examples/replication/job.yaml](../examples/replication/job.yaml) contains a
dedicated ServiceAccount, namespace-scoped RBAC and a Job using in-cluster
credentials. Adapt its namespace and image reference before use.
The Job has no retries: a failure requires investigation and an explicit rerun.

No new container image is published by this change. To package the CLI, build
a static Linux binary from this repository and use the example SUSE BCI image:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o examples/replication/sts-backup .
docker build -t registry.example.com/observability/sts-backup:replication-checker \
  examples/replication
```

Publish the image to your registry through your normal image delivery process
and replace the Job's example image reference. Use the architecture required
by your nodes. The build copies the local binary; it does not download an
unverified executable.

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
or the consequences of removing a particular node. HDFS's default replication setting does not prove that
every file has the same replication policy. It is not a complete implementation
of the product's node-maintenance checklist.

Continue to serialize maintenance, preserve storage redundancy, follow the
documented recovery procedure and check these additional requirements.
Run this checker after the affected node or replacement can schedule workloads.
Only proceed when the report and the remaining maintenance checks pass.

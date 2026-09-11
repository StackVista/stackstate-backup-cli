# Check database replication

`sts-backup replication check` inspects the chart-managed HA databases in a
namespace containing one SUSE Observability installation. It works from a
workstation or a Kubernetes Job and does not require the backup ConfigMap,
backup Secret, or enabled backups.

```bash
sts-backup replication check --namespace observability
```

The command returns exit code **0** only when every selected component reports
healthy replication. Any degraded, missing, inaccessible, unsupported or
incompletely described component returns exit code **1**. A missing component
is never silently skipped. Non-HA installations do not meet the checker's
minimum application replication requirement.

Use JSON for automation:

```bash
sts-backup replication check \
  --namespace observability \
  --output json
```

The report identifies the namespace, observation time, selected
components, status (`healthy`, `degraded` or `unknown`) and diagnostic messages.
The status describes only the selected checks.

## Wait for recovery

```bash
sts-backup replication check \
  --namespace observability \
  --wait --timeout 15m --interval 10s --stable-for 30s \
  --output json > replication.json
```

Wait mode requires consecutive healthy observations spanning `--stable-for`.
An unsuccessful observation resets that period. Progress goes to stderr;
stdout contains one final report. The overall deadline includes database
queries. A timeout or cancellation returns nonzero, even if an earlier
observation was healthy. `--request-timeout` bounds each API request or query.

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
| Kafka | Every described partition has at least two distinct assigned replicas, complete ISR membership and an in-sync leader. Summaries and partition descriptions must agree. Both `__consumer_offsets` and `__transaction_state` must exist. |
| ClickHouse | Every discovered member is queried. Replicated table groups have their expected active replicas, live coordination sessions and no read-only members. Replication logs are caught up, and no replication queue tasks other than background `MERGE_PARTS` remain. Missing or duplicate table replicas and current query exceptions fail the check. |
| ZooKeeper | At least three voting members are available. Every member reports the expected voting membership; exactly one is leader and the rest are followers. The leader reports all expected followers synchronized and is checked again after sampling the ensemble. |

The Kafka check does not create missing internal topics: initialize the
corresponding workloads and repeat the check. The ClickHouse check requires
replicated-table evidence and does not classify an empty result as healthy.
Unreplicated ClickHouse tables are outside its scope.

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
`kafka-topics.sh` in Kafka, `clickhouse-client` in ClickHouse, and Bash TCP
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

The credentials must be able to describe all topics in the installation.
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

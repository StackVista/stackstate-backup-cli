# StackState Backup CLI

A command-line tool for managing backups and restores for SUSE Observability platform running on Kubernetes.

## Overview

This CLI tool replaces the legacy Bash-based backup/restore scripts with a single Go binary that can be run from an operator host. It uses Kubernetes port-forwarding to connect to services and automatically discovers configuration from ConfigMaps and Secrets.

**Current Support:**
- Elasticsearch snapshots and restores
- Stackgraph backups and restores
- VictoriaMetrics backups and restores
- Settings backups and restores

**Planned:** ClickHouse backups

## Installation

Download pre-built binaries from the [releases page](https://github.com/stackvista/stackstate-backup-cli/releases).

### Building from Source

```bash
go build -o sts-backup -ldflags '-s -w -X github.com/stackvista/stackstate-backup-cli/cmd/version.Version=0.0.1 -X github.com/stackvista/stackstate-backup-cli/cmd/version.Commit=abce -X github.com/stackvista/stackstate-backup-cli/cmd/version.Date=2025-10-15'
```

## Usage

```bash
sts-backup [command] [subcommand] [flags]
```

### Global Flags

- `--namespace` - Kubernetes namespace (required)
- `--kubeconfig` - Path to kubeconfig file (default: ~/.kube/config)
- `--configmap` - ConfigMap name containing backup configuration (default: suse-observability-backup-config)
- `--secret` - Secret name containing backup credentials (default: suse-observability-backup-config)
- `--output, -o` - Output format: table, json (default: table)
- `--quiet, -q` - Suppress operational messages
- `--debug` - Enable debug output

## Commands

### version

Display version information.

```bash
sts-backup version
```

### elasticsearch

Manage Elasticsearch snapshots and restores.

#### configure

Configure Elasticsearch snapshot repository and SLM policy.

```bash
sts-backup elasticsearch configure --namespace <namespace>
```

#### list-indices

List Elasticsearch indices.

```bash
sts-backup elasticsearch list-indices --namespace <namespace>
```

#### list-snapshots

List available Elasticsearch snapshots.

```bash
sts-backup elasticsearch list-snapshots --namespace <namespace>
```

#### restore-snapshot

Restore Elasticsearch snapshot. Automatically scales down affected deployments before restore and scales them back up afterward.

```bash
sts-backup elasticsearch restore-snapshot --namespace <namespace> --snapshot-name <name> [flags]
```

**Flags:**
- `--snapshot-name, -s` - Name of snapshot to restore (required)
- `--drop-all-indices, -r` - Delete all existing STS indices before restore
- `--yes` - Skip confirmation prompt

### stackgraph

Manage Stackgraph backups and restores.

#### list

List available Stackgraph backups from S3/Minio.

```bash
sts-backup stackgraph list --namespace <namespace>
```

#### restore

Restore Stackgraph from a backup archive. Automatically scales down affected deployments before restore and scales them back up afterward.

```bash
sts-backup stackgraph restore --namespace <namespace> [--archive <name> | --latest] [flags]
```

**Flags:**
- `--archive` - Specific archive name to restore (e.g., sts-backup-20210216-0300.graph)
- `--latest` - Restore from the most recent backup
- `--background` - Run restore job in background without waiting for completion
- `--yes, -y` - Skip confirmation prompt

**Note**: Either `--archive` or `--latest` must be specified (mutually exclusive).

#### check-and-finalize

Check the status of a background Stackgraph restore job and clean up resources.

```bash
sts-backup stackgraph check-and-finalize --namespace <namespace> --job <job-name> [--wait]
```

**Flags:**

- `--job, -j` - Stackgraph restore job name (required)
- `--wait, -w` - Wait for job to complete before cleanup

**Use Case**: This command is useful when a restore job was started with `--background` flag or was interrupted (
Ctrl+C).

### victoriametrics

Manage VictoriaMetrics backups and restores.

#### list

List available VictoriaMetrics backups from S3/Minio.

```bash
sts-backup victoriametrics list --namespace <namespace>
```

**Note**: In HA mode, backups from both instances (victoria-metrics-0 and victoria-metrics-1) are listed. The restore
command accepts either backup to restore both instances.

#### restore

Restore VictoriaMetrics from a backup archive. Automatically scales down affected StatefulSets before restore and scales
them back up afterward.

```bash
sts-backup victoriametrics restore --namespace <namespace> [--archive <name> | --latest] [flags]
```

**Flags:**

- `--archive` - Specific backup name to restore (e.g., sts-victoria-metrics-backup/victoria-metrics-0-20251030143500)
- `--latest` - Restore from the most recent backup
- `--background` - Run restore job in background without waiting for completion
- `--yes, -y` - Skip confirmation prompt

**Note**: Either `--archive` or `--latest` must be specified (mutually exclusive).

#### check-and-finalize

Check the status of a background VictoriaMetrics restore job and clean up resources.

```bash
sts-backup victoriametrics check-and-finalize --namespace <namespace> --job <job-name> [--wait]
```

**Flags:**

- `--job, -j` - VictoriaMetrics restore job name (required)
- `--wait, -w` - Wait for job to complete before cleanup

**Use Case**: This command is useful when a restore job was started with `--background` flag or was interrupted (
Ctrl+C).

### settings

Manage Settings backups and restores.

#### list

List available Settings backups from S3/Minio.

```bash
sts-backup settings list --namespace <namespace>
```

#### restore

Restore Settings from a backup archive. Automatically scales down affected deployments before restore and scales them
back up afterward.

```bash
sts-backup settings restore --namespace <namespace> [--archive <name> | --latest] [flags]
```

**Flags:**

- `--archive` - Specific archive name to restore (e.g., sts-backup-20251117-1404.sty)
- `--latest` - Restore from the most recent backup
- `--background` - Run restore job in background without waiting for completion
- `--yes, -y` - Skip confirmation prompt

**Note**: Either `--archive` or `--latest` must be specified (mutually exclusive).

#### check-and-finalize

Check the status of a background Settings restore job and clean up resources.

```bash
sts-backup settings check-and-finalize --namespace <namespace> --job <job-name> [--wait]
```

**Flags:**

- `--job, -j` - Settings restore job name (required)
- `--wait, -w` - Wait for job to complete before cleanup

**Use Case**: This command is useful when a restore job was started with `--background` flag or was interrupted (
Ctrl+C).

## Configuration

The CLI uses configuration from Kubernetes ConfigMaps and Secrets with the following precedence:

1. CLI flags (highest priority)
2. Environment variables (prefix: `BACKUP_TOOL_`)
3. Kubernetes Secret (overrides sensitive fields)
4. Kubernetes ConfigMap (base configuration)
5. Defaults (lowest priority)

### Example Configuration

Create a ConfigMap with the following structure:

```yaml
elasticsearch:
  snapshotRepository:
    name: sts-backup
    bucket: sts-elasticsearch-backup
    endpoint: suse-observability-minio:9000
    basepath: ""

  slm:
    name: auto-sts-backup
    schedule: "0 0 3 * * ?"
    snapshotTemplateName: "<sts-backup-{now{yyyyMMdd-HHmm}}>"
    repository: sts-backup
    indices: "sts*"
    retentionExpireAfter: 30d
    retentionMinCount: 5
    retentionMaxCount: 30

  service:
    name: suse-observability-elasticsearch-master-headless
    port: 9200
    localPortForwardPort: 9200

  restore:
    repository: sts-backup
    scaleDownLabelSelector: "observability.suse.com/scalable-during-es-restore=true"
    indexPrefix: sts
    datastreamIndexPrefix: .ds-sts_k8s_logs
    datastreamName: sts_k8s_logs
    indicesPattern: sts*,.ds-sts_k8s_logs*
```

Apply to Kubernetes:

```bash
kubectl create configmap suse-observability-backup-config \
  --from-file=config=config.yaml \
  -n <namespace>
```

For sensitive credentials, create a Secret with S3/Minio access keys:

```bash
kubectl create secret generic suse-observability-backup-config \
  --from-literal=elasticsearch.snapshotRepository.accessKey=<access-key> \
  --from-literal=elasticsearch.snapshotRepository.secretKey=<secret-key> \
  -n <namespace>
```

See [internal/foundation/config/testdata/validConfigMapConfig.yaml](internal/foundation/config/testdata/validConfigMapConfig.yaml) for a complete example.

## Project Structure

```
.
├── cmd/                          # CLI commands (Layer 4)
│   ├── root.go                   # Root command and global flags
│   ├── version/                  # Version command
│   ├── elasticsearch/            # Elasticsearch subcommands
│   │   ├── configure.go          # Configure snapshot repository
│   │   ├── list-indices.go       # List indices
│   │   ├── list-snapshots.go     # List snapshots
│   │   └── restore-snapshot.go   # Restore snapshot
│   ├── stackgraph/               # Stackgraph subcommands
│   │   ├── list.go               # List backups
│   │   ├── restore.go            # Restore backup
│   │   └── check-and-finalize.go # Check and finalize restore job
│   ├── victoriametrics/          # VictoriaMetrics subcommands
│   │   ├── list.go               # List backups
│   │   ├── restore.go            # Restore backup
│   │   └── check-and-finalize.go # Check and finalize restore job
│   └── settings/                 # Settings subcommands
│       ├── list.go               # List backups
│       ├── restore.go            # Restore backup
│       └── check-and-finalize.go # Check and finalize restore job
├── internal/                     # Internal packages (Layers 0-3)
│   ├── foundation/               # Layer 0: Core utilities
│   │   ├── config/               # Configuration management
│   │   ├── logger/               # Structured logging
│   │   └── output/               # Output formatting
│   ├── clients/                  # Layer 1: Service clients
│   │   ├── k8s/                  # Kubernetes client
│   │   ├── elasticsearch/        # Elasticsearch client
│   │   └── s3/                   # S3/Minio client
│   ├── orchestration/            # Layer 2: Workflows
│   │   ├── portforward/          # Port-forwarding lifecycle
│   │   ├── scale/                # Deployment/StatefulSet scaling
│   │   └── restore/              # Restore job orchestration
│   │       ├── confirmation.go   # User confirmation prompts
│   │       ├── finalize.go       # Job status check and cleanup
│   │       ├── job.go            # Job lifecycle management
│   │       └── resources.go      # Restore resource management
│   ├── app/                      # Layer 3: Dependency container
│   │   └── app.go                # Application context and DI
│   └── scripts/                  # Embedded bash scripts
├── main.go                       # Entry point
└── ARCHITECTURE.md               # Detailed architecture documentation
```

### Key Architectural Features

- **Layered Architecture**: Clear separation between commands (Layer 4), dependency injection (Layer 3), workflows (Layer 2), clients (Layer 1), and utilities (Layer 0)
- **Dependency Injection**: Centralized dependency creation via `internal/app/` eliminates boilerplate from commands
- **Testability**: All layers use interfaces for external dependencies, enabling comprehensive unit testing
- **Clean Commands**: Commands are thin (50-100 lines) and focused on business logic

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed information about the layered architecture and design patterns.

## CI/CD

This project uses GitHub Actions and GoReleaser for automated releases:

1. Push a new tag (e.g., `v1.0.0`)
2. GitHub Actions automatically builds binaries for multiple platforms
3. GoReleaser creates a GitHub release and uploads artifacts to S3

## Development

### Running Tests

```bash
go test ./...
```

### Linting

```bash
golangci-lint run --config=.golangci.yml ./...
```

## License

Copyright (c) 2025 SUSE

# SUSE Observability Backup CLI

A command-line tool for managing backups and restores for SUSE Observability platform running on Kubernetes.

## Overview

This CLI tool replaces the legacy Bash-based backup/restore scripts with a single Go binary that can be run from an operator host. It uses Kubernetes port-forwarding to connect to services and automatically discovers configuration from ConfigMaps and Secrets.

**Current Support:**
- Elasticsearch snapshots and restores
- ClickHouse backups and restores
- Stackgraph backups and restores
- VictoriaMetrics backups and restores
- Settings backups and restores

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

#### list

List available Elasticsearch snapshots.

```bash
sts-backup elasticsearch list --namespace <namespace>
```

#### restore

Restore Elasticsearch snapshot. Automatically scales down affected deployments before restore and scales them back up afterward.

```bash
sts-backup elasticsearch restore --namespace <namespace> [--snapshot <name> | --latest] [flags]
```

**Flags:**
- `--snapshot, -s` - Name of snapshot to restore (mutually exclusive with --latest)
- `--latest` - Restore from the most recent snapshot (mutually exclusive with --snapshot)
- `--background` - Run restore in background without waiting for completion
- `--yes, -y` - Skip confirmation prompt

**Note**: Either `--snapshot` or `--latest` must be specified (mutually exclusive).

#### check-and-finalize

Check the status of a restore operation and finalize if complete.

```bash
sts-backup elasticsearch check-and-finalize --namespace <namespace> --operation-id <snapshot> [--wait]
```

**Flags:**
- `--operation-id` - Operation ID of the restore operation (snapshot name) (required)
- `--wait` - Wait for restore to complete if still running

**Use Case**: This command is useful when a restore was started with `--background` flag or was interrupted (Ctrl+C).

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

### clickhouse

Manage ClickHouse backups and restores.

#### list

List available ClickHouse backups from the backup API.

```bash
sts-backup clickhouse list --namespace <namespace>
```

#### restore

Restore ClickHouse from a backup. Automatically scales down affected StatefulSets before restore and scales them back up afterward.

```bash
sts-backup clickhouse restore --namespace <namespace> --backup-name <name> [flags]
```

**Flags:**
- `--backup-name` - Name of the backup to restore (required)
- `--wait` - Wait for restore to complete (default: true)

#### check-and-finalize

Check the status of a ClickHouse restore operation and finalize if complete.

```bash
sts-backup clickhouse check-and-finalize --namespace <namespace> --operation-id <id> [--wait]
```

**Flags:**
- `--operation-id` - Operation ID of the restore operation (required)
- `--wait` - Wait for restore to complete if still running

**Use Case**: This command is useful when checking the status of a restore operation or finalizing after completion.

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
│   │   ├── list.go               # List snapshots
│   │   ├── restore.go            # Restore snapshot
│   │   └── check-and-finalize.go # Check and finalize restore
│   ├── clickhouse/               # ClickHouse subcommands
│   │   ├── list.go               # List backups
│   │   ├── restore.go            # Restore backup
│   │   └── check-and-finalize.go # Check and finalize restore
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
│   │   ├── clickhouse/           # ClickHouse client
│   │   └── s3/                   # S3/Minio client
│   ├── orchestration/            # Layer 2: Workflows
│   │   ├── portforward/          # Port-forwarding lifecycle
│   │   ├── scale/                # Deployment/StatefulSet scaling
│   │   ├── restore/              # Restore job orchestration
│   │   │   ├── confirmation.go   # User confirmation prompts
│   │   │   ├── finalize.go       # Job status check and cleanup
│   │   │   ├── job.go            # Job lifecycle management
│   │   │   └── resources.go      # Restore resource management
│   │   └── restorelock/          # Parallel restore prevention
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
- **Restore Lock Protection**: Prevents parallel restore operations that could corrupt data

### Restore Lock Protection

The CLI prevents parallel restore operations that could corrupt data by using Kubernetes annotations on Deployments and StatefulSets. When a restore starts:

1. The CLI checks for existing restore locks before proceeding
2. If another restore is in progress for the same datastore, the operation is blocked
3. Mutually exclusive datastores are also protected (e.g., Stackgraph and Settings cannot restore simultaneously because they share HBase data)

If a restore operation is interrupted or fails, the lock annotations may remain. To manually remove a stuck lock:

```bash
kubectl annotate deployment,statefulset -l <label-selector> \
  stackstate.com/restore-in-progress- \
  stackstate.com/restore-started-at- \
  -n <namespace>
```

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

### Using OpenCode for Development

We're exploring AI-assisted development with [OpenCode](https://github.com/opencode-ai/opencode). If you'd like to try it for your tasks, here's how to get started.

#### Quick Start

1. Install OpenCode following their [installation guide](https://github.com/opencode-ai/opencode#installation)
2. Run `opencode` in the repository root
3. Start asking questions or requesting changes

#### What Works Well

OpenCode can help with:
- **Understanding the codebase**: Ask about architecture, patterns, or how specific features work
- **Implementing new features**: Describe what you need, and it will follow project conventions
- **Writing tests**: Request table-driven tests following our testify patterns
- **Code reviews**: Ask it to review changes against project guidelines

#### Example: Adding a New Command

Here's an example workflow for implementing a new feature:

```
You: I need to add a "prune" command to elasticsearch that deletes snapshots older than N days.
     It should follow the existing patterns in this codebase.

OpenCode will:
1. Explore existing commands to understand patterns
2. Create cmd/elasticsearch/prune.go following the command runner pattern
3. Add any needed client methods to internal/clients/elasticsearch/
4. Generate table-driven tests
5. Run linting to verify compliance
```

#### Tips

- **Reference the docs**: Mention `ARCHITECTURE.md` or `AGENTS.md` if you want it to follow specific guidelines
- **Ask for reviews**: After implementing, ask OpenCode to review the code against project standards
- **Iterate**: If something doesn't look right, ask for adjustments

#### Code Review Agent

This repository includes a code review agent (`.opencode/agents/code-reviewer.md`) that understands our architecture and coding standards. Use it to validate changes before submitting PRs.

#### Contributing to OpenCode Configuration

The OpenCode configuration files in `.opencode/` are not set in stone. If you find ways to improve the agents or add new ones, feel free to update them. Better prompts, additional review checks, or new specialized agents are all welcome contributions.

> **Note**: We're still experimenting with AI-assisted development. Share your experiences with the team - what works, what doesn't, and any tips you discover.

## License

Copyright (c) 2025 SUSE

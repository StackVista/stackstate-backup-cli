# StackState Backup CLI

A command-line tool for managing backups and restores for SUSE Observability platform running on Kubernetes.

## Overview

This CLI tool replaces the legacy Bash-based backup/restore scripts with a single Go binary that can be run from an operator host. It uses Kubernetes port-forwarding to connect to services and automatically discovers configuration from ConfigMaps and Secrets.

**Current Support:** Elasticsearch snapshots and restores
**Planned:** VictoriaMetrics, ClickHouse, StackGraph, Configuration backups

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

Restore Elasticsearch snapshot.

```bash
sts-backup elasticsearch restore-snapshot --namespace <namespace> --snapshot-name <name> [flags]
```

**Flags:**
- `--snapshot-name` - Name of snapshot to restore (required)
- `--drop-all-indices` - Delete all existing indices before restore
- `--yes` - Skip confirmation prompt

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

See [internal/config/testdata/validConfigMapConfig.yaml](internal/config/testdata/validConfigMapConfig.yaml) for a complete example.

## Project Structure

```
.
├── cmd/                          # CLI commands
│   ├── root.go                   # Root command and flag definitions
│   ├── version/                  # Version command
│   └── elasticsearch/            # Elasticsearch subcommands
│       ├── configure.go          # Configure snapshot repository
│       ├── list-indices.go       # List indices
│       ├── list-snapshots.go     # List snapshots
│       └── restore-snapshot.go   # Restore snapshot
├── internal/                     # Internal packages
│   ├── config/                   # Configuration loading and validation
│   ├── elasticsearch/            # Elasticsearch client
│   ├── k8s/                      # Kubernetes client utilities
│   ├── logger/                   # Structured logging
│   └── output/                   # Output formatting (table, JSON)
└── main.go                       # Entry point
```

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

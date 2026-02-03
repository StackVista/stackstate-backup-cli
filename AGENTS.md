# AGENTS.md - AI Coding Agent Guidelines

Go CLI for managing backups/restores for SUSE Observability on Kubernetes.

> **Important**: Read [ARCHITECTURE.md](ARCHITECTURE.md) for detailed architecture, design patterns, and extension guides.

## Build, Lint, and Test Commands

```bash
# Build
go build -o sts-backup .

# Run all tests
go test ./...

# Run a specific test function
go test -v -run TestClient_ScaleDownDeployments ./internal/clients/k8s/...

# Run tests in a specific package
go test -v ./internal/clients/elasticsearch/...

# Lint (required before committing)
golangci-lint run --config=.golangci.yml ./...

# Verify architecture dependencies
go list -f '{{.ImportPath}}: {{join .Imports "\n"}}' ./internal/foundation/... | grep 'stackvista.*internal' || true
go list -f '{{.ImportPath}}: {{join .Imports "\n"}}' ./internal/clients/... | grep 'stackvista.*internal' | grep -v foundation || true
go list -f '{{.ImportPath}}: {{join .Imports "\n"}}' ./internal/orchestration/... | grep 'stackvista.*orchestration' || true
```

## Code Style Guidelines

### Imports

Organize in three groups: standard library, external deps, internal packages.

```go
import (
    "context"
    "fmt"

    "github.com/spf13/cobra"
    appsv1 "k8s.io/api/apps/v1"

    "github.com/stackvista/stackstate-backup-cli/internal/app"
    es "github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
)
```

### Formatting

- Use `gofmt` and `goimports`
- Max line length: 250 chars, max function: 100 lines/60 statements

### Types

```go
// Client represents an Elasticsearch client (comment starts with type name)
type Client struct {
    es *elasticsearch.Client
}

type Config struct {
    Elasticsearch ElasticsearchConfig `yaml:"elasticsearch" validate:"required"`
}
```

### Naming

- **Files**: lowercase with underscores (`client_test.go`)
- **Packages**: short, lowercase, single-word (`k8s`, `config`)
- **Constants**: PascalCase exported, camelCase unexported

### Error Handling

Always wrap errors: `return fmt.Errorf("failed to get service %s: %w", name, err)`

### Interfaces

Define in consumer package with compile-time check: `var _ Interface = (*Client)(nil)`

### Testing

Table-driven tests with `testify/assert` and `require`, use `fake.NewSimpleClientset()` for K8s:

```go
func TestClient_Scale(t *testing.T) {
    tests := []struct {
        name        string
        expectError bool
    }{
        {name: "success", expectError: false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            fakeClient := fake.NewSimpleClientset()
            require.NoError(t, err)
            assert.Equal(t, expected, actual)
        })
    }
}
```

## Architecture (see ARCHITECTURE.md for details)

### Layered Structure

```
cmd/                    # Layer 4: CLI commands (thin)
internal/
  app/                  # Layer 3: Dependency injection
  orchestration/        # Layer 2: Multi-service workflows
  clients/              # Layer 1: Service clients (k8s, elasticsearch, s3)
  foundation/           # Layer 0: Core utilities (config, logger, output)
```

### Dependency Rules

- **cmd/** imports `internal/app/*` (preferred)
- **orchestration/** imports `clients/*` and `foundation/*`, NOT other orchestration
- **clients/** imports only `foundation/*`
- **foundation/** imports only stdlib

## Common Pitfalls to Avoid

**Don't import clients from other clients** - move logic to `internal/orchestration/`

**Don't put business logic in commands** - extract to orchestration or client packages

**Don't import foundation packages from each other** - keep them independent

**Don't hard-code configuration** - use `config.Elasticsearch.Service.Name`

**Don't create clients directly in commands** - use `app.Context`:

```go
// GOOD
func runRestore(appCtx *app.Context) error {
    appCtx.K8sClient  // Kubernetes client
    appCtx.ESClient   // Elasticsearch client
    appCtx.Config     // Configuration
    appCtx.Logger     // Structured logger
}
```

## Key Patterns

**Port-Forward Lifecycle** - Always defer cleanup:

```go
pf, err := portforward.SetupPortForward(...)
defer close(pf.StopChan)
```

**Scale with Locks** - For restore operations use `scale.ScaleDownWithLock()` from orchestration.

**Restore Lock** - Stackgraph and Settings are mutually exclusive (both modify HBase data).

## Linter Exceptions

Use sparingly: `//nolint:funlen // Table-driven test` or `//nolint:unparam`

## Logging

```go
log.Infof("Starting...")     // General info
log.Debugf("Detail: %v", d)  // Debug details
log.Warningf("Issue: %v", w) // Warnings
log.Errorf("Failed: %v", e)  // Errors
log.Successf("Done")         // Success messages
```

## Configuration

- Loaded from Kubernetes ConfigMap and Secret
- Precedence: CLI flags > Environment > Secret > ConfigMap > Defaults

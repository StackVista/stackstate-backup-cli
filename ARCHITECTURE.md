# Repository Architecture

This document describes the overall architecture and package organization of the StackState Backup CLI.

## Design Philosophy

The codebase follows several key principles:

1. **Layered Architecture**: Dependencies flow from higher layers (commands) to lower layers (foundation utilities)
2. **Self-Documenting Structure**: Directory hierarchy makes dependency rules and module purposes explicit
3. **Clean Separation**: Domain logic, infrastructure, and presentation are clearly separated
4. **Testability**: Lower layers can be tested independently without external dependencies
5. **Reusability**: Shared functionality is extracted into appropriate packages

## Repository Structure

```
stackstate-backup-cli/
├── cmd/                      # Command-line interface (Layer 3)
│   ├── root.go              # Root command and global flags
│   ├── version/             # Version information command
│   ├── elasticsearch/       # Elasticsearch backup/restore commands
│   └── stackgraph/          # Stackgraph backup/restore commands
│
├── internal/                # Internal packages (Layers 0-2)
│   ├── foundation/          # Layer 0: Core utilities
│   │   ├── config/          # Configuration management
│   │   ├── logger/          # Structured logging
│   │   └── output/          # Output formatting
│   │
│   ├── clients/             # Layer 1: Service clients
│   │   ├── k8s/             # Kubernetes client
│   │   ├── elasticsearch/   # Elasticsearch client
│   │   └── s3/              # S3/Minio client
│   │
│   ├── orchestration/       # Layer 2: Workflows
│   │   ├── portforward/     # Port-forwarding orchestration
│   │   └── scale/           # Deployment scaling workflows
│   │
│   └── scripts/             # Embedded bash scripts
│
├── main.go                  # Application entry point
├── ARCHITECTURE.md          # This file
└── README.md                # User documentation
```

## Architectural Layers

### Layer 3: Commands (`cmd/`)

**Purpose**: User-facing CLI commands and application entry points

**Characteristics**:
- Implements the Cobra command structure
- Handles user input validation and flag parsing
- Orchestrates calls to lower layers
- Formats output for end users

**Key Packages**:
- `cmd/elasticsearch/`: Elasticsearch snapshot/restore commands (configure, list-snapshots, list-indices, restore-snapshot)
- `cmd/stackgraph/`: Stackgraph backup/restore commands (list, restore)
- `cmd/version/`: Version information

**Dependency Rules**:
- ✅ Can import: All `internal/` packages
- ❌ Should not: Contain business logic or direct service calls

### Layer 2: Orchestration (`internal/orchestration/`)

**Purpose**: High-level workflows that coordinate multiple services

**Characteristics**:
- Composes multiple clients to implement complex workflows
- Handles sequencing and error recovery
- Provides logging and user feedback
- Stateless operations

**Key Packages**:
- `portforward/`: Manages Kubernetes port-forwarding lifecycle
- `scale/`: Deployment scaling workflows with detailed logging

**Dependency Rules**:
- ✅ Can import: `internal/foundation/*`, `internal/clients/*`
- ❌ Cannot import: Other `internal/orchestration/*` (to prevent circular dependencies)

### Layer 1: Clients (`internal/clients/`)

**Purpose**: Wrappers for external service APIs

**Characteristics**:
- Thin abstraction over external APIs
- Handles connection and authentication
- Translates between external formats and internal types
- No business logic or orchestration

**Key Packages**:
- `k8s/`: Kubernetes API operations (Jobs, Pods, Deployments, ConfigMaps, Secrets, Logs)
- `elasticsearch/`: Elasticsearch HTTP API (snapshots, indices, datastreams)
- `s3/`: S3/Minio operations (client creation, object filtering)

**Dependency Rules**:
- ✅ Can import: `internal/foundation/*`, standard library, external SDKs
- ❌ Cannot import: `internal/orchestration/*`, other `internal/clients/*`

### Layer 0: Foundation (`internal/foundation/`)

**Purpose**: Core utilities with no internal dependencies

**Characteristics**:
- Pure utility functions
- No external service dependencies
- Broadly reusable across the application
- Well-tested and stable

**Key Packages**:
- `config/`: Configuration loading from ConfigMaps, Secrets, environment, and flags
- `logger/`: Structured logging with levels (Debug, Info, Warning, Error, Success)
- `output/`: Output formatting (tables, JSON, YAML, messages)

**Dependency Rules**:
- ✅ Can import: Standard library, external utility libraries
- ❌ Cannot import: Any `internal/` packages

## Data Flow

### Typical Command Execution Flow

```
1. User invokes CLI command
   └─> cmd/elasticsearch/restore-snapshot.go
       │
2. Parse flags and validate input
       │
3. Load configuration
   └─> internal/foundation/config/
       │
4. Create clients
   └─> internal/clients/k8s/
   └─> internal/clients/elasticsearch/
       │
5. Execute orchestration workflow
   └─> internal/orchestration/scale/
       └─> Scale down deployments
   └─> internal/orchestration/portforward/
       └─> Setup port-forward to Elasticsearch
   └─> internal/clients/elasticsearch/
       └─> Perform snapshot restore
   └─> internal/orchestration/scale/
       └─> Scale up deployments
       │
6. Format and display results
   └─> internal/foundation/output/
```

## Key Design Patterns

### 1. Configuration Precedence

Configuration is loaded with the following precedence (highest to lowest):

1. **CLI Flags**: Explicit user input
2. **Environment Variables**: Runtime configuration
3. **Kubernetes Secret**: Sensitive credentials (overrides ConfigMap)
4. **Kubernetes ConfigMap**: Base configuration
5. **Defaults**: Fallback values

Implementation: `internal/foundation/config/config.go`

### 2. Client Factory Pattern

Clients are created with a consistent factory pattern:

```go
// Example from internal/clients/elasticsearch/client.go
func NewClient(endpoint string) (*Client, error) {
    // Initialization logic
    return &Client{...}, nil
}
```

### 3. Port-Forward Lifecycle

Services running in Kubernetes are accessed via automatic port-forwarding:

```go
// Example from internal/orchestration/portforward/portforward.go
pf, err := SetupPortForward(k8sClient, namespace, service, localPort, remotePort, log)
defer close(pf.StopChan)  // Automatic cleanup
```

### 4. Scale Down/Up Pattern

Deployments are scaled down before restore operations and scaled up afterward:

```go
// Example usage
scaledDeployments, _ := scale.ScaleDown(k8sClient, namespace, selector, log)
defer scale.ScaleUp(k8sClient, namespace, scaledDeployments, log)
```

### 5. Structured Logging

All operations use structured logging with consistent levels:

```go
log.Infof("Starting operation...")
log.Debugf("Detail: %v", detail)
log.Warningf("Non-fatal issue: %v", warning)
log.Errorf("Operation failed: %v", err)
log.Successf("Operation completed successfully")
```

## Testing Strategy

### Unit Tests
- **Location**: Same directory as source (e.g., `config_test.go`)
- **Focus**: Business logic, parsing, validation
- **Mocking**: Use interfaces for external dependencies

### Integration Tests
- **Location**: `cmd/*/` directories
- **Focus**: Command execution with mocked Kubernetes
- **Tools**: `fake.NewSimpleClientset()` from `k8s.io/client-go`

### End-to-End Tests
- **Status**: Not yet implemented
- **Future**: Use `kind` or `k3s` for local Kubernetes cluster testing

## Extending the Codebase

### Adding a New Command

1. Create command file in `cmd/<service>/`
2. Implement Cobra command structure
3. Use existing clients or create new ones in `internal/clients/`
4. Implement workflow in `internal/orchestration/` if needed
5. Add tests following existing patterns

### Adding a New Client

1. Create package in `internal/clients/<service>/`
2. Implement client factory: `NewClient(...) (*Client, error)`
3. Only import `internal/foundation/*` packages
4. Add methods for each API operation
5. Write unit tests with mocked HTTP/API calls

### Adding a New Orchestration Workflow

1. Create package in `internal/orchestration/<workflow>/`
2. Import required clients from `internal/clients/*`
3. Import utilities from `internal/foundation/*`
4. Keep workflows stateless
5. Add comprehensive logging

## Common Pitfalls to Avoid

### ❌ Don't: Import Clients from Other Clients

```go
// BAD: internal/clients/elasticsearch/backup.go
import "github.com/.../internal/clients/k8s"  // Violates layer rules
```

**Fix**: Move the orchestration logic to `internal/orchestration/`

### ❌ Don't: Put Business Logic in Commands

```go
// BAD: cmd/elasticsearch/restore.go
func runRestore() {
    // 200 lines of business logic here
}
```

**Fix**: Extract logic to orchestration or client packages

### ❌ Don't: Import Foundation Packages from Each Other

```go
// BAD: internal/foundation/config/loader.go
import "github.com/.../internal/foundation/output"
```

**Fix**: Foundation packages should be independent

### ❌ Don't: Hard-code Configuration

```go
// BAD
endpoint := "http://localhost:9200"
```

**Fix**: Use configuration management: `config.Elasticsearch.Service.Name`

## Automated Enforcement

Verify architectural rules with these commands:

```bash
# Verify foundation/ has no internal/ imports
go list -f '{{.ImportPath}}: {{join .Imports "\n"}}' ./internal/foundation/... | \
  grep 'stackvista.*internal'

# Verify clients/ only imports foundation/
go list -f '{{.ImportPath}}: {{join .Imports "\n"}}' ./internal/clients/... | \
  grep 'stackvista.*internal' | grep -v foundation

# Verify orchestration/ doesn't import other orchestration/
go list -f '{{.ImportPath}}: {{join .Imports "\n"}}' ./internal/orchestration/... | \
  grep 'stackvista.*orchestration'
```

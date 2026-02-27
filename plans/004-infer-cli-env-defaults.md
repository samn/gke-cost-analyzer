# Plan 004: Infer CLI Environment Defaults

## Problem

Users must always specify `--project`, `--cluster-name`, and `--region` explicitly.
These values can be inferred from the environment in both:
- **GKE** (running inside a cluster): via the GCE metadata server
- **Development** (running locally): via the current kubeconfig context

## Design

### New Package: `internal/envdefaults`

A small, testable package that detects defaults from the environment.

```go
type Defaults struct {
    ProjectID   string
    ClusterName string
    Region      string
}
```

### Detection Strategies (tried in order)

1. **GCE Metadata Server** (works inside GKE pods):
   - `GET /computeMetadata/v1/project/project-id` → project ID
   - `GET /computeMetadata/v1/instance/attributes/cluster-name` → cluster name
   - `GET /computeMetadata/v1/instance/zone` → returns `projects/NUM/zones/ZONE`
     - Zone (e.g. `us-central1-a`) → Region (e.g. `us-central1`) by stripping
       the last `-X` segment

2. **Kubeconfig Context Parsing** (works in development):
   - GKE contexts follow the naming pattern: `gke_PROJECT_LOCATION_CLUSTER`
   - Parse the current context name to extract all three values

### Priority

- Explicit CLI flags always win (checked via cobra's `Changed()`)
- Metadata server is tried first (with a short 1-second timeout)
- Kubeconfig context is the fallback
- If neither source provides a value, the existing "required" validation still
  catches it

### Integration

A `PersistentPreRunE` on the root command:
1. Call `envdefaults.Detect()`
2. For each flag (`--region`, `--project`, `--cluster-name`): if not explicitly
   set by the user, apply the detected default

### Testability

The `Detector` struct accepts:
- A custom `*http.Client` (to mock the metadata server)
- A `KubeConfigProvider` interface (to mock kubeconfig reading)

## Files Changed

| File | Change |
|------|--------|
| `internal/envdefaults/envdefaults.go` | New: detection logic |
| `internal/envdefaults/envdefaults_test.go` | New: comprehensive tests |
| `cmd/root.go` | Add `PersistentPreRunE` to apply defaults |
| `cmd/root_test.go` | Test CLI integration with defaults |
| `cmd/record.go` | Remove now-redundant `--project` from help as "required" |
| `cmd/setup.go` | Same |
| `CHANGELOG.md` | Document the feature |

## TDD Approach

**RED**: Write tests for context parsing, metadata fetching, zone→region
conversion, fallback behavior, and CLI override precedence.

**GREEN**: Implement the minimum code to pass each test.

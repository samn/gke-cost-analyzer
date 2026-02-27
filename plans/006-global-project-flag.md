# Make `--project` a global persistent flag

## Context

The `--project` flag was a local flag on both `record` and `setup` commands,
binding to separate variables (`bqProject` and `setupProject`). The `watch`
command had no way to specify a project — it relied solely on auto-detection
from GCE metadata or kubeconfig context. This meant `newPromClient()` in
`cmd/common.go` could not construct the GMP URL when auto-detection failed.

## Changes

1. **Unified `bqProject`/`setupProject` into a single `project` variable** in
   `cmd/root.go`, registered as a persistent flag available to all commands.

2. **Removed local `--project` flags** from `cmd/record.go` and `cmd/setup.go`.

3. **Updated `newPromClient`** in `cmd/common.go` to reference the global
   `project` variable.

4. **Added project display in TUI header**: The `Model` in `internal/tui/model.go`
   now accepts a `promProject` parameter and displays it in the Prometheus status
   line (e.g., `(prometheus my-project: no utilization data)`).

5. **Updated all tests** to use the unified `project` variable.

6. **Updated CHANGELOG.md and SPEC.md** to document the change.

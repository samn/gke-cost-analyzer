# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed
- Upgraded the Go toolchain to 1.26.4 (`mise.toml`, `mise.lock`, and the `go.mod` `go` directive).
- Updated Go module dependencies to their latest releases, including `k8s.io/api`, `k8s.io/apimachinery`, and `k8s.io/client-go` v0.35.3 → v0.36.2, `charm.land/bubbletea/v2` v2.0.2 → v2.0.8, `charm.land/lipgloss/v2` v2.0.2 → v2.0.5, `github.com/getsentry/sentry-go` v0.44.1 → v0.47.0, `github.com/parquet-go/parquet-go` v0.29.0 → v0.30.1, and `golang.org/x/sync` v0.19.0 → v0.21.0.
- **Namespace is now part of the aggregation group identity**: pods in different namespaces with identical labels are recorded as separate groups, and the `namespace` column is deterministic (previously it came from whichever pod was listed first). Both history queries now group by namespace so table rows and sparklines agree.
- **Standard-mode node cost is based on `Capacity` instead of `Allocatable`**, matching what GCE actually bills. Standard-mode dollar figures increase accordingly (previously understated by up to ~20% on memory for small nodes).
- Native sidecar containers (init containers with `restartPolicy: Always`) now count toward pod resource requests — Autopilot bills them and they consume node resources. Classic init containers remain excluded.
- `record` snapshots now cover the actual elapsed time since the last successful snapshot instead of the nominal interval, so missed or slow ticks no longer permanently undercount recorded cost.
- `record` refreshes cached prices every 24h instead of using launch-time prices for the daemon's lifetime.
- The history `AVG $/HR` is computed from the covered time (`SUM(interval_seconds)` over per-snapshot windows) instead of the snapshot timestamp span, removing a systematic overestimate and the $0/hr result for single-snapshot workloads. The aggregated query collapses spot/on-demand sibling rows per snapshot first, so mixed workloads no longer double-count covered time (halving $/hr) or understate AVG PODS/CPU/MEM.
- The `record` per-snapshot deadline is floored at 2 minutes (a short `--interval` could otherwise cancel every legitimately long snapshot forever) and the daily price refresh runs under its own 10-minute bound.
- `setup` command description and README reflect schema migration; `history --help` documents the 5-year duration cap.
- `history` shows the CLUSTER column (with a warning) when cluster auto-detection fails and no `--cluster-name` is given, instead of silently blending clusters.
- Upgraded all GitHub Actions to latest major versions and pinned to commit SHAs: `actions/checkout` v4 → v6, `actions/cache` v4 → v5, `jdx/mise-action` v2 → v4, `softprops/action-gh-release` v2 → v3, `docker/login-action` v3 → v4, `docker/setup-buildx-action` v3 → v4, `docker/build-push-action` v6 → v7

### Added
- NAMESPACE column in `watch` and `history`, shown automatically when rows span more than one namespace (namespace is part of the group identity; without the column, identical-label workloads in different namespaces looked like duplicate rows). Sortable like the other columns.
- Event-log scrollback in `watch`: `[` scrolls into history, `]` back toward the newest events (previously documented but not implemented).
- Sort keys `-` and `=` reach the 11th+ sortable columns (WASTE was unreachable with all optional columns enabled, and the help footer advertised a nonexistent "11=" key).
- `setup` migrates existing tables: missing NULLABLE columns (e.g. `cost_mode`, utilization fields) are added via schema PATCH instead of leaving inserts failing forever.

### Fixed
- **BigQuery rows could be silently dropped**: the streaming-insert dedup ID was a hyphen-joined concatenation of label values, so distinct groups (e.g. team `a`/workload `b-c` vs team `a-b`/workload `c`) could collide and BigQuery would discard one row. IDs are now collision-free hashes, also fixing a potential 128-byte insertId limit violation with long labels.
- **`--namespace` no longer corrupts standard-mode cost attribution**: per-node cost shares are computed from all pods on each node, with the namespace filter applied after cost calculation (previously a single filtered pod could absorb an entire node's cost).
- Compute Engine SKU matching no longer conflates variant SKUs ("N2 Custom", "Sole Tenancy") with the plain family price, and covers previously missing families (M1, M2, M4, C4D, H4D) plus older description forms ("Compute optimized" → c2, "Memory-optimized Instance" → m1). Duplicate price-table keys now resolve deterministically (first wins) and the newest `PricingInfo` record is used instead of a slice-order-dependent one.
- History queries follow BigQuery result pagination; results beyond the first 10,000 rows (easily exceeded by sparkline data) were silently truncated.
- History filter values travel as BigQuery named query parameters; the old single-quote-only escaping broke (and permitted injection into) queries for values containing backslashes. Dataset/table/project identifiers are validated.
- Rows recorded before the `cost_mode` column existed are treated as `autopilot` in history queries (per SPEC) instead of appearing as a separate blank-mode group.
- All commands trap SIGTERM (what Kubernetes sends) in addition to SIGINT, so the `record` daemon shuts down gracefully in-cluster.
- All GCP/Prometheus HTTP clients have a 30s timeout and each `record` snapshot is bounded by the interval; a hung backend can no longer wedge the daemon silently and permanently.
- Parquet appends and price-cache writes go through temp-file + rename; a crash mid-write can no longer destroy previously recorded snapshots or leave a torn cache file.
- Autopilot and Compute Engine price caches use separate default files; sharing one file silently corrupted whichever loaded second.
- Trend detection: `--trend-threshold 0` is honored as "disabled" inside the tracker itself, and a large workload crashing to near-zero cost is now flagged (the noise floor previously masked big drops).
- Watch TUI: the cursor is clamped when a refresh removes rows (previously the selection could vanish and navigation keys stopped responding).
- History sparklines zero-fill missing time buckets instead of compressing gaps into a misleading contiguous trend.
- Environment detection is skipped when `--region`, `--project`, and `--cluster-name` are all set, runs its metadata lookups concurrently, and is bypassed entirely by `version` (previously every command paid up to ~3s of metadata timeouts off-GCP).
- Flag/validation mistakes (e.g. missing `--region`) are no longer reported to Sentry as application errors.
- Event log formatting: zero-rounding changes no longer print "+0%", and ages over a day render as "2d ago".
- `history` durations are capped at 5 years, preventing an int64 overflow that produced a broken SQL time filter.
- A custom `--prometheus-url` with a trailing slash no longer produces a double-slash query path.
- Aggregation output order is deterministic (sorted by group key) instead of map-iteration order.
- README: Go version prerequisite updated from 1.25+ to 1.26+ to match go.mod
- README: Docker section updated to reflect current Dockerfile (copies pre-built binary) and added GHCR pull instructions
- README: Features list updated with all current capabilities (history, utilization, aberration detection, unmatched-pods, env auto-detection, namespace exclusion)
- README: Watch and record command options now document all flags including --mode, --prometheus-url, --dry-run, --output-file, --exclude-namespaces, --trend-threshold
- README: Added missing unmatched-pods and version command documentation
- README: Added environment auto-detection section
- SPEC: Fixed InsertID format to include the cost_mode field (matching implementation)
- SPEC: Updated Pod Discovery section to reflect --mode flag support for both Autopilot and Standard node filtering
- Fixed root command Short/Long descriptions to say "GKE workloads" instead of "GKE Autopilot workloads"

## [0.6.1] - 2026-04-06

### Fixed
- Fix Docker build

## [0.6.0] - 2026-04-06

### Added
- `history` command: query BigQuery for historical cost data and display in an interactive TUI with sparkline trend visualizations
  - Multi-cluster support for `history` command via `--all-clusters` flag: view costs across all GKE clusters with a CLUSTER column in the table
  - `--cluster-name` flag on `history` command: explicitly filter to a specific cluster (defaults to auto-detected cluster, consistent with other commands)
  - Duration argument supports hours (`3h`), days (`3d`), and weeks (`1w`)
  - Adaptive time bucketing for sparklines (5min to 1day based on range)
  - Same interactive features as `watch`: team grouping, expand/collapse, sorting, cursor navigation
  - Optional filters: `--cluster-name`, `--all-clusters`, `--namespace`, `--team`, `--dataset`, `--table`
  - Displays total spend, average $/hr, average pod count, CPU/memory requests
  - Utilization columns (CPU%, MEM%, WASTE) shown when data is available

### Changed
- Upgraded Bubble Tea from v1 (`github.com/charmbracelet/bubbletea`) to v2 (`charm.land/bubbletea/v2`) with declarative view model
- Upgraded Lip Gloss from v1 (`github.com/charmbracelet/lipgloss`) to v2 (`charm.land/lipgloss/v2`)
- Updated all dependencies to latest versions: sentry-go v0.44.1, parquet-go v0.29.0, cobra latest, oauth2 v0.36.0, k8s.io packages v0.35.3
- Dockerfile now copies a pre-built binary instead of downloading from GitHub Releases at build time
- Release workflow builds and pushes a Docker image to `ghcr.io/samn/gke-cost-analyzer` on each tagged release (tagged with version and `latest`), reusing the same binary from the release artifacts

## [0.5.0] - 2026-03-17

### Changed
- Renamed project from `autopilot-cost-analyzer` to `gke-cost-analyzer` to reflect support for both Autopilot and Standard GKE workloads
- Go module path changed from `github.com/samn/autopilot-cost-analyzer` to `github.com/samn/gke-cost-analyzer`
- Binary name changed from `autopilot-cost-analyzer` to `gke-cost-analyzer`
- Cache directory changed from `~/.cache/autopilot-cost-analyzer/` to `~/.cache/gke-cost-analyzer/`
- Default BigQuery dataset name changed from `autopilot_costs` to `gke_costs`
- Upgrade to go 1.26.1
- Release binaries are now gzip-compressed for faster downloads

### Added
- Cost aberration detection in `watch` TUI: tracks per-workload cost trends using EWMA and highlights sudden deviations while tolerating normal cyclical patterns (autoscaling)
- Running event log in `watch` TUI showing cost changes with timestamps and relative time (`e` to toggle, `[`/`]` to scroll)
- Aberration indicators (▲/▼) on $/HR values in the cost table for workloads with active cost deviations
- `--trend-threshold` flag for `watch` command to configure aberration sensitivity (default 3.0 z-scores, 0 to disable)
- Elapsed watch duration displayed in TUI header to contextualize accumulated costs
- Team rollup in `watch` TUI: costs are grouped by team with expand/collapse drill-down into individual workloads (Enter/Space to toggle, `a` to expand/collapse all, ↑↓/j/k to navigate)
- Flat/grouped view toggle (`g` key): grouped mode sorts at team level with nested workloads; flat mode sorts all workloads individually regardless of team
- Horizontal separator line before the TOTAL row in the `watch` TUI table
- TOTAL row now includes total pod count, CPU requests, and memory requests

## [0.4.0] - 2026-03-16

### Added
- Standard GKE workload cost estimation via per-node proportional attribution
- `--mode` flag (`autopilot`, `standard`, `all`; default: `all`) for selecting cost calculation mode
- Compute Engine pricing fetched from Cloud Billing Catalog API (service ID `6F81-5844-456A`)
- Node discovery for machine type and capacity detection
- `cost_mode` field in BigQuery/Parquet schema to distinguish pricing models
- MODE column in TUI `watch` display (shown when `--mode all`)
- Kubernetes RBAC now requires `list` permission on `nodes` (in addition to `pods`) for standard GKE cost attribution

## [0.3.0] - 2026-03-03

### Fixed
- Utilization calculation with partial Prometheus data: when only some pods in a group have metrics, the utilization denominator now uses only the requests of pods with data (not all pods in the group). Previously, pods without metrics inflated the denominator, causing utilization to be significantly underestimated (e.g., 9% instead of 90% when 1 of 10 pods had data).

### Changed
- **Breaking schema change**: BigQuery/Parquet column `wasted_cost_per_hour` renamed to `wasted_cost` and now stores the wasted cost for the snapshot interval window (`wasted_cost_per_hour × interval_hours`) instead of the per-hour rate. This makes `SUM(wasted_cost)` over a time range return the actual wasted cost, consistent with `cpu_cost`, `memory_cost`, and `total_cost`. Requires re-running `setup` or manually altering the table schema.

## [0.2.1] - 2026-03-02

### Fixed
- `record` command: BigQuery snapshots now store the cost for the snapshot interval window (cost_per_hour × interval_hours) instead of the cumulative pod lifetime cost, fixing SUM(total_cost) queries returning values ~100x higher than actual billing
- Release workflow now requires CI (lint, build, tests) to pass before creating a GitHub Release
- Sentry panic recovery was silently broken: `RecoverAndCapture` delegated to `sentry.Recover()` which calls `recover()` one level too deep; per the Go spec `recover()` must be called directly inside the deferred function. Panics were never captured to Sentry. Fixed by calling `recover()` directly in `RecoverAndCapture`, then re-panicking so the process exits non-zero.
- Added `AttachStacktrace: true` to Sentry client options so non-panic errors captured via `CaptureError` include a stack trace.
- Added explicit `EnableTracing: false` to Sentry client options to make the error-reporting-only intent unambiguous.

## [0.2.0] - 2026-02-28

### Added
- Sentry error reporting: errors and panics are automatically sent to Sentry when the `SENTRY_DSN` environment variable is set (no tracing, error reporting only)
- `unmatched-pods` command to list running pods missing the configured team or workload labels, grouped by base name (with Kubernetes random suffixes stripped) and namespace
- Dockerfile for running gke-cost-analyzer from a GitHub Release binary (`distroless/static` runtime, `nonroot` user, `VERSION` build arg defaults to latest release)
- README: Permissions section documenting required Kubernetes RBAC and GCP IAM roles for each command

## [0.1.0] - 2026-02-27

### Added
- `--exclude-namespaces` flag to filter out system namespaces (default: `kube-system,gmp-system`), preventing GKE platform pods from polluting cost attribution
- GCP Managed Prometheus as default utilization source: automatically fetches CPU and memory metrics via GMP when a project ID is available, with OAuth2 authentication; `--prometheus-url` overrides with a custom endpoint
- Prometheus-based utilization metrics: `--prometheus-url` global flag fetches CPU and memory utilization from Prometheus to compute per-workload efficiency scores
- Cost-weighted efficiency score: `efficiency = (cpu_util × cpu_cost + mem_util × mem_cost) / total_cost` identifies workloads with the highest optimization potential
- Wasted cost metric: `wasted_cost_per_hour = cost_per_hour × (1 - efficiency)` quantifies the cost of underutilized resources
- `watch` command: CPU%, MEM%, and WASTE columns displayed when `--prometheus-url` is set, with interactive sorting (keys 8/9 or 9/0 depending on column layout)
- `record` command: utilization data (cpu_utilization, memory_utilization, efficiency_score, wasted_cost_per_hour) included in BigQuery and Parquet snapshots when Prometheus is configured
- BigQuery schema: 4 new NULLABLE FLOAT64 columns for utilization metrics
- `internal/prometheus` package: Prometheus HTTP API client for fetching container CPU/memory usage via PromQL instant queries
- `record` command: `--output-file` flag to append `--dry-run` snapshots to a local Parquet file (same schema as BigQuery table)
- Auto-detect `--region`, `--project`, and `--cluster-name` from GCE metadata server (GKE) and kubeconfig context (development); explicit CLI flags always take priority
- Test coverage improvements across all packages (bigquery 84%→89%, kube 72%→76%, pricing 84%→86%)
- Comprehensive SPEC.md documentation of core data collection pipeline, cost calculation formulas, unit conversions, aggregation logic, and edge cases
- Tests for init container exclusion from resource requests, memory binary-to-SI unit conversion, partial resource requests, cost linearity, CostPerHour duration independence, nil labels aggregation, namespace-from-first-pod behavior, empty subtype grouping, price table duplicate/passthrough behavior, and catalog SKU edge cases
- `watch` command: Prometheus fetch errors and utilization status are now displayed in the TUI header instead of being silently written to stderr (invisible in alt-screen mode)
- BigQuery InsertID now includes Subtype field, preventing silent deduplication of rows that differ only by subtype
- `record` command: snapshot timestamp is now captured before pod listing to accurately reflect the snapshot window start
- `record` command: `--dry-run` flag to log rows without writing to BigQuery
- `watch` command: SUBTYPE column is now hidden when --subtype-label is not set
- Filter out non-Autopilot pods by requiring node names with `gk3-` prefix
- `watch` command: interactive column sorting with number keys (1-7 or 1-8) and asc/desc toggle
- `version` subcommand printing version, commit, and build date
- Makefile for building static binaries with version metadata (supports cross-compilation via GOOS/GOARCH)
- GitHub Actions release workflow: builds linux-amd64 binary and creates GitHub Release on tag push
- Release process documentation in plans/003-binary-build-release.md
- `watch` command: COST column showing accumulated cost alongside $/HR projected rate
- `watch` command: real-time display of GKE Autopilot workload costs in a terminal table
- `record` command: periodically write cost snapshots to BigQuery
- `setup` command: create BigQuery dataset and table for cost recording
- Cloud Billing Catalog API client for fetching Autopilot pod pricing (CPU and Memory)
- Local file-based price cache with configurable TTL (~/.cache/gke-cost-analyzer/)
- Kubernetes pod listing with CPU/memory request extraction
- Automatic SPOT pod detection via node selector labels
- Cost calculation: resource requests x duration x unit price
- Cost aggregation by configurable label hierarchy (team, workload, subtype)
- BigQuery schema with day-partitioned timestamp and team/workload clustering
- Configurable label names via CLI flags (--team-label, --workload-label, --subtype-label)
- Namespace filtering via --namespace flag
- GMP utilization queries now use GKE system metrics (`kubernetes_io:container_cpu_core_usage_time`, `kubernetes_io:container_memory_used_bytes`) which are automatically collected by GKE without requiring managed Prometheus collection to be enabled; `--prometheus-url` still uses standard Prometheus metric names

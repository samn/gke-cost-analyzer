# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
- Upgrade to go 1.26.1

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
- Dockerfile for running autopilot-cost-analyzer from a GitHub Release binary (`distroless/static` runtime, `nonroot` user, `VERSION` build arg defaults to latest release)
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
- Local file-based price cache with configurable TTL (~/.cache/autopilot-cost-analyzer/)
- Kubernetes pod listing with CPU/memory request extraction
- Automatic SPOT pod detection via node selector labels
- Cost calculation: resource requests x duration x unit price
- Cost aggregation by configurable label hierarchy (team, workload, subtype)
- BigQuery schema with day-partitioned timestamp and team/workload clustering
- Configurable label names via CLI flags (--team-label, --workload-label, --subtype-label)
- Namespace filtering via --namespace flag
- GMP utilization queries now use GKE system metrics (`kubernetes_io:container_cpu_core_usage_time`, `kubernetes_io:container_memory_used_bytes`) which are automatically collected by GKE without requiring managed Prometheus collection to be enabled; `--prometheus-url` still uses standard Prometheus metric names

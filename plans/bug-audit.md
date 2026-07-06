# Repo-wide Bug Audit & Fixes

## Context

A first-principles audit of the whole codebase to find latent bugs and fix them
on a stable foundation. Three parallel code reviews (pricing/cost/kube,
bigquery/prometheus/cmd, tui/trend/env) plus direct verification of every core
file produced ~25 confirmed findings; all were fixed with red/green TDD on
branch `claude/repo-bug-audit-pf8fcm`.

Decisions made with the user:
- Standard-mode node cost basis switched from `Allocatable` to **`Capacity`**
  (matches what GCE bills).
- **Namespace joined `GroupKey`** — group identity is deterministic end-to-end
  (aggregation, InsertID, both history queries, sparkline join key).
- Fix everything (high/medium correctness bugs and low/cosmetic issues).

## Findings fixed (by area)

### Data integrity & cost correctness
1. InsertID collision (hyphen-joined labels → silent BigQuery row drops) →
   SHA-256 over the JSON-encoded identity tuple (`internal/bigquery/writer.go`).
2. Nondeterministic namespace + mismatched history GROUP BY → namespace in
   `GroupKey`/`WorkloadKey` and both queries (`internal/cost/aggregator.go`,
   `internal/bigquery/history.go`, `reader.go`).
3. Native sidecars (restartable init containers) excluded from requests →
   now counted (`internal/kube/pods.go`).
4. Node cost from Allocatable → Capacity with Allocatable fallback
   (`internal/kube/nodes.go`).
5. `--namespace` corrupted standard-mode share denominators → cluster-wide
   listing + post-calculation filter (`cost.FilterByNamespace`,
   `cmd/common.go:listNamespace`, record + watch wiring).
6. Compute SKU regex conflated "N2 Custom"/"Sole Tenancy" with plain families;
   missing M1/M2/M4/C4D/H4D; alternate description forms; last-write-wins price
   table; stale PricingInfo record selection (`internal/pricing/compute.go`,
   `catalog.go`).
7. History reader: no pagination (silent truncation), `$/hr` fencepost
   (MAX-MIN span), SQL string interpolation with incomplete escaping, NULL
   cost_mode split → pageToken loop, `SUM(interval_seconds)` denominator,
   named query parameters + identifier validation, `IFNULL(cost_mode,
   'autopilot')` (`internal/bigquery/reader.go`).
8. `setup` never migrated existing tables → schema diff + PATCH of missing
   NULLABLE columns (`internal/bigquery/setup.go`).
9. record daemon: no SIGTERM, no HTTP timeouts, fixed-interval undercounting on
   missed ticks, launch-time prices forever → shutdownSignals, 30s client
   timeouts + per-snapshot context timeout, elapsed-time windows, 24h price
   refresh (`cmd/record.go`, `cmd/common.go`).
10. Parquet append rewrote the file in place (crash = data loss) → temp file +
    rename (`internal/parquet/writer.go`).

### Trend / TUI
11. Tracker: `Threshold<=0` not honored internally; `MinCostPerHour` masked
    large drops to near-zero (`internal/trend/tracker.go`).
12. 11 sortable columns vs 10 keys (WASTE unreachable, "11=" in help) → key
    sequence extended with `-`/`=`, shared source of truth
    (`internal/tui/sort.go`, both models).
13. Cursor not clamped when a refresh shrank the table (`internal/tui/model.go`).
14. Event-log `[`/`]` scroll was documented but unimplemented → implemented
    with clamped offset (`internal/tui/events.go`, `model.go`).
15. Sparklines omitted empty buckets (gap compression) → zero-fill
    (`bigquery.BuildSparklinesWithGaps`).
16. history silently blended all clusters when detection failed → warning +
    CLUSTER column (`cmd/history.go`).

### Smaller fixes
17. Environment detection: skipped when all values set, concurrent metadata
    lookups, `version` exempt (`cmd/root.go`, `internal/envdefaults`).
18. Price cache: atomic writes, separate compute default file, generic warning
    wording (`internal/pricing/cache.go`).
19. Newest `PricingInfo` record selected by `effectiveTime`.
20. Flag/validation errors no longer reported to Sentry (`usageError`,
    `main.go`, `SetFlagErrorFunc`).
21. Prometheus URL trailing-slash join fixed.
22. Deterministic aggregation output order (sorted by GroupKey).
23. history duration capped at 5 years (int64 overflow guard).
24. Trend formatting: no "+0%", day bucket for relative times.

### Verified correct (no change)
mCPU→vCPU ×1000 conversion; SI GB units; spot propagation from node in
standard mode; StandardCalculator locking; zone→region conversion (regression
test added for zonal kubeconfig contexts); billing-catalog fetch pagination;
panic/flush ordering in main.go; Makefile ldflags ↔ version.go; parquet-go
already stores zero-value optional fields as NULL.

## Known limitations (unchanged, documented)
- GPU / local SSD costs are not attributed (spec'd v1 limitation).
- Sustained/committed-use discounts not modeled.
- Only Running pods at snapshot time are visible; pods that start and finish
  entirely between ticks are not captured.
- Standard-mode attribution distributes 100% of node cost across visible pods
  (idle capacity is charged to whatever runs there) — intentional, per spec.

## Verification
- Every fix landed red/green: a failing test first, then the change.
- `gofmt`, `go vet`, `go build`, `go test -race ./...`, `golangci-lint run`
  all clean (CI parity; prek unavailable in this environment due to network
  policy, checks run individually).

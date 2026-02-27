# 005: Utilization & Efficiency Score

## Goal

Pull CPU and memory utilization data from Prometheus, compute an efficiency
score for each workload group, and surface wasted cost. This helps identify
workloads that could be optimized.

## Design

### Prometheus Integration (`internal/prometheus`)

New package to query the Prometheus HTTP API (`/api/v1/query`).

**CLI flag**: `--prometheus-url` (global, optional). When empty, utilization
fields are left at zero/nil and the feature is effectively disabled.

**PromQL queries** (instant queries):

- **CPU usage** (cores per pod):
  `sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`

- **Memory usage** (bytes per pod):
  `sum by (namespace, pod) (container_memory_working_set_bytes{container!="",container!="POD"})`

The 5m rate window is standard for container CPU metrics and provides stable
readings regardless of the scrape/refresh interval.

Results are returned as a map keyed by `(namespace, pod)` → usage value, which
the aggregator can join with pod info to compute utilization ratios.

### Utilization Calculation

Per-pod:
- `cpu_utilization = cpu_usage_cores / cpu_request_vcpu`
- `memory_utilization = memory_usage_bytes / memory_request_bytes`

Per-group (aggregated):
- `cpu_utilization = sum(cpu_usage) / sum(cpu_request)` across all pods in group
- `memory_utilization = sum(mem_usage) / sum(mem_request)` across all pods in group

### Efficiency Score

A cost-weighted utilization ratio (0.0–1.0):

```
efficiency_score = (cpu_util × cpu_cost_per_hour + mem_util × mem_cost_per_hour) / total_cost_per_hour
```

This weights each resource's utilization by its cost contribution. A workload
spending mostly on CPU but barely using CPU will score low even if memory
utilization is high.

### Wasted Cost

```
wasted_cost_per_hour = cost_per_hour × (1 - efficiency_score)
```

This naturally scales with replicas and total cost — a 100-pod deployment
wasting 80% of its requests shows a much larger `wasted_cost_per_hour` than a
2-pod deployment with the same utilization, making it easy to prioritize
optimization efforts.

### Data Model Changes

**`cost.AggregatedCost`** — new fields:
- `CPUUtilization float64` (ratio, 0-1+)
- `MemUtilization float64` (ratio, 0-1)
- `EfficiencyScore float64` (0-1)
- `WastedCostPerHour float64` ($)

**`bigquery.CostSnapshot`** — new NULLABLE FLOAT64 columns:
- `cpu_utilization`
- `memory_utilization`
- `efficiency_score`
- `wasted_cost_per_hour`

NULLABLE so existing rows and runs without `--prometheus-url` don't require
values. Nullable floats represented as `*float64` in Go.

**`parquet.Row`** — matching new fields (optional/pointer for nullable).

### Watch Mode

When `--prometheus-url` is set, show 3 extra columns:
- `CPU%` — CPU utilization as percentage
- `MEM%` — Memory utilization as percentage
- `WASTE` — Wasted cost per hour

When not set, these columns are hidden (same as subtype column behavior).

Sort keys are extended to cover the new columns.

### Record Mode

When `--prometheus-url` is set, fetch utilization before each snapshot write
and populate the new fields. When not set, pointer fields remain nil.

## Implementation Steps

1. **`internal/prometheus/client.go`** — Prometheus API client
   - `Client` struct with `baseURL` and `httpClient`
   - `QueryCPUUsage(ctx, namespace)` → `map[PodKey]float64`
   - `QueryMemoryUsage(ctx, namespace)` → `map[PodKey]float64`
   - Tests with httptest server

2. **`internal/cost/aggregator.go`** — extend aggregation
   - New `PodUtilization` struct: `{CPUUsage, MemUsage float64}` per pod
   - New `AggregateWithUtilization()` that takes a utilization map
   - Compute per-group utilization ratios, efficiency score, wasted cost
   - Original `Aggregate()` unchanged (no Prometheus = no utilization)

3. **`internal/bigquery/schema.go`** — new columns
   - Add 4 nullable float columns to `CostSnapshot` and `TableSchema()`

4. **`internal/bigquery/writer.go`** — serialize new fields
   - Update `snapshotToRow()` to include new fields (skip nil)

5. **`internal/parquet/writer.go`** — new fields in Row
   - Add matching parquet columns

6. **`cmd/root.go`** — `--prometheus-url` global flag

7. **`cmd/record.go`** — integrate utilization fetching
   - Create prometheus client if URL set
   - Fetch utilization in `recordSnapshot()`
   - Use `AggregateWithUtilization()` when available

8. **`cmd/watch.go`** — pass prometheus client to TUI

9. **`internal/tui/`** — display utilization columns
   - Model: fetch utilization alongside costs
   - Table: render CPU%, MEM%, WASTE columns
   - Sort: new sort columns for utilization/waste

10. **Update SPEC.md, CHANGELOG.md**

## Edge Cases

- **Prometheus unavailable**: Log warning, proceed with nil utilization
- **Pod not found in Prometheus**: That pod gets 0 utilization (unknown)
- **Utilization > 1.0**: Valid for CPU (burst); capped at 1.0 for efficiency
  score so it doesn't produce negative waste
- **Zero cost_per_hour**: Efficiency score = 0 (avoid division by zero)
- **No namespace filter**: Query all namespaces from Prometheus

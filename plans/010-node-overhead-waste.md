# Node Overhead Waste for Standard GKE Workloads

## Goal
For standard workloads, track unallocated node capacity as "Waste". When a node
is not fully allocated (total pod requests < node capacity), the excess cost
is already distributed proportionally among pods. We now label that overhead
portion separately and count it toward wasted cost.

## Calculation
For each resource (CPU, memory) on a node:
- `allocation_ratio = min(total_requests / node_capacity, 1.0)`
- `overhead_fraction = 1 - allocation_ratio`
- Per pod: `overhead_cost = pod_proportional_cost × overhead_fraction`

Equivalently: `overhead = proportional_cost - (pod_request / node_capacity × node_cost)`

When the node is overcommitted (`total_requests > node_capacity`), overhead is 0.

## Changes

### 1. PodCost struct (`internal/cost/calculator.go`)
- Add `CPUOverheadCostPerHour` / `MemOverheadCostPerHour float64` — the per-resource
  node overhead portions of this pod's hourly cost. Always zero for Autopilot pods.
  (Per-resource, not a single total, so the aggregator can separate own cost from
  overhead cost per resource.)

### 2. StandardCalculator (`internal/cost/standard.go`)
- Compute the per-node CPU/memory overhead fraction and apply it to each pod's
  proportional cost share to populate the two overhead fields.

### 3. AggregatedCost (`internal/cost/aggregator.go`)
- Add `CPUOverheadCostPerHour` / `MemOverheadCostPerHour float64`, summed from the
  pod costs.

### 4. Aggregator waste logic (`internal/cost/aggregator.go`)
Waste is the sum of two **disjoint** components, so overhead is never dropped and
never double-counted:
- **Node overhead** (`cpu_overhead + mem_overhead`) — always counted, whether or
  not Prometheus data is present. Unallocated capacity is pure waste.
- **Request waste** (Prometheus only) — the requested-but-unused portion of the
  group's *own* (non-overhead) cost:
  `own_r × (1 - min(util_r, 1))` summed over CPU and memory.

  `WastedCostPerHour = node_overhead + request_waste` (Prometheus present),
  else `WastedCostPerHour = node_overhead`.

  The efficiency score measures usage against *requests*, so it can never reflect
  capacity-level overhead — hence overhead is added separately rather than folded
  into `CostPerHour × (1 - efficiency)`. For Autopilot (overhead = 0) the result
  is identical to the old `CostPerHour × (1 - efficiency)`.

### 5. Record command (`cmd/record.go`)
- `aggregatedToSnapshot` records `WastedCost` whenever there is either utilization
  data **or** a positive `WastedCostPerHour` (i.e. node overhead without
  Prometheus). Overhead is folded into the existing `wasted_cost` column rather
  than persisted as a separate BigQuery/Parquet field. For standard-mode groups
  without Prometheus, `wasted_cost` is populated while the utilization columns
  remain NULL — this is expected.

### 6. TUI (`internal/tui/table.go`, `internal/tui/history_table.go`)
- The `WASTE` cell is shown whenever a group has a waste figure (not gated on the
  presence of utilization data), so standard-mode node overhead is visible even
  without Prometheus and the per-row column reconciles with the TOTAL/team WASTE
  rows. `CPU%`/`MEM%` remain gated on utilization data.
- `internal/tui/sort.go` needs no overhead-specific rollup — team `WastedCostPerHour`
  already includes overhead.

### 7. Tests
- `standard_test.go`: verify overhead calculation for fully-allocated,
  half-allocated, two-pod, and overcommitted nodes.
- `aggregator_test.go`: verify waste = overhead + request waste (Prometheus),
  waste = overhead at full utilization (regression guard), and waste = overhead
  without Prometheus.
- `record_test.go`: snapshot omits `WastedCost` only when there is neither
  utilization data nor overhead; records it for the no-Prometheus overhead path.

## Note
An earlier draft of this plan proposed a dedicated `node_overhead_cost` column in
the BigQuery schema and Parquet writer. That was not implemented: node overhead is
surfaced through the existing `wasted_cost` column, keeping the schema unchanged.

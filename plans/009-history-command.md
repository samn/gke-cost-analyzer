# Plan: `history` Command — Query & Display Recent Cost History from BigQuery

## Context

The `record` command writes periodic cost snapshots to BigQuery. Currently there's no way to query that data back. Users want to quickly see recent cost trends (e.g., last 3 days or 1 week) in a TUI similar to `watch` mode, with sparklines showing cost trends over time.

The BigQuery integration currently only **writes** (via REST API). We need to add **read** capability, a new CLI command, and a new TUI model.

## BubbleTea v2 / Lipgloss v2 Notes

The project has been upgraded to **bubbletea v2** (`charm.land/bubbletea/v2 v2.0.2`) and **lipgloss v2** (`charm.land/lipgloss/v2 v2.0.2`). Key API differences from v1:

- **Import paths**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/lipgloss/v2/table`
- **Key events**: `tea.KeyPressMsg` (was `tea.KeyMsg` in v1)
- **Space key**: matched as `"space"` (was `" "` in v1)
- All new TUI code must use these v2 APIs. Follow the patterns in the existing `internal/tui/model.go` and `internal/tui/table.go`.

## Overview

- New `history` command: `gke-cost-analyzer history 3d` (positional arg: `3h`, `3d`, `1w`)
- Queries BigQuery for aggregated costs + time-bucketed data for sparklines
- Displays in a BubbleTea TUI with the same grouped/flat, sort, expand/collapse UX as `watch`
- Adds a TREND column with Unicode sparklines (▁▂▃▄▅▆▇█)

---

## Step 1: Sparkline rendering — `internal/tui/sparkline.go`

Pure function, zero dependencies. Uses Unicode block characters `▁▂▃▄▅▆▇█` to map a `[]float64` to a compact trend string.

```go
func Sparkline(values []float64) string
```

- Empty input → empty string
- All-same values → flat bars
- Scales linearly between min and max

**Test** (`sparkline_test.go`): empty, single value, ascending, descending, all-same, all-zero.

---

## Step 2: Data model — `internal/bigquery/history.go`

New types for history query results:

```go
// HistoryCostRow — one row per team/workload/subtype/cost_mode in the aggregated query
type HistoryCostRow struct {
    Team, Workload, Subtype, Namespace, CostMode string
    HasSpot        bool
    AvgPods        float64
    AvgCPUVCPU     float64
    AvgMemoryGB    float64
    TotalCost      float64
    TotalCPUCost   float64
    TotalMemCost   float64
    AvgCostPerHour float64
    TotalWastedCost float64
    AvgCPUUtil     *float64
    AvgMemUtil     *float64
    AvgEfficiency  *float64
}

// WorkloadKey — map key for sparkline lookup (matches grouping in the query)
type WorkloadKey struct {
    Team, Workload, Subtype, CostMode string
}

// TimeSeriesPoint — one bucket in the time-series query
type TimeSeriesPoint struct {
    Key        WorkloadKey
    Bucket     time.Time
    BucketCost float64
}
```

**Test** (`history_test.go`): struct construction, WorkloadKey equality for map usage.

---

## Step 3: BigQuery Reader — `internal/bigquery/reader.go`

Follow the exact same pattern as `Writer` and `SetupClient`: raw HTTP REST API, functional options, injectable HTTP client + base URL for testing.

```go
type Reader struct {
    httpClient *http.Client
    project, dataset, table, baseURL string
}

func NewReader(project, dataset, table string, opts ...ReaderOption) *Reader
```

**Options**: `WithReaderHTTPClient`, `WithReaderBaseURL`

**Low-level method**: `query(ctx, sql) (*queryResponse, error)` — POSTs to `POST /projects/{project}/queries` endpoint.

**High-level methods**:
```go
func (r *Reader) QueryAggregatedCosts(ctx, since time.Time, filters QueryFilters) ([]HistoryCostRow, error)
func (r *Reader) QueryTimeSeries(ctx, since time.Time, bucketSeconds int64, filters QueryFilters) ([]TimeSeriesPoint, error)
```

`QueryFilters` struct holds optional `ClusterName`, `Namespace`, `Team` filters.

**BigQuery REST API**: The synchronous query endpoint `POST /projects/{projectId}/queries` sends SQL and returns results inline. Response rows are `[{"f": [{"v": "value"}, ...]}]` with a schema. Parse into typed structs.

**SQL for aggregated costs**:
```sql
SELECT team, workload, subtype, namespace, cost_mode,
  LOGICAL_OR(is_spot) AS has_spot,
  AVG(pod_count) AS avg_pods,
  AVG(cpu_request_vcpu) AS avg_cpu_vcpu,
  AVG(memory_request_gb) AS avg_memory_gb,
  SUM(total_cost) AS total_cost,
  SUM(cpu_cost) AS total_cpu_cost,
  SUM(memory_cost) AS total_mem_cost,
  SAFE_DIVIDE(SUM(total_cost), TIMESTAMP_DIFF(MAX(timestamp), MIN(timestamp), SECOND) / 3600.0) AS avg_cost_per_hour,
  SUM(IFNULL(wasted_cost, 0)) AS total_wasted_cost,
  AVG(cpu_utilization) AS avg_cpu_util,
  AVG(memory_utilization) AS avg_mem_util,
  AVG(efficiency_score) AS avg_efficiency
FROM `{project}.{dataset}.{table}`
WHERE timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL {seconds} SECOND)
  [AND cluster_name = '{cluster}']
  [AND namespace = '{namespace}']
  [AND team = '{team}']
GROUP BY team, workload, subtype, namespace, cost_mode
ORDER BY total_cost DESC
```

**SQL for time series** (sparklines):
```sql
SELECT team, workload, subtype, cost_mode,
  TIMESTAMP_SECONDS(DIV(UNIX_SECONDS(timestamp), {bucket}) * {bucket}) AS bucket,
  SUM(total_cost) AS bucket_cost
FROM `{project}.{dataset}.{table}`
WHERE timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL {seconds} SECOND)
  [AND cluster_name = '{cluster}']
  [AND namespace = '{namespace}']
  [AND team = '{team}']
GROUP BY team, workload, subtype, cost_mode, bucket
ORDER BY team, workload, subtype, cost_mode, bucket
```

**Adaptive bucket sizing**:
| Range  | Bucket   | ~Points |
|--------|----------|---------|
| ≤6h    | 5 min    | ≤72     |
| ≤1d    | 30 min   | ≤48     |
| ≤3d    | 1 hour   | ≤72     |
| ≤1w    | 4 hours  | ≤42     |
| >1w    | 1 day    | varies  |

**Test** (`reader_test.go`): httptest server with canned BigQuery JSON responses. Test SQL generation, row parsing, error cases (403, empty results, job not complete), filter inclusion.

---

## Step 4: Duration parsing + command skeleton — `cmd/history.go`

```go
var historyCmd = &cobra.Command{
    Use:   "history <duration>",
    Short: "View historical cost data from BigQuery",
    Args:  cobra.ExactArgs(1),
    RunE:  runHistory,
}
```

**Flags**: `--dataset` (default `gke_costs`), `--table` (default `cost_snapshots`), `--cluster-name`, `--team` (optional filter). Reuses global `--project`, `--namespace`.

**Duration parsing** (`parseDuration`): handles `h`, `d`, `w` suffixes. Distinct from `time.ParseDuration` which doesn't support days/weeks.

**`runHistory` flow**:
1. Validate `--project` required
2. Parse duration argument
3. Create authenticated HTTP client (reuse `gcpHTTPClientFn`)
4. Create `bigquery.Reader`
5. Compute adaptive bucket size
6. Create `tui.HistoryModel` with reader + config
7. Run `tea.NewProgram(model, tea.WithAltScreen())`

**Test** (`history_test.go`): duration parsing (valid/invalid), validation errors.

---

## Step 5: History sort infrastructure — `internal/tui/history_sort.go`

Follows `sort.go` pattern but for history-specific columns.

**Sort columns**: `HistSortByTeam`, `HistSortByWorkload`, `HistSortBySubtype`, `HistSortByMode`, `HistSortByAvgPods`, `HistSortByAvgCPU`, `HistSortByAvgMem`, `HistSortByAvgCostPerHour`, `HistSortByTotalCost`, `HistSortByWaste`

**Types**: `HistorySortColumn`, `HistorySortConfig`, `historyColumnDef`

**Functions**: `historyVisibleColumns(vis)`, `SortHistoryRows()`, `HistoryTeamGroup`, `GroupHistoryByTeam()`, `SortHistoryTeamGroups()`, `HistoryColumnForKey()`

**Test** (`history_sort_test.go`): sort by each column, toggle direction, team grouping.

---

## Step 6: History table rendering — `internal/tui/history_table.go`

Follows `table.go` pattern.

**Columns**:
| Column   | Description              | Sortable | Numeric |
|----------|--------------------------|----------|---------|
| TEAM     | Team name                | Yes      | No      |
| WORKLOAD | Workload name            | Yes      | No      |
| SUBTYPE  | (if enabled)             | Yes      | No      |
| MODE     | (if multi-mode)          | Yes      | No      |
| AVG PODS | Average pod count        | Yes      | Yes     |
| AVG CPU  | Average CPU request      | Yes      | Yes     |
| AVG MEM  | Average memory request   | Yes      | Yes     |
| AVG $/HR | Average cost per hour    | Yes      | Yes     |
| TOTAL    | Total spend in period    | Yes      | Yes     |
| TREND    | Sparkline                | No       | No      |
| SPOT     | Has spot workloads       | No       | No      |

**Types**: `HistoryDisplayRow` (analogous to `DisplayRow`), with `HistoryCostRow` instead of `AggregatedCost`.

**Functions**: `RenderHistoryTable()`, `buildHistoryDisplayRows()`, `buildFlatHistoryDisplayRows()`

Reuses existing Lipgloss styles (`headerStyle`, `cellStyle`, `numericStyle`, etc.) from `table.go`.

**Test** (`history_table_test.go`): render with sample data, verify headers, sparklines in output, totals.

---

## Step 7: History TUI model — `internal/tui/history_model.go`

New BubbleTea model, separate from watch `Model`.

```go
type HistoryModel struct {
    // Data
    rows       []bigquery.HistoryCostRow
    sparklines map[bigquery.WorkloadKey]string  // pre-rendered
    err        error
    loading    bool

    // Display state (same patterns as watch model)
    sortCfg       HistorySortConfig
    showSubtype   bool
    showMode      bool
    hasUtilization bool
    grouped       bool
    expandedTeams map[string]bool
    cursor        int
    displayRows   []HistoryDisplayRow
    teamGroups    []HistoryTeamGroup

    // Metadata
    timeRange     time.Duration
    totalCost     float64
    workloadCount int

    // Data fetching
    reader       *bigquery.Reader
    filters      bigquery.QueryFilters
    bucketSecs   int64
}
```

**Lifecycle**:
- `Init()` → fires async `fetchHistory` command
- `fetchHistory()` → calls `reader.QueryAggregatedCosts()` + `reader.QueryTimeSeries()`, builds sparkline strings, returns `historyDataMsg`
- `Update()` → handles data message (one-time), key presses (sort, navigate, expand/collapse, quit)
- `View()` → header + table + help footer
- No periodic refresh (data loaded once)

**Header format**:
```
GKE Cost Analyzer — History (3d) — $142.37 total — 15 workloads
```

**Key bindings**: same as watch, using bubbletea v2 `tea.KeyPressMsg` (not `tea.KeyMsg`):
- `q`, `ctrl+c`: quit
- `1-9,0`: sort by column
- `up`/`down`, `j`/`k`: navigate
- `enter`/`space` (matched as `"space"` in v2): expand/collapse
- `a`: toggle all, `g`: grouped/flat

**Edge case**: If no data found, display message and exit cleanly.

**Test** (`history_model_test.go`): Init returns fetch command, data message updates state, key presses (sort, navigate, quit), loading/error states.

---

## Step 8: Documentation

- **`SPEC.md`**: Add `history` command section with flags, behavior, query details
- **`CHANGELOG.md`**: Add entries under `[Unreleased]` → `Added`
- **`plans/009-history-command.md`**: Copy of this plan (per CLAUDE.md convention)

---

## Files to create

| File | Purpose |
|------|---------|
| `internal/tui/sparkline.go` | Sparkline rendering |
| `internal/tui/sparkline_test.go` | Tests |
| `internal/bigquery/history.go` | History data model structs |
| `internal/bigquery/history_test.go` | Tests |
| `internal/bigquery/reader.go` | BigQuery query client |
| `internal/bigquery/reader_test.go` | Tests |
| `internal/tui/history_sort.go` | Sort infrastructure for history |
| `internal/tui/history_sort_test.go` | Tests |
| `internal/tui/history_table.go` | History table rendering |
| `internal/tui/history_table_test.go` | Tests |
| `internal/tui/history_model.go` | BubbleTea model for history TUI |
| `internal/tui/history_model_test.go` | Tests |
| `cmd/history.go` | Cobra command + duration parsing |
| `cmd/history_test.go` | Tests |

## Files to modify

| File | Change |
|------|--------|
| `SPEC.md` | Add `history` command spec |
| `CHANGELOG.md` | Add entries under `[Unreleased]` |

No existing Go source files need modification — the command registers itself via `init()`.

## Verification

1. `go build ./...` — compiles without warnings
2. `go test -race ./...` — all tests pass
3. `go vet ./...` — no issues
4. `mise exec -- golangci-lint run` — no lint warnings
5. Manual test: `gke-cost-analyzer history 3d --project <PROJECT>` against a real BigQuery table with recorded data

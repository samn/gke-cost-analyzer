# 007: Normalize wasted cost to interval window

## Problem

The `wasted_cost_per_hour` field in BigQuery/Parquet stored the per-hour waste
rate, while all other cost fields (`cpu_cost`, `memory_cost`, `total_cost`) were
already normalized to the snapshot interval window. This inconsistency meant
`SUM(wasted_cost_per_hour)` over a time range did NOT return the actual wasted
cost — it returned a meaningless sum of per-hour rates.

## Solution

Rename `wasted_cost_per_hour` → `wasted_cost` in the BigQuery/Parquet schema and
normalize the value to the interval window, matching the pattern used by the
other cost fields:

```
wasted_cost = wasted_cost_per_hour × (interval_seconds / 3600)
```

The aggregator (`internal/cost/aggregator.go`) continues to compute
`WastedCostPerHour` as a per-hour rate — this is still used by the TUI `watch`
command for real-time display. The conversion to interval-normalized cost happens
in `cmd/record.go:aggregatedToSnapshot()`, the same place where `cpu_cost`,
`memory_cost`, and `total_cost` are already normalized.

## Files changed

| File | Change |
|------|--------|
| `internal/bigquery/schema.go` | Rename `WastedCostPerHour` → `WastedCost`, update column name and description |
| `internal/bigquery/writer.go` | Update `snapshotToRow()` to use new field name |
| `cmd/record.go` | Multiply `WastedCostPerHour × intervalHours` before storing in snapshot |
| `internal/parquet/writer.go` | Rename field in `Row` struct and conversion functions |
| `internal/bigquery/schema_test.go` | Update expected field name |
| `cmd/record_test.go` | Add tests for interval normalization and daily summation |
| `SPEC.md` | Document interval normalization for wasted cost |
| `CHANGELOG.md` | Add breaking schema change entry |

## Key invariant

```
SUM(wasted_cost) over 24h = wasted_cost_per_hour × 24
```

This is now tested in `TestWastedCostSumsCorrectlyOverDay`.

## Migration

This is a **breaking schema change**. Existing BigQuery tables have a
`wasted_cost_per_hour` column; the new code writes to `wasted_cost`. Users must
either:

1. Re-run `setup` to recreate the table (loses historical data), or
2. Manually `ALTER TABLE ... ADD COLUMN wasted_cost FLOAT64` and backfill

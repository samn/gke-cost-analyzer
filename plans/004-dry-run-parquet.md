# Plan 004: Dry-Run Parquet Output

## Problem

When using `--dry-run` mode, data is printed as JSON to stdout. This makes it
difficult to accumulate test data across multiple runs and to query it with
tools that support parquet (DuckDB, pandas, BigQuery load, etc.). We need a way
to append dry-run snapshots to a local parquet file with the same schema as the
BigQuery `cost_snapshots` table.

## Design

### New flag

Add `--output-file` to the `record` command. When combined with `--dry-run`,
snapshots are appended to the specified parquet file instead of printing JSON to
stdout. The summary line is still printed.

- `--dry-run` alone → JSON to stdout (existing behavior, unchanged)
- `--dry-run --output-file foo.parquet` → append to parquet file
- `--output-file` without `--dry-run` → validation error

### Parquet schema

The parquet file uses the same column names and compatible types as the BigQuery
table:

| Column             | Parquet Type              |
|--------------------|---------------------------|
| timestamp          | INT64 (timestamp micros)  |
| project_id         | BYTE_ARRAY (string)       |
| region             | BYTE_ARRAY (string)       |
| cluster_name       | BYTE_ARRAY (string)       |
| namespace          | BYTE_ARRAY (string)       |
| team               | BYTE_ARRAY (string)       |
| workload           | BYTE_ARRAY (string)       |
| subtype            | BYTE_ARRAY (string)       |
| pod_count          | INT64                     |
| cpu_request_vcpu   | DOUBLE                    |
| memory_request_gb  | DOUBLE                    |
| cpu_cost           | DOUBLE                    |
| memory_cost        | DOUBLE                    |
| total_cost         | DOUBLE                    |
| is_spot            | BOOLEAN                   |
| interval_seconds   | INT64                     |

### Append strategy

Parquet files are immutable once written (sealed footer). To append:
1. If the file exists, read all existing rows.
2. Write all rows (existing + new) to the file atomically.

This is simple and correct. For testing data volumes this is efficient enough.

### Library

`github.com/parquet-go/parquet-go` — the standard community-maintained Go
parquet library with a clean generic API (`parquet.ReadFile[T]`,
`parquet.WriteFile`).

### Package layout

```
internal/parquet/
  writer.go       — Row struct, CostSnapshot→Row conversion, AppendToFile()
  writer_test.go  — Tests
```

### Changes

| File                            | Change                                        |
|---------------------------------|-----------------------------------------------|
| go.mod                          | Add parquet-go dependency                     |
| internal/bigquery/schema.go     | No changes (Row struct lives in parquet pkg)  |
| internal/parquet/writer.go      | New: Row type + AppendToFile function         |
| internal/parquet/writer_test.go | New: Tests for write, append, read-back       |
| cmd/record.go                   | Add --output-file flag, wire parquet writing  |
| cmd/record_test.go              | Add tests for --output-file behavior          |
| CHANGELOG.md                    | Document the new feature                      |

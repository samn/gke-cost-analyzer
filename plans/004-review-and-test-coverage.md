# Plan 004: Code Review & Test Coverage Improvement

## Defects Found

### Bug: BigQuery InsertID Missing Subtype Field
**File:** `internal/bigquery/writer.go:85`
**Severity:** Medium — data loss
**Description:** The InsertID for BigQuery streaming inserts omits the `Subtype`
field. Two rows that differ only in Subtype will have identical InsertIDs, causing
BigQuery to silently deduplicate one of them. This means subtypes would lose data
when the `--subtype-label` flag is used.
**Fix:** Include `s.Subtype` in the InsertID format string.

### Bug: parseUnitPrice Silently Ignores Parse Errors
**File:** `internal/pricing/catalog.go:248-253`
**Severity:** Low — silent failure
**Description:** `strconv.ParseFloat` error is discarded. If the billing API returns
an unexpected non-numeric string in the `units` field, the price defaults to 0.0
without any indication. This could lead to all costs being calculated as $0.
**Fix:** Return an error from parseUnitPrice and propagate it up.

### Issue: Snapshot Timestamp Captured After Processing
**File:** `cmd/record.go:136`
**Severity:** Low — imprecise timestamps
**Description:** `time.Now()` is called after pod listing and cost calculation,
which could take seconds if the cluster is large. The timestamp should represent
when the snapshot window begins, not when processing completes.
**Fix:** Capture timestamp before pod listing.

## Test Coverage Improvements

Current coverage:
- `cmd` — 37.1% (target: 60%+)
- `internal/kube` — 71.7% (target: 85%+)
- `internal/pricing` — 83.8% (target: 90%+)
- `internal/bigquery` — 84.3% (target: 90%+)

### cmd package gaps
- `labelConfig()` untested
- `loadPrices()` untested (hard to test without mocking, but the function composition is testable)
- `setup` command beyond validation untested
- `record` negative interval validation untested

### kube package gaps
- `buildClient()` untested (requires kubeconfig — acceptable)
- `NewTestPodInfo` not tested for edge cases

### pricing package gaps
- `extractAutopilotPrices` with `ServiceRegions` fallback not tested
- `FetchPrices` with SKU that has zero-price rates not tested
- `parseUnitPrice` with invalid units string not tested

### bigquery package gaps
- `NewSetupClient` default values untested
- `NewWriter` default values untested
- `Write` with `httpClient` that returns SetupHTTPClient defaults

## Approach
- Red/green TDD: Write failing test first, then fix
- Minimal changes — only fix actual defects
- No refactoring beyond what's needed for fixes

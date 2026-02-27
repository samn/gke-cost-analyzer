# autopilot-cost-analyzer

## Problem

We want to clearly see the cost for a workload (the aggregate cost of Pods grouped by specific labels) over time, and understand its efficiency.
While GKE (Google Kubernetes Engine) supports exporting billing & consumption data to BigQuery, it's difficult to query (especially for Autopilot workloads).

While cost attribution is quite complex for standard GKE workloads, we can take a simpler approach with Autopilot (on standard or Autopilot clusters).
Autopilot Pods are billed for their resource requests * running duration * resource cost.

## autopilot-cost-analyzer CLI

A tool (written in golang) to monitor usage of Autopilot workloads and either display a table of cost over time (`watch`) or write to bigquery.
There should be a command to create the necessary BigQuery dataset & tables. Prices should be fetched from the prices API and cached in `~/.cache`.
SPOT prices must be supported.

The data written to BigQuery must support the following types of queries:
- What is the total cost of a workload by a given label

Include the project id, region, and cluster name too.

The collected labels can be configured in advance to reduce cardinality.
We'll want to roll up metrics along several dimensions, e.g.
- team (top level)
- workload (i.e. Deployment/Argo Workflow name)
- subtype (optional, i.e. a specific step in an Argo Workflow)

The user should be able to configure the actual label names for that hiearchy.

The program should be efficient so that it can run collecting metrics for many Pods.

## Commands

### `watch`
Real-time terminal display of workload costs. Fetches pod data on an interval
(default 10s), calculates costs, aggregates by label hierarchy, and renders an
interactive table via BubbleTea. Supports interactive column sorting with number
keys.

Flags: `--interval`, `--region` (required), `--namespace`, `--team-label`,
`--workload-label`, `--subtype-label`.

### `record`
Daemon mode: periodically snapshot pod costs and write aggregated records to
BigQuery. Runs once immediately on startup, then on a ticker.

Flags: `--interval` (default 5m), `--region` (required), `--project` (required),
`--cluster-name` (required), `--dataset`, `--table`, `--namespace`,
`--dry-run`, `--output-file` (requires `--dry-run`; writes Parquet locally).

### `setup`
Create the BigQuery dataset and table with the correct schema, partitioning,
and clustering configuration.

### `version`
Print version, git commit, and build date.

### Global flags
`--team-label` (default `team`), `--workload-label` (default `app`),
`--subtype-label` (default empty / disabled), `--namespace` (default all),
`--region` (auto-detected from GCE metadata or kubeconfig).

Environment defaults: `--region`, `--project`, and `--cluster-name` are
auto-detected from the GCE metadata server (when running on GKE) or from the
current kubeconfig context. Explicit CLI flags always take priority.

## Core Data Collection Pipeline

### 1. Price Fetching (`internal/pricing`)

Prices are fetched from the **Cloud Billing Catalog API** for the Kubernetes
Engine service (ID `CCD8-9BF1-090E`). The API is paginated (page size 5000).

**SKU Matching**: SKUs are matched by description substring:
- `"Autopilot Pod mCPU Requests"` → CPU, On-Demand
- `"Autopilot Pod Memory Requests"` → Memory, On-Demand
- `"Autopilot Spot Pod mCPU Requests"` → CPU, Spot
- `"Autopilot Spot Pod Memory Requests"` → Memory, Spot

Non-matching SKUs are silently ignored. If no Autopilot SKUs are found at all,
an error is returned.

**Price Extraction**: For each matched SKU, prices are extracted per-region
from `GeoTaxonomy.Regions` (falling back to `ServiceRegions` if empty). The
unit price is taken from `PricingInfo[].PricingExpression.TieredRates[]`: the
first tiered rate with a non-zero price is used. The price is constructed from
`Units` (string, parsed as float) + `Nanos / 1e9`.

**Unit Conversion**: CPU prices from the billing API are **per mCPU-hour**.
`FromPrices()` converts them to **per vCPU-hour** by multiplying by 1000.
Memory prices are **per GB-hour** and require no conversion.

**Caching**: Prices are cached to `~/.cache/autopilot-cost-analyzer/prices.json`
with a default TTL of 24 hours. Corrupt cache files are treated as cache misses
(not errors). Cache save failures are logged as warnings but don't block
operation.

#### Edge cases
- **Zero-price tiered rates**: Skipped (the first non-zero rate is used).
- **Multiple tiered rates**: Only the first non-zero rate is used (free-tier
  rates with `StartUsageAmount: 0` and zero price are common).
- **Invalid `Units` string**: If `Units` cannot be parsed as a float, it
  defaults to 0; only the `Nanos` portion contributes.
- **Empty regions**: Falls back from `GeoTaxonomy.Regions` to `ServiceRegions`.
- **Missing region in PriceTable**: `Lookup()` returns 0, which causes the
  cost calculator to produce $0 for pods in that region.

### 2. Pod Discovery (`internal/kube`)

Pods are listed from the Kubernetes API with a field selector
`status.phase=Running`. Multi-namespace or single-namespace listing is
supported via `--namespace`.

**Autopilot filtering**: Only pods on nodes with the `gk3-` name prefix are
included. Standard GKE nodes use `gke-` and are filtered out. Unscheduled pods
(empty `NodeName`) are also excluded.

**Resource extraction**: CPU and memory **requests** are summed across all
containers in the pod (init containers are not included — Autopilot billing
is based on regular container requests). Resources are stored in both raw
units (millicores, bytes) and derived units (vCPU, GB).

**Unit handling**:
- CPU: Kubernetes millicores → `CPURequestVCPU = millicores / 1000.0`
- Memory: Kubernetes bytes → `MemRequestGB = bytes / 1e9` (SI gigabytes, i.e.
  10^9, matching GCP billing units). Note: Kubernetes `Mi` (mebibytes, 2^20)
  and `Gi` (gibibytes, 2^30) are binary units, so `256Mi = 268435456 bytes =
  ~0.268 GB`, not 0.256 GB.

**SPOT detection**: A pod is considered SPOT if its `NodeSelector` contains
either:
- `cloud.google.com/gke-spot: "true"`, or
- `cloud.google.com/compute-class: "autopilot-spot"`

**Start time**: Extracted from `pod.Status.StartTime`. If nil (pod not yet
started), it is stored as zero time.

#### Edge cases
- **Containers with no resource requests**: Contribute 0 to the sum (no error).
- **Pods with no containers**: Produce zero CPU/memory (valid but unusual).
- **Nil StartTime**: Stored as zero time; cost calculator treats this as 0
  duration → $0 cost.
- **Non-Autopilot pods**: Filtered out by `gk3-` node name check.
- **Kubernetes API errors**: Propagated to the caller.

### 3. Cost Calculation (`internal/cost`)

The core formula for a single pod:

```
cpu_cost = CPURequestVCPU × duration_hours × cpu_price_per_vcpu_hour
mem_cost = MemRequestGB   × duration_hours × mem_price_per_gb_hour
total_cost = cpu_cost + mem_cost
cost_per_hour = (CPURequestVCPU × cpu_price) + (MemRequestGB × mem_price)
```

Where:
- `duration_hours = max(0, now - pod.StartTime)` in hours
- Prices are looked up by `(region, resource_type, tier)` from the PriceTable

The calculator uses an injectable `now` function for deterministic testing.

#### Edge cases
- **Zero start time** (never started): `duration_hours = 0` → `total_cost = 0`,
  but `cost_per_hour` is still calculated (shows projected rate).
- **Future start time** (clock skew): `duration_hours = 0` → `total_cost = 0`,
  `cost_per_hour` is still non-zero.
- **Just-started pod** (`startTime == now`): `duration_hours = 0` →
  `total_cost = 0`, `cost_per_hour` shows the rate.
- **Zero resource requests**: Both costs are 0 regardless of duration.
- **Missing region prices**: Price lookup returns 0 → costs are $0 (silent,
  no error).
- **SPOT vs On-Demand**: Tier is selected based on `pod.IsSpot`; each tier
  has independent prices.

### 4. Cost Aggregation (`internal/cost`)

Pod costs are grouped by a **GroupKey** consisting of:
- `Team`: value of the team label (or `""` if label missing/unconfigured)
- `Workload`: value of the workload label
- `Subtype`: value of the subtype label
- `IsSpot`: whether the pod is SPOT

SPOT and On-Demand pods with the same labels are **always separate groups**
because they have different pricing tiers.

Aggregated fields are summed across all pods in the group: `PodCount`,
`TotalCPUVCPU`, `TotalMemGB`, `CPUCost`, `MemCost`, `TotalCost`,
`CostPerHour`. The `Namespace` is taken from the first pod in the group.

#### Edge cases
- **Missing labels**: Pods without a configured label get `""` as the value;
  they group together separately from pods that have the label set.
- **Empty subtype label config**: When `--subtype-label` is empty, all pods
  get `Subtype: ""` and are not differentiated by subtype.
- **Empty input**: Returns an empty slice (not nil, not an error).

### 5. BigQuery Snapshot Model (`internal/bigquery`)

Each record is a `CostSnapshot` with 16 fields: timestamp, project_id, region,
cluster_name, namespace, team, workload, subtype, pod_count, cpu_request_vcpu,
memory_request_gb, cpu_cost, memory_cost, total_cost, is_spot, interval_seconds.

**Table configuration**:
- Partitioned by DAY on `timestamp`
- Clustered by `team`, `workload`

**InsertID** for deduplication:
`{project}-{cluster}-{namespace}-{team}-{workload}-{subtype}-{is_spot}-{timestamp_nanos}`

This ensures that rows differing only by subtype or spot status get unique IDs,
preventing silent deduplication.

**Snapshot timing**: The timestamp is captured **before** listing pods, so it
reflects the start of the snapshot window rather than when processing completed.

The `record` command writes the cost snapshot at each interval. These snapshots
represent a **point-in-time** view of running pod costs. The `interval_seconds`
field records the configured interval for downstream cost-over-time calculations.

#### Edge cases
- **Empty snapshot list**: `Write()` returns nil immediately (no API call).
- **BigQuery API errors**: HTTP status != 200 returns an error with the
  response body.
- **Insert errors** (partial failures): All row-level errors are collected and
  returned as a single error with details.

## Building
- Use golang
- Use mise to manage the environment
- IMPORTANT: everything must be tested.
- Use red/green TDD
- Ensure there are no warnings or errors
- Before committing make sure the lint/format/tests/compile all work
- Use prek for precommit hooks
- Make sure functionality is documented

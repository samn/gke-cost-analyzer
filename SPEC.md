# autopilot-cost-analyzer

## Problem

We want to clearly see the cost for a workload (the aggregate cost of Pods grouped by specific labels) over time, and understand its efficiency.
While GKE (Google Kubernetes Engine) supports exporting billing & consumption data to BigQuery, it's difficult to query (especially for Autopilot workloads).

While cost attribution is quite complex for standard GKE workloads, we can take a simpler approach with Autopilot (on standard or Autopilot clusters).
Autopilot Pods are billed for their resource requests * running duration * resource cost.

## autopilot-cost-analyzer CLI

A tool (written in golang) to monitor usage of GKE workloads (Autopilot and standard) and either display a table of cost over time (`watch`) or write to bigquery.
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

Flags: `--interval`, `--region` (required), `--project` (global), `--namespace`,
`--team-label`, `--workload-label`, `--subtype-label`, `--prometheus-url` (optional).

Utilization columns (CPU%, MEM%, WASTE) are automatically displayed when a
Prometheus source is available — either GCP Managed Prometheus (default when
project is detected) or a custom `--prometheus-url`. Utilization data is
fetched on each refresh cycle.

### `record`
Daemon mode: periodically snapshot pod costs and write aggregated records to
BigQuery. Runs once immediately on startup, then on a ticker.

Flags: `--interval` (default 5m), `--region` (required), `--project` (required, global),
`--cluster-name` (required), `--dataset`, `--table`, `--namespace`,
`--dry-run`, `--output-file` (requires `--dry-run`; writes Parquet locally),
`--prometheus-url` (optional).

Utilization metrics are automatically fetched from GCP Managed Prometheus
(default when project is detected) or a custom `--prometheus-url` before each
snapshot. If the fetch fails, the snapshot proceeds without utilization data (a
warning is logged to stderr).

### `setup`
Create the BigQuery dataset and table with the correct schema, partitioning,
and clustering configuration.

### `version`
Print version, git commit, and build date.

### Global flags
`--team-label` (default `team`), `--workload-label` (default `app`),
`--subtype-label` (default empty / disabled), `--namespace` (default all),
`--region` (auto-detected from GCE metadata or kubeconfig),
`--exclude-namespaces` (default `kube-system,gmp-system`) — comma-separated
list of namespaces to exclude from pod listing. When `--namespace` targets a
specific namespace the exclusion list is effectively a no-op. Set to an empty
string to include all namespaces.
`--project` (GCP project ID; auto-detected from GCE metadata or kubeconfig;
used by `record`/`setup` for BigQuery and by all commands for GMP Prometheus).
`--prometheus-url` (override Prometheus API base URL; defaults to GCP Managed
Prometheus when a project ID is available).
`--mode` (cost calculation mode: `autopilot`, `standard`, or `all`; default
`all`). In `all` mode, pods are partitioned by node type and costed with the
appropriate calculator; a MODE column is shown in the TUI.

Environment defaults: `--region`, `--project`, and `--cluster-name` are
auto-detected from the GCE metadata server (when running on GKE) or from the
current kubeconfig context. Explicit CLI flags always take priority.

**Prometheus auto-detection**: When `--prometheus-url` is not set and a GCP
project ID is available (via `--project` or auto-detected), utilization metrics
are automatically fetched from Google Cloud Managed Service for Prometheus (GMP)
at `https://monitoring.googleapis.com/v1/projects/{project}/location/global/prometheus`.
The request is authenticated using Application Default Credentials with the
`monitoring.read` scope. Set `--prometheus-url` to a custom URL to use a
self-hosted Prometheus instead.

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

**Namespace exclusion**: Pods in namespaces listed in `--exclude-namespaces`
(default `kube-system,gmp-system`) are filtered out post-fetch. This removes
GKE platform overhead pods (DaemonSets like fluentbit, pdcsi-node, gke-metrics,
GMP collectors) that cannot be user-labeled and would inflate the "unlabeled"
bucket. The exclusion uses an O(1) set lookup built once per `ListPods` call.

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
- **Excluded namespaces**: Pods in excluded namespaces are dropped post-fetch.
  Empty exclusion list disables the filter.
- **Kubernetes API errors**: Propagated to the caller.

### 2b. Node Discovery (`internal/kube`)

Nodes are listed from the Kubernetes API for standard GKE cost attribution.
Only nodes with the `gke-` name prefix are included (Autopilot `gk3-` nodes
are excluded).

**Machine type**: Extracted from the `node.kubernetes.io/instance-type` label
(e.g., `n2-standard-4`).

**Machine family**: Parsed from the machine type by taking the first segment
before the first `-`. Special case: bare `custom-N-M` types map to family
`n1` (GCP bills N1 rates for these).

**Allocatable resources**: vCPU and memory are read from
`node.Status.Allocatable` (reliable for all machine types including custom).

**Spot detection**: A node is considered Spot if its label
`cloud.google.com/gke-spot` is `"true"`.

### 1b. Compute Engine Price Fetching (`internal/pricing`)

Prices are fetched from the **Cloud Billing Catalog API** for the Compute
Engine service (ID `6F81-5844-456A`). The API is paginated (page size 5000).

**SKU Matching**: SKUs are matched by regex:
`^(Spot Preemptible )?(N2|E2|N1|C3|...) Instance (Core|Ram) running in`

- `"Core"` → CPU, `"Ram"` → Memory
- `"Spot Preemptible"` prefix → Spot tier
- Family name → MachineFamily (lowercased)

Unlike Autopilot (per-mCPU), Compute Engine CPU prices are already per-vCPU-hour
— no ×1000 conversion is needed.

**Caching**: Prices are cached to
`~/.cache/autopilot-cost-analyzer/compute_prices.json` with the same 24-hour
TTL as Autopilot prices.

### 3b. Standard Cost Calculation (`internal/cost`)

For standard GKE, costs are attributed to pods proportionally by their resource
requests on each node:

```
node_cpu_cost = node.VCPU × cpu_price_per_vcpu_hour
node_mem_cost = node.MemoryGB × mem_price_per_gb_hour
pod_cpu_share = pod.CPURequest / total_cpu_requests_on_node
pod_cost_per_hour = pod_cpu_share × node_cpu_cost + pod_mem_share × node_mem_cost
```

Edge cases:
- **Zero total requests on a node**: Cost share is 0 for all pods
- **Pod on unknown node**: Skipped with warning, $0 cost
- **System pods excluded**: Their cost is absorbed by visible workloads

**Known limitations (v1)**:
- GPU and local SSD costs are not attributed
- Sustained use discounts and committed use discounts are not accounted for

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
- `CostMode`: `"autopilot"` or `"standard"` (derived from `pod.IsAutopilot`)

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

### 5. Utilization Metrics (`internal/prometheus`)

By default, utilization metrics are fetched from **Google Cloud Managed Service
for Prometheus (GMP)** when a GCP project ID is available. The GMP query
endpoint is:
`https://monitoring.googleapis.com/v1/projects/{project}/location/global/prometheus`

Requests to GMP are authenticated using Application Default Credentials with
the `https://www.googleapis.com/auth/monitoring.read` OAuth2 scope.

A custom `--prometheus-url` can be specified to query a self-hosted Prometheus
instance instead (no GCP auth is applied for custom URLs).

CPU and memory utilization are fetched using instant queries. The client
supports two query modes:

**GMP system metrics** (default when using GMP with auto-detected project):
Uses GKE system metrics that are automatically collected without requiring
managed Prometheus collection. Metric names follow the Cloud Monitoring →
PromQL naming convention (first `/` → `:`, remaining special chars → `_`).

- **CPU**: `sum by (namespace_name, pod_name) (rate(kubernetes_io:container_cpu_core_usage_time[5m]))`
  Returns actual CPU usage in cores per pod.
- **Memory**: `sum by (namespace_name, pod_name) (kubernetes_io:container_memory_used_bytes{memory_type="non-evictable"})`
  Returns non-evictable memory usage in bytes per pod (closest equivalent to
  working set).

**Standard Prometheus** (when `--prometheus-url` is set):
Uses standard cAdvisor/kubelet metric names for self-hosted Prometheus or GMP
with managed collection enabled.

- **CPU**: `sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`
  Returns actual CPU usage in cores per pod.
- **Memory**: `sum by (namespace, pod) (container_memory_working_set_bytes{container!="",container!="POD"})`
  Returns actual memory usage in bytes per pod.

Utilization ratios are computed per aggregation group:

```
cpu_utilization = sum(actual_cpu_cores) / sum(requested_cpu_vcpu)
mem_utilization = sum(actual_mem_bytes / 1e9) / sum(requested_mem_gb)
```

**Efficiency score** (cost-weighted utilization, 0–1):

```
efficiency = (min(cpu_util, 1.0) × cpu_cost_per_hour + min(mem_util, 1.0) × mem_cost_per_hour) / cost_per_hour
```

CPU utilization is capped at 1.0 for the efficiency calculation — CPU burst
above 100% still means the requested resources are fully utilized.

**Wasted cost** (per-hour rate, used by the TUI `watch` command):

```
wasted_cost_per_hour = cost_per_hour × (1 - efficiency_score)
```

When recorded to BigQuery/Parquet, the wasted cost is normalized to the snapshot
interval window (same as the other cost fields):

```
wasted_cost = wasted_cost_per_hour × interval_hours
```

#### Edge cases
- **Prometheus unavailable**: Utilization fields are nil/zero; snapshot proceeds
  without utilization data (warning logged to stderr).
- **Partial pod data**: If some pods have no Prometheus metrics, only pods with
  data contribute to the utilization calculation.
- **Zero cost**: If `cost_per_hour == 0`, efficiency score is 0 and wasted cost
  is 0.
- **CPU burst**: CPU utilization > 100% is valid (burst above requests); capped
  at 1.0 for the efficiency score to avoid negative wasted cost.

### 6. BigQuery Snapshot Model (`internal/bigquery`)

Each record is a `CostSnapshot` with 21 fields: timestamp, project_id, region,
cluster_name, namespace, team, workload, subtype, pod_count, cpu_request_vcpu,
memory_request_gb, cpu_cost, memory_cost, total_cost, is_spot, interval_seconds,
cost_mode, cpu_utilization, memory_utilization, efficiency_score, wasted_cost.

The `cost_mode` field is a NULLABLE STRING (`"autopilot"` or `"standard"`).
Existing rows with NULL are treated as `"autopilot"` (backward compatible).

The last four fields are NULLABLE FLOAT64 columns — they are only populated
when `--prometheus-url` is configured and utilization data is available.

**Table configuration**:
- Partitioned by DAY on `timestamp`
- Clustered by `team`, `workload`

**InsertID** for deduplication:
`{project}-{cluster}-{namespace}-{team}-{workload}-{subtype}-{is_spot}-{timestamp_nanos}`

This ensures that rows differing only by subtype or spot status get unique IDs,
preventing silent deduplication.

**Snapshot timing**: The timestamp is captured **before** listing pods, so it
reflects the start of the snapshot window rather than when processing completed.

The `record` command writes the cost snapshot at each interval. All cost fields
(`cpu_cost`, `memory_cost`, `total_cost`, `wasted_cost`) represent the cost
incurred during the snapshot **interval window** only, computed as
`cost_per_hour × interval_hours`. This ensures that `SUM(total_cost)` (or
`SUM(wasted_cost)`) over a time range equals the actual cost (or waste) for that
period.

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

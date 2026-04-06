# gke-cost-analyzer

A CLI tool to monitor and analyze costs of GKE workloads (Autopilot and standard), with support for real-time display and BigQuery export.

## Features

- **Real-time cost monitoring** (`watch`): Displays a live table of GKE workload costs aggregated by configurable labels
- **Standard GKE cost estimation**: Per-node proportional attribution for standard GKE workloads based on Compute Engine pricing
- **BigQuery recording** (`record`): Periodically writes cost snapshots to BigQuery for historical analysis
- **Automated setup** (`setup`): Creates BigQuery dataset and table with the correct schema
- **Price caching**: Fetches Autopilot pricing from the Cloud Billing Catalog API and caches locally
- **SPOT support**: Automatically detects and separately prices Spot pods
- **Configurable label hierarchy**: Group costs by team, workload, and subtype using custom label names

## Installation

### Prerequisites

- Go 1.25+
- Access to a GKE cluster (via kubeconfig)
- GCP credentials (for BigQuery features)

### Build

```bash
go build -o gke-cost-analyzer .
```

### Docker

Build an image from a GitHub Release binary:

```bash
# Uses the latest release by default
docker build -t gke-cost-analyzer .

# Pin to a specific version
docker build --build-arg VERSION=v0.1.0 -t gke-cost-analyzer:0.1.0 .
```

Run with arguments passed directly:

```bash
docker run --rm gke-cost-analyzer version

docker run --rm gke-cost-analyzer record \
  --region us-central1 \
  --project my-gcp-project \
  --cluster-name my-cluster
```

## Permissions

### Kubernetes RBAC

The tool needs read access to pods and nodes. It does not create, modify, or delete any Kubernetes resources.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: gke-cost-analyzer
rules:
- apiGroups: [""]
  resources: ["pods", "nodes"]
  verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: gke-cost-analyzer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: gke-cost-analyzer
subjects:
- kind: ServiceAccount
  name: gke-cost-analyzer
  namespace: <namespace>
```

To restrict to specific namespaces, use a `Role` + `RoleBinding` per namespace instead of `ClusterRole`.

### GCP IAM

All GCP API calls use [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials). On GKE, use [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) to bind the Kubernetes ServiceAccount to a GCP service account with the required roles.

| Role | Used by | Purpose |
|------|---------|---------|
| `roles/billing.viewer` | `watch`, `record` | Read Autopilot pricing SKUs from the Cloud Billing Catalog API |
| `roles/bigquery.dataViewer` | `history` | Read cost snapshots from BigQuery |
| `roles/bigquery.dataEditor` | `record` | Stream-insert cost snapshots into BigQuery |
| `roles/bigquery.dataOwner` | `setup` | Create BigQuery datasets and tables (only needed once) |
| `roles/monitoring.metricReader` | `watch`, `record` | Query CPU/memory utilization via GCP Managed Prometheus |

**Notes:**
- `roles/monitoring.metricReader` is only needed when using GCP Managed Prometheus (the default when `--project` is set). It is not needed with `--prometheus-url` (custom Prometheus endpoint) or `--dry-run`.
- `roles/bigquery.dataEditor` is not needed in `--dry-run` mode.
- If you pre-create the dataset and table manually, `roles/bigquery.dataEditor` is sufficient for `setup` as well.

## Usage

### Watch costs in real-time

```bash
gke-cost-analyzer watch --region us-central1
```

Options:
- `--region` (required): GCP region for pricing lookup
- `--mode`: Cost calculation mode: `autopilot`, `standard`, or `all` (default: `all`)
- `--interval`: Refresh interval (default: 10s)
- `--namespace`: Filter to a specific namespace (default: all)
- `--team-label`: Pod label for team grouping (default: "team")
- `--workload-label`: Pod label for workload grouping (default: "app")
- `--subtype-label`: Pod label for subtype grouping (optional)

### Record costs to BigQuery

First, set up the BigQuery dataset and table:

```bash
gke-cost-analyzer setup \
  --project my-gcp-project \
  --location US
```

Then start recording:

```bash
gke-cost-analyzer record \
  --region us-central1 \
  --project my-gcp-project \
  --cluster-name my-cluster \
  --interval 5m
```

Options:
- `--region` (required): GCP region for pricing lookup
- `--project` (required): GCP project ID for BigQuery
- `--cluster-name` (required): GKE cluster name
- `--dataset`: BigQuery dataset name (default: "gke_costs")
- `--table`: BigQuery table name (default: "cost_snapshots")
- `--interval`: Snapshot interval (default: 5m)

### View historical costs

Query BigQuery for recorded cost data:

```bash
gke-cost-analyzer history 3d --project my-gcp-project
```

View costs across all clusters:

```bash
gke-cost-analyzer history 1w --project my-gcp-project --all-clusters
```

Options:
- `--project` (required): GCP project ID for BigQuery
- `--dataset`: BigQuery dataset name (default: "gke_costs")
- `--table`: BigQuery table name (default: "cost_snapshots")
- `--cluster-name`: Filter by cluster name (defaults to auto-detected cluster)
- `--all-clusters`: Show costs from all clusters (adds a CLUSTER column)
- `--namespace`: Filter to a specific namespace
- `--team`: Filter by team name

### Example BigQuery queries

```sql
-- Total cost by team for today
SELECT team, SUM(total_cost) as cost
FROM `project.gke_costs.cost_snapshots`
WHERE DATE(timestamp) = CURRENT_DATE()
GROUP BY team;

-- Hourly cost breakdown for a workload
SELECT
  TIMESTAMP_TRUNC(timestamp, HOUR) as hour,
  workload,
  SUM(total_cost) as cost
FROM `project.gke_costs.cost_snapshots`
WHERE DATE(timestamp) = CURRENT_DATE()
  AND team = 'my-team'
GROUP BY hour, workload
ORDER BY hour;

-- Daily cost trend for past week
SELECT
  DATE(timestamp) as day,
  team,
  SUM(total_cost) as cost,
  SUM(CASE WHEN is_spot THEN total_cost ELSE 0 END) as spot_cost
FROM `project.gke_costs.cost_snapshots`
WHERE timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 7 DAY)
GROUP BY day, team
ORDER BY day;
```

## How it works

1. **Pricing**: Fetches pricing from the Cloud Billing Catalog API. For Autopilot: pod-level CPU/Memory SKUs from the Kubernetes Engine service. For standard GKE: Compute Engine per-vCPU and per-GB instance SKUs by machine family. Prices are cached locally in `~/.cache/gke-cost-analyzer/` for 24 hours.

2. **Pod and node data**: Lists running pods (and nodes for standard GKE) from the current kubeconfig context, extracting CPU/memory requests, labels, start time, node placement, and Spot detection.

3. **Cost calculation**:
   - **Autopilot**: `cost_per_hour = resource_requests × unit_price`. No historical data is needed — the hourly rate is derived purely from each pod's resource requests and the region's pricing.
   - **Standard GKE**: Per-node proportional attribution. Each node's cost (based on its machine type) is distributed to pods proportionally by their resource requests on that node.

4. **Aggregation**: Costs are grouped by the configured label hierarchy (team, workload, subtype), Spot status, and cost mode (autopilot/standard).

### Cost semantics: `watch` vs `record`

The two modes display cost differently:

| Column | `watch` | `record` (BigQuery) |
|--------|---------|---------------------|
| **$/HR** | Hourly rate based on current resource requests × unit price. Always non-zero for pods with requests. | Same formula, stored as `cost_per_hour`. |
| **Cost** | Cumulative cost since each pod started: `cost_per_hour × hours_since_pod_start`. Pods running for hours show meaningful totals; just-started pods show ~$0. | Cost for the snapshot **interval window** only: `cost_per_hour × interval_hours`. This ensures `SUM(total_cost)` over a time range equals the actual cost for that period. |

**Why `watch` shows values immediately**: The TUI computes everything from the current Kubernetes state (pod specs, start times) and cached pricing tables. No prior snapshots or historical data are required. The $/HR column appears instantly because it depends only on resource requests and prices; the Cost column uses each pod's `StartTime` from Kubernetes to calculate accumulated cost.

## Development

```bash
# Install tools
mise install

# Install pre-commit hooks
prek install

# Run tests
go test ./...

# Run linter
golangci-lint run ./...

# Build
go build ./...
```

## Releasing

Releases are automated via GitHub Actions. Pushing a `v`-prefixed tag triggers
the [release workflow](.github/workflows/release.yaml), which runs tests, builds
a static `linux/amd64` binary, compresses it with gzip, and creates a GitHub
Release with changelog notes. Release artifacts are named
`gke-cost-analyzer-<os>-<arch>.gz`.

### Steps

1. **Update the changelog.** Move items from `[Unreleased]` in `CHANGELOG.md`
   into a new version section:

   ```markdown
   ## [X.Y.Z] - YYYY-MM-DD

   ### Added
   - ...
   ```

   Leave an empty `## [Unreleased]` header at the top for future changes.

2. **Commit the changelog.**

   ```bash
   git add CHANGELOG.md
   git commit -m "Release vX.Y.Z"
   ```

3. **Tag and push.**

   ```bash
   git tag vX.Y.Z
   git push origin main vX.Y.Z
   ```

4. **Verify.** The [Release](https://github.com/samn/gke-cost-analyzer/actions/workflows/release.yaml)
   action will build the binary and create the GitHub Release. The release body
   is extracted automatically from the matching `CHANGELOG.md` section.

### Version injection

Version metadata is injected at build time via `-ldflags` (see `Makefile`).
The `version` subcommand prints the tag, short commit hash, and build
timestamp. During development the version defaults to `dev`.

### Building locally

```bash
make build                          # linux/amd64 (default)
make build GOOS=darwin GOARCH=arm64 # macOS Apple Silicon
```

Binaries are written to `dist/`.

## License

MIT

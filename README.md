# autopilot-cost-analyzer

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
go build -o autopilot-cost-analyzer .
```

### Docker

Build an image from a GitHub Release binary:

```bash
# Uses the latest release by default
docker build -t autopilot-cost-analyzer .

# Pin to a specific version
docker build --build-arg VERSION=v0.1.0 -t autopilot-cost-analyzer:0.1.0 .
```

Run with arguments passed directly:

```bash
docker run --rm autopilot-cost-analyzer version

docker run --rm autopilot-cost-analyzer record \
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
  name: autopilot-cost-analyzer
rules:
- apiGroups: [""]
  resources: ["pods", "nodes"]
  verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: autopilot-cost-analyzer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: autopilot-cost-analyzer
subjects:
- kind: ServiceAccount
  name: autopilot-cost-analyzer
  namespace: <namespace>
```

To restrict to specific namespaces, use a `Role` + `RoleBinding` per namespace instead of `ClusterRole`.

### GCP IAM

All GCP API calls use [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials). On GKE, use [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) to bind the Kubernetes ServiceAccount to a GCP service account with the required roles.

| Role | Used by | Purpose |
|------|---------|---------|
| `roles/billing.viewer` | `watch`, `record` | Read Autopilot pricing SKUs from the Cloud Billing Catalog API |
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
autopilot-cost-analyzer watch --region us-central1
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
autopilot-cost-analyzer setup \
  --project my-gcp-project \
  --location US
```

Then start recording:

```bash
autopilot-cost-analyzer record \
  --region us-central1 \
  --project my-gcp-project \
  --cluster-name my-cluster \
  --interval 5m
```

Options:
- `--region` (required): GCP region for pricing lookup
- `--project` (required): GCP project ID for BigQuery
- `--cluster-name` (required): GKE cluster name
- `--dataset`: BigQuery dataset name (default: "autopilot_costs")
- `--table`: BigQuery table name (default: "cost_snapshots")
- `--interval`: Snapshot interval (default: 5m)

### Example BigQuery queries

```sql
-- Total cost by team for today
SELECT team, SUM(total_cost) as cost
FROM `project.autopilot_costs.cost_snapshots`
WHERE DATE(timestamp) = CURRENT_DATE()
GROUP BY team;

-- Hourly cost breakdown for a workload
SELECT
  TIMESTAMP_TRUNC(timestamp, HOUR) as hour,
  workload,
  SUM(total_cost) as cost
FROM `project.autopilot_costs.cost_snapshots`
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
FROM `project.autopilot_costs.cost_snapshots`
WHERE timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 7 DAY)
GROUP BY day, team
ORDER BY day;
```

## How it works

1. **Pricing**: Fetches pricing from the Cloud Billing Catalog API. For Autopilot: pod-level CPU/Memory SKUs from the Kubernetes Engine service. For standard GKE: Compute Engine per-vCPU and per-GB instance SKUs by machine family. Prices are cached locally in `~/.cache/autopilot-cost-analyzer/` for 24 hours.

2. **Pod and node data**: Lists running pods (and nodes for standard GKE) from the current kubeconfig context, extracting CPU/memory requests, labels, start time, node placement, and Spot detection.

3. **Cost calculation**:
   - **Autopilot**: `cost = resource_requests x duration x unit_price`
   - **Standard GKE**: Per-node proportional attribution. Each node's cost (based on its machine type) is distributed to pods proportionally by their resource requests on that node.

4. **Aggregation**: Costs are grouped by the configured label hierarchy (team, workload, subtype), Spot status, and cost mode (autopilot/standard).

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
a static `linux/amd64` binary, and creates a GitHub Release with changelog notes.

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

4. **Verify.** The [Release](https://github.com/samn/autopilot-cost-analyzer/actions/workflows/release.yaml)
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

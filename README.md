# autopilot-cost-analyzer

A CLI tool to monitor and analyze costs of GKE Autopilot workloads, with support for real-time display and BigQuery export.

## Features

- **Real-time cost monitoring** (`watch`): Displays a live table of Autopilot workload costs aggregated by configurable labels
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

## Usage

### Watch costs in real-time

```bash
autopilot-cost-analyzer watch --region us-central1
```

Options:
- `--region` (required): GCP region for pricing lookup
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

1. **Pricing**: Fetches Autopilot pod-level SKUs from the Cloud Billing Catalog API (CPU and Memory, both on-demand and Spot). Prices are cached locally in `~/.cache/autopilot-cost-analyzer/` for 24 hours.

2. **Pod data**: Lists running pods from the current kubeconfig context, extracting CPU/memory requests, labels, start time, and Spot detection.

3. **Cost calculation**: `cost = resource_requests x duration x unit_price`. SPOT prices are used automatically when pods are detected as Spot (via `cloud.google.com/gke-spot=true` or `cloud.google.com/compute-class=autopilot-spot` node selectors).

4. **Aggregation**: Costs are grouped by the configured label hierarchy (team, workload, subtype) and Spot status.

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

## License

MIT

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- `record` command: `--output-file` flag to append `--dry-run` snapshots to a local Parquet file (same schema as BigQuery table)
- Auto-detect `--region`, `--project`, and `--cluster-name` from GCE metadata server (GKE) and kubeconfig context (development); explicit CLI flags always take priority
- Test coverage improvements across all packages (bigquery 84%→89%, kube 72%→76%, pricing 84%→86%)

### Fixed
- BigQuery InsertID now includes Subtype field, preventing silent deduplication of rows that differ only by subtype
- `record` command: snapshot timestamp is now captured before pod listing to accurately reflect the snapshot window start
- `record` command: `--dry-run` flag to log rows without writing to BigQuery
- `watch` command: SUBTYPE column is now hidden when --subtype-label is not set
- Filter out non-Autopilot pods by requiring node names with `gk3-` prefix
- `watch` command: interactive column sorting with number keys (1-7 or 1-8) and asc/desc toggle
- `version` subcommand printing version, commit, and build date
- Makefile for building static binaries with version metadata (supports cross-compilation via GOOS/GOARCH)
- GitHub Actions release workflow: builds linux-amd64 binary and creates GitHub Release on tag push
- Release process documentation in plans/003-binary-build-release.md
- `watch` command: COST column showing accumulated cost alongside $/HR projected rate
- `watch` command: real-time display of GKE Autopilot workload costs in a terminal table
- `record` command: periodically write cost snapshots to BigQuery
- `setup` command: create BigQuery dataset and table for cost recording
- Cloud Billing Catalog API client for fetching Autopilot pod pricing (CPU and Memory)
- Local file-based price cache with configurable TTL (~/.cache/autopilot-cost-analyzer/)
- Kubernetes pod listing with CPU/memory request extraction
- Automatic SPOT pod detection via node selector labels
- Cost calculation: resource requests x duration x unit price
- Cost aggregation by configurable label hierarchy (team, workload, subtype)
- BigQuery schema with day-partitioned timestamp and team/workload clustering
- Configurable label names via CLI flags (--team-label, --workload-label, --subtype-label)
- Namespace filtering via --namespace flag

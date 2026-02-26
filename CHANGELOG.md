# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

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

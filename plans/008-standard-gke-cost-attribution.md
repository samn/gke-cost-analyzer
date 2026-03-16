# Plan: Per-Node Proportional Cost Attribution for Standard GKE Workloads

## Status: Implemented

## Summary

Extended the tool to support standard GKE workloads alongside Autopilot. For
standard GKE, costs are attributed to pods proportionally by their resource
requests on each node, using Compute Engine per-vCPU and per-GB pricing by
machine family.

## Key Changes

1. **Pod Discovery**: Added `ClusterMode` (autopilot/standard/all), `NodeName`,
   and `IsAutopilot` fields to `PodInfo`.
2. **Node Discovery**: New `NodeLister` in `internal/kube/nodes.go` for
   extracting machine type, allocatable resources, and spot status.
3. **Compute Engine Pricing**: New `FetchComputePrices` on `CatalogClient`,
   regex-based SKU matching for instance Core/Ram by machine family.
4. **Standard Calculator**: Per-node proportional attribution in
   `internal/cost/standard.go`.
5. **Interface**: `PodCostCalculator` interface satisfied by both calculators.
6. **`--mode` flag**: autopilot, standard, or all (default: all).
7. **Schema**: Added `cost_mode` field to BigQuery/Parquet.
8. **TUI**: MODE column shown in all mode.

## Known Limitations

- GPU and local SSD costs not attributed
- No CUD/SUD discount support
- `custom-N-M` uses N1 family pricing

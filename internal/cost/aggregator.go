package cost

import (
	"sort"

	"github.com/samn/gke-cost-analyzer/internal/prometheus"
)

// LabelConfig specifies which pod labels to use for grouping.
type LabelConfig struct {
	TeamLabel     string
	WorkloadLabel string
	SubtypeLabel  string
}

// GroupKey uniquely identifies an aggregation group.
type GroupKey struct {
	Namespace string
	Team      string
	Workload  string
	Subtype   string
	IsSpot    bool
	CostMode  string // "autopilot" or "standard"; empty treated as "autopilot"
}

// AggregatedCost holds aggregated cost metrics for a group of pods.
type AggregatedCost struct {
	Key            GroupKey
	CostMode       string // "autopilot" or "standard"
	Namespace      string // mirrors Key.Namespace (for display convenience)
	PodCount       int
	TotalCPUVCPU   float64
	TotalMemGB     float64
	CPUCost        float64
	MemCost        float64
	TotalCost      float64
	CostPerHour    float64
	CPUCostPerHour float64
	MemCostPerHour float64

	// Utilization fields — populated when Prometheus data is available.
	// Zero values indicate no utilization data.
	HasUtilization    bool
	CPUUtilization    float64 // ratio: actual / requested (0–1+)
	MemUtilization    float64 // ratio: actual / requested (0–1)
	EfficiencyScore   float64 // cost-weighted utilization (0–1)
	WastedCostPerHour float64 // cost_per_hour × (1 - efficiency)
}

// Aggregate groups pod costs by the configured label hierarchy.
func Aggregate(costs []PodCost, labels LabelConfig) []AggregatedCost {
	return AggregateWithUtilization(costs, labels, nil)
}

// AggregateWithUtilization groups pod costs by the configured label hierarchy
// and enriches each group with utilization metrics from Prometheus.
// If usage is nil, utilization fields are left at zero.
func AggregateWithUtilization(costs []PodCost, labels LabelConfig, usage map[prometheus.PodKey]prometheus.PodUsage) []AggregatedCost {
	type groupAccum struct {
		agg          AggregatedCost
		totalCPUUsed float64 // sum of CPU cores used across pods in group
		totalMemUsed float64 // sum of memory bytes used across pods in group
		// Requested resources for only pods with Prometheus data, used as
		// the denominator in utilization ratios so that pods without metrics
		// don't artificially deflate the ratio.
		cpuRequestWithUsage float64
		memRequestWithUsage float64
		hasUsage            bool
	}

	groups := make(map[GroupKey]*groupAccum)

	for _, pc := range costs {
		costMode := "standard"
		if pc.Pod.IsAutopilot {
			costMode = "autopilot"
		}
		key := GroupKey{
			Namespace: pc.Pod.Namespace,
			Team:      labelValue(pc.Pod.Labels, labels.TeamLabel),
			Workload:  labelValue(pc.Pod.Labels, labels.WorkloadLabel),
			Subtype:   labelValue(pc.Pod.Labels, labels.SubtypeLabel),
			IsSpot:    pc.Pod.IsSpot,
			CostMode:  costMode,
		}

		ga, ok := groups[key]
		if !ok {
			ga = &groupAccum{
				agg: AggregatedCost{
					Key:       key,
					CostMode:  costMode,
					Namespace: key.Namespace,
				},
			}
			groups[key] = ga
		}

		ga.agg.PodCount++
		ga.agg.TotalCPUVCPU += pc.Pod.CPURequestVCPU
		ga.agg.TotalMemGB += pc.Pod.MemRequestGB
		ga.agg.CPUCost += pc.CPUCost
		ga.agg.MemCost += pc.MemCost
		ga.agg.TotalCost += pc.TotalCost
		ga.agg.CostPerHour += pc.CostPerHour
		ga.agg.CPUCostPerHour += pc.CPUCostPerHour
		ga.agg.MemCostPerHour += pc.MemCostPerHour

		if usage != nil {
			podKey := prometheus.PodKey{Namespace: pc.Pod.Namespace, Pod: pc.Pod.Name}
			if u, found := usage[podKey]; found {
				ga.totalCPUUsed += u.CPUCores
				ga.totalMemUsed += u.MemoryBytes / 1e9 // convert bytes → GB (SI)
				ga.cpuRequestWithUsage += pc.Pod.CPURequestVCPU
				ga.memRequestWithUsage += pc.Pod.MemRequestGB
				ga.hasUsage = true
			}
		}
	}

	// Deterministic output order: map iteration would make Parquet row
	// order (and any snapshot-style consumer) flaky.
	keys := make([]GroupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return lessGroupKey(keys[i], keys[j]) })

	result := make([]AggregatedCost, 0, len(groups))
	for _, key := range keys {
		ga := groups[key]
		if ga.hasUsage {
			ga.agg.HasUtilization = true
			// Only pods with Prometheus data contribute to the utilization
			// calculation (both numerator and denominator) so that pods
			// without metrics don't artificially deflate the ratio.
			if ga.cpuRequestWithUsage > 0 {
				ga.agg.CPUUtilization = ga.totalCPUUsed / ga.cpuRequestWithUsage
			}
			if ga.memRequestWithUsage > 0 {
				ga.agg.MemUtilization = ga.totalMemUsed / ga.memRequestWithUsage
			}
			ga.agg.EfficiencyScore = computeEfficiency(
				ga.agg.CPUUtilization, ga.agg.MemUtilization,
				ga.agg.CPUCostPerHour, ga.agg.MemCostPerHour, ga.agg.CostPerHour,
			)
			ga.agg.WastedCostPerHour = ga.agg.CostPerHour * (1 - ga.agg.EfficiencyScore)
		}
		result = append(result, ga.agg)
	}
	return result
}

// computeEfficiency returns a cost-weighted utilization score (0–1).
// CPU utilization is capped at 1.0 for the score so burst doesn't produce
// negative waste values.
func computeEfficiency(cpuUtil, memUtil, cpuCostPerHour, memCostPerHour, totalCostPerHour float64) float64 {
	if totalCostPerHour <= 0 {
		return 0
	}
	// Cap utilization at 1.0 for efficiency calculation
	cpuCapped := min(cpuUtil, 1.0)
	memCapped := min(memUtil, 1.0)
	return (cpuCapped*cpuCostPerHour + memCapped*memCostPerHour) / totalCostPerHour
}

// lessGroupKey orders GroupKeys by namespace, team, workload, subtype,
// cost mode, then spot status.
func lessGroupKey(a, b GroupKey) bool {
	if a.Namespace != b.Namespace {
		return a.Namespace < b.Namespace
	}
	if a.Team != b.Team {
		return a.Team < b.Team
	}
	if a.Workload != b.Workload {
		return a.Workload < b.Workload
	}
	if a.Subtype != b.Subtype {
		return a.Subtype < b.Subtype
	}
	if a.CostMode != b.CostMode {
		return a.CostMode < b.CostMode
	}
	return !a.IsSpot && b.IsSpot
}

// FilterByNamespace returns only the pod costs whose pod is in namespace ns.
// An empty ns returns the input unchanged. Use this to narrow display to one
// namespace AFTER cost calculation, so that standard-mode per-node share
// denominators were computed from the full pod set.
func FilterByNamespace(costs []PodCost, ns string) []PodCost {
	if ns == "" {
		return costs
	}
	filtered := make([]PodCost, 0, len(costs))
	for _, pc := range costs {
		if pc.Pod.Namespace == ns {
			filtered = append(filtered, pc)
		}
	}
	return filtered
}

func labelValue(labels map[string]string, key string) string {
	if key == "" || labels == nil {
		return ""
	}
	return labels[key]
}

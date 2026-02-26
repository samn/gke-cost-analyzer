package cost

// LabelConfig specifies which pod labels to use for grouping.
type LabelConfig struct {
	TeamLabel     string
	WorkloadLabel string
	SubtypeLabel  string
}

// GroupKey uniquely identifies an aggregation group.
type GroupKey struct {
	Team     string
	Workload string
	Subtype  string
	IsSpot   bool
}

// AggregatedCost holds aggregated cost metrics for a group of pods.
type AggregatedCost struct {
	Key          GroupKey
	Namespace    string // namespace of the first pod (for reference)
	PodCount     int
	TotalCPUVCPU float64
	TotalMemGB   float64
	CPUCost      float64
	MemCost      float64
	TotalCost    float64
	CostPerHour  float64
}

// Aggregate groups pod costs by the configured label hierarchy.
func Aggregate(costs []PodCost, labels LabelConfig) []AggregatedCost {
	groups := make(map[GroupKey]*AggregatedCost)

	for _, pc := range costs {
		key := GroupKey{
			Team:     labelValue(pc.Pod.Labels, labels.TeamLabel),
			Workload: labelValue(pc.Pod.Labels, labels.WorkloadLabel),
			Subtype:  labelValue(pc.Pod.Labels, labels.SubtypeLabel),
			IsSpot:   pc.Pod.IsSpot,
		}

		agg, ok := groups[key]
		if !ok {
			agg = &AggregatedCost{
				Key:       key,
				Namespace: pc.Pod.Namespace,
			}
			groups[key] = agg
		}

		agg.PodCount++
		agg.TotalCPUVCPU += pc.Pod.CPURequestVCPU
		agg.TotalMemGB += pc.Pod.MemRequestGB
		agg.CPUCost += pc.CPUCost
		agg.MemCost += pc.MemCost
		agg.TotalCost += pc.TotalCost
		agg.CostPerHour += pc.CostPerHour
	}

	result := make([]AggregatedCost, 0, len(groups))
	for _, agg := range groups {
		result = append(result, *agg)
	}
	return result
}

func labelValue(labels map[string]string, key string) string {
	if key == "" || labels == nil {
		return ""
	}
	return labels[key]
}

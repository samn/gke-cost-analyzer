package bigquery

import "time"

// HistoryCostRow holds aggregated cost data for a workload over a time range.
type HistoryCostRow struct {
	Team            string
	Workload        string
	Subtype         string
	Namespace       string
	CostMode        string
	HasSpot         bool
	AvgPods         float64
	AvgCPUVCPU      float64
	AvgMemoryGB     float64
	TotalCost       float64
	TotalCPUCost    float64
	TotalMemCost    float64
	AvgCostPerHour  float64
	TotalWastedCost float64
	AvgCPUUtil      *float64
	AvgMemUtil      *float64
	AvgEfficiency   *float64
}

// WorkloadKey identifies a workload for sparkline lookup.
type WorkloadKey struct {
	Team     string
	Workload string
	Subtype  string
	CostMode string
}

// KeyFromRow returns the WorkloadKey for a HistoryCostRow.
func KeyFromRow(r HistoryCostRow) WorkloadKey {
	return WorkloadKey{
		Team:     r.Team,
		Workload: r.Workload,
		Subtype:  r.Subtype,
		CostMode: r.CostMode,
	}
}

// TimeSeriesPoint holds cost data for a single time bucket.
type TimeSeriesPoint struct {
	Key        WorkloadKey
	Bucket     time.Time
	BucketCost float64
}

// QueryFilters holds optional filters for BigQuery queries.
type QueryFilters struct {
	ClusterName string
	Namespace   string
	Team        string
}

// BucketSeconds returns the adaptive bucket size in seconds for a given time range.
func BucketSeconds(d time.Duration) int64 {
	hours := d.Hours()
	switch {
	case hours <= 6:
		return 300 // 5 minutes
	case hours <= 24:
		return 1800 // 30 minutes
	case hours <= 72:
		return 3600 // 1 hour
	case hours <= 168: // 1 week
		return 14400 // 4 hours
	default:
		return 86400 // 1 day
	}
}

// BuildSparklines groups time-series points by workload and returns
// pre-rendered sparkline strings keyed by WorkloadKey.
func BuildSparklines(points []TimeSeriesPoint) map[WorkloadKey][]float64 {
	grouped := make(map[WorkloadKey][]float64)
	for _, p := range points {
		grouped[p.Key] = append(grouped[p.Key], p.BucketCost)
	}
	return grouped
}

package bigquery

import "time"

// HistoryCostRow holds aggregated cost data for a workload over a time range.
type HistoryCostRow struct {
	ClusterName     string
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
	ClusterName string
	Team        string
	Workload    string
	Subtype     string
	Namespace   string
	CostMode    string
}

// KeyFromRow returns the WorkloadKey for a HistoryCostRow.
func KeyFromRow(r HistoryCostRow) WorkloadKey {
	return WorkloadKey{
		ClusterName: r.ClusterName,
		Team:        r.Team,
		Workload:    r.Workload,
		Subtype:     r.Subtype,
		Namespace:   r.Namespace,
		CostMode:    r.CostMode,
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

// BuildSparklinesWithGaps groups time-series points by workload, ordering by
// bucket time and zero-filling missing buckets between each workload's first
// and last bucket. A missing bucket means no cost was recorded for it;
// omitting it would compress time and misrepresent the trend. bucketSeconds
// must be positive (it always is: BucketSeconds never returns less than 300).
func BuildSparklinesWithGaps(points []TimeSeriesPoint, bucketSeconds int64) map[WorkloadKey][]float64 {
	type window struct {
		costs    map[int64]float64 // bucket unix seconds → cost
		min, max int64
	}
	windows := make(map[WorkloadKey]*window)
	for _, p := range points {
		bucket := p.Bucket.Unix()
		w, ok := windows[p.Key]
		if !ok {
			w = &window{costs: make(map[int64]float64), min: bucket, max: bucket}
			windows[p.Key] = w
		}
		w.costs[bucket] += p.BucketCost
		if bucket < w.min {
			w.min = bucket
		}
		if bucket > w.max {
			w.max = bucket
		}
	}

	grouped := make(map[WorkloadKey][]float64, len(windows))
	for key, w := range windows {
		series := make([]float64, 0, (w.max-w.min)/bucketSeconds+1)
		for b := w.min; b <= w.max; b += bucketSeconds {
			series = append(series, w.costs[b])
		}
		grouped[key] = series
	}
	return grouped
}

// Package bigquery provides BigQuery integration for writing cost snapshots.
package bigquery

import "time"

// CostSnapshot is a single row in the BigQuery cost_snapshots table.
type CostSnapshot struct {
	Timestamp       time.Time `json:"timestamp"`
	ProjectID       string    `json:"project_id"`
	Region          string    `json:"region"`
	ClusterName     string    `json:"cluster_name"`
	Namespace       string    `json:"namespace"`
	Team            string    `json:"team"`
	Workload        string    `json:"workload"`
	Subtype         string    `json:"subtype"`
	PodCount        int       `json:"pod_count"`
	CPURequestVCPU  float64   `json:"cpu_request_vcpu"`
	MemoryRequestGB float64   `json:"memory_request_gb"`
	CPUCost         float64   `json:"cpu_cost"`
	MemoryCost      float64   `json:"memory_cost"`
	TotalCost       float64   `json:"total_cost"`
	IsSpot          bool      `json:"is_spot"`
	IntervalSeconds int64     `json:"interval_seconds"`

	// Utilization fields — nil when Prometheus data is not available.
	CPUUtilization    *float64 `json:"cpu_utilization,omitempty"`
	MemoryUtilization *float64 `json:"memory_utilization,omitempty"`
	EfficiencyScore   *float64 `json:"efficiency_score,omitempty"`
	WastedCost        *float64 `json:"wasted_cost,omitempty"`
}

// TableSchema returns the BigQuery table schema as a JSON-compatible structure
// suitable for the BigQuery REST API.
func TableSchema() []FieldSchema {
	return []FieldSchema{
		{Name: "timestamp", Type: "TIMESTAMP", Mode: "REQUIRED", Description: "Snapshot time"},
		{Name: "project_id", Type: "STRING", Mode: "REQUIRED", Description: "GCP project ID"},
		{Name: "region", Type: "STRING", Mode: "REQUIRED", Description: "Cluster region"},
		{Name: "cluster_name", Type: "STRING", Mode: "REQUIRED", Description: "GKE cluster name"},
		{Name: "namespace", Type: "STRING", Mode: "NULLABLE", Description: "Kubernetes namespace"},
		{Name: "team", Type: "STRING", Mode: "NULLABLE", Description: "Team label value"},
		{Name: "workload", Type: "STRING", Mode: "NULLABLE", Description: "Workload label value"},
		{Name: "subtype", Type: "STRING", Mode: "NULLABLE", Description: "Subtype label value"},
		{Name: "pod_count", Type: "INT64", Mode: "REQUIRED", Description: "Number of pods in this group"},
		{Name: "cpu_request_vcpu", Type: "FLOAT64", Mode: "REQUIRED", Description: "Total vCPU requests"},
		{Name: "memory_request_gb", Type: "FLOAT64", Mode: "REQUIRED", Description: "Total memory requests (GB)"},
		{Name: "cpu_cost", Type: "FLOAT64", Mode: "REQUIRED", Description: "CPU cost for this window ($)"},
		{Name: "memory_cost", Type: "FLOAT64", Mode: "REQUIRED", Description: "Memory cost for this window ($)"},
		{Name: "total_cost", Type: "FLOAT64", Mode: "REQUIRED", Description: "Total cost for this window ($)"},
		{Name: "is_spot", Type: "BOOL", Mode: "REQUIRED", Description: "Whether these pods are SPOT"},
		{Name: "interval_seconds", Type: "INT64", Mode: "REQUIRED", Description: "Snapshot interval in seconds"},
		{Name: "cpu_utilization", Type: "FLOAT64", Mode: "NULLABLE", Description: "Average CPU utilization ratio (actual/requested)"},
		{Name: "memory_utilization", Type: "FLOAT64", Mode: "NULLABLE", Description: "Average memory utilization ratio (actual/requested)"},
		{Name: "efficiency_score", Type: "FLOAT64", Mode: "NULLABLE", Description: "Cost-weighted utilization score (0-1)"},
		{Name: "wasted_cost", Type: "FLOAT64", Mode: "NULLABLE", Description: "Estimated wasted cost for this interval window ($)"},
	}
}

// FieldSchema represents a BigQuery table field definition.
type FieldSchema struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Mode        string `json:"mode"`
	Description string `json:"description"`
}

// TimePartitioning returns the time partitioning config for the table.
func TimePartitioning() map[string]string {
	return map[string]string{
		"type":  "DAY",
		"field": "timestamp",
	}
}

// Clustering returns the clustering fields for the table.
func Clustering() []string {
	return []string{"team", "workload"}
}

// Package parquet writes cost snapshots to local Parquet files.
package parquet

import (
	"fmt"
	"os"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
)

// Row is the Parquet representation of a cost snapshot, matching the BigQuery
// table schema column-for-column.
type Row struct {
	Timestamp         int64    `parquet:"timestamp,timestamp(microsecond)"`
	ProjectID         string   `parquet:"project_id"`
	Region            string   `parquet:"region"`
	ClusterName       string   `parquet:"cluster_name"`
	Namespace         string   `parquet:"namespace"`
	Team              string   `parquet:"team"`
	Workload          string   `parquet:"workload"`
	Subtype           string   `parquet:"subtype"`
	PodCount          int64    `parquet:"pod_count"`
	CPURequestVCPU    float64  `parquet:"cpu_request_vcpu"`
	MemoryRequestGB   float64  `parquet:"memory_request_gb"`
	CPUCost           float64  `parquet:"cpu_cost"`
	MemoryCost        float64  `parquet:"memory_cost"`
	TotalCost         float64  `parquet:"total_cost"`
	IsSpot            bool     `parquet:"is_spot"`
	IntervalSeconds   int64    `parquet:"interval_seconds"`
	CostMode          string   `parquet:"cost_mode,optional"`
	CPUUtilization    *float64 `parquet:"cpu_utilization,optional"`
	MemoryUtilization *float64 `parquet:"memory_utilization,optional"`
	EfficiencyScore   *float64 `parquet:"efficiency_score,optional"`
	WastedCost        *float64 `parquet:"wasted_cost,optional"`
}

// SnapshotToRow converts a BigQuery CostSnapshot to a Parquet Row.
func SnapshotToRow(s bigquery.CostSnapshot) Row {
	return Row{
		Timestamp:         s.Timestamp.UnixMicro(),
		ProjectID:         s.ProjectID,
		Region:            s.Region,
		ClusterName:       s.ClusterName,
		Namespace:         s.Namespace,
		Team:              s.Team,
		Workload:          s.Workload,
		Subtype:           s.Subtype,
		PodCount:          int64(s.PodCount),
		CPURequestVCPU:    s.CPURequestVCPU,
		MemoryRequestGB:   s.MemoryRequestGB,
		CPUCost:           s.CPUCost,
		MemoryCost:        s.MemoryCost,
		TotalCost:         s.TotalCost,
		IsSpot:            s.IsSpot,
		IntervalSeconds:   s.IntervalSeconds,
		CostMode:          s.CostMode,
		CPUUtilization:    s.CPUUtilization,
		MemoryUtilization: s.MemoryUtilization,
		EfficiencyScore:   s.EfficiencyScore,
		WastedCost:        s.WastedCost,
	}
}

// RowToSnapshot converts a Parquet Row back to a BigQuery CostSnapshot.
func RowToSnapshot(r Row) bigquery.CostSnapshot {
	return bigquery.CostSnapshot{
		Timestamp:         time.UnixMicro(r.Timestamp),
		ProjectID:         r.ProjectID,
		Region:            r.Region,
		ClusterName:       r.ClusterName,
		Namespace:         r.Namespace,
		Team:              r.Team,
		Workload:          r.Workload,
		Subtype:           r.Subtype,
		PodCount:          int(r.PodCount),
		CPURequestVCPU:    r.CPURequestVCPU,
		MemoryRequestGB:   r.MemoryRequestGB,
		CPUCost:           r.CPUCost,
		MemoryCost:        r.MemoryCost,
		TotalCost:         r.TotalCost,
		IsSpot:            r.IsSpot,
		IntervalSeconds:   r.IntervalSeconds,
		CostMode:          r.CostMode,
		CPUUtilization:    r.CPUUtilization,
		MemoryUtilization: r.MemoryUtilization,
		EfficiencyScore:   r.EfficiencyScore,
		WastedCost:        r.WastedCost,
	}
}

// AppendToFile appends cost snapshots to a Parquet file. If the file already
// exists its rows are preserved. If it does not exist a new file is created.
func AppendToFile(path string, snapshots []bigquery.CostSnapshot) error {
	rows, err := readExisting(path)
	if err != nil {
		return fmt.Errorf("reading existing parquet file: %w", err)
	}

	for _, s := range snapshots {
		rows = append(rows, SnapshotToRow(s))
	}

	if err := parquet.WriteFile(path, rows); err != nil {
		return fmt.Errorf("writing parquet file: %w", err)
	}
	return nil
}

// ReadFile reads all rows from an existing Parquet file and returns them as
// CostSnapshots. Returns an error if the file does not exist.
func ReadFile(path string) ([]bigquery.CostSnapshot, error) {
	rows, err := parquet.ReadFile[Row](path)
	if err != nil {
		return nil, fmt.Errorf("reading parquet file: %w", err)
	}
	snapshots := make([]bigquery.CostSnapshot, len(rows))
	for i, r := range rows {
		snapshots[i] = RowToSnapshot(r)
	}
	return snapshots, nil
}

func readExisting(path string) ([]Row, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	rows, err := parquet.ReadFile[Row](path)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

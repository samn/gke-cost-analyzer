package bigquery

import (
	"testing"
	"time"
)

func TestKeyFromRow(t *testing.T) {
	row := HistoryCostRow{
		ClusterName: "prod-cluster",
		Team:        "platform",
		Workload:    "web",
		Subtype:     "api",
		CostMode:    "autopilot",
	}
	key := KeyFromRow(row)
	if key.ClusterName != "prod-cluster" || key.Team != "platform" || key.Workload != "web" || key.Subtype != "api" || key.CostMode != "autopilot" {
		t.Errorf("KeyFromRow mismatch: %+v", key)
	}
}

func TestWorkloadKeyMapEquality(t *testing.T) {
	k1 := WorkloadKey{ClusterName: "c1", Team: "a", Workload: "b", Subtype: "c", CostMode: "autopilot"}
	k2 := WorkloadKey{ClusterName: "c1", Team: "a", Workload: "b", Subtype: "c", CostMode: "autopilot"}
	k3 := WorkloadKey{ClusterName: "c1", Team: "a", Workload: "b", Subtype: "d", CostMode: "autopilot"}
	k4 := WorkloadKey{ClusterName: "c2", Team: "a", Workload: "b", Subtype: "c", CostMode: "autopilot"}

	m := map[WorkloadKey]int{k1: 1}
	if m[k2] != 1 {
		t.Error("identical WorkloadKeys should map to same entry")
	}
	if m[k3] != 0 {
		t.Error("different WorkloadKeys (subtype) should not collide")
	}
	if m[k4] != 0 {
		t.Error("different WorkloadKeys (cluster) should not collide")
	}
}

func TestBucketSeconds(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     int64
	}{
		{"1 hour", 1 * time.Hour, 300},
		{"6 hours", 6 * time.Hour, 300},
		{"12 hours", 12 * time.Hour, 1800},
		{"1 day", 24 * time.Hour, 1800},
		{"3 days", 3 * 24 * time.Hour, 3600},
		{"1 week", 7 * 24 * time.Hour, 14400},
		{"30 days", 30 * 24 * time.Hour, 86400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BucketSeconds(tt.duration)
			if got != tt.want {
				t.Errorf("BucketSeconds(%v) = %d, want %d", tt.duration, got, tt.want)
			}
		})
	}
}

func TestBuildSparklines(t *testing.T) {
	k1 := WorkloadKey{Team: "a", Workload: "svc1"}
	k2 := WorkloadKey{Team: "b", Workload: "svc2"}
	now := time.Now()

	points := []TimeSeriesPoint{
		{Key: k1, Bucket: now, BucketCost: 1.0},
		{Key: k1, Bucket: now.Add(time.Hour), BucketCost: 2.0},
		{Key: k1, Bucket: now.Add(2 * time.Hour), BucketCost: 3.0},
		{Key: k2, Bucket: now, BucketCost: 10.0},
	}

	result := BuildSparklines(points)

	if len(result) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(result))
	}

	k1Data := result[k1]
	if len(k1Data) != 3 {
		t.Fatalf("k1: expected 3 points, got %d", len(k1Data))
	}
	if k1Data[0] != 1.0 || k1Data[1] != 2.0 || k1Data[2] != 3.0 {
		t.Errorf("k1 data = %v, want [1 2 3]", k1Data)
	}

	k2Data := result[k2]
	if len(k2Data) != 1 || k2Data[0] != 10.0 {
		t.Errorf("k2 data = %v, want [10]", k2Data)
	}
}

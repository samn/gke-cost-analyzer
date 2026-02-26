package cmd

import (
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
)

func TestAggregatedToSnapshot(t *testing.T) {
	// Set package-level flags for the conversion
	bqProject = "test-project"
	region = "us-central1"
	clusterName = "test-cluster"

	agg := cost.AggregatedCost{
		Key: cost.GroupKey{
			Team:     "platform",
			Workload: "web",
			Subtype:  "api",
			IsSpot:   false,
		},
		Namespace:    "default",
		PodCount:     3,
		TotalCPUVCPU: 1.5,
		TotalMemGB:   3.0,
		CPUCost:      0.105,
		MemCost:      0.024,
		TotalCost:    0.129,
	}

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	snap := aggregatedToSnapshot(agg, ts, 300)

	if snap.ProjectID != "test-project" {
		t.Errorf("project = %s, want test-project", snap.ProjectID)
	}
	if snap.Region != "us-central1" {
		t.Errorf("region = %s, want us-central1", snap.Region)
	}
	if snap.ClusterName != "test-cluster" {
		t.Errorf("cluster = %s, want test-cluster", snap.ClusterName)
	}
	if snap.Team != "platform" {
		t.Errorf("team = %s, want platform", snap.Team)
	}
	if snap.Workload != "web" {
		t.Errorf("workload = %s, want web", snap.Workload)
	}
	if snap.PodCount != 3 {
		t.Errorf("pod count = %d, want 3", snap.PodCount)
	}
	if snap.TotalCost != 0.129 {
		t.Errorf("total cost = %f, want 0.129", snap.TotalCost)
	}
	if snap.IntervalSeconds != 300 {
		t.Errorf("interval = %d, want 300", snap.IntervalSeconds)
	}
	if snap.IsSpot {
		t.Error("should not be spot")
	}
}

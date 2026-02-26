package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

func TestRenderTable(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
			map[string]string{"team": "platform", "app": "web"}),
		kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false,
			map[string]string{"team": "platform", "app": "web"}),
		kube.NewTestPodInfo("worker-1", "batch", 1000, 1024, startTime, true,
			map[string]string{"team": "data", "app": "etl"}),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.01},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := cost.Aggregate(costs, lc)

	output := RenderTable(aggs)

	// Verify header is present
	for _, header := range []string{"TEAM", "WORKLOAD", "SUBTYPE", "PODS", "CPU REQ", "MEM REQ", "$/HR", "SPOT"} {
		if !strings.Contains(output, header) {
			t.Errorf("missing %s header", header)
		}
	}

	// Verify team names appear
	if !strings.Contains(output, "platform") {
		t.Error("missing platform team")
	}
	if !strings.Contains(output, "data") {
		t.Error("missing data team")
	}

	// Verify SPOT column renders "yes" for spot pods
	if !strings.Contains(output, "yes") {
		t.Error("missing 'yes' in SPOT column for spot pods")
	}

	// Verify TOTAL row
	if !strings.Contains(output, "TOTAL") {
		t.Error("missing TOTAL row")
	}

	// Verify cost values are present
	if !strings.Contains(output, "$") {
		t.Error("missing dollar amounts in output")
	}
}

func TestRenderTableEmpty(t *testing.T) {
	output := RenderTable(nil)

	// Empty table with no data should still render something (headers + total)
	if !strings.Contains(output, "TOTAL") {
		t.Error("missing TOTAL for empty table")
	}
	if !strings.Contains(output, "TEAM") {
		t.Error("missing header for empty table")
	}
}

func TestRenderTableAllColumns(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "zeta", Workload: "api", Subtype: "grpc"}, PodCount: 1, CostPerHour: 0.01, TotalCPUVCPU: 2.0, TotalMemGB: 4.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api", IsSpot: true}, PodCount: 3, CostPerHour: 0.03, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
	}

	output := RenderTable(aggs)

	// Verify all teams and workloads appear
	for _, s := range []string{"zeta", "alpha", "api", "web", "grpc", "yes"} {
		if !strings.Contains(output, s) {
			t.Errorf("missing %q in output", s)
		}
	}

	// Verify total cost is the sum
	if !strings.Contains(output, "$0.0600") {
		t.Errorf("expected total $0.0600 in output, got:\n%s", output)
	}
}

func TestRenderTableMissingLabels(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{}, PodCount: 5, CostPerHour: 0.10},
	}

	output := RenderTable(aggs)

	// Missing labels should show as "-"
	if !strings.Contains(output, "-") {
		t.Error("missing '-' placeholder for empty labels")
	}
}

func TestOrDefault(t *testing.T) {
	if orDefault("hello", "-") != "hello" {
		t.Error("expected hello")
	}
	if orDefault("", "-") != "-" {
		t.Error("expected -")
	}
}

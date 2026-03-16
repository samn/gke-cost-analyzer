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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.00001},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := cost.Aggregate(costs, lc)

	output := RenderTable(aggs, false, false, false, DefaultSort())

	// Verify header is present (no SUBTYPE since showSubtype=false)
	for _, header := range []string{"TEAM", "WORKLOAD", "PODS", "CPU REQ", "MEM REQ", "$/HR", "COST", "SPOT"} {
		if !strings.Contains(output, header) {
			t.Errorf("missing %s header", header)
		}
	}
	if strings.Contains(output, "SUBTYPE") {
		t.Error("SUBTYPE header should not be present when showSubtype is false")
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
	output := RenderTable(nil, false, false, false, DefaultSort())

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
		{Key: cost.GroupKey{Team: "zeta", Workload: "api", Subtype: "grpc"}, PodCount: 1, CostPerHour: 0.01, TotalCost: 0.05, TotalCPUVCPU: 2.0, TotalMemGB: 4.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api", IsSpot: true}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
	}

	output := RenderTable(aggs, true, false, false, DefaultSort())

	// Verify SUBTYPE header is present when showSubtype=true
	if !strings.Contains(output, "SUBTYPE") {
		t.Error("missing SUBTYPE header when showSubtype is true")
	}

	// Verify all teams and workloads appear
	for _, s := range []string{"zeta", "alpha", "api", "web", "grpc", "yes"} {
		if !strings.Contains(output, s) {
			t.Errorf("missing %q in output", s)
		}
	}

	// Verify total $/HR is the sum
	if !strings.Contains(output, "$0.0600") {
		t.Errorf("expected total $/HR $0.0600 in output, got:\n%s", output)
	}

	// Verify total accumulated cost is the sum
	if !strings.Contains(output, "$0.3000") {
		t.Errorf("expected total COST $0.3000 in output, got:\n%s", output)
	}
}

func TestRenderTableMissingLabels(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{}, PodCount: 5, CostPerHour: 0.10},
	}

	output := RenderTable(aggs, true, false, false, DefaultSort())

	// Missing labels should show as "-"
	if !strings.Contains(output, "-") {
		t.Error("missing '-' placeholder for empty labels")
	}
}

func TestRenderTableSortIndicator(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1, CostPerHour: 0.01, TotalCost: 0.05},
	}

	// Sort by cost descending — only COST header should have indicator
	cfg := SortConfig{Column: SortByCost, Asc: false}
	output := RenderTable(aggs, false, false, false, cfg)

	if !strings.Contains(output, "COST v") {
		t.Errorf("expected 'COST v' indicator in output:\n%s", output)
	}
	// TEAM should NOT have an indicator
	if strings.Contains(output, "TEAM ^") || strings.Contains(output, "TEAM v") {
		t.Errorf("TEAM should not have a sort indicator:\n%s", output)
	}

	// Sort by team ascending — TEAM header should have ^ indicator
	cfg2 := SortConfig{Column: SortByTeam, Asc: true}
	output2 := RenderTable(aggs, false, false, false, cfg2)

	if !strings.Contains(output2, "TEAM ^") {
		t.Errorf("expected 'TEAM ^' indicator in output:\n%s", output2)
	}
	// COST should NOT have an indicator
	if strings.Contains(output2, "COST ^") || strings.Contains(output2, "COST v") {
		t.Errorf("COST should not have a sort indicator:\n%s", output2)
	}
}

func TestRenderTableSortIndicatorWithSubtype(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web", Subtype: "grpc"}, PodCount: 1},
	}

	cfg := SortConfig{Column: SortBySubtype, Asc: true}
	output := RenderTable(aggs, true, false, false, cfg)

	if !strings.Contains(output, "SUBTYPE ^") {
		t.Errorf("expected 'SUBTYPE ^' indicator in output:\n%s", output)
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

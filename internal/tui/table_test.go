package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

// helper to build display rows from aggs in collapsed mode (no teams expanded).
func collapsedDisplayRows(aggs []cost.AggregatedCost) []DisplayRow {
	groups := groupByTeam(aggs)
	return buildDisplayRows(groups, nil)
}

// helper to build display rows with all teams expanded.
func expandedDisplayRows(aggs []cost.AggregatedCost) []DisplayRow {
	groups := groupByTeam(aggs)
	expanded := make(map[string]bool)
	for _, g := range groups {
		expanded[g.Team] = true
	}
	return buildDisplayRows(groups, expanded)
}

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

	// Use expanded mode to see workload details like the old flat view.
	drs := expandedDisplayRows(aggs)
	output := RenderTable(drs, false, false, false, DefaultSort(), -1)

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
	output := RenderTable(nil, false, false, false, DefaultSort(), -1)

	// Empty table with no data should still render something (headers + total)
	if !strings.Contains(output, "TOTAL") {
		t.Error("missing TOTAL for empty table")
	}
	if !strings.Contains(output, "TEAM") {
		t.Error("missing header for empty table")
	}
}

func TestRenderTableCollapsedTeams(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api", IsSpot: true}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
		{Key: cost.GroupKey{Team: "zeta", Workload: "worker"}, PodCount: 1, CostPerHour: 0.01, TotalCost: 0.05, TotalCPUVCPU: 2.0, TotalMemGB: 4.0},
	}

	drs := collapsedDisplayRows(aggs)
	output := RenderTable(drs, false, false, false, DefaultSort(), -1)

	// Collapsed view should show team summary with workload count
	if !strings.Contains(output, "2 workloads") {
		t.Errorf("expected '2 workloads' for alpha team in output:\n%s", output)
	}
	if !strings.Contains(output, "1 workloads") {
		t.Errorf("expected '1 workloads' for zeta team in output:\n%s", output)
	}
	// Should show collapsed arrow
	if !strings.Contains(output, "▶") {
		t.Errorf("expected collapsed arrow ▶ in output:\n%s", output)
	}
	// Alpha team should show rolled-up pod count: 2+3=5
	if !strings.Contains(output, "5") {
		t.Errorf("expected rolled-up pod count 5 for alpha in output:\n%s", output)
	}
}

func TestRenderTableExpandedTeam(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
	}

	groups := groupByTeam(aggs)
	expanded := map[string]bool{"alpha": true}
	drs := buildDisplayRows(groups, expanded)
	output := RenderTable(drs, false, false, false, DefaultSort(), -1)

	// Expanded view should show the expanded arrow
	if !strings.Contains(output, "▼") {
		t.Errorf("expected expanded arrow ▼ in output:\n%s", output)
	}
	// Should show individual workloads
	if !strings.Contains(output, "web") {
		t.Errorf("expected workload 'web' in expanded output:\n%s", output)
	}
	if !strings.Contains(output, "api") {
		t.Errorf("expected workload 'api' in expanded output:\n%s", output)
	}
}

func TestRenderTableTotalIncludesPodsAndResources(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
	}

	drs := collapsedDisplayRows(aggs)
	output := RenderTable(drs, false, false, false, DefaultSort(), -1)

	// Total row should include pod count (5), CPU (4.00), MEM (8.0 GB)
	lines := strings.Split(output, "\n")
	var totalLine string
	for _, line := range lines {
		if strings.Contains(line, "TOTAL") {
			totalLine = line
			break
		}
	}
	if totalLine == "" {
		t.Fatal("missing TOTAL row")
	}
	// Check total pods
	if !strings.Contains(totalLine, "5") {
		t.Errorf("expected total pods 5 in TOTAL row: %s", totalLine)
	}
	// Check total CPU
	if !strings.Contains(totalLine, "4.00") {
		t.Errorf("expected total CPU 4.00 in TOTAL row: %s", totalLine)
	}
	// Check total MEM
	if !strings.Contains(totalLine, "8.0 GB") {
		t.Errorf("expected total MEM 8.0 GB in TOTAL row: %s", totalLine)
	}
	// Check total $/HR
	if !strings.Contains(totalLine, "$0.0500") {
		t.Errorf("expected total $/HR $0.0500 in TOTAL row: %s", totalLine)
	}
	// Check total COST
	if !strings.Contains(totalLine, "$0.2500") {
		t.Errorf("expected total COST $0.2500 in TOTAL row: %s", totalLine)
	}
}

func TestRenderTableAllColumns(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "zeta", Workload: "api", Subtype: "grpc"}, PodCount: 1, CostPerHour: 0.01, TotalCost: 0.05, TotalCPUVCPU: 2.0, TotalMemGB: 4.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api", IsSpot: true}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
	}

	// Use expanded mode to see all workloads.
	drs := expandedDisplayRows(aggs)
	output := RenderTable(drs, true, false, false, DefaultSort(), -1)

	// Verify SUBTYPE header is present when showSubtype=true
	if !strings.Contains(output, "SUBTYPE") {
		t.Error("missing SUBTYPE header when showSubtype is true")
	}

	// Verify teams and workloads appear
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

	drs := expandedDisplayRows(aggs)
	output := RenderTable(drs, true, false, false, DefaultSort(), -1)

	// Missing labels should show as "-"
	if !strings.Contains(output, "-") {
		t.Error("missing '-' placeholder for empty labels")
	}
}

func TestRenderTableSortIndicator(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1, CostPerHour: 0.01, TotalCost: 0.05},
	}

	drs := collapsedDisplayRows(aggs)

	// Sort by cost descending — only COST header should have indicator
	cfg := SortConfig{Column: SortByCost, Asc: false}
	output := RenderTable(drs, false, false, false, cfg, -1)

	if !strings.Contains(output, "COST v") {
		t.Errorf("expected 'COST v' indicator in output:\n%s", output)
	}
	// TEAM should NOT have an indicator
	if strings.Contains(output, "TEAM ^") || strings.Contains(output, "TEAM v") {
		t.Errorf("TEAM should not have a sort indicator:\n%s", output)
	}

	// Sort by team ascending — TEAM header should have ^ indicator
	cfg2 := SortConfig{Column: SortByTeam, Asc: true}
	output2 := RenderTable(drs, false, false, false, cfg2, -1)

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

	drs := collapsedDisplayRows(aggs)
	cfg := SortConfig{Column: SortBySubtype, Asc: true}
	output := RenderTable(drs, true, false, false, cfg, -1)

	if !strings.Contains(output, "SUBTYPE ^") {
		t.Errorf("expected 'SUBTYPE ^' indicator in output:\n%s", output)
	}
}

func TestRenderTableCursorHighlight(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 2},
	}

	drs := collapsedDisplayRows(aggs)
	// Cursor at row 0 - this should render without errors.
	output := RenderTable(drs, false, false, false, DefaultSort(), 0)
	if !strings.Contains(output, "alpha") {
		t.Errorf("expected alpha in output:\n%s", output)
	}
}

func TestBuildDisplayRowsCollapsed(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3},
		{Key: cost.GroupKey{Team: "beta", Workload: "worker"}, PodCount: 1},
	}

	groups := groupByTeam(aggs)
	drs := buildDisplayRows(groups, nil)

	// Should have 2 team summary rows (collapsed).
	if len(drs) != 2 {
		t.Fatalf("expected 2 display rows, got %d", len(drs))
	}
	if drs[0].Kind != rowTeamSummary || drs[0].TeamName != "alpha" {
		t.Errorf("expected alpha team summary, got %+v", drs[0])
	}
	if drs[0].WorkloadCount != 2 {
		t.Errorf("expected 2 workloads for alpha, got %d", drs[0].WorkloadCount)
	}
	if drs[0].Expanded {
		t.Error("expected alpha to be collapsed")
	}
	if drs[1].Kind != rowTeamSummary || drs[1].TeamName != "beta" {
		t.Errorf("expected beta team summary, got %+v", drs[1])
	}
}

func TestBuildDisplayRowsExpanded(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3},
		{Key: cost.GroupKey{Team: "beta", Workload: "worker"}, PodCount: 1},
	}

	groups := groupByTeam(aggs)
	expanded := map[string]bool{"alpha": true}
	drs := buildDisplayRows(groups, expanded)

	// alpha expanded: 1 team header + 2 workloads = 3
	// beta collapsed: 1 team header = 1
	// Total: 4
	if len(drs) != 4 {
		t.Fatalf("expected 4 display rows, got %d", len(drs))
	}
	if drs[0].Kind != rowTeamSummary || !drs[0].Expanded {
		t.Error("expected alpha team summary expanded")
	}
	if drs[1].Kind != rowWorkloadDetail || drs[1].Agg.Key.Workload != "web" {
		t.Errorf("expected web workload detail, got %+v", drs[1])
	}
	if drs[2].Kind != rowWorkloadDetail || drs[2].Agg.Key.Workload != "api" {
		t.Errorf("expected api workload detail, got %+v", drs[2])
	}
	if drs[3].Kind != rowTeamSummary || drs[3].Expanded {
		t.Error("expected beta team summary collapsed")
	}
}

func TestRenderTableSeparatorRow(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 2},
	}

	drs := collapsedDisplayRows(aggs)
	output := RenderTable(drs, false, false, false, DefaultSort(), -1)

	// Separator row should contain horizontal line characters between data and TOTAL.
	if !strings.Contains(output, "─") {
		t.Errorf("expected separator line with ─ characters in output:\n%s", output)
	}

	// Verify TOTAL appears after separator.
	sepIdx := strings.Index(output, "──")
	totalIdx := strings.Index(output, "TOTAL")
	if sepIdx < 0 || totalIdx < 0 {
		t.Fatalf("expected both separator and TOTAL in output:\n%s", output)
	}
	if sepIdx > totalIdx {
		t.Errorf("expected separator before TOTAL, sep@%d total@%d", sepIdx, totalIdx)
	}
}

func TestRenderTableCursorOnLastRow(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "gamma", Workload: "worker"}, PodCount: 3},
	}

	drs := collapsedDisplayRows(aggs)
	lastIdx := len(drs) - 1

	// Cursor on last row should render without issues.
	output := RenderTable(drs, false, false, false, DefaultSort(), lastIdx)
	if !strings.Contains(output, "gamma") {
		t.Errorf("expected gamma in output:\n%s", output)
	}
	if !strings.Contains(output, "TOTAL") {
		t.Errorf("expected TOTAL in output:\n%s", output)
	}
}

func TestRenderTableFlatMode(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10, TotalCPUVCPU: 1.0, TotalMemGB: 2.0},
		{Key: cost.GroupKey{Team: "beta", Workload: "api", IsSpot: true}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15, TotalCPUVCPU: 3.0, TotalMemGB: 6.0},
	}

	drs := buildFlatDisplayRows(aggs)
	output := RenderTable(drs, false, false, false, DefaultSort(), -1)

	// Flat mode should show both team and workload for each row
	if !strings.Contains(output, "alpha") {
		t.Errorf("expected team 'alpha' in flat output:\n%s", output)
	}
	if !strings.Contains(output, "web") {
		t.Errorf("expected workload 'web' in flat output:\n%s", output)
	}
	if !strings.Contains(output, "beta") {
		t.Errorf("expected team 'beta' in flat output:\n%s", output)
	}
	if !strings.Contains(output, "yes") {
		t.Errorf("expected 'yes' for spot in flat output:\n%s", output)
	}

	// Total should include all rows
	lines := strings.Split(output, "\n")
	var totalLine string
	for _, line := range lines {
		if strings.Contains(line, "TOTAL") {
			totalLine = line
			break
		}
	}
	if !strings.Contains(totalLine, "5") {
		t.Errorf("expected total pods 5 in TOTAL row: %s", totalLine)
	}
	if !strings.Contains(totalLine, "$0.0500") {
		t.Errorf("expected total $/HR $0.0500 in TOTAL row: %s", totalLine)
	}
}

func TestBuildFlatDisplayRows(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 3},
	}

	drs := buildFlatDisplayRows(aggs)
	if len(drs) != 2 {
		t.Fatalf("expected 2 flat display rows, got %d", len(drs))
	}
	for i, dr := range drs {
		if dr.Kind != rowFlat {
			t.Errorf("expected rowFlat at index %d, got %v", i, dr.Kind)
		}
	}
	if drs[0].Agg.Key.Team != "alpha" {
		t.Errorf("expected alpha, got %s", drs[0].Agg.Key.Team)
	}
	if drs[1].Agg.Key.Team != "beta" {
		t.Errorf("expected beta, got %s", drs[1].Agg.Key.Team)
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

package tui

import (
	"testing"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
)

func TestDefaultSort(t *testing.T) {
	cfg := DefaultSort()
	if cfg.Column != SortByTeam {
		t.Errorf("expected SortByTeam, got %v", cfg.Column)
	}
	if !cfg.Asc {
		t.Error("expected ascending by default")
	}
}

func testAggs() []cost.AggregatedCost {
	return []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "beta", Workload: "api", Subtype: "grpc"}, PodCount: 3, TotalCPUVCPU: 2.0, TotalMemGB: 4.0, CostPerHour: 0.03, TotalCost: 0.15},
		{Key: cost.GroupKey{Team: "alpha", Workload: "web", Subtype: "http"}, PodCount: 1, TotalCPUVCPU: 0.5, TotalMemGB: 1.0, CostPerHour: 0.01, TotalCost: 0.05},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api", Subtype: "rest"}, PodCount: 5, TotalCPUVCPU: 4.0, TotalMemGB: 8.0, CostPerHour: 0.05, TotalCost: 0.25},
	}
}

func TestSortByTeamAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByTeam, Asc: true})

	if aggs[0].Key.Team != "alpha" || aggs[0].Key.Workload != "api" {
		t.Errorf("expected alpha/api first, got %s/%s", aggs[0].Key.Team, aggs[0].Key.Workload)
	}
	if aggs[1].Key.Team != "alpha" || aggs[1].Key.Workload != "web" {
		t.Errorf("expected alpha/web second, got %s/%s", aggs[1].Key.Team, aggs[1].Key.Workload)
	}
	if aggs[2].Key.Team != "beta" {
		t.Errorf("expected beta third, got %s", aggs[2].Key.Team)
	}
}

func TestSortByTeamDesc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByTeam, Asc: false})

	if aggs[0].Key.Team != "beta" {
		t.Errorf("expected beta first, got %s", aggs[0].Key.Team)
	}
	if aggs[1].Key.Team != "alpha" || aggs[1].Key.Workload != "api" {
		t.Errorf("expected alpha/api second, got %s/%s", aggs[1].Key.Team, aggs[1].Key.Workload)
	}
}

func TestSortByWorkloadAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByWorkload, Asc: true})

	if aggs[0].Key.Workload != "api" {
		t.Errorf("expected api first, got %s", aggs[0].Key.Workload)
	}
	// Among the two "api" entries, secondary sort by team: alpha < beta
	if aggs[0].Key.Team != "alpha" {
		t.Errorf("expected alpha team for first api, got %s", aggs[0].Key.Team)
	}
	if aggs[1].Key.Workload != "api" || aggs[1].Key.Team != "beta" {
		t.Errorf("expected beta/api second, got %s/%s", aggs[1].Key.Team, aggs[1].Key.Workload)
	}
	if aggs[2].Key.Workload != "web" {
		t.Errorf("expected web third, got %s", aggs[2].Key.Workload)
	}
}

func TestSortByWorkloadDesc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByWorkload, Asc: false})

	if aggs[0].Key.Workload != "web" {
		t.Errorf("expected web first, got %s", aggs[0].Key.Workload)
	}
}

func TestSortBySubtypeAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortBySubtype, Asc: true})

	if aggs[0].Key.Subtype != "grpc" {
		t.Errorf("expected grpc first, got %s", aggs[0].Key.Subtype)
	}
	if aggs[1].Key.Subtype != "http" {
		t.Errorf("expected http second, got %s", aggs[1].Key.Subtype)
	}
	if aggs[2].Key.Subtype != "rest" {
		t.Errorf("expected rest third, got %s", aggs[2].Key.Subtype)
	}
}

func TestSortByPodsAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByPods, Asc: true})

	if aggs[0].PodCount != 1 {
		t.Errorf("expected 1 pod first, got %d", aggs[0].PodCount)
	}
	if aggs[1].PodCount != 3 {
		t.Errorf("expected 3 pods second, got %d", aggs[1].PodCount)
	}
	if aggs[2].PodCount != 5 {
		t.Errorf("expected 5 pods third, got %d", aggs[2].PodCount)
	}
}

func TestSortByPodsDesc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByPods, Asc: false})

	if aggs[0].PodCount != 5 {
		t.Errorf("expected 5 pods first, got %d", aggs[0].PodCount)
	}
	if aggs[2].PodCount != 1 {
		t.Errorf("expected 1 pod last, got %d", aggs[2].PodCount)
	}
}

func TestSortByCPUAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByCPU, Asc: true})

	if aggs[0].TotalCPUVCPU != 0.5 {
		t.Errorf("expected 0.5 CPU first, got %f", aggs[0].TotalCPUVCPU)
	}
	if aggs[2].TotalCPUVCPU != 4.0 {
		t.Errorf("expected 4.0 CPU last, got %f", aggs[2].TotalCPUVCPU)
	}
}

func TestSortByMemAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByMem, Asc: true})

	if aggs[0].TotalMemGB != 1.0 {
		t.Errorf("expected 1.0 GB first, got %f", aggs[0].TotalMemGB)
	}
	if aggs[2].TotalMemGB != 8.0 {
		t.Errorf("expected 8.0 GB last, got %f", aggs[2].TotalMemGB)
	}
}

func TestSortByCostPerHourAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByCostPerHour, Asc: true})

	if aggs[0].CostPerHour != 0.01 {
		t.Errorf("expected $0.01/hr first, got %f", aggs[0].CostPerHour)
	}
	if aggs[2].CostPerHour != 0.05 {
		t.Errorf("expected $0.05/hr last, got %f", aggs[2].CostPerHour)
	}
}

func TestSortByCostPerHourDesc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByCostPerHour, Asc: false})

	if aggs[0].CostPerHour != 0.05 {
		t.Errorf("expected $0.05/hr first, got %f", aggs[0].CostPerHour)
	}
	if aggs[2].CostPerHour != 0.01 {
		t.Errorf("expected $0.01/hr last, got %f", aggs[2].CostPerHour)
	}
}

func TestSortByCostAsc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByCost, Asc: true})

	if aggs[0].TotalCost != 0.05 {
		t.Errorf("expected $0.05 first, got %f", aggs[0].TotalCost)
	}
	if aggs[2].TotalCost != 0.25 {
		t.Errorf("expected $0.25 last, got %f", aggs[2].TotalCost)
	}
}

func TestSortByCostDesc(t *testing.T) {
	aggs := testAggs()
	SortAggs(aggs, SortConfig{Column: SortByCost, Asc: false})

	if aggs[0].TotalCost != 0.25 {
		t.Errorf("expected $0.25 first, got %f", aggs[0].TotalCost)
	}
	if aggs[2].TotalCost != 0.05 {
		t.Errorf("expected $0.05 last, got %f", aggs[2].TotalCost)
	}
}

func TestSortSecondaryBreaksTies(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "beta", Workload: "z-svc"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "b-svc"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "a-svc"}, PodCount: 2},
	}
	SortAggs(aggs, SortConfig{Column: SortByPods, Asc: true})

	// All have the same pod count, so secondary sort by team → workload
	if aggs[0].Key.Team != "alpha" || aggs[0].Key.Workload != "a-svc" {
		t.Errorf("expected alpha/a-svc first, got %s/%s", aggs[0].Key.Team, aggs[0].Key.Workload)
	}
	if aggs[1].Key.Team != "alpha" || aggs[1].Key.Workload != "b-svc" {
		t.Errorf("expected alpha/b-svc second, got %s/%s", aggs[1].Key.Team, aggs[1].Key.Workload)
	}
	if aggs[2].Key.Team != "beta" {
		t.Errorf("expected beta third, got %s", aggs[2].Key.Team)
	}
}

func TestColumnForKeyWithSubtype(t *testing.T) {
	tests := []struct {
		key  rune
		want SortColumn
	}{
		{'1', SortByTeam},
		{'2', SortByWorkload},
		{'3', SortBySubtype},
		{'4', SortByPods},
		{'5', SortByCPU},
		{'6', SortByMem},
		{'7', SortByCostPerHour},
		{'8', SortByCost},
	}
	for _, tt := range tests {
		col, ok := ColumnForKey(tt.key, true, false, false)
		if !ok {
			t.Errorf("key %c should be valid with subtype", tt.key)
		}
		if col != tt.want {
			t.Errorf("key %c: expected %v, got %v", tt.key, tt.want, col)
		}
	}

	// Invalid key
	_, ok := ColumnForKey('9', true, false, false)
	if ok {
		t.Error("key 9 should be invalid with subtype")
	}
}

func TestGroupByTeam(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, TotalCPUVCPU: 1.0, TotalMemGB: 2.0, CostPerHour: 0.02, TotalCost: 0.10},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3, TotalCPUVCPU: 3.0, TotalMemGB: 6.0, CostPerHour: 0.03, TotalCost: 0.15},
		{Key: cost.GroupKey{Team: "beta", Workload: "worker"}, PodCount: 1, TotalCPUVCPU: 2.0, TotalMemGB: 4.0, CostPerHour: 0.01, TotalCost: 0.05},
	}

	groups := groupByTeam(aggs)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	alpha := groups[0]
	if alpha.Team != "alpha" {
		t.Errorf("expected first group alpha, got %s", alpha.Team)
	}
	if len(alpha.Workloads) != 2 {
		t.Errorf("expected 2 workloads in alpha, got %d", len(alpha.Workloads))
	}
	if alpha.Summary.PodCount != 5 {
		t.Errorf("expected alpha summary pods=5, got %d", alpha.Summary.PodCount)
	}
	if alpha.Summary.TotalCPUVCPU != 4.0 {
		t.Errorf("expected alpha summary CPU=4.0, got %f", alpha.Summary.TotalCPUVCPU)
	}
	if alpha.Summary.CostPerHour != 0.05 {
		t.Errorf("expected alpha summary $/HR=0.05, got %f", alpha.Summary.CostPerHour)
	}

	beta := groups[1]
	if beta.Team != "beta" {
		t.Errorf("expected second group beta, got %s", beta.Team)
	}
	if beta.Summary.PodCount != 1 {
		t.Errorf("expected beta summary pods=1, got %d", beta.Summary.PodCount)
	}
}

func TestGroupByTeamEmptyTeam(t *testing.T) {
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "", Workload: "orphan"}, PodCount: 1},
	}

	groups := groupByTeam(aggs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Team != "-" {
		t.Errorf("expected team '-' for empty, got %s", groups[0].Team)
	}
}

func TestSortTeamGroups(t *testing.T) {
	groups := []TeamGroup{
		{Team: "beta", Summary: cost.AggregatedCost{CostPerHour: 0.01}},
		{Team: "alpha", Summary: cost.AggregatedCost{CostPerHour: 0.05}},
		{Team: "gamma", Summary: cost.AggregatedCost{CostPerHour: 0.03}},
	}

	// Sort by cost per hour descending
	SortTeamGroups(groups, SortConfig{Column: SortByCostPerHour, Asc: false})

	if groups[0].Team != "alpha" {
		t.Errorf("expected alpha first (highest cost), got %s", groups[0].Team)
	}
	if groups[1].Team != "gamma" {
		t.Errorf("expected gamma second, got %s", groups[1].Team)
	}
	if groups[2].Team != "beta" {
		t.Errorf("expected beta last, got %s", groups[2].Team)
	}
}

func TestSortTeamGroupsByTeamAsc(t *testing.T) {
	groups := []TeamGroup{
		{Team: "gamma", Summary: cost.AggregatedCost{Key: cost.GroupKey{Team: "gamma"}}},
		{Team: "alpha", Summary: cost.AggregatedCost{Key: cost.GroupKey{Team: "alpha"}}},
		{Team: "beta", Summary: cost.AggregatedCost{Key: cost.GroupKey{Team: "beta"}}},
	}

	SortTeamGroups(groups, SortConfig{Column: SortByTeam, Asc: true})

	if groups[0].Team != "alpha" {
		t.Errorf("expected alpha first, got %s", groups[0].Team)
	}
	if groups[1].Team != "beta" {
		t.Errorf("expected beta second, got %s", groups[1].Team)
	}
	if groups[2].Team != "gamma" {
		t.Errorf("expected gamma third, got %s", groups[2].Team)
	}
}

func TestSortTeamGroupsSortsWorkloads(t *testing.T) {
	groups := []TeamGroup{
		{
			Team:    "alpha",
			Summary: cost.AggregatedCost{Key: cost.GroupKey{Team: "alpha"}},
			Workloads: []cost.AggregatedCost{
				{Key: cost.GroupKey{Team: "alpha", Workload: "z-svc"}, CostPerHour: 0.01},
				{Key: cost.GroupKey{Team: "alpha", Workload: "a-svc"}, CostPerHour: 0.05},
			},
		},
	}

	SortTeamGroups(groups, SortConfig{Column: SortByCostPerHour, Asc: false})

	// Workloads should be sorted by cost descending
	if groups[0].Workloads[0].Key.Workload != "a-svc" {
		t.Errorf("expected a-svc first (highest cost), got %s", groups[0].Workloads[0].Key.Workload)
	}
	if groups[0].Workloads[1].Key.Workload != "z-svc" {
		t.Errorf("expected z-svc second, got %s", groups[0].Workloads[1].Key.Workload)
	}
}

func TestColumnForKeyWithoutSubtype(t *testing.T) {
	tests := []struct {
		key  rune
		want SortColumn
	}{
		{'1', SortByTeam},
		{'2', SortByWorkload},
		{'3', SortByPods},
		{'4', SortByCPU},
		{'5', SortByMem},
		{'6', SortByCostPerHour},
		{'7', SortByCost},
	}
	for _, tt := range tests {
		col, ok := ColumnForKey(tt.key, false, false, false)
		if !ok {
			t.Errorf("key %c should be valid without subtype", tt.key)
		}
		if col != tt.want {
			t.Errorf("key %c: expected %v, got %v", tt.key, tt.want, col)
		}
	}

	// Key 8 is invalid without subtype
	_, ok := ColumnForKey('8', false, false, false)
	if ok {
		t.Error("key 8 should be invalid without subtype")
	}
}

package tui

import (
	"testing"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
)

func TestSortHistoryRowsByTotalCost(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{Team: "a", Workload: "svc1", TotalCost: 5.0},
		{Team: "b", Workload: "svc2", TotalCost: 15.0},
		{Team: "c", Workload: "svc3", TotalCost: 10.0},
	}

	SortHistoryRows(rows, HistorySortConfig{Column: HistSortByTotalCost, Asc: false})
	if rows[0].TotalCost != 15.0 || rows[1].TotalCost != 10.0 || rows[2].TotalCost != 5.0 {
		t.Errorf("descending sort failed: %v", rows)
	}

	SortHistoryRows(rows, HistorySortConfig{Column: HistSortByTotalCost, Asc: true})
	if rows[0].TotalCost != 5.0 || rows[1].TotalCost != 10.0 || rows[2].TotalCost != 15.0 {
		t.Errorf("ascending sort failed: %v", rows)
	}
}

func TestSortHistoryRowsByTeam(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{Team: "charlie", Workload: "x"},
		{Team: "alpha", Workload: "x"},
		{Team: "bravo", Workload: "x"},
	}

	SortHistoryRows(rows, HistorySortConfig{Column: HistSortByTeam, Asc: true})
	if rows[0].Team != "alpha" || rows[1].Team != "bravo" || rows[2].Team != "charlie" {
		t.Errorf("team sort failed: %v", rows)
	}
}

func TestSortHistoryRowsSecondarySortTiebreaker(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{Team: "a", Workload: "z", TotalCost: 10.0},
		{Team: "a", Workload: "a", TotalCost: 10.0},
	}

	SortHistoryRows(rows, HistorySortConfig{Column: HistSortByTotalCost, Asc: false})
	if rows[0].Workload != "a" || rows[1].Workload != "z" {
		t.Errorf("secondary sort by workload failed: %v", rows)
	}
}

func TestGroupHistoryByTeam(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{Team: "alpha", Workload: "svc1", TotalCost: 5.0, AvgPods: 2},
		{Team: "alpha", Workload: "svc2", TotalCost: 3.0, AvgPods: 1},
		{Team: "beta", Workload: "svc3", TotalCost: 10.0, AvgPods: 4},
	}

	groups := GroupHistoryByTeam(rows)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	alpha := groups[0]
	if alpha.Team != "alpha" {
		t.Errorf("first group = %s, want alpha", alpha.Team)
	}
	if len(alpha.Workloads) != 2 {
		t.Errorf("alpha workloads = %d, want 2", len(alpha.Workloads))
	}
	if alpha.Summary.TotalCost != 8.0 {
		t.Errorf("alpha total = %f, want 8.0", alpha.Summary.TotalCost)
	}
	if alpha.Summary.AvgPods != 3 {
		t.Errorf("alpha avg_pods = %f, want 3", alpha.Summary.AvgPods)
	}

	beta := groups[1]
	if beta.Summary.TotalCost != 10.0 {
		t.Errorf("beta total = %f, want 10.0", beta.Summary.TotalCost)
	}
}

func TestGroupHistoryByTeamEmptyTeam(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{Team: "", Workload: "orphan", TotalCost: 1.0},
	}

	groups := GroupHistoryByTeam(rows)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Team != "-" {
		t.Errorf("empty team should become '-', got %q", groups[0].Team)
	}
}

func TestSortHistoryTeamGroups(t *testing.T) {
	groups := []HistoryTeamGroup{
		{Team: "a", Summary: bigquery.HistoryCostRow{TotalCost: 5}},
		{Team: "b", Summary: bigquery.HistoryCostRow{TotalCost: 15}},
		{Team: "c", Summary: bigquery.HistoryCostRow{TotalCost: 10}},
	}

	SortHistoryTeamGroups(groups, HistorySortConfig{Column: HistSortByTotalCost, Asc: false})
	if groups[0].Team != "b" || groups[1].Team != "c" || groups[2].Team != "a" {
		t.Errorf("team group sort failed: %s %s %s", groups[0].Team, groups[1].Team, groups[2].Team)
	}
}

func TestHistoryColumnForKey(t *testing.T) {
	// Default columns: TEAM(1), WORKLOAD(2), PODS(3), CPU(4), MEM(5), $/HR(6), TOTAL(7), WASTE not shown
	col, ok := HistoryColumnForKey('1', false, false, false, false)
	if !ok || col != HistSortByTeam {
		t.Errorf("key '1' should map to team, got %d ok=%v", col, ok)
	}

	col, ok = HistoryColumnForKey('7', false, false, false, false)
	if !ok || col != HistSortByTotalCost {
		t.Errorf("key '7' should map to total cost, got %d ok=%v", col, ok)
	}

	// Invalid key
	_, ok = HistoryColumnForKey('9', false, false, false, false)
	if ok {
		t.Error("key '9' should be out of range without utilization columns")
	}
}

func TestDefaultHistorySort(t *testing.T) {
	cfg := DefaultHistorySort()
	if cfg.Column != HistSortByTotalCost {
		t.Errorf("default column = %d, want TotalCost", cfg.Column)
	}
	if cfg.Asc {
		t.Error("default should be descending")
	}
}

func TestHistorySortIndicator(t *testing.T) {
	cfg := HistorySortConfig{Column: HistSortByTotalCost, Asc: false}

	got := historySortIndicator("TOTAL", HistSortByTotalCost, true, cfg)
	if got != "TOTAL v" {
		t.Errorf("got %q, want %q", got, "TOTAL v")
	}

	got = historySortIndicator("TEAM", HistSortByTeam, true, cfg)
	if got != "TEAM" {
		t.Errorf("non-active column should have no indicator, got %q", got)
	}

	got = historySortIndicator("TREND", HistSortByTeam, false, cfg)
	if got != "TREND" {
		t.Errorf("non-sortable should have no indicator, got %q", got)
	}
}

func TestHistoryVisibleColumnsDefault(t *testing.T) {
	cols := historyVisibleColumns(ColumnVisibility{})
	var headers []string
	for _, c := range cols {
		headers = append(headers, c.header)
	}

	expected := []string{"TEAM", "WORKLOAD", "AVG PODS", "AVG CPU", "AVG MEM", "AVG $/HR", "TOTAL", "TREND", "SPOT"}
	if len(headers) != len(expected) {
		t.Fatalf("columns = %v, want %v", headers, expected)
	}
	for i, h := range headers {
		if h != expected[i] {
			t.Errorf("column %d = %s, want %s", i, h, expected[i])
		}
	}
}

func TestHistoryVisibleColumnsWithCluster(t *testing.T) {
	cols := historyVisibleColumns(ColumnVisibility{Cluster: true})
	if cols[0].header != "CLUSTER" {
		t.Errorf("first column should be CLUSTER, got %s", cols[0].header)
	}
	if cols[1].header != "TEAM" {
		t.Errorf("second column should be TEAM, got %s", cols[1].header)
	}
}

func TestSortHistoryRowsByCluster(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{ClusterName: "z-cluster", Team: "a", Workload: "svc1", TotalCost: 5.0},
		{ClusterName: "a-cluster", Team: "a", Workload: "svc2", TotalCost: 10.0},
		{ClusterName: "m-cluster", Team: "a", Workload: "svc3", TotalCost: 3.0},
	}

	SortHistoryRows(rows, HistorySortConfig{Column: HistSortByCluster, Asc: true})
	if rows[0].ClusterName != "a-cluster" || rows[1].ClusterName != "m-cluster" || rows[2].ClusterName != "z-cluster" {
		t.Errorf("cluster sort failed: %s %s %s", rows[0].ClusterName, rows[1].ClusterName, rows[2].ClusterName)
	}
}

func TestHistoryColumnForKeyWithCluster(t *testing.T) {
	// With cluster visible: CLUSTER(1), TEAM(2), WORKLOAD(3), ...
	col, ok := HistoryColumnForKey('1', true, false, false, false)
	if !ok || col != HistSortByCluster {
		t.Errorf("key '1' with cluster should map to cluster, got %d ok=%v", col, ok)
	}

	col, ok = HistoryColumnForKey('2', true, false, false, false)
	if !ok || col != HistSortByTeam {
		t.Errorf("key '2' with cluster should map to team, got %d ok=%v", col, ok)
	}
}

func TestHistoryVisibleColumnsWithUtilization(t *testing.T) {
	cols := historyVisibleColumns(ColumnVisibility{Utilization: true})
	// Should include CPU%, MEM%, WASTE at the end
	last3 := cols[len(cols)-3:]
	if last3[0].header != "CPU%" || last3[1].header != "MEM%" || last3[2].header != "WASTE" {
		t.Errorf("utilization columns = %s %s %s", last3[0].header, last3[1].header, last3[2].header)
	}
}

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
)

type mockFetcher struct {
	rows   []bigquery.HistoryCostRow
	series []bigquery.TimeSeriesPoint
	err    error
}

func (m *mockFetcher) QueryAggregatedCosts(_ context.Context, _ time.Time, _ bigquery.QueryFilters) ([]bigquery.HistoryCostRow, error) {
	return m.rows, m.err
}

func (m *mockFetcher) QueryTimeSeries(_ context.Context, _ time.Time, _ int64, _ bigquery.QueryFilters) ([]bigquery.TimeSeriesPoint, error) {
	return m.series, m.err
}

func testHistoryModel(fetcher HistoryDataFetcher) HistoryModel {
	ctx, cancel := context.WithCancel(context.Background())
	return NewHistoryModel(ctx, cancel, fetcher, 3*24*time.Hour, 3600, bigquery.QueryFilters{}, ColumnVisibility{})
}

func TestHistoryModelInitialView(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	view := m.View().Content
	if !strings.Contains(view, "Querying") {
		t.Errorf("initial view should show loading, got: %s", view)
	}
}

func TestHistoryModelDataUpdate(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})

	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "platform", Workload: "web", TotalCost: 12.50, AvgCostPerHour: 0.52, AvgPods: 3},
			{Team: "data", Workload: "etl", TotalCost: 5.00, AvgCostPerHour: 0.20, AvgPods: 1},
		},
		series: []bigquery.TimeSeriesPoint{
			{Key: bigquery.WorkloadKey{Team: "platform", Workload: "web"}, BucketCost: 1.0},
			{Key: bigquery.WorkloadKey{Team: "platform", Workload: "web"}, BucketCost: 2.0},
		},
	}

	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	if hm.loading {
		t.Error("should not be loading after data")
	}
	if hm.workloadCount != 2 {
		t.Errorf("workloadCount = %d, want 2", hm.workloadCount)
	}
	if hm.totalCost != 17.50 {
		t.Errorf("totalCost = %f, want 17.50", hm.totalCost)
	}

	view := hm.View().Content
	if strings.Contains(view, "Querying") {
		t.Error("view should not show loading after data")
	}
	if !strings.Contains(view, "History") {
		t.Error("view should contain History header")
	}
	if !strings.Contains(view, "$17.50") {
		t.Errorf("view should contain total cost, got:\n%s", view)
	}
	if !strings.Contains(view, "platform") {
		t.Error("view should contain team name")
	}
}

func TestHistoryModelErrorUpdate(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})

	msg := historyErrMsg{err: errTest}
	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	if hm.loading {
		t.Error("should not be loading after error")
	}
	view := hm.View().Content
	if !strings.Contains(view, "Error") {
		t.Errorf("view should show error, got: %s", view)
	}
}

var errTest = fmt.Errorf("test error")

func TestHistoryModelEmptyResults(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})

	msg := historyDataMsg{rows: nil, series: nil}
	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	view := hm.View().Content
	if !strings.Contains(view, "No cost data") {
		t.Errorf("empty results should show no data message, got: %s", view)
	}
}

func TestHistoryModelQuit(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("'q' should return a quit command")
	}
}

func TestHistoryModelSortToggle(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	// Load data first
	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "a", Workload: "svc1", TotalCost: 5},
			{Team: "b", Workload: "svc2", TotalCost: 10},
		},
	}
	updated, _ := m.Update(msg)
	m = updated.(HistoryModel)

	// Press '1' to sort by team
	updated, _ = m.Update(tea.KeyPressMsg{Code: '1'})
	m = updated.(HistoryModel)
	if m.sortCfg.Column != HistSortByTeam {
		t.Errorf("sort column should be team, got %d", m.sortCfg.Column)
	}
	if !m.sortCfg.Asc {
		t.Error("first press should be ascending")
	}

	// Press '1' again to toggle direction
	updated, _ = m.Update(tea.KeyPressMsg{Code: '1'})
	m = updated.(HistoryModel)
	if m.sortCfg.Asc {
		t.Error("second press should be descending")
	}
}

func TestHistoryModelCursorNavigation(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "a", Workload: "svc1"},
			{Team: "b", Workload: "svc2"},
			{Team: "c", Workload: "svc3"},
		},
	}
	updated, _ := m.Update(msg)
	m = updated.(HistoryModel)

	// Navigate down
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(HistoryModel)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	// Navigate up
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(HistoryModel)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}

	// Navigate up at top (should stay at 0)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(HistoryModel)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped)", m.cursor)
	}
}

func TestHistoryModelGroupedFlatToggle(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "a", Workload: "svc1"},
		},
	}
	updated, _ := m.Update(msg)
	m = updated.(HistoryModel)

	if !m.grouped {
		t.Error("should start grouped")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g'})
	m = updated.(HistoryModel)
	if m.grouped {
		t.Error("should be flat after pressing 'g'")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g'})
	m = updated.(HistoryModel)
	if !m.grouped {
		t.Error("should be grouped after pressing 'g' again")
	}
}

func TestHistoryModelExpandCollapse(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "alpha", Workload: "svc1", TotalCost: 5},
			{Team: "alpha", Workload: "svc2", TotalCost: 3},
		},
	}
	updated, _ := m.Update(msg)
	m = updated.(HistoryModel)

	// Initially 1 row (collapsed team)
	if len(m.displayRows) != 1 {
		t.Fatalf("collapsed: expected 1 row, got %d", len(m.displayRows))
	}

	// Expand
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(HistoryModel)
	if len(m.displayRows) != 3 {
		t.Fatalf("expanded: expected 3 rows, got %d", len(m.displayRows))
	}

	// Collapse
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(HistoryModel)
	if len(m.displayRows) != 1 {
		t.Fatalf("collapsed again: expected 1 row, got %d", len(m.displayRows))
	}
}

func TestHistoryModelSparklines(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})

	key := bigquery.WorkloadKey{Team: "platform", Workload: "web"}
	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "platform", Workload: "web", TotalCost: 10},
		},
		series: []bigquery.TimeSeriesPoint{
			{Key: key, BucketCost: 0},
			{Key: key, BucketCost: 1},
			{Key: key, BucketCost: 2},
			{Key: key, BucketCost: 3},
			{Key: key, BucketCost: 4},
			{Key: key, BucketCost: 5},
			{Key: key, BucketCost: 6},
			{Key: key, BucketCost: 7},
		},
	}

	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	spark, ok := hm.sparklines[key]
	if !ok {
		t.Fatal("sparkline should exist for the workload")
	}
	if spark != "▁▂▃▄▅▆▇█" {
		t.Errorf("sparkline = %q, want ascending blocks", spark)
	}
}

func TestHistoryModelUtilizationDetection(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	cpuUtil := 0.75

	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "a", Workload: "svc1", TotalCost: 5, AvgCPUUtil: &cpuUtil},
		},
	}

	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	if !hm.vis.Utilization {
		t.Error("should detect utilization data")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{3 * time.Hour, "3h"},
		{24 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
		{7 * 24 * time.Hour, "1w"},
		{14 * 24 * time.Hour, "2w"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestHistoryModelToggleExpandAll(t *testing.T) {
	m := testHistoryModel(&mockFetcher{})
	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{Team: "alpha", Workload: "svc1", TotalCost: 5},
			{Team: "beta", Workload: "svc2", TotalCost: 3},
		},
	}
	updated, _ := m.Update(msg)
	m = updated.(HistoryModel)

	// 2 collapsed team rows
	if len(m.displayRows) != 2 {
		t.Fatalf("expected 2 collapsed rows, got %d", len(m.displayRows))
	}

	// Press 'a' to expand all
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a'})
	m = updated.(HistoryModel)
	if len(m.displayRows) != 4 {
		t.Fatalf("expected 4 rows (2 teams + 2 workloads), got %d", len(m.displayRows))
	}

	// Press 'a' again to collapse all
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a'})
	m = updated.(HistoryModel)
	if len(m.displayRows) != 2 {
		t.Fatalf("expected 2 collapsed rows, got %d", len(m.displayRows))
	}
}

func TestHistoryModelAllClusters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := NewHistoryModel(ctx, cancel, &mockFetcher{}, 3*24*time.Hour, 3600, bigquery.QueryFilters{}, ColumnVisibility{Cluster: true})

	if !m.vis.Cluster {
		t.Error("showCluster should be true")
	}

	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{ClusterName: "prod", Team: "platform", Workload: "web", TotalCost: 10},
			{ClusterName: "staging", Team: "data", Workload: "etl", TotalCost: 2},
		},
	}

	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	// Switch to flat mode so cluster names are visible in rows
	updated, _ = hm.Update(tea.KeyPressMsg{Code: 'g'})
	hm = updated.(HistoryModel)

	view := hm.View().Content
	if !strings.Contains(view, "CLUSTER") {
		t.Error("view should contain CLUSTER header when showCluster=true")
	}
	if !strings.Contains(view, "prod") {
		t.Error("view should contain prod cluster name")
	}
	if !strings.Contains(view, "staging") {
		t.Error("view should contain staging cluster name")
	}
}

func TestHistoryModelSingleClusterHeader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	filters := bigquery.QueryFilters{ClusterName: "my-cluster"}
	m := NewHistoryModel(ctx, cancel, &mockFetcher{}, 3*24*time.Hour, 3600, filters, ColumnVisibility{})

	msg := historyDataMsg{
		rows: []bigquery.HistoryCostRow{
			{ClusterName: "my-cluster", Team: "a", Workload: "svc1", TotalCost: 5},
		},
	}

	updated, _ := m.Update(msg)
	hm := updated.(HistoryModel)

	view := hm.View().Content
	if !strings.Contains(view, "cluster: my-cluster") {
		t.Errorf("view should show cluster in header when filtering single cluster, got:\n%s", view)
	}
	if strings.Contains(view, "CLUSTER") {
		t.Error("view should NOT contain CLUSTER column header when showCluster=false")
	}
}

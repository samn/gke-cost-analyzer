package tui

import (
	"strings"
	"testing"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
)

func TestRenderHistoryTableBasic(t *testing.T) {
	rows := []HistoryDisplayRow{
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				Team: "platform", Workload: "web",
				AvgPods: 3, AvgCPUVCPU: 1.5, AvgMemoryGB: 4.0,
				AvgCostPerHour: 0.52, TotalCost: 12.50,
			},
		},
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				Team: "data", Workload: "etl",
				AvgPods: 2, AvgCPUVCPU: 0.5, AvgMemoryGB: 2.0,
				AvgCostPerHour: 0.10, TotalCost: 2.40, HasSpot: true,
			},
		},
	}

	result := RenderHistoryTable(rows, false, false, false, false, DefaultHistorySort(), -1, nil)

	if !strings.Contains(result, "TEAM") {
		t.Error("should contain TEAM header")
	}
	if !strings.Contains(result, "TOTAL") {
		t.Error("should contain TOTAL header")
	}
	if !strings.Contains(result, "platform") {
		t.Error("should contain team name")
	}
	if !strings.Contains(result, "web") {
		t.Error("should contain workload name")
	}
	if !strings.Contains(result, "$12.5000") {
		t.Errorf("should contain cost, got:\n%s", result)
	}
	if !strings.Contains(result, "TREND") {
		t.Error("should contain TREND header")
	}
}

func TestRenderHistoryTableWithSparklines(t *testing.T) {
	sparklines := map[bigquery.WorkloadKey]string{
		{Team: "platform", Workload: "web"}: "▁▂▃▄▅▆▇█",
	}

	rows := []HistoryDisplayRow{
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				Team: "platform", Workload: "web",
				AvgPods: 3, AvgCPUVCPU: 1.5, AvgMemoryGB: 4.0,
				AvgCostPerHour: 0.52, TotalCost: 12.50,
			},
		},
	}

	result := RenderHistoryTable(rows, false, false, false, false, DefaultHistorySort(), -1, sparklines)
	if !strings.Contains(result, "▁▂▃▄▅▆▇█") {
		t.Errorf("should contain sparkline, got:\n%s", result)
	}
}

func TestRenderHistoryTableGrouped(t *testing.T) {
	groups := []HistoryTeamGroup{
		{
			Team: "platform",
			Workloads: []bigquery.HistoryCostRow{
				{Team: "platform", Workload: "web", TotalCost: 10},
				{Team: "platform", Workload: "api", TotalCost: 5},
			},
			Summary: bigquery.HistoryCostRow{Team: "platform", TotalCost: 15, AvgPods: 5},
		},
	}

	expanded := map[string]bool{"platform": true}
	displayRows := buildHistoryDisplayRows(groups, expanded)

	if len(displayRows) != 3 {
		t.Fatalf("expected 3 display rows (1 summary + 2 workloads), got %d", len(displayRows))
	}

	if displayRows[0].Kind != rowTeamSummary {
		t.Error("first row should be team summary")
	}
	if !displayRows[0].Expanded {
		t.Error("team should be expanded")
	}
	if displayRows[1].Kind != rowWorkloadDetail {
		t.Error("second row should be workload detail")
	}

	result := RenderHistoryTable(displayRows, false, false, false, false, DefaultHistorySort(), -1, nil)
	if !strings.Contains(result, "platform") {
		t.Error("should contain team name")
	}
	if !strings.Contains(result, "2 workloads") {
		t.Errorf("should contain workload count, got:\n%s", result)
	}
}

func TestRenderHistoryTableCollapsed(t *testing.T) {
	groups := []HistoryTeamGroup{
		{
			Team: "platform",
			Workloads: []bigquery.HistoryCostRow{
				{Team: "platform", Workload: "web", TotalCost: 10},
				{Team: "platform", Workload: "api", TotalCost: 5},
			},
			Summary: bigquery.HistoryCostRow{Team: "platform", TotalCost: 15},
		},
	}

	displayRows := buildHistoryDisplayRows(groups, map[string]bool{})
	if len(displayRows) != 1 {
		t.Fatalf("collapsed should have 1 row, got %d", len(displayRows))
	}
	if displayRows[0].Expanded {
		t.Error("should not be expanded")
	}
}

func TestBuildFlatHistoryDisplayRows(t *testing.T) {
	rows := []bigquery.HistoryCostRow{
		{Team: "a", Workload: "svc1"},
		{Team: "b", Workload: "svc2"},
	}

	display := buildFlatHistoryDisplayRows(rows)
	if len(display) != 2 {
		t.Fatalf("expected 2, got %d", len(display))
	}
	if display[0].Kind != rowFlat || display[1].Kind != rowFlat {
		t.Error("all rows should be flat kind")
	}
}

func TestRenderHistoryTableWithUtilization(t *testing.T) {
	cpuUtil := 0.75
	memUtil := 0.50

	rows := []HistoryDisplayRow{
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				Team: "team1", Workload: "svc1",
				AvgPods: 2, AvgCPUVCPU: 1.0, AvgMemoryGB: 2.0,
				AvgCostPerHour: 0.10, TotalCost: 2.40,
				AvgCPUUtil: &cpuUtil, AvgMemUtil: &memUtil, TotalWastedCost: 0.60,
			},
		},
	}

	result := RenderHistoryTable(rows, false, false, true, false, DefaultHistorySort(), -1, nil)
	if !strings.Contains(result, "CPU%") {
		t.Error("should contain CPU% header with utilization")
	}
	if !strings.Contains(result, "75%") {
		t.Errorf("should contain cpu utilization percentage, got:\n%s", result)
	}
}

func TestRenderHistoryTableWithSubtype(t *testing.T) {
	rows := []HistoryDisplayRow{
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				Team: "team1", Workload: "svc1", Subtype: "extract",
				AvgPods: 1, AvgCPUVCPU: 0.5, AvgMemoryGB: 1.0,
				AvgCostPerHour: 0.05, TotalCost: 1.20,
			},
		},
	}

	result := RenderHistoryTable(rows, false, true, false, false, DefaultHistorySort(), -1, nil)
	if !strings.Contains(result, "SUBTYPE") {
		t.Error("should contain SUBTYPE header")
	}
	if !strings.Contains(result, "extract") {
		t.Error("should contain subtype value")
	}
}

func TestRenderHistoryTableWithCluster(t *testing.T) {
	rows := []HistoryDisplayRow{
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				ClusterName: "prod-cluster",
				Team:        "platform", Workload: "web",
				AvgPods: 3, AvgCPUVCPU: 1.5, AvgMemoryGB: 4.0,
				AvgCostPerHour: 0.52, TotalCost: 12.50,
			},
		},
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				ClusterName: "staging-cluster",
				Team:        "platform", Workload: "web",
				AvgPods: 1, AvgCPUVCPU: 0.5, AvgMemoryGB: 1.0,
				AvgCostPerHour: 0.10, TotalCost: 2.40,
			},
		},
	}

	result := RenderHistoryTable(rows, true, false, false, false, DefaultHistorySort(), -1, nil)
	if !strings.Contains(result, "CLUSTER") {
		t.Error("should contain CLUSTER header")
	}
	if !strings.Contains(result, "prod-cluster") {
		t.Error("should contain prod-cluster")
	}
	if !strings.Contains(result, "staging-cluster") {
		t.Error("should contain staging-cluster")
	}
}

func TestRenderHistoryTableSpotIndicator(t *testing.T) {
	rows := []HistoryDisplayRow{
		{
			Kind: rowFlat,
			Row: bigquery.HistoryCostRow{
				Team: "team1", Workload: "svc1",
				AvgPods: 1, TotalCost: 1.0, HasSpot: true,
			},
		},
	}

	result := RenderHistoryTable(rows, false, false, false, false, DefaultHistorySort(), -1, nil)
	if !strings.Contains(result, "yes") {
		t.Error("should contain 'yes' for spot workload")
	}
}

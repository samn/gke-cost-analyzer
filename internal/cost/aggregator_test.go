package cost

import (
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

func TestAggregateSingleGroup(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false, labels),
		kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false, labels),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(costs, lc)

	if len(aggs) != 1 {
		t.Fatalf("expected 1 group, got %d", len(aggs))
	}

	agg := aggs[0]
	if agg.Key.Team != "platform" {
		t.Errorf("team = %s, want platform", agg.Key.Team)
	}
	if agg.Key.Workload != "web" {
		t.Errorf("workload = %s, want web", agg.Key.Workload)
	}
	if agg.PodCount != 2 {
		t.Errorf("pod count = %d, want 2", agg.PodCount)
	}
	if !approxEqual(agg.TotalCPUVCPU, 1.0, 0.001) {
		t.Errorf("total CPU = %f, want 1.0", agg.TotalCPUVCPU)
	}

	// Verify cost sums: each pod = 0.5 vCPU * 1h * 0.035 = 0.0175 CPU cost
	expectedCPUCost := 2 * (0.5 * 1.0 * 0.035) // 0.035
	if !approxEqual(agg.CPUCost, expectedCPUCost, 0.0001) {
		t.Errorf("CPUCost = %f, want %f", agg.CPUCost, expectedCPUCost)
	}

	// Each pod: 0.512 GB * 1h * 0.004
	memGB := 512.0 / 1000.0 // 0.512 GB (SI)
	expectedMemCost := 2 * (memGB * 1.0 * 0.004)
	if !approxEqual(agg.MemCost, expectedMemCost, 0.0001) {
		t.Errorf("MemCost = %f, want %f", agg.MemCost, expectedMemCost)
	}

	if !approxEqual(agg.TotalCost, agg.CPUCost+agg.MemCost, 0.0001) {
		t.Errorf("TotalCost = %f, want CPUCost+MemCost = %f", agg.TotalCost, agg.CPUCost+agg.MemCost)
	}

	expectedTotalMemGB := 2 * memGB
	if !approxEqual(agg.TotalMemGB, expectedTotalMemGB, 0.001) {
		t.Errorf("TotalMemGB = %f, want %f", agg.TotalMemGB, expectedTotalMemGB)
	}

	// CostPerHour should be sum of individual hourly rates
	expectedCostPerHour := 2 * (0.5*0.035 + memGB*0.004)
	if !approxEqual(agg.CostPerHour, expectedCostPerHour, 0.0001) {
		t.Errorf("CostPerHour = %f, want %f", agg.CostPerHour, expectedCostPerHour)
	}
}

func TestAggregateMultipleGroups(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
			map[string]string{"team": "platform", "app": "web"}),
		kube.NewTestPodInfo("worker-1", "batch", 1000, 1024, startTime, true,
			map[string]string{"team": "data", "app": "etl"}),
		kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false,
			map[string]string{"team": "platform", "app": "web"}),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.01},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(costs, lc)

	if len(aggs) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(aggs))
	}

	// Find groups
	groups := make(map[string]AggregatedCost)
	for _, a := range aggs {
		groups[a.Key.Team+"/"+a.Key.Workload] = a
	}

	platformWeb, ok := groups["platform/web"]
	if !ok {
		t.Fatal("missing platform/web group")
	}
	if platformWeb.PodCount != 2 {
		t.Errorf("platform/web pod count = %d, want 2", platformWeb.PodCount)
	}
	if platformWeb.Key.IsSpot {
		t.Error("platform/web should not be spot")
	}

	// Verify platform/web cost sums
	webMemGB := 512.0 / 1000.0
	expectedWebCPUCost := 2 * (0.5 * 1.0 * 0.035)
	expectedWebMemCost := 2 * (webMemGB * 1.0 * 0.004)
	if !approxEqual(platformWeb.CPUCost, expectedWebCPUCost, 0.0001) {
		t.Errorf("platform/web CPUCost = %f, want %f", platformWeb.CPUCost, expectedWebCPUCost)
	}
	if !approxEqual(platformWeb.MemCost, expectedWebMemCost, 0.0001) {
		t.Errorf("platform/web MemCost = %f, want %f", platformWeb.MemCost, expectedWebMemCost)
	}
	if !approxEqual(platformWeb.TotalCost, expectedWebCPUCost+expectedWebMemCost, 0.0001) {
		t.Errorf("platform/web TotalCost = %f, want %f", platformWeb.TotalCost, expectedWebCPUCost+expectedWebMemCost)
	}

	dataETL, ok := groups["data/etl"]
	if !ok {
		t.Fatal("missing data/etl group")
	}
	if dataETL.PodCount != 1 {
		t.Errorf("data/etl pod count = %d, want 1", dataETL.PodCount)
	}
	if !dataETL.Key.IsSpot {
		t.Error("data/etl should be spot")
	}

	// Verify data/etl cost sums (spot pricing)
	etlMemGB := 1024.0 / 1000.0
	expectedETLCPUCost := 1.0 * 1.0 * 0.01
	expectedETLMemCost := etlMemGB * 1.0 * 0.0012
	if !approxEqual(dataETL.CPUCost, expectedETLCPUCost, 0.0001) {
		t.Errorf("data/etl CPUCost = %f, want %f", dataETL.CPUCost, expectedETLCPUCost)
	}
	if !approxEqual(dataETL.MemCost, expectedETLMemCost, 0.0001) {
		t.Errorf("data/etl MemCost = %f, want %f", dataETL.MemCost, expectedETLMemCost)
	}
}

func TestAggregateMissingLabels(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("pod1", "default", 100, 128, startTime, false, nil),
		kube.NewTestPodInfo("pod2", "default", 100, 128, startTime, false, map[string]string{"team": "ops"}),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(costs, lc)

	if len(aggs) != 2 {
		t.Fatalf("expected 2 groups (different team labels), got %d", len(aggs))
	}
}

func TestAggregateWithSubtype(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("step1-a", "default", 500, 512, startTime, false,
			map[string]string{"team": "data", "workflow": "pipeline", "step": "extract"}),
		kube.NewTestPodInfo("step2-a", "default", 1000, 2048, startTime, false,
			map[string]string{"team": "data", "workflow": "pipeline", "step": "transform"}),
		kube.NewTestPodInfo("step1-b", "default", 500, 512, startTime, false,
			map[string]string{"team": "data", "workflow": "pipeline", "step": "extract"}),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "workflow", SubtypeLabel: "step"}
	aggs := Aggregate(costs, lc)

	if len(aggs) != 2 {
		t.Fatalf("expected 2 groups (extract + transform), got %d", len(aggs))
	}

	groups := make(map[string]AggregatedCost)
	for _, a := range aggs {
		groups[a.Key.Subtype] = a
	}

	extract, ok := groups["extract"]
	if !ok {
		t.Fatal("missing extract group")
	}
	if extract.PodCount != 2 {
		t.Errorf("extract pod count = %d, want 2", extract.PodCount)
	}

	// Verify extract cost sums
	extractMemGB := 512.0 / 1000.0
	expectedExtractCPU := 2 * (0.5 * 1.0 * 0.035)
	expectedExtractMem := 2 * (extractMemGB * 1.0 * 0.004)
	if !approxEqual(extract.CPUCost, expectedExtractCPU, 0.0001) {
		t.Errorf("extract CPUCost = %f, want %f", extract.CPUCost, expectedExtractCPU)
	}
	if !approxEqual(extract.MemCost, expectedExtractMem, 0.0001) {
		t.Errorf("extract MemCost = %f, want %f", extract.MemCost, expectedExtractMem)
	}

	transform, ok := groups["transform"]
	if !ok {
		t.Fatal("missing transform group")
	}
	if transform.PodCount != 1 {
		t.Errorf("transform pod count = %d, want 1", transform.PodCount)
	}

	// Verify transform cost sums (1 vCPU, 2048 MB = 2.048 GB)
	transformMemGB := 2048.0 / 1000.0
	expectedTransformCPU := 1.0 * 1.0 * 0.035
	expectedTransformMem := transformMemGB * 1.0 * 0.004
	if !approxEqual(transform.CPUCost, expectedTransformCPU, 0.0001) {
		t.Errorf("transform CPUCost = %f, want %f", transform.CPUCost, expectedTransformCPU)
	}
	if !approxEqual(transform.TotalCost, expectedTransformCPU+expectedTransformMem, 0.0001) {
		t.Errorf("transform TotalCost = %f, want %f", transform.TotalCost, expectedTransformCPU+expectedTransformMem)
	}
}

func TestAggregateSpotAndOnDemandSeparate(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-od", "default", 500, 512, startTime, false, labels),
		kube.NewTestPodInfo("web-spot", "default", 500, 512, startTime, true, labels),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.01},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(costs, lc)

	// Spot and on-demand should be separate groups
	if len(aggs) != 2 {
		t.Fatalf("expected 2 groups (spot + on-demand), got %d", len(aggs))
	}

	// Verify that the spot group has lower costs than on-demand
	groups := make(map[bool]AggregatedCost)
	for _, a := range aggs {
		groups[a.Key.IsSpot] = a
	}
	od := groups[false]
	spot := groups[true]
	if spot.TotalCost >= od.TotalCost {
		t.Errorf("spot cost (%f) should be less than on-demand cost (%f)", spot.TotalCost, od.TotalCost)
	}
}

func TestAggregateEmpty(t *testing.T) {
	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(nil, lc)

	if len(aggs) != 0 {
		t.Errorf("expected 0 groups for empty input, got %d", len(aggs))
	}
}

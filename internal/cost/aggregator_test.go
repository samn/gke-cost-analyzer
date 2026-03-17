package cost

import (
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/samn/autopilot-cost-analyzer/internal/prometheus"
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.00001},
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.00001},
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

func TestAggregateEmptySlice(t *testing.T) {
	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate([]PodCost{}, lc)

	if len(aggs) != 0 {
		t.Errorf("expected 0 groups for empty slice, got %d", len(aggs))
	}
}

func TestAggregateNamespaceFromFirstPod(t *testing.T) {
	// When pods from different namespaces share the same labels,
	// the aggregated group takes the namespace from the first pod seen.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "ns-a", 500, 512, startTime, false, labels),
		kube.NewTestPodInfo("web-2", "ns-b", 500, 512, startTime, false, labels),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(costs, lc)

	if len(aggs) != 1 {
		t.Fatalf("expected 1 group, got %d", len(aggs))
	}

	// Namespace comes from the first pod seen (ns-a)
	if aggs[0].Namespace != "ns-a" {
		t.Errorf("namespace = %s, want ns-a (from first pod)", aggs[0].Namespace)
	}
	if aggs[0].PodCount != 2 {
		t.Errorf("pod count = %d, want 2", aggs[0].PodCount)
	}
}

func TestAggregateNoSubtypeLabelGroupsTogether(t *testing.T) {
	// When SubtypeLabel is empty, pods with different values in what would be
	// the subtype label should still group together.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("step1", "default", 500, 512, startTime, false,
			map[string]string{"team": "data", "app": "pipeline", "step": "extract"}),
		kube.NewTestPodInfo("step2", "default", 500, 512, startTime, false,
			map[string]string{"team": "data", "app": "pipeline", "step": "transform"}),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	// No subtype label configured — all pods group together
	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app", SubtypeLabel: ""}
	aggs := Aggregate(costs, lc)

	if len(aggs) != 1 {
		t.Fatalf("expected 1 group when SubtypeLabel is empty, got %d", len(aggs))
	}
	if aggs[0].PodCount != 2 {
		t.Errorf("pod count = %d, want 2", aggs[0].PodCount)
	}
	if aggs[0].Key.Subtype != "" {
		t.Errorf("subtype should be empty when SubtypeLabel is not configured, got %q", aggs[0].Key.Subtype)
	}
}

func TestAggregateNilLabelsMap(t *testing.T) {
	// Pods with nil labels should be handled gracefully.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("pod1", "default", 100, 128, startTime, false, nil),
		kube.NewTestPodInfo("pod2", "default", 100, 128, startTime, false, nil),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := Aggregate(costs, lc)

	// Both pods have nil labels → both get "" for team and workload → one group
	if len(aggs) != 1 {
		t.Fatalf("expected 1 group for nil-label pods, got %d", len(aggs))
	}
	if aggs[0].Key.Team != "" {
		t.Errorf("team should be empty for nil labels, got %q", aggs[0].Key.Team)
	}
	if aggs[0].Key.Workload != "" {
		t.Errorf("workload should be empty for nil labels, got %q", aggs[0].Key.Workload)
	}
	if aggs[0].PodCount != 2 {
		t.Errorf("pod count = %d, want 2", aggs[0].PodCount)
	}
}

func TestAggregateWithUtilizationBasic(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 1000, 1000, startTime, false, labels), // 1 vCPU, 1 GB
		kube.NewTestPodInfo("web-2", "default", 1000, 1000, startTime, false, labels), // 1 vCPU, 1 GB
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	usage := map[prometheus.PodKey]prometheus.PodUsage{
		{Namespace: "default", Pod: "web-1"}: {CPUCores: 0.5, MemoryBytes: 500_000_000},  // 50% CPU, 50% mem
		{Namespace: "default", Pod: "web-2"}: {CPUCores: 0.25, MemoryBytes: 250_000_000}, // 25% CPU, 25% mem
	}

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := AggregateWithUtilization(costs, lc, usage)

	if len(aggs) != 1 {
		t.Fatalf("expected 1 group, got %d", len(aggs))
	}

	agg := aggs[0]
	if !agg.HasUtilization {
		t.Fatal("expected HasUtilization = true")
	}

	// CPU: total used = 0.75 cores, total requested = 2 vCPU → 37.5%
	if !approxEqual(agg.CPUUtilization, 0.375, 0.001) {
		t.Errorf("CPUUtilization = %f, want 0.375", agg.CPUUtilization)
	}

	// Memory: total used = 0.75 GB, total requested = 2 GB → 37.5%
	if !approxEqual(agg.MemUtilization, 0.375, 0.001) {
		t.Errorf("MemUtilization = %f, want 0.375", agg.MemUtilization)
	}

	// Efficiency = (0.375 * cpuCostPerHour + 0.375 * memCostPerHour) / totalCostPerHour
	// Since both utilizations are the same, efficiency should be 0.375
	if !approxEqual(agg.EfficiencyScore, 0.375, 0.001) {
		t.Errorf("EfficiencyScore = %f, want 0.375", agg.EfficiencyScore)
	}

	// WastedCostPerHour = costPerHour * (1 - 0.375) = costPerHour * 0.625
	expectedWaste := agg.CostPerHour * 0.625
	if !approxEqual(agg.WastedCostPerHour, expectedWaste, 0.0001) {
		t.Errorf("WastedCostPerHour = %f, want %f", agg.WastedCostPerHour, expectedWaste)
	}
}

func TestAggregateWithUtilizationCostWeighted(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	// Pod with expensive CPU and cheap memory
	labels := map[string]string{"team": "ml", "app": "training"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("train-1", "ml", 4000, 1000, startTime, false, labels), // 4 vCPU, 1 GB
	}

	// UnitPrice for CPU is per-mCPU — FromPrices multiplies by 1000 → per-vCPU
	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	// High CPU utilization (90%), low memory utilization (10%)
	usage := map[prometheus.PodKey]prometheus.PodUsage{
		{Namespace: "ml", Pod: "train-1"}: {CPUCores: 3.6, MemoryBytes: 100_000_000},
	}

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := AggregateWithUtilization(costs, lc, usage)

	agg := aggs[0]

	// CPU: 3.6 / 4.0 = 0.9
	if !approxEqual(agg.CPUUtilization, 0.9, 0.001) {
		t.Errorf("CPUUtilization = %f, want 0.9", agg.CPUUtilization)
	}

	// Memory: 0.1 GB / 1.0 GB = 0.1
	if !approxEqual(agg.MemUtilization, 0.1, 0.001) {
		t.Errorf("MemUtilization = %f, want 0.1", agg.MemUtilization)
	}

	// Efficiency should be weighted towards CPU since it's the dominant cost
	// After FromPrices conversion: CPU = 0.035/vCPU-hr, Memory = 0.004/GB-hr
	// CPU cost/hr = 4 * 0.035 = 0.14, Mem cost/hr = 1 * 0.004 = 0.004
	// efficiency = (0.9 * 0.14 + 0.1 * 0.004) / 0.144 ≈ 0.878
	cpuCostPerHour := 4.0 * 0.035
	memCostPerHour := 1.0 * 0.004
	totalCostPerHour := cpuCostPerHour + memCostPerHour
	expectedEfficiency := (0.9*cpuCostPerHour + 0.1*memCostPerHour) / totalCostPerHour

	if !approxEqual(agg.EfficiencyScore, expectedEfficiency, 0.001) {
		t.Errorf("EfficiencyScore = %f, want %f", agg.EfficiencyScore, expectedEfficiency)
	}

	// High efficiency since CPU (dominant cost) is well-utilized
	if agg.EfficiencyScore < 0.8 {
		t.Errorf("expected high efficiency score for CPU-dominated workload with high CPU util, got %f", agg.EfficiencyScore)
	}
}

func TestAggregateWithUtilizationNilUsage(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false, labels),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := AggregateWithUtilization(costs, lc, nil)

	if len(aggs) != 1 {
		t.Fatalf("expected 1 group, got %d", len(aggs))
	}

	agg := aggs[0]
	if agg.HasUtilization {
		t.Error("expected HasUtilization = false when usage is nil")
	}
	if agg.CPUUtilization != 0 {
		t.Errorf("CPUUtilization = %f, want 0", agg.CPUUtilization)
	}
	if agg.EfficiencyScore != 0 {
		t.Errorf("EfficiencyScore = %f, want 0", agg.EfficiencyScore)
	}
}

func TestAggregateWithUtilizationPartialPods(t *testing.T) {
	// Only one of two pods has usage data. Per the spec, only pods with
	// Prometheus data contribute to the utilization calculation — both
	// numerator (actual usage) and denominator (requested resources).
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 1000, 1000, startTime, false, labels), // 1 vCPU, 1 GB
		kube.NewTestPodInfo("web-2", "default", 1000, 1000, startTime, false, labels), // 1 vCPU, 1 GB
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	// Only web-1 has usage data
	usage := map[prometheus.PodKey]prometheus.PodUsage{
		{Namespace: "default", Pod: "web-1"}: {CPUCores: 0.5, MemoryBytes: 500_000_000},
	}

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := AggregateWithUtilization(costs, lc, usage)

	agg := aggs[0]
	if !agg.HasUtilization {
		t.Fatal("expected HasUtilization = true")
	}

	// CPU: used = 0.5 (web-1), requested = 1.0 (only web-1 has data) → 0.5
	if !approxEqual(agg.CPUUtilization, 0.5, 0.001) {
		t.Errorf("CPUUtilization = %f, want 0.5", agg.CPUUtilization)
	}

	// Memory: used = 0.5 GB (web-1), requested = 1.0 GB (only web-1) → 0.5
	if !approxEqual(agg.MemUtilization, 0.5, 0.001) {
		t.Errorf("MemUtilization = %f, want 0.5", agg.MemUtilization)
	}

	// Resource totals should still reflect ALL pods (not just those with data)
	if !approxEqual(agg.TotalCPUVCPU, 2.0, 0.001) {
		t.Errorf("TotalCPUVCPU = %f, want 2.0 (all pods)", agg.TotalCPUVCPU)
	}
	if !approxEqual(agg.TotalMemGB, 2.0, 0.001) {
		t.Errorf("TotalMemGB = %f, want 2.0 (all pods)", agg.TotalMemGB)
	}
}

func TestAggregateWithUtilizationCPUBurst(t *testing.T) {
	// CPU utilization > 1.0 (burst) — efficiency should still cap at 1.0
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 1000, 1000, startTime, false, labels),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	usage := map[prometheus.PodKey]prometheus.PodUsage{
		{Namespace: "default", Pod: "web-1"}: {CPUCores: 1.5, MemoryBytes: 1_000_000_000}, // 150% CPU, 100% mem
	}

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := AggregateWithUtilization(costs, lc, usage)

	agg := aggs[0]

	// Raw CPU utilization should be > 1.0
	if agg.CPUUtilization <= 1.0 {
		t.Errorf("CPUUtilization = %f, expected > 1.0 for burst", agg.CPUUtilization)
	}

	// Efficiency should be capped at 1.0 (no negative waste)
	if agg.EfficiencyScore > 1.0 {
		t.Errorf("EfficiencyScore = %f, should be capped at 1.0", agg.EfficiencyScore)
	}

	// No wasted cost when fully utilized
	if agg.WastedCostPerHour < 0 {
		t.Errorf("WastedCostPerHour = %f, should not be negative", agg.WastedCostPerHour)
	}
}

func TestComputeEfficiencyFullUtilization(t *testing.T) {
	// When both CPU and memory are at 100%, efficiency should be 1.0
	eff := computeEfficiency(1.0, 1.0, 0.10, 0.05, 0.15)
	if !approxEqual(eff, 1.0, 1e-9) {
		t.Errorf("efficiency = %f, want 1.0 for full utilization", eff)
	}
}

func TestComputeEfficiencyBurstCapped(t *testing.T) {
	// When CPU is > 1.0 (burst), it should be capped at 1.0 for the efficiency
	// calculation, preventing efficiency > 1.0
	eff := computeEfficiency(2.0, 1.0, 0.10, 0.05, 0.15)
	if !approxEqual(eff, 1.0, 1e-9) {
		t.Errorf("efficiency = %f, want 1.0 when both resources capped at 100%%", eff)
	}

	// Memory burst also capped
	eff = computeEfficiency(1.0, 1.5, 0.10, 0.05, 0.15)
	if !approxEqual(eff, 1.0, 1e-9) {
		t.Errorf("efficiency = %f, want 1.0 when mem burst capped", eff)
	}
}

func TestComputeEfficiencyZeroTotalCost(t *testing.T) {
	eff := computeEfficiency(0.5, 0.5, 0, 0, 0)
	if eff != 0 {
		t.Errorf("efficiency = %f, want 0 for zero total cost", eff)
	}
}

func TestComputeEfficiencyCostWeighting(t *testing.T) {
	// CPU dominates: cpuCost=0.90, memCost=0.10, total=1.0
	// CPU util=0.8, mem util=0.2
	// Expected: (0.8*0.90 + 0.2*0.10) / 1.0 = 0.72 + 0.02 = 0.74
	eff := computeEfficiency(0.8, 0.2, 0.90, 0.10, 1.0)
	if !approxEqual(eff, 0.74, 0.001) {
		t.Errorf("efficiency = %f, want 0.74", eff)
	}
}

func TestAggregateWithUtilizationZeroCost(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	// Pod with zero requests → zero cost
	labels := map[string]string{"team": "platform", "app": "web"}
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 0, 0, now, false, labels),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	usage := map[prometheus.PodKey]prometheus.PodUsage{
		{Namespace: "default", Pod: "web-1"}: {CPUCores: 0.1, MemoryBytes: 100_000_000},
	}

	lc := LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := AggregateWithUtilization(costs, lc, usage)

	agg := aggs[0]
	// With zero cost, efficiency should be 0 (avoid division by zero)
	if agg.EfficiencyScore != 0 {
		t.Errorf("EfficiencyScore = %f, want 0 for zero cost", agg.EfficiencyScore)
	}
	if agg.WastedCostPerHour != 0 {
		t.Errorf("WastedCostPerHour = %f, want 0 for zero cost", agg.WastedCostPerHour)
	}
}

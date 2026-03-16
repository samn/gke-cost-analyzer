package cost

import (
	"math"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

func testPriceTable() pricing.PriceTable {
	// CPU prices are per-mCPU-hour (as stored by the billing API / cache).
	// FromPrices converts them to per-vCPU-hour (×1000).
	return pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.00001},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})
}

func approxEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCalculateOnDemandPod(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-2 * time.Hour) // running for 2 hours

	pod := kube.NewTestPodInfo("web", "default", 500, 512, startTime, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	// CPU: 0.5 vCPU * 2h * 0.035 = 0.035
	if !approxEqual(cost.CPUCost, 0.035, 0.0001) {
		t.Errorf("CPU cost = %f, want ~0.035", cost.CPUCost)
	}

	// Memory: 512 MB = 0.512 GB (SI), 0.512 * 2h * 0.004 = 0.004096
	expectedMemGB := 512.0 / 1000.0 // 0.512 GB (SI units)
	expectedMemCost := expectedMemGB * 2 * 0.004
	if !approxEqual(cost.MemCost, expectedMemCost, 0.0001) {
		t.Errorf("Memory cost = %f, want ~%f", cost.MemCost, expectedMemCost)
	}

	if !approxEqual(cost.TotalCost, cost.CPUCost+cost.MemCost, 0.0001) {
		t.Errorf("Total cost = %f, want CPU+Mem = %f", cost.TotalCost, cost.CPUCost+cost.MemCost)
	}

	if !approxEqual(cost.DurationHours, 2.0, 0.001) {
		t.Errorf("Duration = %f hours, want 2.0", cost.DurationHours)
	}
}

func TestCalculateSpotPod(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pod := kube.NewTestPodInfo("worker", "batch", 1000, 1024, startTime, true, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	// CPU: 1.0 vCPU * 1h * 0.01 = 0.01
	if !approxEqual(cost.CPUCost, 0.01, 0.0001) {
		t.Errorf("Spot CPU cost = %f, want 0.01", cost.CPUCost)
	}

	// Memory: 1024 MB = 1.024 GB (SI), 1.024 * 1h * 0.0012 = 0.0012288
	expectedSpotMem := 1.024 * 0.0012
	if !approxEqual(cost.MemCost, expectedSpotMem, 0.0001) {
		t.Errorf("Spot Memory cost = %f, want %f", cost.MemCost, expectedSpotMem)
	}
}

func TestCalculateZeroRequests(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pod := kube.NewTestPodInfo("empty", "default", 0, 0, startTime, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost for zero requests, got %f", cost.TotalCost)
	}
}

func TestCalculateJustStarted(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	pod := kube.NewTestPodInfo("new", "default", 1000, 1024, now, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost for just-started pod, got %f", cost.TotalCost)
	}

	// But cost per hour should be non-zero
	if cost.CostPerHour == 0 {
		t.Error("expected non-zero cost per hour")
	}
}

func TestCalculateNoStartTime(t *testing.T) {
	pod := kube.NewTestPodInfo("pending", "default", 1000, 1024, time.Time{}, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), nil)
	cost := calc.Calculate(pod)

	if cost.DurationHours != 0 {
		t.Errorf("expected 0 duration for no start time, got %f", cost.DurationHours)
	}
	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost for no start time, got %f", cost.TotalCost)
	}
}

func TestCalculateAll(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("pod1", "default", 500, 512, startTime, false, nil),
		kube.NewTestPodInfo("pod2", "default", 1000, 1024, startTime, true, nil),
	}

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	costs := calc.CalculateAll(pods)

	if len(costs) != 2 {
		t.Fatalf("expected 2 costs, got %d", len(costs))
	}
	if costs[0].Pod.Name != "pod1" {
		t.Errorf("first cost should be pod1, got %s", costs[0].Pod.Name)
	}
	if costs[1].Pod.Name != "pod2" {
		t.Errorf("second cost should be pod2, got %s", costs[1].Pod.Name)
	}
}

func TestCalculateFutureStartTime(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	futureStart := now.Add(1 * time.Hour) // start time in the future

	pod := kube.NewTestPodInfo("future", "default", 1000, 1024, futureStart, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	if cost.DurationHours != 0 {
		t.Errorf("expected 0 duration for future start time, got %f", cost.DurationHours)
	}
	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost for future start time, got %f", cost.TotalCost)
	}
	// But cost per hour should still be non-zero
	if cost.CostPerHour == 0 {
		t.Error("expected non-zero cost per hour even with future start time")
	}
}

func TestCalculateMissingRegionPrices(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pod := kube.NewTestPodInfo("app", "default", 1000, 1024, startTime, false, nil)

	// Use a price table that doesn't have the requested region
	pt := pricing.FromPrices([]pricing.Price{
		{Region: "europe-west1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.00004},
	})

	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	cost := calc.Calculate(pod)

	// No matching prices → zero cost
	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost for unknown region, got %f", cost.TotalCost)
	}
	if cost.CostPerHour != 0 {
		t.Errorf("expected 0 $/hr for unknown region, got %f", cost.CostPerHour)
	}
}

func TestCostPerHour(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-3 * time.Hour)

	// 2 vCPU, 4GB
	pod := kube.NewTestPodInfo("app", "default", 2000, 4096, startTime, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	// CPU per hour: 2.0 * 0.035 = 0.07
	// Mem per hour: 4.096 GB * 0.004 = 0.016384 (4096 MB = 4.096 GB in SI units)
	expectedPerHour := 2.0*0.035 + 4.096*0.004
	if !approxEqual(cost.CostPerHour, expectedPerHour, 0.0001) {
		t.Errorf("CostPerHour = %f, want %f", cost.CostPerHour, expectedPerHour)
	}
}

func TestCostPerHourIndependentOfDuration(t *testing.T) {
	// CostPerHour should be the same regardless of how long the pod has been running.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	pod1h := kube.NewTestPodInfo("app", "default", 1000, 1024, now.Add(-1*time.Hour), false, nil)
	pod10h := kube.NewTestPodInfo("app", "default", 1000, 1024, now.Add(-10*time.Hour), false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost1h := calc.Calculate(pod1h)
	cost10h := calc.Calculate(pod10h)

	if !approxEqual(cost1h.CostPerHour, cost10h.CostPerHour, 1e-9) {
		t.Errorf("CostPerHour should be duration-independent: 1h=%f, 10h=%f",
			cost1h.CostPerHour, cost10h.CostPerHour)
	}
}

func TestCalculateTotalCostScalesLinearly(t *testing.T) {
	// Total cost should scale linearly with duration.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	pod2h := kube.NewTestPodInfo("app", "default", 1000, 1024, now.Add(-2*time.Hour), false, nil)
	pod4h := kube.NewTestPodInfo("app", "default", 1000, 1024, now.Add(-4*time.Hour), false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost2h := calc.Calculate(pod2h)
	cost4h := calc.Calculate(pod4h)

	// 4h cost should be exactly 2x the 2h cost
	if !approxEqual(cost4h.TotalCost, 2*cost2h.TotalCost, 1e-9) {
		t.Errorf("cost should scale linearly: 2h=%f, 4h=%f (expected 2x)", cost2h.TotalCost, cost4h.TotalCost)
	}
}

func TestCalculateNilNowUsesRealTime(t *testing.T) {
	// Passing nil for now should use time.Now (calculator should not panic).
	startTime := time.Now().Add(-1 * time.Hour)
	pod := kube.NewTestPodInfo("app", "default", 1000, 1024, startTime, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), nil)
	cost := calc.Calculate(pod)

	// Should have roughly 1 hour of cost
	if cost.DurationHours < 0.9 || cost.DurationHours > 1.1 {
		t.Errorf("expected ~1h duration with nil now, got %f", cost.DurationHours)
	}
	if cost.TotalCost <= 0 {
		t.Error("expected positive cost with nil now and real running pod")
	}
}

func TestPartitionAndCalculateBothCalcs(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	autopilotCalc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	standardCalc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	standardCalc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("ap-pod", "default", 500, 512, startTime, false, nil, "gk3-cluster-abc"),
		kube.NewTestPodInfoOnNode("std-pod", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
	}

	costs := PartitionAndCalculate(pods, autopilotCalc, standardCalc)
	if len(costs) != 2 {
		t.Fatalf("expected 2 costs, got %d", len(costs))
	}

	// Verify autopilot pod was routed to autopilot calculator
	var apCost, stdCost PodCost
	for _, c := range costs {
		if c.Pod.Name == "ap-pod" {
			apCost = c
		} else {
			stdCost = c
		}
	}
	if apCost.Pod.Name != "ap-pod" || stdCost.Pod.Name != "std-pod" {
		t.Fatal("expected both pods to have costs")
	}
	// Both should have non-zero costs
	if apCost.CostPerHour == 0 {
		t.Error("autopilot pod should have non-zero cost")
	}
	if stdCost.CostPerHour == 0 {
		t.Error("standard pod should have non-zero cost")
	}
}

func TestPartitionAndCalculateAutopilotOnly(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 500, 512, startTime, false, nil, "gk3-cluster-abc"),
	}

	costs := PartitionAndCalculate(pods, calc, nil)
	if len(costs) != 1 {
		t.Fatalf("expected 1 cost, got %d", len(costs))
	}
}

func TestPartitionAndCalculateStandardOnly(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16},
	})
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
	}

	costs := PartitionAndCalculate(pods, nil, sc)
	if len(costs) != 1 {
		t.Fatalf("expected 1 cost, got %d", len(costs))
	}
}

func TestPartitionAndCalculateNilCalcs(t *testing.T) {
	pods := []kube.PodInfo{
		kube.NewTestPodInfo("pod-1", "default", 500, 512, time.Now(), false, nil),
	}
	costs := PartitionAndCalculate(pods, nil, nil)
	if costs != nil {
		t.Errorf("expected nil costs with nil calculators, got %d", len(costs))
	}
}

func TestCalculateEmptyPriceTable(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)
	pod := kube.NewTestPodInfo("app", "default", 1000, 1024, startTime, false, nil)

	pt := pricing.FromPrices(nil)
	calc := NewCalculator("us-central1", pt, func() time.Time { return now })
	cost := calc.Calculate(pod)

	if cost.TotalCost != 0 {
		t.Errorf("expected 0 cost with empty price table, got %f", cost.TotalCost)
	}
	if cost.CostPerHour != 0 {
		t.Errorf("expected 0 $/hr with empty price table, got %f", cost.CostPerHour)
	}
	// Duration should still be computed
	if !approxEqual(cost.DurationHours, 1.0, 0.001) {
		t.Errorf("duration should be 1h even with no prices, got %f", cost.DurationHours)
	}
}

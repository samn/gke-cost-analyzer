package cost

import (
	"math"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

func testPriceTable() pricing.PriceTable {
	return pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.01},
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
		{Region: "europe-west1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.04},
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

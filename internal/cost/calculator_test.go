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

	// Memory: 0.5 GB (512MB) * 2h * 0.004 = 0.004 (approximately, 512MB = 0.5GB)
	expectedMemGB := 512.0 / 1024.0 // 0.5 GB
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

	// Memory: 1.0 GB * 1h * 0.0012 = 0.0012
	if !approxEqual(cost.MemCost, 0.0012, 0.0001) {
		t.Errorf("Spot Memory cost = %f, want 0.0012", cost.MemCost)
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

func TestCostPerHour(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-3 * time.Hour)

	// 2 vCPU, 4GB
	pod := kube.NewTestPodInfo("app", "default", 2000, 4096, startTime, false, nil)

	calc := NewCalculator("us-central1", testPriceTable(), func() time.Time { return now })
	cost := calc.Calculate(pod)

	// CPU per hour: 2.0 * 0.035 = 0.07
	// Mem per hour: 4.0 GB * 0.004 = 0.016
	expectedPerHour := 2.0*0.035 + 4.0*0.004
	if !approxEqual(cost.CostPerHour, expectedPerHour, 0.0001) {
		t.Errorf("CostPerHour = %f, want %f", cost.CostPerHour, expectedPerHour)
	}
}

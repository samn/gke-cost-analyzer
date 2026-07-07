package cost

import (
	"testing"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/kube"
	"github.com/samn/gke-cost-analyzer/internal/pricing"
)

// podOverhead returns the pod's total node-overhead cost per hour (CPU + memory).
func podOverhead(c PodCost) float64 {
	return c.CPUOverheadCostPerHour + c.MemOverheadCostPerHour
}

func testComputePriceTable() pricing.ComputePriceTable {
	return pricing.FromComputePrices([]pricing.ComputePrice{
		{Region: "us-central1", MachineFamily: "n2", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.031611},
		{Region: "us-central1", MachineFamily: "n2", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004237},
		{Region: "us-central1", MachineFamily: "n2", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.007594},
		{Region: "us-central1", MachineFamily: "n2", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.001017},
		{Region: "us-central1", MachineFamily: "e2", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.022152},
		{Region: "us-central1", MachineFamily: "e2", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.002970},
	})
}

func TestStandardCalculatorSinglePodFullNode(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 4000, 16000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)
	if len(costs) != 1 {
		t.Fatalf("expected 1 cost, got %d", len(costs))
	}

	c := costs[0]

	// Single pod = 100% of node cost
	// Node CPU cost/hr: 4 * 0.031611 = 0.126444
	// Node Mem cost/hr: 16 * 0.004237 = 0.067792
	expectedCPUPerHr := 4.0 * 0.031611
	expectedMemPerHr := 16.0 * 0.004237

	if !approxEqual(c.CPUCostPerHour, expectedCPUPerHr, 0.0001) {
		t.Errorf("CPUCostPerHour = %f, want %f", c.CPUCostPerHour, expectedCPUPerHr)
	}
	if !approxEqual(c.MemCostPerHour, expectedMemPerHr, 0.0001) {
		t.Errorf("MemCostPerHour = %f, want %f", c.MemCostPerHour, expectedMemPerHr)
	}
	if !approxEqual(c.TotalCost, (expectedCPUPerHr+expectedMemPerHr)*1.0, 0.001) {
		t.Errorf("TotalCost = %f, want %f", c.TotalCost, (expectedCPUPerHr+expectedMemPerHr)*1.0)
	}
}

func TestStandardCalculatorTwoPodsEqualRequests(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-2 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
		kube.NewTestPodInfoOnNode("pod-2", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)
	if len(costs) != 2 {
		t.Fatalf("expected 2 costs, got %d", len(costs))
	}

	// Equal requests → 50/50 split
	if !approxEqual(costs[0].CostPerHour, costs[1].CostPerHour, 1e-9) {
		t.Errorf("equal pods should have equal cost: %f vs %f", costs[0].CostPerHour, costs[1].CostPerHour)
	}

	// Total of both pods should equal node cost
	nodeCPUPerHr := 4.0 * 0.031611
	nodeMemPerHr := 16.0 * 0.004237
	totalPerHr := costs[0].CostPerHour + costs[1].CostPerHour
	if !approxEqual(totalPerHr, nodeCPUPerHr+nodeMemPerHr, 0.001) {
		t.Errorf("sum of pod costs/hr (%f) != node cost/hr (%f)", totalPerHr, nodeCPUPerHr+nodeMemPerHr)
	}

	// Duration check
	if !approxEqual(costs[0].DurationHours, 2.0, 0.001) {
		t.Errorf("duration = %f, want 2.0", costs[0].DurationHours)
	}
}

func TestStandardCalculatorUnequalRequests(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	// Pod A: 3 vCPU, 12 GB; Pod B: 1 vCPU, 4 GB → 3:1 CPU split, 3:1 memory split
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-a", "default", 3000, 12000, startTime, false, nil, "gke-node-1"),
		kube.NewTestPodInfoOnNode("pod-b", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)

	// Pod A should get 75% of the cost, Pod B 25%
	ratio := costs[0].CostPerHour / costs[1].CostPerHour
	if !approxEqual(ratio, 3.0, 0.01) {
		t.Errorf("cost ratio = %f, want 3.0 (75/25 split)", ratio)
	}
}

func TestStandardCalculatorZeroRequests(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	// All pods have zero requests
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 0, 0, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)
	if costs[0].CostPerHour != 0 {
		t.Errorf("expected 0 cost for zero requests, got %f", costs[0].CostPerHour)
	}
}

func TestStandardCalculatorSpotNode(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-spot-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: true},
	})

	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 4000, 16000, startTime, false, nil, "gke-spot-1"),
	}

	costs := sc.CalculateAll(pods)

	// Spot prices should be used
	expectedCPUPerHr := 4.0 * 0.007594
	expectedMemPerHr := 16.0 * 0.001017

	if !approxEqual(costs[0].CPUCostPerHour, expectedCPUPerHr, 0.0001) {
		t.Errorf("Spot CPUCostPerHour = %f, want %f", costs[0].CPUCostPerHour, expectedCPUPerHr)
	}
	if !approxEqual(costs[0].MemCostPerHour, expectedMemPerHr, 0.0001) {
		t.Errorf("Spot MemCostPerHour = %f, want %f", costs[0].MemCostPerHour, expectedMemPerHr)
	}
}

func TestStandardCalculatorSpotPropagation(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-spot-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: true},
		{Name: "gke-ondemand-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	pods := []kube.PodInfo{
		// Pod on spot node without spot NodeSelector (common for standard GKE)
		kube.NewTestPodInfoOnNode("spot-pod", "default", 2000, 8000, startTime, false, nil, "gke-spot-1"),
		// Pod on on-demand node
		kube.NewTestPodInfoOnNode("ondemand-pod", "default", 2000, 8000, startTime, false, nil, "gke-ondemand-1"),
	}

	costs := sc.CalculateAll(pods)
	if len(costs) != 2 {
		t.Fatalf("expected 2 costs, got %d", len(costs))
	}

	var spotCost, ondemandCost PodCost
	for _, c := range costs {
		if c.Pod.Name == "spot-pod" {
			spotCost = c
		} else {
			ondemandCost = c
		}
	}

	// Spot status should be propagated from the node
	if !spotCost.Pod.IsSpot {
		t.Error("pod on spot node should have IsSpot=true propagated from node")
	}
	if ondemandCost.Pod.IsSpot {
		t.Error("pod on on-demand node should have IsSpot=false")
	}

	// Spot pod should be cheaper than on-demand pod (same resources)
	if spotCost.CostPerHour >= ondemandCost.CostPerHour {
		t.Errorf("spot pod (%.4f/hr) should be cheaper than on-demand pod (%.4f/hr)",
			spotCost.CostPerHour, ondemandCost.CostPerHour)
	}
}

func TestStandardCalculatorMultipleNodes(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-n2-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
		{Name: "gke-e2-1", MachineType: "e2-medium", MachineFamily: "e2", VCPU: 1, MemoryGB: 4, IsSpot: false},
	})

	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("n2-pod", "default", 2000, 8000, startTime, false, nil, "gke-n2-1"),
		kube.NewTestPodInfoOnNode("e2-pod", "default", 500, 2000, startTime, false, nil, "gke-e2-1"),
	}

	costs := sc.CalculateAll(pods)
	if len(costs) != 2 {
		t.Fatalf("expected 2 costs, got %d", len(costs))
	}

	// n2-pod is the only pod on its node, so gets full node cost
	n2CPUPerHr := 4.0 * 0.031611
	n2MemPerHr := 16.0 * 0.004237

	var n2Cost, e2Cost PodCost
	for _, c := range costs {
		if c.Pod.Name == "n2-pod" {
			n2Cost = c
		} else {
			e2Cost = c
		}
	}

	if !approxEqual(n2Cost.CostPerHour, n2CPUPerHr+n2MemPerHr, 0.001) {
		t.Errorf("n2-pod cost/hr = %f, want %f", n2Cost.CostPerHour, n2CPUPerHr+n2MemPerHr)
	}

	// e2-pod is the only pod on its node
	e2CPUPerHr := 1.0 * 0.022152
	e2MemPerHr := 4.0 * 0.002970

	if !approxEqual(e2Cost.CostPerHour, e2CPUPerHr+e2MemPerHr, 0.001) {
		t.Errorf("e2-pod cost/hr = %f, want %f", e2Cost.CostPerHour, e2CPUPerHr+e2MemPerHr)
	}

	// n2 should be more expensive than e2
	if n2Cost.CostPerHour <= e2Cost.CostPerHour {
		t.Errorf("n2-standard-4 should be more expensive than e2-medium")
	}
}

func TestStandardCalculatorUnknownNode(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	// No nodes set

	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 1000, 1024, startTime, false, nil, "gke-unknown-1"),
	}

	costs := sc.CalculateAll(pods)
	if costs[0].CostPerHour != 0 {
		t.Errorf("expected 0 cost for unknown node, got %f", costs[0].CostPerHour)
	}
}

func TestStandardCalculatorConcurrentAccess(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })

	// Run SetNodes and CalculateAll concurrently to verify no data race
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			sc.SetNodes([]kube.NodeInfo{
				{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16},
			})
		}
	}()

	for i := 0; i < 100; i++ {
		pods := []kube.PodInfo{
			kube.NewTestPodInfoOnNode("pod-1", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
		}
		sc.CalculateAll(pods)
	}
	<-done
}

func TestStandardCalculatorOverheadFullyAllocated(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	// Pod requests exactly match node capacity — no overhead
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 4000, 16000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)
	if podOverhead(costs[0]) != 0 {
		t.Errorf("overhead = %f, want 0 (fully allocated)", podOverhead(costs[0]))
	}
}

func TestStandardCalculatorOverheadHalfAllocated(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	// Pod requests half the node → 50% overhead
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-1", "default", 2000, 8000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)

	// Node cost/hr: CPU = 4 * 0.031611 = 0.126444, Mem = 16 * 0.004237 = 0.067792
	// Pod is the only one, so gets 100% of node cost = 0.194236
	// But only half the node is allocated, so overhead = 50% of each resource cost
	nodeCPUPerHr := 4.0 * 0.031611
	nodeMemPerHr := 16.0 * 0.004237
	expectedOverhead := 0.5*nodeCPUPerHr + 0.5*nodeMemPerHr

	if !approxEqual(podOverhead(costs[0]), expectedOverhead, 0.0001) {
		t.Errorf("overhead = %f, want %f", podOverhead(costs[0]), expectedOverhead)
	}

	// Total cost should still equal full node cost (overhead is part of total, not additional)
	if !approxEqual(costs[0].CostPerHour, nodeCPUPerHr+nodeMemPerHr, 0.0001) {
		t.Errorf("CostPerHour = %f, want %f", costs[0].CostPerHour, nodeCPUPerHr+nodeMemPerHr)
	}
}

func TestStandardCalculatorOverheadTwoPods(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	// Two pods requesting 1 vCPU / 4 GB each = 2 of 4 vCPU, 8 of 16 GB (50% allocated)
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-a", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
		kube.NewTestPodInfoOnNode("pod-b", "default", 1000, 4000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)

	// Each pod gets 50% of node cost, and 50% of each resource is unallocated
	nodeCPUPerHr := 4.0 * 0.031611
	nodeMemPerHr := 16.0 * 0.004237

	// Each pod's share of overhead = 50% share × 50% overhead fraction
	expectedOverheadPerPod := 0.5*nodeCPUPerHr*0.5 + 0.5*nodeMemPerHr*0.5

	for i, c := range costs {
		if !approxEqual(podOverhead(c), expectedOverheadPerPod, 0.0001) {
			t.Errorf("pod %d overhead = %f, want %f", i, podOverhead(c), expectedOverheadPerPod)
		}
	}

	// Sum of overhead should equal total unallocated cost
	totalOverhead := podOverhead(costs[0]) + podOverhead(costs[1])
	expectedTotalOverhead := 0.5*nodeCPUPerHr + 0.5*nodeMemPerHr
	if !approxEqual(totalOverhead, expectedTotalOverhead, 0.0001) {
		t.Errorf("total overhead = %f, want %f", totalOverhead, expectedTotalOverhead)
	}
}

func TestStandardCalculatorOverheadOvercommitted(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	sc := NewStandardCalculator("us-central1", testComputePriceTable(), func() time.Time { return now })
	sc.SetNodes([]kube.NodeInfo{
		{Name: "gke-node-1", MachineType: "n2-standard-4", MachineFamily: "n2", VCPU: 4, MemoryGB: 16, IsSpot: false},
	})

	// Requests exceed node capacity — no overhead
	pods := []kube.PodInfo{
		kube.NewTestPodInfoOnNode("pod-a", "default", 3000, 10000, startTime, false, nil, "gke-node-1"),
		kube.NewTestPodInfoOnNode("pod-b", "default", 3000, 10000, startTime, false, nil, "gke-node-1"),
	}

	costs := sc.CalculateAll(pods)
	for i, c := range costs {
		if podOverhead(c) != 0 {
			t.Errorf("pod %d overhead = %f, want 0 (overcommitted)", i, podOverhead(c))
		}
	}
}

func TestStandardCalculatorImplementsInterface(t *testing.T) {
	sc := NewStandardCalculator("us-central1", testComputePriceTable(), nil)
	var _ PodCostCalculator = sc
}

func TestCalculatorImplementsInterface(t *testing.T) {
	calc := NewCalculator("us-central1", testPriceTable(), nil)
	var _ PodCostCalculator = calc
}

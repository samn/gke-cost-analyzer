package cost

import (
	"log"
	"sync"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/kube"
	"github.com/samn/gke-cost-analyzer/internal/pricing"
)

// StandardCalculator computes costs for standard GKE pods via per-node
// proportional attribution. Each pod's cost is its share of the node cost,
// proportional to its resource requests relative to the total requests on that node.
type StandardCalculator struct {
	prices pricing.ComputePriceTable
	region string
	mu     sync.RWMutex
	nodes  map[string]kube.NodeInfo // keyed by node name
	now    func() time.Time
}

// NewStandardCalculator creates a cost calculator for standard GKE workloads.
func NewStandardCalculator(region string, prices pricing.ComputePriceTable, now func() time.Time) *StandardCalculator {
	if now == nil {
		now = time.Now
	}
	return &StandardCalculator{
		prices: prices,
		region: region,
		nodes:  make(map[string]kube.NodeInfo),
		now:    now,
	}
}

// SetNodes updates the node inventory used for cost attribution.
func (sc *StandardCalculator) SetNodes(nodes []kube.NodeInfo) {
	m := make(map[string]kube.NodeInfo, len(nodes))
	for _, n := range nodes {
		m[n.Name] = n
	}
	sc.mu.Lock()
	sc.nodes = m
	sc.mu.Unlock()
}

// CalculateAll computes costs for a list of pods using per-node proportional attribution.
func (sc *StandardCalculator) CalculateAll(pods []kube.PodInfo) []PodCost {
	sc.mu.RLock()
	nodes := sc.nodes
	sc.mu.RUnlock()

	// Group pods by node
	podsByNode := make(map[string][]int) // node name → indices into pods
	for i, pod := range pods {
		podsByNode[pod.NodeName] = append(podsByNode[pod.NodeName], i)
	}

	costs := make([]PodCost, len(pods))

	for nodeName, podIndices := range podsByNode {
		node, ok := nodes[nodeName]
		if !ok {
			log.Printf("Warning: pod on unknown node %q, skipping cost attribution", nodeName)
			for _, i := range podIndices {
				costs[i] = PodCost{Pod: pods[i]}
			}
			continue
		}

		tier := pricing.OnDemand
		if node.IsSpot {
			tier = pricing.Spot
		}

		cpuPrice := sc.prices.Lookup(sc.region, node.MachineFamily, pricing.CPU, tier)
		memPrice := sc.prices.Lookup(sc.region, node.MachineFamily, pricing.Memory, tier)

		if cpuPrice == 0 && memPrice == 0 && node.MachineFamily != "" {
			log.Printf("Warning: no %s prices for family=%q region=%q on node %q; pods will show $0", tier, node.MachineFamily, sc.region, nodeName)
		}

		nodeCPUCostPerHr := node.VCPU * cpuPrice
		nodeMemCostPerHr := node.MemoryGB * memPrice

		// Sum total requests on this node
		var totalCPUReqs, totalMemReqs float64
		for _, i := range podIndices {
			totalCPUReqs += pods[i].CPURequestVCPU
			totalMemReqs += pods[i].MemRequestGB
		}

		// Compute the fraction of node capacity that is unallocated.
		// When total requests exceed capacity (overcommitted), overhead is 0.
		var cpuOverheadFrac, memOverheadFrac float64
		if node.VCPU > 0 && totalCPUReqs < node.VCPU {
			cpuOverheadFrac = 1 - totalCPUReqs/node.VCPU
		}
		if node.MemoryGB > 0 && totalMemReqs < node.MemoryGB {
			memOverheadFrac = 1 - totalMemReqs/node.MemoryGB
		}

		for _, i := range podIndices {
			// Value copy — pods[i] is a struct, so this creates a local
			// copy that we can safely mutate without affecting the caller's
			// slice. We propagate spot status from the node so that
			// downstream aggregation correctly groups standard pods by
			// their actual scheduling tier instead of relying on pod-level
			// NodeSelector labels (which are typically absent for standard
			// GKE workloads).
			pod := pods[i]
			pod.IsSpot = node.IsSpot
			durationHours := sc.durationHours(pod)

			var cpuShare, memShare float64
			if totalCPUReqs > 0 {
				cpuShare = pod.CPURequestVCPU / totalCPUReqs
			}
			if totalMemReqs > 0 {
				memShare = pod.MemRequestGB / totalMemReqs
			}

			cpuCostPerHr := cpuShare * nodeCPUCostPerHr
			memCostPerHr := memShare * nodeMemCostPerHr

			// Overhead: the portion of this pod's cost that comes from
			// unallocated node capacity rather than its own requests.
			cpuOverheadPerHr := cpuCostPerHr * cpuOverheadFrac
			memOverheadPerHr := memCostPerHr * memOverheadFrac

			costs[i] = PodCost{
				Pod:                    pod,
				CPUCost:                cpuCostPerHr * durationHours,
				MemCost:                memCostPerHr * durationHours,
				TotalCost:              (cpuCostPerHr + memCostPerHr) * durationHours,
				DurationHours:          durationHours,
				CostPerHour:            cpuCostPerHr + memCostPerHr,
				CPUCostPerHour:         cpuCostPerHr,
				MemCostPerHour:         memCostPerHr,
				CPUOverheadCostPerHour: cpuOverheadPerHr,
				MemOverheadCostPerHour: memOverheadPerHr,
			}
		}
	}

	return costs
}

func (sc *StandardCalculator) durationHours(pod kube.PodInfo) float64 {
	if pod.StartTime.IsZero() {
		return 0
	}
	d := sc.now().Sub(pod.StartTime)
	if d < 0 {
		return 0
	}
	return d.Hours()
}

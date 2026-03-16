package cost

import (
	"log"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

// StandardCalculator computes costs for standard GKE pods via per-node
// proportional attribution. Each pod's cost is its share of the node cost,
// proportional to its resource requests relative to the total requests on that node.
type StandardCalculator struct {
	prices pricing.ComputePriceTable
	region string
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
	sc.nodes = make(map[string]kube.NodeInfo, len(nodes))
	for _, n := range nodes {
		sc.nodes[n.Name] = n
	}
}

// CalculateAll computes costs for a list of pods using per-node proportional attribution.
func (sc *StandardCalculator) CalculateAll(pods []kube.PodInfo) []PodCost {
	// Group pods by node
	podsByNode := make(map[string][]int) // node name → indices into pods
	for i, pod := range pods {
		podsByNode[pod.NodeName] = append(podsByNode[pod.NodeName], i)
	}

	costs := make([]PodCost, len(pods))

	for nodeName, podIndices := range podsByNode {
		node, ok := sc.nodes[nodeName]
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

		nodeCPUCostPerHr := node.VCPU * cpuPrice
		nodeMemCostPerHr := node.MemoryGB * memPrice

		// Sum total requests on this node
		var totalCPUReqs, totalMemReqs float64
		for _, i := range podIndices {
			totalCPUReqs += pods[i].CPURequestVCPU
			totalMemReqs += pods[i].MemRequestGB
		}

		for _, i := range podIndices {
			pod := pods[i]
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

			costs[i] = PodCost{
				Pod:            pod,
				CPUCost:        cpuCostPerHr * durationHours,
				MemCost:        memCostPerHr * durationHours,
				TotalCost:      (cpuCostPerHr + memCostPerHr) * durationHours,
				DurationHours:  durationHours,
				CostPerHour:    cpuCostPerHr + memCostPerHr,
				CPUCostPerHour: cpuCostPerHr,
				MemCostPerHour: memCostPerHr,
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

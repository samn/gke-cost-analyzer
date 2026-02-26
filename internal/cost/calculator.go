// Package cost provides cost calculation and aggregation for GKE Autopilot pods.
package cost

import (
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

// PodCost represents the calculated cost for a single pod.
type PodCost struct {
	Pod           kube.PodInfo
	CPUCost       float64
	MemCost       float64
	TotalCost     float64
	DurationHours float64
	// CostPerHour is the hourly rate (cpu + memory)
	CostPerHour float64
}

// Calculator computes costs for pods given a pricing table.
type Calculator struct {
	prices pricing.PriceTable
	region string
	now    func() time.Time
}

// NewCalculator creates a cost calculator for the given region and price table.
func NewCalculator(region string, prices pricing.PriceTable, now func() time.Time) *Calculator {
	if now == nil {
		now = time.Now
	}
	return &Calculator{prices: prices, region: region, now: now}
}

// Calculate computes the cost for a single pod.
func (c *Calculator) Calculate(pod kube.PodInfo) PodCost {
	tier := pricing.OnDemand
	if pod.IsSpot {
		tier = pricing.Spot
	}

	cpuPrice := c.prices.Lookup(c.region, pricing.CPU, tier)
	memPrice := c.prices.Lookup(c.region, pricing.Memory, tier)

	durationHours := c.durationHours(pod)

	cpuCost := pod.CPURequestVCPU * durationHours * cpuPrice
	memCost := pod.MemRequestGB * durationHours * memPrice
	cpuPerHour := pod.CPURequestVCPU * cpuPrice
	memPerHour := pod.MemRequestGB * memPrice

	return PodCost{
		Pod:           pod,
		CPUCost:       cpuCost,
		MemCost:       memCost,
		TotalCost:     cpuCost + memCost,
		DurationHours: durationHours,
		CostPerHour:   cpuPerHour + memPerHour,
	}
}

// CalculateAll computes costs for a list of pods.
func (c *Calculator) CalculateAll(pods []kube.PodInfo) []PodCost {
	costs := make([]PodCost, len(pods))
	for i, pod := range pods {
		costs[i] = c.Calculate(pod)
	}
	return costs
}

func (c *Calculator) durationHours(pod kube.PodInfo) float64 {
	if pod.StartTime.IsZero() {
		return 0
	}
	d := c.now().Sub(pod.StartTime)
	if d < 0 {
		return 0
	}
	return d.Hours()
}

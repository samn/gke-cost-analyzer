package pricing

import (
	"math"
	"testing"
)

func TestPriceTableLookup(t *testing.T) {
	// CPU prices are per-mCPU-hour (as stored by the billing API / cache).
	// FromPrices converts CPU to per-vCPU-hour (×1000).
	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: Memory, Tier: OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: CPU, Tier: Spot, UnitPrice: 0.00001},
		{Region: "europe-west1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.00004},
	}

	pt := FromPrices(prices)

	tests := []struct {
		name     string
		region   string
		rt       ResourceType
		tier     Tier
		expected float64
	}{
		{"cpu on-demand us-central1", "us-central1", CPU, OnDemand, 0.035},
		{"memory on-demand us-central1", "us-central1", Memory, OnDemand, 0.004},
		{"cpu spot us-central1", "us-central1", CPU, Spot, 0.01},
		{"cpu on-demand europe-west1", "europe-west1", CPU, OnDemand, 0.04},
		{"missing region", "asia-east1", CPU, OnDemand, 0},
		{"missing tier", "us-central1", Memory, Spot, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pt.Lookup(tt.region, tt.rt, tt.tier)
			if math.Abs(got-tt.expected) > 1e-9 {
				t.Errorf("Lookup(%s, %s, %s) = %f, want %f",
					tt.region, tt.rt, tt.tier, got, tt.expected)
			}
		})
	}
}

func TestFromPricesEmpty(t *testing.T) {
	pt := FromPrices(nil)
	if len(pt) != 0 {
		t.Errorf("expected empty price table, got %d entries", len(pt))
	}
}

func TestFromPricesCPUConversion(t *testing.T) {
	// CPU prices from the billing API are per-mCPU-hour.
	// FromPrices must multiply by 1000 to get per-vCPU-hour.
	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.000035},
	}
	pt := FromPrices(prices)
	got := pt.Lookup("us-central1", CPU, OnDemand)
	// 0.000035 * 1000 = 0.035
	if math.Abs(got-0.035) > 1e-9 {
		t.Errorf("CPU price should be converted from mCPU to vCPU: got %f, want 0.035", got)
	}
}

func TestFromPricesMemoryPassthrough(t *testing.T) {
	// Memory prices are per-GB-hour and should NOT be multiplied.
	prices := []Price{
		{Region: "us-central1", ResourceType: Memory, Tier: OnDemand, UnitPrice: 0.004},
	}
	pt := FromPrices(prices)
	got := pt.Lookup("us-central1", Memory, OnDemand)
	if math.Abs(got-0.004) > 1e-9 {
		t.Errorf("Memory price should pass through unchanged: got %f, want 0.004", got)
	}
}

func TestFromPricesDuplicateLastWins(t *testing.T) {
	// If two prices have the same key, the last one wins.
	prices := []Price{
		{Region: "us-central1", ResourceType: Memory, Tier: OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: Memory, Tier: OnDemand, UnitPrice: 0.006},
	}
	pt := FromPrices(prices)
	got := pt.Lookup("us-central1", Memory, OnDemand)
	if math.Abs(got-0.006) > 1e-9 {
		t.Errorf("duplicate key should use last price: got %f, want 0.006", got)
	}
}

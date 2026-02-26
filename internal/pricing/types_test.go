package pricing

import "testing"

func TestPriceTableLookup(t *testing.T) {
	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: Memory, Tier: OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: CPU, Tier: Spot, UnitPrice: 0.01},
		{Region: "europe-west1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.04},
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
			if got != tt.expected {
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

package pricing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchComputePricesFromFixture(t *testing.T) {
	skus := []catalogSKU{
		{
			Description: "N2 Instance Core running in Americas",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 31611000}},
					},
				}},
			},
		},
		{
			Description: "N2 Instance Ram running in Americas",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 4237000}},
					},
				}},
			},
		},
		{
			Description: "Spot Preemptible N2 Instance Core running in Americas",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 7594000}},
					},
				}},
			},
		},
		{
			Description: "Spot Preemptible N2 Instance Ram running in Americas",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 1017000}},
					},
				}},
			},
		},
		{
			Description: "E2 Instance Core running in Americas",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 22152000}},
					},
				}},
			},
		},
		{
			// Non-matching SKU — should be ignored
			Description: "Storage PD Capacity",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 40000000}},
					},
				}},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := catalogSKUResponse{SKUs: skus}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewCatalogClient(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	prices, err := client.FetchComputePrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 5 matching SKUs (N2 CPU, N2 Mem, Spot N2 CPU, Spot N2 Mem, E2 CPU)
	if len(prices) != 5 {
		t.Fatalf("expected 5 compute prices, got %d: %+v", len(prices), prices)
	}

	cpt := FromComputePrices(prices)

	// N2 on-demand CPU: 0.031611 per vCPU-hour
	n2CPU := cpt.Lookup("us-central1", "n2", CPU, OnDemand)
	if !approxEqual(n2CPU, 0.031611, 1e-9) {
		t.Errorf("N2 CPU on-demand = %f, want 0.031611", n2CPU)
	}

	// N2 on-demand Memory: 0.004237 per GB-hour
	n2Mem := cpt.Lookup("us-central1", "n2", Memory, OnDemand)
	if !approxEqual(n2Mem, 0.004237, 1e-9) {
		t.Errorf("N2 Memory on-demand = %f, want 0.004237", n2Mem)
	}

	// N2 Spot CPU: 0.007594 per vCPU-hour
	n2SpotCPU := cpt.Lookup("us-central1", "n2", CPU, Spot)
	if !approxEqual(n2SpotCPU, 0.007594, 1e-9) {
		t.Errorf("N2 CPU spot = %f, want 0.007594", n2SpotCPU)
	}

	// E2 on-demand CPU: 0.022152 per vCPU-hour
	e2CPU := cpt.Lookup("us-central1", "e2", CPU, OnDemand)
	if !approxEqual(e2CPU, 0.022152, 1e-9) {
		t.Errorf("E2 CPU on-demand = %f, want 0.022152", e2CPU)
	}
}

func TestFetchComputePricesNoMatchingSKUs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := catalogSKUResponse{
			SKUs: []catalogSKU{
				{Description: "Storage PD Capacity"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewCatalogClient(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.FetchComputePrices(context.Background())
	if err == nil {
		t.Fatal("expected error for no matching Compute Engine SKUs")
	}
}

func TestExtractComputePricesMultipleRegions(t *testing.T) {
	sku := catalogSKU{
		Description: "N2 Instance Core running in Americas",
		GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1", "us-east1"}},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 31611000}},
				},
			}},
		},
	}

	prices := extractComputePrices(sku)
	if len(prices) != 2 {
		t.Fatalf("expected 2 prices (one per region), got %d", len(prices))
	}

	for _, p := range prices {
		if p.MachineFamily != "n2" {
			t.Errorf("expected family n2, got %s", p.MachineFamily)
		}
		if p.ResourceType != CPU {
			t.Errorf("expected CPU, got %s", p.ResourceType)
		}
		if p.Tier != OnDemand {
			t.Errorf("expected on-demand, got %s", p.Tier)
		}
	}
}

func TestExtractComputePricesServiceRegionsFallback(t *testing.T) {
	sku := catalogSKU{
		Description:    "E2 Instance Ram running in EMEA",
		GeoTaxonomy:    geoTaxonomy{Regions: nil},
		ServiceRegions: []string{"europe-west1"},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 4237000}},
				},
			}},
		},
	}

	prices := extractComputePrices(sku)
	if len(prices) != 1 {
		t.Fatalf("expected 1 price, got %d", len(prices))
	}
	if prices[0].Region != "europe-west1" {
		t.Errorf("expected region europe-west1, got %s", prices[0].Region)
	}
}

func TestExtractComputePricesNonMatchingSKU(t *testing.T) {
	sku := catalogSKU{
		Description: "Licensing Fee for NVIDIA L4",
		GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Units: "1", Nanos: 0}},
				},
			}},
		},
	}

	prices := extractComputePrices(sku)
	if len(prices) != 0 {
		t.Errorf("expected 0 prices for non-matching SKU, got %d", len(prices))
	}
}

func TestComputePriceTableLookupMissing(t *testing.T) {
	cpt := FromComputePrices(nil)
	price := cpt.Lookup("us-central1", "n2", CPU, OnDemand)
	if price != 0 {
		t.Errorf("expected 0 for empty table, got %f", price)
	}
}

func TestExtractComputePricesAllFamilies(t *testing.T) {
	families := []string{"N2", "E2", "N1", "C2", "C2D", "C3", "C3D", "C4", "C4A", "T2D", "T2A", "N2D", "N4", "M3", "A2", "A3", "G2", "H3", "X4", "Z3"}
	for _, fam := range families {
		sku := catalogSKU{
			Description: fam + " Instance Core running in Americas",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{UnitPrice: unitPrice{Nanos: 10000000}},
					},
				}},
			},
		}

		prices := extractComputePrices(sku)
		if len(prices) != 1 {
			t.Errorf("family %s: expected 1 price, got %d", fam, len(prices))
			continue
		}
		if prices[0].MachineFamily != strings.ToLower(fam) {
			t.Errorf("family %s: expected %s, got %s", fam, strings.ToLower(fam), prices[0].MachineFamily)
		}
	}
}

func TestExtractComputePricesSpotDetection(t *testing.T) {
	sku := catalogSKU{
		Description: "Spot Preemptible C3 Instance Ram running in Americas",
		GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 1000000}},
				},
			}},
		},
	}

	prices := extractComputePrices(sku)
	if len(prices) != 1 {
		t.Fatalf("expected 1 price, got %d", len(prices))
	}
	if prices[0].Tier != Spot {
		t.Errorf("expected Spot, got %s", prices[0].Tier)
	}
	if prices[0].MachineFamily != "c3" {
		t.Errorf("expected c3, got %s", prices[0].MachineFamily)
	}
	if prices[0].ResourceType != Memory {
		t.Errorf("expected Memory, got %s", prices[0].ResourceType)
	}
}

func TestExtractComputePricesArchQualifier(t *testing.T) {
	tests := []struct {
		desc   string
		family string
		tier   Tier
		rt     ResourceType
	}{
		{"T2D AMD Instance Core running in Americas", "t2d", OnDemand, CPU},
		{"Spot Preemptible T2D AMD Instance Core running in Americas", "t2d", Spot, CPU},
		{"T2D AMD Instance Ram running in Americas", "t2d", OnDemand, Memory},
		{"T2A Arm Instance Core running in Americas", "t2a", OnDemand, CPU},
		{"Spot Preemptible T2A Arm Instance Ram running in Americas", "t2a", Spot, Memory},
		{"N2D AMD Instance Core running in Americas", "n2d", OnDemand, CPU},
		{"Spot Preemptible N2D AMD Instance Core running in Americas", "n2d", Spot, CPU},
		{"C2D AMD Instance Ram running in EMEA", "c2d", OnDemand, Memory},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			sku := catalogSKU{
				Description: tt.desc,
				GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
				PricingInfo: []skuPricingInfo{
					{PricingExpression: pricingExpression{
						TieredRates: []tieredRate{
							{UnitPrice: unitPrice{Nanos: 10000000}},
						},
					}},
				},
			}

			prices := extractComputePrices(sku)
			if len(prices) != 1 {
				t.Fatalf("expected 1 price, got %d", len(prices))
			}
			if prices[0].MachineFamily != tt.family {
				t.Errorf("family = %s, want %s", prices[0].MachineFamily, tt.family)
			}
			if prices[0].Tier != tt.tier {
				t.Errorf("tier = %s, want %s", prices[0].Tier, tt.tier)
			}
			if prices[0].ResourceType != tt.rt {
				t.Errorf("resource = %s, want %s", prices[0].ResourceType, tt.rt)
			}
		})
	}
}

func TestExtractComputePricesRealCatalogDescriptions(t *testing.T) {
	// Descriptions as they appear in the real Cloud Billing catalog —
	// including shapes that don't carry the family token and qualifier words
	// beyond the AMD/Arm arch markers.
	tests := []struct {
		desc   string
		family string
		tier   Tier
		rt     ResourceType
	}{
		{"N1 Predefined Instance Core running in Americas", "n1", OnDemand, CPU},
		{"Compute optimized Core running in Americas", "c2", OnDemand, CPU},
		{"Spot Preemptible Compute optimized Ram running in Americas", "c2", Spot, Memory},
		{"Memory-optimized Instance Core running in Americas", "m1", OnDemand, CPU},
		{"Memory-optimized Instance Ram running in Americas", "m1", OnDemand, Memory},
		{"M3 Memory-optimized Instance Core running in Americas", "m3", OnDemand, CPU},
		{"C4D AMD Instance Core running in Americas", "c4d", OnDemand, CPU},
		{"M4 Instance Core running in Americas", "m4", OnDemand, CPU},
		{"H4D Instance Core running in Americas", "h4d", OnDemand, CPU},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			sku := catalogSKU{
				Description: tt.desc,
				GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
				PricingInfo: []skuPricingInfo{
					{PricingExpression: pricingExpression{
						TieredRates: []tieredRate{
							{UnitPrice: unitPrice{Nanos: 10000000}},
						},
					}},
				},
			}

			prices := extractComputePrices(sku)
			if len(prices) != 1 {
				t.Fatalf("expected 1 price, got %d", len(prices))
			}
			if prices[0].MachineFamily != tt.family {
				t.Errorf("family = %s, want %s", prices[0].MachineFamily, tt.family)
			}
			if prices[0].Tier != tt.tier {
				t.Errorf("tier = %s, want %s", prices[0].Tier, tt.tier)
			}
			if prices[0].ResourceType != tt.rt {
				t.Errorf("resource = %s, want %s", prices[0].ResourceType, tt.rt)
			}
		})
	}
}

func TestExtractComputePricesRejectsCustomAndSoleTenancy(t *testing.T) {
	// Custom and sole-tenancy SKUs have different pricing and must not be
	// conflated with (or clobber) the predefined family price.
	descs := []string{
		"N2 Custom Instance Core running in Americas",
		"Spot Preemptible N2 Custom Instance Ram running in Americas",
		"E2 Custom Instance Core running in Americas",
		"Custom Instance Core running in Americas",           // bare custom (N1)
		"Sole Tenancy Instance N2 Core running in Americas",  // sole tenancy
		"N1 Sole Tenancy Instance Core running in Americas",  // qualifier form
	}

	for _, desc := range descs {
		sku := catalogSKU{
			Description: desc,
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{UnitPrice: unitPrice{Nanos: 10000000}},
					},
				}},
			},
		}
		if prices := extractComputePrices(sku); len(prices) != 0 {
			t.Errorf("%q: expected no prices, got %+v", desc, prices)
		}
	}
}

func TestFromComputePricesFirstWins(t *testing.T) {
	// Duplicate keys must resolve deterministically to the first price seen
	// rather than whatever SKU happens to come last in the catalog pages.
	prices := []ComputePrice{
		{Region: "us-central1", MachineFamily: "n2", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.031},
		{Region: "us-central1", MachineFamily: "n2", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.099},
	}
	cpt := FromComputePrices(prices)
	if got := cpt.Lookup("us-central1", "n2", CPU, OnDemand); got != 0.031 {
		t.Errorf("Lookup = %f, want first price 0.031", got)
	}
}

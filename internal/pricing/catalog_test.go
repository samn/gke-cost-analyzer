package pricing

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func approxEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestFetchPricesFromFixture(t *testing.T) {
	skus := []catalogSKU{
		{
			// CPU prices from the API are per-mCPU-hour; Nanos: 35000 → $0.000035/mCPU-hr
			// After conversion: $0.035/vCPU-hr
			Description: "Autopilot Pod mCPU Requests",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 35000}},
					},
				}},
			},
		},
		{
			Description: "Autopilot Pod Memory Requests",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 4000000}},
					},
				}},
			},
		},
		{
			// Spot CPU: Nanos: 10000 → $0.00001/mCPU-hr → $0.01/vCPU-hr
			Description: "Autopilot Spot Pod mCPU Requests",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 10000}},
					},
				}},
			},
		},
		{
			Description: "Autopilot Spot Pod Memory Requests",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 1200000}},
					},
				}},
			},
		},
		{
			Description: "Some other Compute Engine SKU",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "1", Nanos: 0}},
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
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Expect 4 Autopilot prices (the non-Autopilot SKU should be filtered out)
	if len(prices) != 4 {
		t.Fatalf("expected 4 prices, got %d: %+v", len(prices), prices)
	}

	// Verify specific prices
	pt := FromPrices(prices)

	cpuOnDemand := pt.Lookup("us-central1", CPU, OnDemand)
	if !approxEqual(cpuOnDemand, 0.035, 1e-9) {
		t.Errorf("CPU on-demand price = %f, want 0.035", cpuOnDemand)
	}

	memOnDemand := pt.Lookup("us-central1", Memory, OnDemand)
	if !approxEqual(memOnDemand, 0.004, 1e-9) {
		t.Errorf("Memory on-demand price = %f, want 0.004", memOnDemand)
	}

	cpuSpot := pt.Lookup("us-central1", CPU, Spot)
	if !approxEqual(cpuSpot, 0.01, 1e-9) {
		t.Errorf("CPU spot price = %f, want 0.01", cpuSpot)
	}

	memSpot := pt.Lookup("us-central1", Memory, Spot)
	if !approxEqual(memSpot, 0.0012, 1e-9) {
		t.Errorf("Memory spot price = %f, want 0.0012", memSpot)
	}
}

func TestFetchPricesPagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp catalogSKUResponse
		if page == 0 {
			resp = catalogSKUResponse{
				SKUs: []catalogSKU{
					{
						Description: "Autopilot Pod mCPU Requests",
						GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
						PricingInfo: []skuPricingInfo{
							{PricingExpression: pricingExpression{
								TieredRates: []tieredRate{
									{UnitPrice: unitPrice{Nanos: 35000}},
								},
							}},
						},
					},
				},
				NextPageToken: "page2",
			}
			page++
		} else {
			resp = catalogSKUResponse{
				SKUs: []catalogSKU{
					{
						Description: "Autopilot Pod Memory Requests",
						GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
						PricingInfo: []skuPricingInfo{
							{PricingExpression: pricingExpression{
								TieredRates: []tieredRate{
									{UnitPrice: unitPrice{Nanos: 4000000}},
								},
							}},
						},
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewCatalogClient(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(prices) != 2 {
		t.Fatalf("expected 2 prices across pages, got %d", len(prices))
	}
}

func TestFetchPricesMultipleRegions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := catalogSKUResponse{
			SKUs: []catalogSKU{
				{
					Description: "Autopilot Pod mCPU Requests",
					GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1", "europe-west1"}},
					PricingInfo: []skuPricingInfo{
						{PricingExpression: pricingExpression{
							TieredRates: []tieredRate{
								{UnitPrice: unitPrice{Nanos: 35000}},
							},
						}},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewCatalogClient(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(prices) != 2 {
		t.Fatalf("expected 2 prices (one per region), got %d", len(prices))
	}

	pt := FromPrices(prices)
	if !approxEqual(pt.Lookup("us-central1", CPU, OnDemand), 0.035, 1e-9) {
		t.Errorf("missing us-central1 price, got %f", pt.Lookup("us-central1", CPU, OnDemand))
	}
	if !approxEqual(pt.Lookup("europe-west1", CPU, OnDemand), 0.035, 1e-9) {
		t.Errorf("missing europe-west1 price, got %f", pt.Lookup("europe-west1", CPU, OnDemand))
	}
}

func TestFetchPricesNoAutopilotSKUs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := catalogSKUResponse{
			SKUs: []catalogSKU{
				{Description: "Not an Autopilot SKU"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewCatalogClient(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.FetchPrices(context.Background())
	if err == nil {
		t.Fatal("expected error for no Autopilot SKUs")
	}
}

func TestFetchPricesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client, err := NewCatalogClient(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.FetchPrices(context.Background())
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestExtractAutopilotPricesServiceRegionsFallback(t *testing.T) {
	// When GeoTaxonomy.Regions is empty, fall back to ServiceRegions.
	sku := catalogSKU{
		Description:    "Autopilot Pod mCPU Requests",
		GeoTaxonomy:    geoTaxonomy{Regions: nil},
		ServiceRegions: []string{"us-west1", "us-east1"},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 35000}},
				},
			}},
		},
	}

	prices := extractAutopilotPrices(sku)
	if len(prices) != 2 {
		t.Fatalf("expected 2 prices (one per ServiceRegion), got %d", len(prices))
	}

	regions := map[string]bool{}
	for _, p := range prices {
		regions[p.Region] = true
	}
	if !regions["us-west1"] || !regions["us-east1"] {
		t.Errorf("expected us-west1 and us-east1, got %v", regions)
	}
}

func TestExtractAutopilotPricesSkipsZeroPrice(t *testing.T) {
	// A SKU with only zero-price tiered rates should produce no prices.
	sku := catalogSKU{
		Description: "Autopilot Pod mCPU Requests",
		GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{StartUsageAmount: 0, UnitPrice: unitPrice{Units: "0", Nanos: 0}},
				},
			}},
		},
	}

	prices := extractAutopilotPrices(sku)
	if len(prices) != 0 {
		t.Errorf("expected 0 prices for zero-rate SKU, got %d", len(prices))
	}
}

func TestExtractUnitPriceMultipleTiers(t *testing.T) {
	// The first non-zero tier should be returned.
	pi := skuPricingInfo{
		PricingExpression: pricingExpression{
			TieredRates: []tieredRate{
				{StartUsageAmount: 0, UnitPrice: unitPrice{Units: "0", Nanos: 0}},
				{StartUsageAmount: 1, UnitPrice: unitPrice{Units: "0", Nanos: 50000}},
			},
		},
	}

	price := extractUnitPrice(pi)
	if !approxEqual(price, 0.00005, 1e-9) {
		t.Errorf("expected 0.00005, got %f", price)
	}
}

func TestExtractUnitPriceNoTiers(t *testing.T) {
	pi := skuPricingInfo{
		PricingExpression: pricingExpression{
			TieredRates: nil,
		},
	}

	price := extractUnitPrice(pi)
	if price != 0 {
		t.Errorf("expected 0 for empty tiers, got %f", price)
	}
}

func TestParseUnitPrice(t *testing.T) {
	tests := []struct {
		name     string
		units    string
		nanos    int64
		expected float64
	}{
		{"zero", "0", 0, 0},
		{"units only", "1", 0, 1.0},
		{"nanos only", "0", 35000000, 0.035},
		{"both", "1", 500000000, 1.5},
		{"empty units", "", 4000000, 0.004},
		{"invalid units falls back to nanos only", "not-a-number", 4000000, 0.004},
		{"large units", "100", 0, 100.0},
		{"small nanos", "0", 1, 1e-9},
		{"max nanos", "0", 999999999, 0.999999999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUnitPrice(tt.units, tt.nanos)
			if !approxEqual(got, tt.expected, 1e-12) {
				t.Errorf("parseUnitPrice(%q, %d) = %f, want %f", tt.units, tt.nanos, got, tt.expected)
			}
		})
	}
}

func TestExtractAutopilotPricesNonMatchingSKU(t *testing.T) {
	// An SKU that doesn't match any Autopilot substring should return nil.
	sku := catalogSKU{
		Description: "Some totally unrelated Compute Engine SKU",
		GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Units: "1", Nanos: 0}},
				},
			}},
		},
	}

	prices := extractAutopilotPrices(sku)
	if len(prices) != 0 {
		t.Errorf("expected 0 prices for non-matching SKU, got %d", len(prices))
	}
}

func TestExtractAutopilotPricesMultiplePricingInfos(t *testing.T) {
	// SKU with multiple PricingInfo entries — each should produce a price.
	sku := catalogSKU{
		Description: "Autopilot Pod mCPU Requests",
		GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 35000}},
				},
			}},
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 40000}},
				},
			}},
		},
	}

	prices := extractAutopilotPrices(sku)
	// Each PricingInfo × each region = 2 prices
	if len(prices) != 2 {
		t.Fatalf("expected 2 prices (one per PricingInfo), got %d", len(prices))
	}
	if prices[0].ResourceType != CPU || prices[0].Tier != OnDemand {
		t.Errorf("first price should be CPU/OnDemand, got %s/%s", prices[0].ResourceType, prices[0].Tier)
	}
}

func TestExtractAutopilotPricesEmptyRegionsAndServiceRegions(t *testing.T) {
	// SKU with no regions at all should produce no prices.
	sku := catalogSKU{
		Description: "Autopilot Pod mCPU Requests",
		GeoTaxonomy: geoTaxonomy{Regions: nil},
		PricingInfo: []skuPricingInfo{
			{PricingExpression: pricingExpression{
				TieredRates: []tieredRate{
					{UnitPrice: unitPrice{Nanos: 35000}},
				},
			}},
		},
	}

	prices := extractAutopilotPrices(sku)
	if len(prices) != 0 {
		t.Errorf("expected 0 prices when both Regions and ServiceRegions are empty, got %d", len(prices))
	}
}

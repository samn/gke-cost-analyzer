package pricing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPricesFromFixture(t *testing.T) {
	skus := []catalogSKU{
		{
			Description: "Autopilot Pod mCPU Requests",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 35000000}},
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
			Description: "Autopilot Spot Pod mCPU Requests",
			GeoTaxonomy: geoTaxonomy{Regions: []string{"us-central1"}},
			PricingInfo: []skuPricingInfo{
				{PricingExpression: pricingExpression{
					TieredRates: []tieredRate{
						{StartUsageAmount: 0, UnitPrice: unitPrice{CurrencyCode: "USD", Units: "0", Nanos: 10000000}},
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
	if cpuOnDemand != 0.035 {
		t.Errorf("CPU on-demand price = %f, want 0.035", cpuOnDemand)
	}

	memOnDemand := pt.Lookup("us-central1", Memory, OnDemand)
	if memOnDemand != 0.004 {
		t.Errorf("Memory on-demand price = %f, want 0.004", memOnDemand)
	}

	cpuSpot := pt.Lookup("us-central1", CPU, Spot)
	if cpuSpot != 0.01 {
		t.Errorf("CPU spot price = %f, want 0.01", cpuSpot)
	}

	memSpot := pt.Lookup("us-central1", Memory, Spot)
	if memSpot != 0.0012 {
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
									{UnitPrice: unitPrice{Nanos: 35000000}},
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
								{UnitPrice: unitPrice{Nanos: 35000000}},
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
	if pt.Lookup("us-central1", CPU, OnDemand) != 0.035 {
		t.Error("missing us-central1 price")
	}
	if pt.Lookup("europe-west1", CPU, OnDemand) != 0.035 {
		t.Error("missing europe-west1 price")
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUnitPrice(tt.units, tt.nanos)
			if got != tt.expected {
				t.Errorf("parseUnitPrice(%q, %d) = %f, want %f", tt.units, tt.nanos, got, tt.expected)
			}
		})
	}
}

package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// Compute Engine service ID in the Cloud Billing Catalog
	computeEngineServiceID = "6F81-5844-456A"
	billingCatalogBaseURL  = "https://cloudbilling.googleapis.com/v1"
)

// SKU description substrings for matching Autopilot Pod-level pricing.
var autopilotSKUMatchers = []skuMatcher{
	{substr: "Autopilot Pod Minimum", resource: CPU, tier: OnDemand},
	{substr: "Autopilot Pod mCPU", resource: CPU, tier: OnDemand},
	{substr: "Autopilot Pod Memory", resource: Memory, tier: OnDemand},
	{substr: "Autopilot Spot Pod Minimum", resource: CPU, tier: Spot},
	{substr: "Autopilot Spot Pod mCPU", resource: CPU, tier: Spot},
	{substr: "Autopilot Spot Pod Memory", resource: Memory, tier: Spot},
}

type skuMatcher struct {
	substr   string
	resource ResourceType
	tier     Tier
}

// CatalogClient fetches pricing data from the Cloud Billing Catalog API.
type CatalogClient struct {
	httpClient *http.Client
	baseURL    string
}

// CatalogOption configures a CatalogClient.
type CatalogOption func(*CatalogClient)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) CatalogOption {
	return func(cc *CatalogClient) { cc.httpClient = c }
}

// WithBaseURL overrides the billing API base URL (for testing).
func WithBaseURL(url string) CatalogOption {
	return func(cc *CatalogClient) { cc.baseURL = url }
}

// NewCatalogClient creates a new CatalogClient.
func NewCatalogClient(opts ...CatalogOption) *CatalogClient {
	cc := &CatalogClient{
		httpClient: http.DefaultClient,
		baseURL:    billingCatalogBaseURL,
	}
	for _, opt := range opts {
		opt(cc)
	}
	return cc
}

// FetchPrices fetches Autopilot pod pricing from the Cloud Billing Catalog API.
// The billing catalog is a public API that doesn't require authentication.
func (cc *CatalogClient) FetchPrices(ctx context.Context) ([]Price, error) {
	var allPrices []Price
	pageToken := ""

	for {
		skus, nextToken, err := cc.fetchSKUPage(ctx, pageToken)
		if err != nil {
			return nil, err
		}

		for _, sku := range skus {
			prices := extractAutopilotPrices(sku)
			allPrices = append(allPrices, prices...)
		}

		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	if len(allPrices) == 0 {
		return nil, fmt.Errorf("no Autopilot pricing SKUs found in billing catalog")
	}

	return allPrices, nil
}

// catalogSKUResponse represents the API response for listing SKUs.
type catalogSKUResponse struct {
	SKUs          []catalogSKU `json:"skus"`
	NextPageToken string       `json:"nextPageToken"`
}

// catalogSKU represents a single SKU from the billing catalog.
type catalogSKU struct {
	Description    string           `json:"description"`
	Category       skuCategory      `json:"category"`
	ServiceRegions []string         `json:"serviceRegions"`
	PricingInfo    []skuPricingInfo `json:"pricingInfo"`
	GeoTaxonomy    geoTaxonomy      `json:"geoTaxonomy"`
}

type skuCategory struct {
	ServiceDisplayName string `json:"serviceDisplayName"`
	ResourceFamily     string `json:"resourceFamily"`
	ResourceGroup      string `json:"resourceGroup"`
	UsageType          string `json:"usageType"`
}

type geoTaxonomy struct {
	Regions []string `json:"regions"`
}

type skuPricingInfo struct {
	PricingExpression pricingExpression `json:"pricingExpression"`
}

type pricingExpression struct {
	UsageUnit            string       `json:"usageUnit"`
	TieredRates          []tieredRate `json:"tieredRates"`
	UsageUnitDescription string       `json:"usageUnitDescription"`
}

type tieredRate struct {
	StartUsageAmount float64   `json:"startUsageAmount"`
	UnitPrice        unitPrice `json:"unitPrice"`
}

type unitPrice struct {
	CurrencyCode string `json:"currencyCode"`
	Units        string `json:"units"`
	Nanos        int64  `json:"nanos"`
}

func (cc *CatalogClient) fetchSKUPage(ctx context.Context, pageToken string) ([]catalogSKU, string, error) {
	reqURL := fmt.Sprintf("%s/services/%s/skus?pageSize=5000", cc.baseURL, computeEngineServiceID)
	if pageToken != "" {
		reqURL += "&pageToken=" + url.QueryEscape(pageToken)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := cc.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching SKUs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("billing catalog API returned status %d", resp.StatusCode)
	}

	var result catalogSKUResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("decoding SKU response: %w", err)
	}

	return result.SKUs, result.NextPageToken, nil
}

// extractAutopilotPrices extracts Autopilot Pod pricing from a SKU.
func extractAutopilotPrices(sku catalogSKU) []Price {
	var matcher *skuMatcher
	for i := range autopilotSKUMatchers {
		if strings.Contains(sku.Description, autopilotSKUMatchers[i].substr) {
			matcher = &autopilotSKUMatchers[i]
			break
		}
	}
	if matcher == nil {
		return nil
	}

	regions := sku.GeoTaxonomy.Regions
	if len(regions) == 0 {
		regions = sku.ServiceRegions
	}

	var prices []Price
	for _, region := range regions {
		for _, pi := range sku.PricingInfo {
			unitPrice := extractUnitPrice(pi)
			if unitPrice > 0 {
				prices = append(prices, Price{
					Region:       region,
					ResourceType: matcher.resource,
					Tier:         matcher.tier,
					UnitPrice:    unitPrice,
				})
			}
		}
	}
	return prices
}

// extractUnitPrice gets the hourly unit price from pricing info.
func extractUnitPrice(pi skuPricingInfo) float64 {
	for _, rate := range pi.PricingExpression.TieredRates {
		up := rate.UnitPrice
		price := parseUnitPrice(up.Units, up.Nanos)
		if price > 0 {
			return price
		}
	}
	return 0
}

// parseUnitPrice combines the units and nanos fields into a float64 price.
func parseUnitPrice(units string, nanos int64) float64 {
	var u float64
	if units != "" && units != "0" {
		u, _ = strconv.ParseFloat(units, 64)
	}
	return u + float64(nanos)/1e9
}

package pricing

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	// Compute Engine service ID in the Cloud Billing Catalog
	computeEngineServiceID = "6F81-5844-456A"
)

// computeSKURegex matches Compute Engine instance Core/Ram SKUs.
// Examples:
//   - "N2 Instance Core running in Americas"
//   - "Spot Preemptible N2 Instance Core running in Americas"
//   - "E2 Instance Ram running in EMEA"
var computeSKURegex = regexp.MustCompile(
	`^(Spot Preemptible )?(N2|E2|N1|C3|C3D|T2D|T2A|N2D|N4|C4|M3|A2|G2) Instance (Core|Ram) running in`,
)

// ComputePrice represents the unit price for a specific machine family resource in a region and tier.
type ComputePrice struct {
	Region        string       `json:"region"`
	MachineFamily string       `json:"machine_family"`
	ResourceType  ResourceType `json:"resource_type"`
	Tier          Tier         `json:"tier"`
	UnitPrice     float64      `json:"unit_price"` // per vCPU-hour or per GB-hour
}

// ComputePriceKey uniquely identifies a compute price lookup.
type ComputePriceKey struct {
	Region        string
	MachineFamily string
	ResourceType  ResourceType
	Tier          Tier
}

// ComputePriceTable maps ComputePriceKey to unit prices for fast lookups.
type ComputePriceTable map[ComputePriceKey]float64

// Lookup returns the unit price for the given key, or 0 if not found.
func (cpt ComputePriceTable) Lookup(region, family string, rt ResourceType, tier Tier) float64 {
	return cpt[ComputePriceKey{Region: region, MachineFamily: family, ResourceType: rt, Tier: tier}]
}

// FromComputePrices builds a ComputePriceTable from a slice of ComputePrice values.
// Unlike Autopilot (per-mCPU), Compute Engine CPU prices are already per-vCPU-hour —
// no conversion is needed.
func FromComputePrices(prices []ComputePrice) ComputePriceTable {
	cpt := make(ComputePriceTable, len(prices))
	for _, p := range prices {
		cpt[ComputePriceKey{
			Region:        p.Region,
			MachineFamily: p.MachineFamily,
			ResourceType:  p.ResourceType,
			Tier:          p.Tier,
		}] = p.UnitPrice
	}
	return cpt
}

// FetchComputePrices fetches Compute Engine instance pricing from the Cloud Billing Catalog API.
func (cc *CatalogClient) FetchComputePrices(ctx context.Context) ([]ComputePrice, error) {
	var allPrices []ComputePrice
	pageToken := ""

	for {
		skus, nextToken, err := cc.fetchSKUPageForService(ctx, computeEngineServiceID, pageToken)
		if err != nil {
			return nil, err
		}

		for _, sku := range skus {
			prices := extractComputePrices(sku)
			allPrices = append(allPrices, prices...)
		}

		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	if len(allPrices) == 0 {
		return nil, fmt.Errorf("no Compute Engine pricing SKUs found in billing catalog")
	}

	return allPrices, nil
}

// fetchSKUPageForService fetches a page of SKUs for the given service ID.
func (cc *CatalogClient) fetchSKUPageForService(ctx context.Context, serviceID, pageToken string) ([]catalogSKU, string, error) {
	return cc.fetchSKUPageWithServiceID(ctx, serviceID, pageToken)
}

// extractComputePrices extracts Compute Engine instance pricing from a SKU.
func extractComputePrices(sku catalogSKU) []ComputePrice {
	matches := computeSKURegex.FindStringSubmatch(sku.Description)
	if matches == nil {
		return nil
	}

	isSpot := matches[1] != ""
	familyUpper := matches[2]
	resourceKind := matches[3]

	family := strings.ToLower(familyUpper)

	var rt ResourceType
	switch resourceKind {
	case "Core":
		rt = CPU
	case "Ram":
		rt = Memory
	default:
		return nil
	}

	tier := OnDemand
	if isSpot {
		tier = Spot
	}

	regions := sku.GeoTaxonomy.Regions
	if len(regions) == 0 {
		regions = sku.ServiceRegions
	}

	var prices []ComputePrice
	for _, region := range regions {
		for _, pi := range sku.PricingInfo {
			unitPrice := extractUnitPrice(pi)
			if unitPrice > 0 {
				prices = append(prices, ComputePrice{
					Region:        region,
					MachineFamily: family,
					ResourceType:  rt,
					Tier:          tier,
					UnitPrice:     unitPrice,
				})
			}
		}
	}
	return prices
}

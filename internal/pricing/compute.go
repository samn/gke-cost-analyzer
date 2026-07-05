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
// Some families include a qualifier between the family name and "Instance":
// an architecture marker ("AMD", "Arm"), "Predefined" (N1), or a category
// word ("Memory-optimized" for M3). Qualifiers are captured so that variant
// SKUs with different pricing ("Custom", "Sole Tenancy") can be rejected —
// otherwise e.g. "N2 Custom Instance Core" would clobber the plain N2 price.
// Examples:
//   - "N2 Instance Core running in Americas"
//   - "Spot Preemptible N2 Instance Core running in Americas"
//   - "E2 Instance Ram running in EMEA"
//   - "T2D AMD Instance Core running in Americas"
//   - "N1 Predefined Instance Core running in Americas"
//   - "M3 Memory-optimized Instance Core running in Americas"
var computeSKURegex = regexp.MustCompile(
	`^(Spot Preemptible )?(N2|E2|N1|C2|C2D|C3|C3D|C4|C4A|C4D|T2D|T2A|N2D|N4|M1|M2|M3|M4|A2|A3|G2|H3|H4D|X4|Z3)(?: ([A-Za-z-]+(?: [A-Za-z-]+)*?))? Instance (Core|Ram) running in`,
)

// computeSKUAltRegex matches families whose SKU descriptions don't carry the
// family token (older catalog naming).
var computeSKUAltRegex = regexp.MustCompile(
	`^(Spot Preemptible )?(Compute optimized|Memory-optimized Instance) (Core|Ram) running in`,
)

// altSKUFamilies maps alternate SKU description prefixes to machine families.
// M2 is billed as M1 plus an "Upgrade Premium" SKU; the base rate maps to m1.
var altSKUFamilies = map[string]string{
	"Compute optimized":         "c2",
	"Memory-optimized Instance": "m1",
}

// rejectedSKUQualifiers are variant qualifiers with pricing that differs from
// the plain family SKU; matching them would corrupt the family price.
var rejectedSKUQualifiers = map[string]bool{
	"Custom":       true,
	"Sole Tenancy": true,
}

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
// no conversion is needed. Duplicate keys keep the first price seen so the
// result doesn't depend on catalog page ordering.
func FromComputePrices(prices []ComputePrice) ComputePriceTable {
	cpt := make(ComputePriceTable, len(prices))
	for _, p := range prices {
		key := ComputePriceKey{
			Region:        p.Region,
			MachineFamily: p.MachineFamily,
			ResourceType:  p.ResourceType,
			Tier:          p.Tier,
		}
		if _, exists := cpt[key]; !exists {
			cpt[key] = p.UnitPrice
		}
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
	var isSpot bool
	var family, resourceKind string

	if matches := computeSKURegex.FindStringSubmatch(sku.Description); matches != nil {
		if rejectedSKUQualifiers[matches[3]] {
			return nil
		}
		isSpot = matches[1] != ""
		family = strings.ToLower(matches[2])
		resourceKind = matches[4]
	} else if matches := computeSKUAltRegex.FindStringSubmatch(sku.Description); matches != nil {
		isSpot = matches[1] != ""
		family = altSKUFamilies[matches[2]]
		resourceKind = matches[3]
	} else {
		return nil
	}

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

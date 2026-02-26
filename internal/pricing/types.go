// Package pricing provides GKE Autopilot pricing data from the Cloud Billing Catalog API.
package pricing

// ResourceType represents a billable resource type.
type ResourceType string

// Resource type constants.
const (
	CPU    ResourceType = "cpu"
	Memory ResourceType = "memory"
)

// Tier represents the pricing tier for a resource.
type Tier string

// Pricing tier constants.
const (
	OnDemand Tier = "on-demand"
	Spot     Tier = "spot"
)

// Price represents the unit price for a specific resource in a region and tier.
type Price struct {
	Region       string       `json:"region"`
	ResourceType ResourceType `json:"resource_type"`
	Tier         Tier         `json:"tier"`
	// UnitPrice is the hourly cost: per-vCPU-hour for CPU, per-GB-hour for memory.
	UnitPrice float64 `json:"unit_price"`
}

// PriceKey uniquely identifies a price lookup.
type PriceKey struct {
	Region       string
	ResourceType ResourceType
	Tier         Tier
}

// PriceTable maps PriceKey to unit prices for fast lookups.
type PriceTable map[PriceKey]float64

// Lookup returns the unit price for the given key, or 0 if not found.
func (pt PriceTable) Lookup(region string, rt ResourceType, tier Tier) float64 {
	return pt[PriceKey{Region: region, ResourceType: rt, Tier: tier}]
}

// FromPrices builds a PriceTable from a slice of Price values.
func FromPrices(prices []Price) PriceTable {
	pt := make(PriceTable, len(prices))
	for _, p := range prices {
		pt[PriceKey{Region: p.Region, ResourceType: p.ResourceType, Tier: p.Tier}] = p.UnitPrice
	}
	return pt
}

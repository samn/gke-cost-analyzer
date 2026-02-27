package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

// podLister is an interface for listing pods, enabling testing without a real cluster.
type podLister interface {
	ListPods(ctx context.Context) ([]kube.PodInfo, error)
}

func labelConfig() cost.LabelConfig {
	return cost.LabelConfig{
		TeamLabel:     teamLabel,
		WorkloadLabel: workloadLabel,
		SubtypeLabel:  subtypeLabel,
	}
}

func loadPrices(ctx context.Context) ([]pricing.Price, error) {
	cache, err := pricing.NewCache()
	if err != nil {
		return nil, fmt.Errorf("creating price cache: %w", err)
	}

	cached, err := cache.Load()
	if err != nil {
		return nil, fmt.Errorf("loading cached prices: %w", err)
	}
	if cached != nil {
		return cached.Prices, nil
	}

	// Fetch from API
	client, err := pricing.NewCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("creating catalog client: %w", err)
	}
	prices, err := client.FetchPrices(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching prices: %w", err)
	}

	if err := cache.Save(prices); err != nil {
		// Log but don't fail — prices are still usable
		fmt.Fprintf(os.Stderr, "Warning: failed to cache prices: %v\n", err)
	}

	return prices, nil
}

func newPodLister() (*kube.PodLister, error) {
	var opts []kube.PodListerOption
	if namespace != "" {
		opts = append(opts, kube.WithNamespace(namespace))
	}
	if len(excludeNamespaces) > 0 {
		opts = append(opts, kube.WithExcludeNamespaces(excludeNamespaces))
	}
	return kube.NewPodLister(opts...)
}

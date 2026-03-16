package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/samn/autopilot-cost-analyzer/internal/prometheus"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
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

func clusterMode() kube.ClusterMode {
	switch mode {
	case "autopilot":
		return kube.ClusterModeAutopilot
	case "standard":
		return kube.ClusterModeStandard
	default:
		return kube.ClusterModeAll
	}
}

func newPodLister() (*kube.PodLister, error) {
	var opts []kube.PodListerOption
	if namespace != "" {
		opts = append(opts, kube.WithNamespace(namespace))
	}
	if len(excludeNamespaces) > 0 {
		opts = append(opts, kube.WithExcludeNamespaces(excludeNamespaces))
	}
	opts = append(opts, kube.WithClusterMode(clusterMode()))
	return kube.NewPodLister(opts...)
}

func newNodeLister() (*kube.NodeLister, error) {
	return kube.NewNodeLister()
}

func loadComputePrices(ctx context.Context) ([]pricing.ComputePrice, error) {
	cache, err := pricing.NewCache(pricing.WithCacheFileName("compute_prices.json"))
	if err != nil {
		return nil, fmt.Errorf("creating compute price cache: %w", err)
	}

	cached, err := cache.LoadComputePrices()
	if err != nil {
		return nil, fmt.Errorf("loading cached compute prices: %w", err)
	}
	if cached != nil {
		return cached.Prices, nil
	}

	client, err := pricing.NewCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("creating catalog client: %w", err)
	}
	prices, err := client.FetchComputePrices(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching compute prices: %w", err)
	}

	if err := cache.SaveComputePrices(prices); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cache compute prices: %v\n", err)
	}

	return prices, nil
}

func needsStandard() bool {
	return mode == "standard" || mode == "all"
}

func needsAutopilot() bool {
	return mode == "autopilot" || mode == "all"
}

// gcpHTTPClientFn is the function used to create GCP-authenticated HTTP clients.
// Overridable for testing.
var gcpHTTPClientFn = defaultGCPHTTPClient

func defaultGCPHTTPClient(ctx context.Context, scopes ...string) (*http.Client, error) {
	ts, err := google.DefaultTokenSource(ctx, scopes...)
	if err != nil {
		return nil, fmt.Errorf("getting default credentials: %w", err)
	}
	return oauth2.NewClient(ctx, ts), nil
}

// newPromClient creates a Prometheus client based on the configuration:
//   - If --prometheus-url is set, use that URL with a plain HTTP client.
//   - Otherwise, if a GCP project ID is available, default to Google Cloud
//     Managed Service for Prometheus (GMP) with OAuth2 authentication.
//   - If neither, return nil (no utilization metrics).
func newPromClient(ctx context.Context) (*prometheus.Client, error) {
	if prometheusURL != "" {
		fmt.Printf("Fetching utilization metrics from %s\n", prometheusURL)
		return prometheus.NewClient(prometheusURL), nil
	}

	// Auto-default to GMP when project ID is available.
	if project == "" {
		return nil, nil
	}

	gmpURL := prometheus.GMPBaseURL(project)
	httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/monitoring.read")
	if err != nil {
		return nil, fmt.Errorf("creating monitoring credentials: %w", err)
	}

	fmt.Printf("Fetching utilization metrics from GCP Managed Prometheus (project %s)\n", project)
	return prometheus.NewClient(gmpURL, prometheus.WithHTTPClient(httpClient), prometheus.WithGMPSystemMetrics()), nil
}

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/cost"
	"github.com/samn/gke-cost-analyzer/internal/kube"
	"github.com/samn/gke-cost-analyzer/internal/pricing"
	"github.com/samn/gke-cost-analyzer/internal/prometheus"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// shutdownSignals are the signals that trigger graceful shutdown. SIGTERM is
// what Kubernetes (and Docker) send on pod termination; SIGINT covers Ctrl-C.
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// usageError marks operator input mistakes (missing/invalid flags or
// arguments) so main() can skip reporting them to Sentry.
type usageError struct{ error }

// usageErrorf builds a usage-classified error.
func usageErrorf(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

// IsUsageError reports whether err is an operator input mistake rather than
// an application failure.
func IsUsageError(err error) bool {
	var ue usageError
	return errors.As(err, &ue)
}

// usageArgs wraps a cobra positional-args validator so its failures (e.g.
// a missing argument) classify as usage errors rather than being reported
// to Sentry as application errors.
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// bigQueryProject returns the project owning the BigQuery dataset: the explicit
// --bigquery-project-id, else the auto-detected environment project.
func bigQueryProject() string {
	if bigqueryProjectID != "" {
		return bigqueryProjectID
	}
	return detectedProject
}

// prometheusProject returns the project queried for GCP Managed Prometheus
// metrics: the explicit --prometheus-project-id, else the auto-detected
// environment project.
func prometheusProject() string {
	if prometheusProjectID != "" {
		return prometheusProjectID
	}
	return detectedProject
}

// bqIdentifierRegex matches valid BigQuery project/dataset/table identifiers
// (projects also allow hyphens). Mirrors the validation in the bigquery reader.
var bqIdentifierRegex = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseTableRef resolves the effective BigQuery project, dataset, and table from
// a --table value that may be dot-qualified, falling back to defaultProject and
// defaultDataset for omitted parts. Accepts:
//
//	table                    -> (defaultProject, defaultDataset, table)
//	dataset.table            -> (defaultProject, dataset, table)
//	project.dataset.table    -> (project, dataset, table)
//
// defaultProject may be empty (unset); the caller enforces the required-project
// check. Every part supplied in the value must be a valid identifier.
func parseTableRef(table, defaultProject, defaultDataset string) (project, dataset, tbl string, err error) {
	parts := strings.Split(table, ".")
	if len(parts) > 3 {
		return "", "", "", usageErrorf("invalid --table %q: use table, dataset.table, or project.dataset.table", table)
	}
	for _, p := range parts {
		if !bqIdentifierRegex.MatchString(p) {
			return "", "", "", usageErrorf("invalid --table %q: %q is not a valid BigQuery identifier", table, p)
		}
	}
	switch len(parts) {
	case 1:
		return defaultProject, defaultDataset, parts[0], nil
	case 2:
		return defaultProject, parts[0], parts[1], nil
	default:
		return parts[0], parts[1], parts[2], nil
	}
}

// gcpHTTPTimeout bounds each GCP API request so a hung backend cannot wedge
// a daemon loop indefinitely.
const gcpHTTPTimeout = 30 * time.Second

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

// listNamespace decides where the --namespace filter is applied. Standard-mode
// cost attribution divides each node's cost by the total requests of the pods
// on it, so the API listing must stay cluster-wide and the namespace filter is
// applied to pod costs after calculation. Autopilot costs are per-pod, so
// API-side filtering is safe and cheaper there.
func listNamespace() (apiNS, postFilterNS string) {
	if namespace != "" && needsStandard() {
		return "", namespace
	}
	return namespace, ""
}

// newPodLister builds a pod lister restricted to apiNS at the Kubernetes API
// level (empty = cluster-wide). Cost-computing commands must pass the apiNS
// from listNamespace(); commands that don't compute costs (unmatched-pods)
// can filter API-side unconditionally.
func newPodLister(apiNS string) (*kube.PodLister, error) {
	var opts []kube.PodListerOption
	if apiNS != "" {
		opts = append(opts, kube.WithNamespace(apiNS))
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
	client := oauth2.NewClient(ctx, ts)
	client.Timeout = gcpHTTPTimeout
	return client, nil
}

// newPromClient creates a Prometheus client based on the configuration:
//   - If --prometheus-url is set, use that URL with a plain HTTP client.
//   - Otherwise, if a Prometheus project ID is available (via
//     --prometheus-project-id or auto-detection), default to Google Cloud
//     Managed Service for Prometheus (GMP) with OAuth2 authentication.
//   - If neither, return nil (no utilization metrics).
func newPromClient(ctx context.Context) (*prometheus.Client, error) {
	if prometheusURL != "" {
		fmt.Printf("Fetching utilization metrics from %s\n", prometheusURL)
		return prometheus.NewClient(prometheusURL), nil
	}

	// Auto-default to GMP when a project ID is available.
	p := prometheusProject()
	if p == "" {
		return nil, nil
	}

	gmpURL := prometheus.GMPBaseURL(p)
	httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/monitoring.read")
	if err != nil {
		return nil, fmt.Errorf("creating monitoring credentials: %w", err)
	}

	fmt.Printf("Fetching utilization metrics from GCP Managed Prometheus (project %s)\n", p)
	return prometheus.NewClient(gmpURL, prometheus.WithHTTPClient(httpClient), prometheus.WithGMPSystemMetrics()), nil
}

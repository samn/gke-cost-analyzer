// Package prometheus queries CPU and memory utilization metrics from a
// Prometheus-compatible API (e.g. Google Cloud Managed Service for Prometheus).
package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

// GMPBaseURL returns the Google Cloud Managed Prometheus query API base URL
// for the given project ID.
func GMPBaseURL(projectID string) string {
	return fmt.Sprintf("https://monitoring.googleapis.com/v1/projects/%s/location/global/prometheus", projectID)
}

// PodKey identifies a pod by namespace and name.
type PodKey struct {
	Namespace string
	Pod       string
}

// PodUsage holds raw resource usage values for a single pod.
type PodUsage struct {
	CPUCores    float64 // CPU usage in cores (vCPU)
	MemoryBytes float64 // Memory working set in bytes
}

// Client queries a Prometheus HTTP API for container utilization metrics.
type Client struct {
	httpClient       *http.Client
	baseURL          string
	gmpSystemMetrics bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) { cl.httpClient = c }
}

// WithGMPSystemMetrics configures the client to query GKE system metrics
// (automatically collected by GKE) instead of standard Prometheus metric names.
// This should be used when querying GCP Managed Prometheus without managed
// collection enabled.
func WithGMPSystemMetrics() ClientOption {
	return func(cl *Client) { cl.gmpSystemMetrics = true }
}

// NewClient creates a Prometheus API client.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		baseURL:    baseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// queryConfig holds PromQL queries and label names for a specific backend.
type queryConfig struct {
	cpuQuery string
	memQuery string
	nsLabel  string
	podLabel string
}

// Standard Prometheus queries (self-hosted or GMP with managed collection).
var standardQueries = queryConfig{
	cpuQuery: `sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`,
	memQuery: `sum by (namespace, pod) (container_memory_working_set_bytes{container!="",container!="POD"})`,
	nsLabel:  "namespace",
	podLabel: "pod",
}

// GMP system metrics queries (auto-collected by GKE, no managed collection required).
// Uses Cloud Monitoring metric names exposed via PromQL naming convention.
// Memory is filtered to non-evictable (closest equivalent to working_set_bytes).
var gmpSystemQueries = queryConfig{
	cpuQuery: `sum by (namespace_name, pod_name) (rate(kubernetes_io:container_cpu_core_usage_time[5m]))`,
	memQuery: `sum by (namespace_name, pod_name) (kubernetes_io:container_memory_used_bytes{memory_type="non-evictable"})`,
	nsLabel:  "namespace_name",
	podLabel: "pod_name",
}

// queries returns the appropriate query config for this client's mode.
func (c *Client) queries() queryConfig {
	if c.gmpSystemMetrics {
		return gmpSystemQueries
	}
	return standardQueries
}

// FetchUsage queries Prometheus for CPU and memory usage of all pods and
// returns a map of pod key to usage. Pods missing from either query get zero
// for the missing metric.
func (c *Client) FetchUsage(ctx context.Context) (map[PodKey]PodUsage, error) {
	q := c.queries()

	cpuResults, err := c.instantQuery(ctx, q.cpuQuery, q.nsLabel, q.podLabel)
	if err != nil {
		return nil, fmt.Errorf("querying CPU usage: %w", err)
	}

	memResults, err := c.instantQuery(ctx, q.memQuery, q.nsLabel, q.podLabel)
	if err != nil {
		return nil, fmt.Errorf("querying memory usage: %w", err)
	}

	usage := make(map[PodKey]PodUsage)

	for key, val := range cpuResults {
		u := usage[key]
		u.CPUCores = val
		usage[key] = u
	}

	for key, val := range memResults {
		u := usage[key]
		u.MemoryBytes = val
		usage[key] = u
	}

	return usage, nil
}

// instantQuery runs a Prometheus instant query and returns results keyed by
// (namespace, pod). The nsLabel and podLabel parameters specify which metric
// labels to use for the pod key (e.g. "namespace"/"pod" for standard
// Prometheus, "namespace_name"/"pod_name" for GMP system metrics).
func (c *Client) instantQuery(ctx context.Context, query, nsLabel, podLabel string) (map[PodKey]float64, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing base URL: %w", err)
	}
	u.Path += "/api/v1/query"
	params := url.Values{"query": {query}}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned status %d: %s", resp.StatusCode, string(body))
	}

	var promResp promResponse
	if err := json.Unmarshal(body, &promResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if promResp.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: %s", promResp.Status)
	}

	if promResp.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected result type: %s", promResp.Data.ResultType)
	}

	results := make(map[PodKey]float64, len(promResp.Data.Result))
	for _, r := range promResp.Data.Result {
		ns := r.Metric[nsLabel]
		pod := r.Metric[podLabel]
		if ns == "" || pod == "" {
			continue
		}

		if len(r.Value) != 2 {
			continue
		}

		strVal, ok := r.Value[1].(string)
		if !ok {
			continue
		}

		val, err := strconv.ParseFloat(strVal, 64)
		if err != nil {
			continue
		}
		// Skip NaN/Inf values — they would propagate through arithmetic
		// and corrupt utilization calculations.
		if math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}

		results[PodKey{Namespace: ns, Pod: pod}] = val
	}

	return results, nil
}

// promResponse models the Prometheus instant query API response.
type promResponse struct {
	Status string   `json:"status"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"` // [timestamp, "value"]
}

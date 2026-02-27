// Package prometheus queries CPU and memory utilization metrics from a
// Prometheus-compatible API (e.g. Google Cloud Managed Service for Prometheus).
package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

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
	httpClient *http.Client
	baseURL    string
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) { cl.httpClient = c }
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

// cpuQuery returns the PromQL query for per-pod CPU usage in cores.
const cpuQuery = `sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`

// memQuery returns the PromQL query for per-pod memory usage in bytes.
const memQuery = `sum by (namespace, pod) (container_memory_working_set_bytes{container!="",container!="POD"})`

// FetchUsage queries Prometheus for CPU and memory usage of all pods and
// returns a map of pod key to usage. Pods missing from either query get zero
// for the missing metric.
func (c *Client) FetchUsage(ctx context.Context) (map[PodKey]PodUsage, error) {
	cpuResults, err := c.instantQuery(ctx, cpuQuery)
	if err != nil {
		return nil, fmt.Errorf("querying CPU usage: %w", err)
	}

	memResults, err := c.instantQuery(ctx, memQuery)
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
// (namespace, pod).
func (c *Client) instantQuery(ctx context.Context, query string) (map[PodKey]float64, error) {
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
		ns := r.Metric["namespace"]
		pod := r.Metric["pod"]
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

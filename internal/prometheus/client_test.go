package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchUsageCombinesCPUAndMemory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")

		var resp promResponse
		if strings.Contains(query, "cpu_usage_seconds_total") {
			resp = promResponse{
				Status: "success",
				Data: promData{
					ResultType: "vector",
					Result: []promResult{
						{
							Metric: map[string]string{"namespace": "default", "pod": "web-1"},
							Value:  []any{1234567890.0, "0.25"},
						},
						{
							Metric: map[string]string{"namespace": "default", "pod": "web-2"},
							Value:  []any{1234567890.0, "0.50"},
						},
					},
				},
			}
		} else {
			resp = promResponse{
				Status: "success",
				Data: promData{
					ResultType: "vector",
					Result: []promResult{
						{
							Metric: map[string]string{"namespace": "default", "pod": "web-1"},
							Value:  []any{1234567890.0, "268435456"}, // ~256 MiB
						},
						{
							Metric: map[string]string{"namespace": "default", "pod": "web-2"},
							Value:  []any{1234567890.0, "536870912"}, // ~512 MiB
						},
					},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(usage) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(usage))
	}

	web1 := usage[PodKey{Namespace: "default", Pod: "web-1"}]
	if web1.CPUCores != 0.25 {
		t.Errorf("web-1 CPU = %f, want 0.25", web1.CPUCores)
	}
	if web1.MemoryBytes != 268435456 {
		t.Errorf("web-1 memory = %f, want 268435456", web1.MemoryBytes)
	}

	web2 := usage[PodKey{Namespace: "default", Pod: "web-2"}]
	if web2.CPUCores != 0.50 {
		t.Errorf("web-2 CPU = %f, want 0.50", web2.CPUCores)
	}
	if web2.MemoryBytes != 536870912 {
		t.Errorf("web-2 memory = %f, want 536870912", web2.MemoryBytes)
	}
}

func TestFetchUsagePartialData(t *testing.T) {
	// Pod appears in CPU query but not memory query
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")

		var resp promResponse
		if strings.Contains(query, "cpu_usage_seconds_total") {
			resp = promResponse{
				Status: "success",
				Data: promData{
					ResultType: "vector",
					Result: []promResult{
						{
							Metric: map[string]string{"namespace": "default", "pod": "web-1"},
							Value:  []any{1234567890.0, "0.25"},
						},
					},
				},
			}
		} else {
			resp = promResponse{
				Status: "success",
				Data:   promData{ResultType: "vector", Result: []promResult{}},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(usage))
	}

	web1 := usage[PodKey{Namespace: "default", Pod: "web-1"}]
	if web1.CPUCores != 0.25 {
		t.Errorf("CPU = %f, want 0.25", web1.CPUCores)
	}
	if web1.MemoryBytes != 0 {
		t.Errorf("memory = %f, want 0 (missing)", web1.MemoryBytes)
	}
}

func TestFetchUsageEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data:   promData{ResultType: "vector", Result: []promResult{}},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(usage) != 0 {
		t.Errorf("expected 0 pods, got %d", len(usage))
	}
}

func TestFetchUsageHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchUsage(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention 503, got: %v", err)
	}
}

func TestFetchUsageQueryFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{Status: "error"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchUsage(context.Background())
	if err == nil {
		t.Fatal("expected error for failed query")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Errorf("error should mention query failed, got: %v", err)
	}
}

func TestFetchUsageSkipsMissingLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data: promData{
				ResultType: "vector",
				Result: []promResult{
					{
						Metric: map[string]string{"namespace": "default"},
						Value:  []any{1234567890.0, "0.25"},
					},
					{
						Metric: map[string]string{"pod": "web-1"},
						Value:  []any{1234567890.0, "0.50"},
					},
					{
						Metric: map[string]string{"namespace": "default", "pod": "web-2"},
						Value:  []any{1234567890.0, "0.75"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only web-2 has both namespace and pod labels
	if len(usage) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(usage))
	}
	if _, ok := usage[PodKey{Namespace: "default", Pod: "web-2"}]; !ok {
		t.Error("expected web-2 in results")
	}
}

func TestFetchUsageSkipsNaNValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data: promData{
				ResultType: "vector",
				Result: []promResult{
					{
						Metric: map[string]string{"namespace": "default", "pod": "nan-val"},
						Value:  []any{1234567890.0, "NaN"},
					},
					{
						Metric: map[string]string{"namespace": "default", "pod": "inf-val"},
						Value:  []any{1234567890.0, "+Inf"},
					},
					{
						Metric: map[string]string{"namespace": "default", "pod": "neg-inf-val"},
						Value:  []any{1234567890.0, "-Inf"},
					},
					{
						Metric: map[string]string{"namespace": "default", "pod": "good-val"},
						Value:  []any{1234567890.0, "1.5"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NaN and Inf values should be filtered out to prevent corruption
	// of downstream utilization calculations.
	if len(usage) != 1 {
		t.Fatalf("expected 1 pod (NaN/Inf filtered), got %d", len(usage))
	}
	if _, ok := usage[PodKey{Namespace: "default", Pod: "good-val"}]; !ok {
		t.Error("expected good-val in results")
	}
}

func TestFetchUsageUnexpectedResultType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data:   promData{ResultType: "matrix", Result: []promResult{}},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchUsage(context.Background())
	if err == nil {
		t.Fatal("expected error for unexpected result type")
	}
	if !strings.Contains(err.Error(), "unexpected result type") {
		t.Errorf("error should mention result type, got: %v", err)
	}
}

func TestInstantQuerySendsCorrectPromQL(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data:   promData{ResultType: "vector", Result: []promResult{}},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.instantQuery(context.Background(), "test_query{foo=\"bar\"}", "namespace", "pod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedQuery != `test_query{foo="bar"}` {
		t.Errorf("query = %q, want %q", receivedQuery, `test_query{foo="bar"}`)
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	client := NewClient("http://example.com", WithHTTPClient(custom))
	if client.httpClient != custom {
		t.Error("custom HTTP client not set")
	}
}

func TestGMPBaseURL(t *testing.T) {
	got := GMPBaseURL("my-project")
	want := "https://monitoring.googleapis.com/v1/projects/my-project/location/global/prometheus"
	if got != want {
		t.Errorf("GMPBaseURL = %q, want %q", got, want)
	}
}

func TestWithGMPSystemMetrics(t *testing.T) {
	client := NewClient("http://example.com", WithGMPSystemMetrics())
	if !client.gmpSystemMetrics {
		t.Error("gmpSystemMetrics should be true")
	}
}

func TestFetchUsageGMPSystemMetrics(t *testing.T) {
	var receivedQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		receivedQueries = append(receivedQueries, query)
		w.Header().Set("Content-Type", "application/json")

		var resp promResponse
		if strings.Contains(query, "core_usage_time") {
			resp = promResponse{
				Status: "success",
				Data: promData{
					ResultType: "vector",
					Result: []promResult{
						{
							Metric: map[string]string{"namespace_name": "default", "pod_name": "web-1"},
							Value:  []any{1234567890.0, "0.25"},
						},
					},
				},
			}
		} else {
			resp = promResponse{
				Status: "success",
				Data: promData{
					ResultType: "vector",
					Result: []promResult{
						{
							Metric: map[string]string{"namespace_name": "default", "pod_name": "web-1"},
							Value:  []any{1234567890.0, "268435456"},
						},
					},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, WithGMPSystemMetrics())
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(usage))
	}

	// Verify GMP system metric queries were sent
	if len(receivedQueries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(receivedQueries))
	}
	if !strings.Contains(receivedQueries[0], "kubernetes_io:container_cpu_core_usage_time") {
		t.Errorf("CPU query should use GMP system metric name, got: %s", receivedQueries[0])
	}
	if !strings.Contains(receivedQueries[1], "kubernetes_io:container_memory_used_bytes") {
		t.Errorf("memory query should use GMP system metric name, got: %s", receivedQueries[1])
	}

	// Verify results are keyed by namespace_name/pod_name labels
	web1 := usage[PodKey{Namespace: "default", Pod: "web-1"}]
	if web1.CPUCores != 0.25 {
		t.Errorf("web-1 CPU = %f, want 0.25", web1.CPUCores)
	}
	if web1.MemoryBytes != 268435456 {
		t.Errorf("web-1 memory = %f, want 268435456", web1.MemoryBytes)
	}
}

func TestFetchUsageGMPIgnoresStandardLabels(t *testing.T) {
	// In GMP mode, results with standard "namespace"/"pod" labels should be
	// skipped because GMP system metrics use "namespace_name"/"pod_name".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data: promData{
				ResultType: "vector",
				Result: []promResult{
					{
						Metric: map[string]string{"namespace": "default", "pod": "web-1"},
						Value:  []any{1234567890.0, "0.25"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, WithGMPSystemMetrics())
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be empty because GMP mode looks for namespace_name/pod_name
	if len(usage) != 0 {
		t.Errorf("expected 0 pods (wrong labels for GMP mode), got %d", len(usage))
	}
}

func TestFetchUsageStandardModeUsesCorrectQueries(t *testing.T) {
	var receivedQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQueries = append(receivedQueries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data:   promData{ResultType: "vector", Result: []promResult{}},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL) // No GMP option = standard mode
	_, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(receivedQueries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(receivedQueries))
	}
	if !strings.Contains(receivedQueries[0], "container_cpu_usage_seconds_total") {
		t.Errorf("CPU query should use standard metric name, got: %s", receivedQueries[0])
	}
	if !strings.Contains(receivedQueries[1], "container_memory_working_set_bytes") {
		t.Errorf("memory query should use standard metric name, got: %s", receivedQueries[1])
	}
}

func TestGMPBaseURLUsedByClient(t *testing.T) {
	// Verify that a client created with the GMP base URL appends the correct
	// /api/v1/query path when making queries.
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse{
			Status: "success",
			Data:   promData{ResultType: "vector", Result: []promResult{}},
		})
	}))
	defer srv.Close()

	// Simulate GMP-like base URL structure
	client := NewClient(srv.URL + "/v1/projects/my-project/location/global/prometheus")
	_, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPath := "/v1/projects/my-project/location/global/prometheus/api/v1/query"
	if requestedPath != wantPath {
		t.Errorf("request path = %q, want %q", requestedPath, wantPath)
	}
}

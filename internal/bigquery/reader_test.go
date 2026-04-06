package bigquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewReaderDefaults(t *testing.T) {
	r := NewReader("proj", "ds", "tbl")
	if r.project != "proj" {
		t.Errorf("project = %s, want proj", r.project)
	}
	if r.dataset != "ds" {
		t.Errorf("dataset = %s, want ds", r.dataset)
	}
	if r.table != "tbl" {
		t.Errorf("table = %s, want tbl", r.table)
	}
	if r.baseURL != bigqueryAPIBase {
		t.Errorf("baseURL = %s, want %s", r.baseURL, bigqueryAPIBase)
	}
}

func TestWithReaderHTTPClient(t *testing.T) {
	c := &http.Client{}
	r := NewReader("p", "d", "t", WithReaderHTTPClient(c))
	if r.httpClient != c {
		t.Error("expected custom HTTP client")
	}
}

func TestQueryAggregatedCosts(t *testing.T) {
	var receivedSQL string

	cpuUtil := 0.75
	memUtil := 0.5
	efficiency := 0.65

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		expectedPath := "/projects/my-project/queries"
		if r.URL.Path != expectedPath {
			t.Errorf("path = %s, want %s", r.URL.Path, expectedPath)
		}

		var req queryRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedSQL = req.Query

		resp := queryResponse{
			JobComplete: true,
			TotalRows:   "1",
			Schema: responseSchema{
				Fields: []responseField{
					{Name: "team", Type: "STRING"},
					{Name: "workload", Type: "STRING"},
					{Name: "subtype", Type: "STRING"},
					{Name: "namespace", Type: "STRING"},
					{Name: "cost_mode", Type: "STRING"},
					{Name: "has_spot", Type: "BOOLEAN"},
					{Name: "avg_pods", Type: "FLOAT"},
					{Name: "avg_cpu_vcpu", Type: "FLOAT"},
					{Name: "avg_memory_gb", Type: "FLOAT"},
					{Name: "total_cost", Type: "FLOAT"},
					{Name: "total_cpu_cost", Type: "FLOAT"},
					{Name: "total_mem_cost", Type: "FLOAT"},
					{Name: "avg_cost_per_hour", Type: "FLOAT"},
					{Name: "total_wasted_cost", Type: "FLOAT"},
					{Name: "avg_cpu_util", Type: "FLOAT"},
					{Name: "avg_mem_util", Type: "FLOAT"},
					{Name: "avg_efficiency", Type: "FLOAT"},
				},
			},
			Rows: []responseRow{
				{F: []responseCell{
					{V: "platform"},  // team
					{V: "web"},       // workload
					{V: "api"},       // subtype
					{V: "default"},   // namespace
					{V: "autopilot"}, // cost_mode
					{V: "true"},      // has_spot
					{V: "3.5"},       // avg_pods
					{V: "1.5"},       // avg_cpu
					{V: "4.0"},       // avg_mem
					{V: "12.50"},     // total_cost
					{V: "8.00"},      // total_cpu_cost
					{V: "4.50"},      // total_mem_cost
					{V: "0.52"},      // avg_cost_per_hour
					{V: "2.10"},      // total_wasted
					{V: "0.75"},      // avg_cpu_util
					{V: "0.50"},      // avg_mem_util
					{V: "0.65"},      // avg_efficiency
				}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reader := NewReader("my-project", "gke_costs", "cost_snapshots",
		WithReaderBaseURL(srv.URL))

	since := time.Now().Add(-3 * 24 * time.Hour)
	rows, err := reader.QueryAggregatedCosts(context.Background(), since, QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify SQL contains correct table reference
	if !strings.Contains(receivedSQL, "`my-project.gke_costs.cost_snapshots`") {
		t.Errorf("SQL should contain table ref, got: %s", receivedSQL)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	r := rows[0]
	if r.Team != "platform" {
		t.Errorf("team = %s, want platform", r.Team)
	}
	if r.Workload != "web" {
		t.Errorf("workload = %s, want web", r.Workload)
	}
	if r.Subtype != "api" {
		t.Errorf("subtype = %s, want api", r.Subtype)
	}
	if r.CostMode != "autopilot" {
		t.Errorf("cost_mode = %s, want autopilot", r.CostMode)
	}
	if !r.HasSpot {
		t.Error("expected HasSpot = true")
	}
	if r.TotalCost != 12.50 {
		t.Errorf("total_cost = %f, want 12.50", r.TotalCost)
	}
	if r.AvgCostPerHour != 0.52 {
		t.Errorf("avg_cost_per_hour = %f, want 0.52", r.AvgCostPerHour)
	}
	if r.AvgCPUUtil == nil || *r.AvgCPUUtil != cpuUtil {
		t.Errorf("avg_cpu_util = %v, want %f", r.AvgCPUUtil, cpuUtil)
	}
	if r.AvgMemUtil == nil || *r.AvgMemUtil != memUtil {
		t.Errorf("avg_mem_util = %v, want %f", r.AvgMemUtil, memUtil)
	}
	if r.AvgEfficiency == nil || *r.AvgEfficiency != efficiency {
		t.Errorf("avg_efficiency = %v, want %f", r.AvgEfficiency, efficiency)
	}
}

func TestQueryAggregatedCostsWithFilters(t *testing.T) {
	var receivedSQL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedSQL = req.Query

		resp := queryResponse{JobComplete: true, TotalRows: "0"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	since := time.Now().Add(-1 * time.Hour)
	filters := QueryFilters{
		ClusterName: "prod-cluster",
		Namespace:   "default",
		Team:        "platform",
	}

	_, err := reader.QueryAggregatedCosts(context.Background(), since, filters)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(receivedSQL, "cluster_name = 'prod-cluster'") {
		t.Errorf("SQL should contain cluster filter, got: %s", receivedSQL)
	}
	if !strings.Contains(receivedSQL, "namespace = 'default'") {
		t.Errorf("SQL should contain namespace filter, got: %s", receivedSQL)
	}
	if !strings.Contains(receivedSQL, "team = 'platform'") {
		t.Errorf("SQL should contain team filter, got: %s", receivedSQL)
	}
}

func TestQueryAggregatedCostsNullUtilization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := queryResponse{
			JobComplete: true,
			TotalRows:   "1",
			Rows: []responseRow{
				{F: []responseCell{
					{V: "team1"}, {V: "svc1"}, {V: nil}, {V: "ns"},
					{V: "autopilot"}, {V: "false"},
					{V: "2.0"}, {V: "1.0"}, {V: "2.0"},
					{V: "5.00"}, {V: "3.00"}, {V: "2.00"},
					{V: "0.20"}, {V: "0.00"},
					{V: nil}, {V: nil}, {V: nil},
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	rows, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	r := rows[0]
	if r.AvgCPUUtil != nil {
		t.Errorf("expected nil AvgCPUUtil, got %v", r.AvgCPUUtil)
	}
	if r.AvgMemUtil != nil {
		t.Errorf("expected nil AvgMemUtil, got %v", r.AvgMemUtil)
	}
	if r.AvgEfficiency != nil {
		t.Errorf("expected nil AvgEfficiency, got %v", r.AvgEfficiency)
	}
	if r.Subtype != "" {
		t.Errorf("expected empty subtype for null, got %q", r.Subtype)
	}
}

func TestQueryTimeSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		json.NewDecoder(r.Body).Decode(&req)

		if !strings.Contains(req.Query, "DIV(UNIX_SECONDS(timestamp), 3600)") {
			t.Errorf("SQL should contain bucket expression, got: %s", req.Query)
		}

		resp := queryResponse{
			JobComplete: true,
			TotalRows:   "3",
			Rows: []responseRow{
				{F: []responseCell{
					{V: "platform"}, {V: "web"}, {V: ""}, {V: "autopilot"},
					{V: "1.7050464e+09"}, {V: "0.50"},
				}},
				{F: []responseCell{
					{V: "platform"}, {V: "web"}, {V: ""}, {V: "autopilot"},
					{V: "1.7050500e+09"}, {V: "0.75"},
				}},
				{F: []responseCell{
					{V: "platform"}, {V: "web"}, {V: ""}, {V: "autopilot"},
					{V: "1.7050536e+09"}, {V: "1.00"},
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	since := time.Now().Add(-3 * time.Hour)
	points, err := reader.QueryTimeSeries(context.Background(), since, 3600, QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}

	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(points))
	}

	if points[0].Key.Team != "platform" || points[0].Key.Workload != "web" {
		t.Errorf("unexpected key: %+v", points[0].Key)
	}
	if points[0].BucketCost != 0.50 {
		t.Errorf("bucket_cost = %f, want 0.50", points[0].BucketCost)
	}
	if points[2].BucketCost != 1.00 {
		t.Errorf("last bucket_cost = %f, want 1.00", points[2].BucketCost)
	}
}

func TestQueryAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"access denied"}`))
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	_, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestQueryJobNotComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := queryResponse{JobComplete: false}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	_, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err == nil {
		t.Fatal("expected error for incomplete job")
	}
	if !strings.Contains(err.Error(), "did not complete") {
		t.Errorf("error should mention incomplete, got: %v", err)
	}
}

func TestQueryEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := queryResponse{JobComplete: true, TotalRows: "0"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	rows, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestEscapeSQLString(t *testing.T) {
	got := escapeSQLString("O'Brien")
	want := "O\\'Brien"
	if got != want {
		t.Errorf("escapeSQLString = %q, want %q", got, want)
	}
}

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
					{Name: "cluster_name", Type: "STRING"},
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
					{V: "prod-cluster"}, // cluster_name
					{V: "platform"},     // team
					{V: "web"},          // workload
					{V: "api"},          // subtype
					{V: "default"},      // namespace
					{V: "autopilot"},    // cost_mode
					{V: "true"},         // has_spot
					{V: "3.5"},          // avg_pods
					{V: "1.5"},          // avg_cpu
					{V: "4.0"},          // avg_mem
					{V: "12.50"},        // total_cost
					{V: "8.00"},         // total_cpu_cost
					{V: "4.50"},         // total_mem_cost
					{V: "0.52"},         // avg_cost_per_hour
					{V: "2.10"},         // total_wasted
					{V: "0.75"},         // avg_cpu_util
					{V: "0.50"},         // avg_mem_util
					{V: "0.65"},         // avg_efficiency
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
	if r.ClusterName != "prod-cluster" {
		t.Errorf("cluster_name = %s, want prod-cluster", r.ClusterName)
	}
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

	if !strings.Contains(receivedSQL, "cluster_name = @cluster_name") {
		t.Errorf("SQL should contain cluster filter, got: %s", receivedSQL)
	}
	if !strings.Contains(receivedSQL, "namespace = @namespace") {
		t.Errorf("SQL should contain namespace filter, got: %s", receivedSQL)
	}
	if !strings.Contains(receivedSQL, "team = @team") {
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
					{V: "my-cluster"},
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
		// Namespace is part of the workload identity, so the time-series
		// query must group by it (matching the aggregated query).
		if !strings.Contains(req.Query, "GROUP BY cluster_name, team, workload, subtype, namespace, IFNULL(cost_mode, 'autopilot'), bucket") {
			t.Errorf("SQL should group by namespace and normalized cost_mode, got: %s", req.Query)
		}

		resp := queryResponse{
			JobComplete: true,
			TotalRows:   "3",
			Rows: []responseRow{
				{F: []responseCell{
					{V: "prod-cluster"}, {V: "platform"}, {V: "web"}, {V: ""}, {V: "default"}, {V: "autopilot"},
					{V: "1.7050464e+09"}, {V: "0.50"},
				}},
				{F: []responseCell{
					{V: "prod-cluster"}, {V: "platform"}, {V: "web"}, {V: ""}, {V: "default"}, {V: "autopilot"},
					{V: "1.7050500e+09"}, {V: "0.75"},
				}},
				{F: []responseCell{
					{V: "prod-cluster"}, {V: "platform"}, {V: "web"}, {V: ""}, {V: "default"}, {V: "autopilot"},
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

	if points[0].Key.ClusterName != "prod-cluster" {
		t.Errorf("cluster_name = %s, want prod-cluster", points[0].Key.ClusterName)
	}
	if points[0].Key.Team != "platform" || points[0].Key.Workload != "web" {
		t.Errorf("unexpected key: %+v", points[0].Key)
	}
	if points[0].Key.Namespace != "default" {
		t.Errorf("namespace = %s, want default", points[0].Key.Namespace)
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

func TestBuildFilterClause(t *testing.T) {
	// Empty filters — no clause, no params
	clause, params := buildFilterClause(QueryFilters{})
	if clause != "" || len(params) != 0 {
		t.Errorf("empty filters should produce empty clause/params, got %q / %v", clause, params)
	}

	// Single cluster filter — named parameter, not interpolated value
	clause, params = buildFilterClause(QueryFilters{ClusterName: "prod"})
	if !strings.Contains(clause, "cluster_name = @cluster_name") {
		t.Errorf("expected parameterized cluster filter, got %q", clause)
	}
	if len(params) != 1 || params[0].Name != "cluster_name" || params[0].ParameterValue.Value != "prod" {
		t.Errorf("unexpected params: %+v", params)
	}

	// All filters
	clause, params = buildFilterClause(QueryFilters{ClusterName: "prod", Namespace: "default", Team: "platform"})
	if !strings.Contains(clause, "cluster_name = @cluster_name") ||
		!strings.Contains(clause, "namespace = @namespace") ||
		!strings.Contains(clause, "team = @team") {
		t.Errorf("all filters expected, got %q", clause)
	}
	if len(params) != 3 {
		t.Errorf("expected 3 params, got %+v", params)
	}
}

func TestQueryFiltersNotInterpolated(t *testing.T) {
	// Filter values with SQL metacharacters must travel as query parameters,
	// never appear in the SQL text (the old escaping missed backslashes:
	// a value ending in \ escaped the closing quote).
	var receivedReq queryRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		json.NewEncoder(w).Encode(queryResponse{JobComplete: true, TotalRows: "0"})
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	hostile := `evil\' OR '1'='1`
	_, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour),
		QueryFilters{Team: hostile})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(receivedReq.Query, "evil") {
		t.Errorf("filter value leaked into SQL text: %s", receivedReq.Query)
	}
	if receivedReq.ParameterMode != "NAMED" {
		t.Errorf("parameterMode = %q, want NAMED", receivedReq.ParameterMode)
	}
	found := false
	for _, p := range receivedReq.QueryParameters {
		if p.Name == "team" && p.ParameterValue.Value == hostile {
			found = true
		}
	}
	if !found {
		t.Errorf("team parameter missing, got %+v", receivedReq.QueryParameters)
	}
}

func TestReaderRejectsInvalidIdentifiers(t *testing.T) {
	// Project/dataset/table are interpolated into the table reference and
	// must be validated as identifiers.
	reader := NewReader("proj", "ds`; DROP TABLE x;--", "tbl")
	_, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid-identifier error, got %v", err)
	}

	reader = NewReader("proj", "ds", "tbl")
	if err := reader.validateIdentifiers(); err != nil {
		t.Errorf("valid identifiers rejected: %v", err)
	}
}

func TestQueryAggregatedCostsSQLShape(t *testing.T) {
	var receivedSQL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedSQL = req.Query
		json.NewEncoder(w).Encode(queryResponse{JobComplete: true, TotalRows: "0"})
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	_, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}

	// avg $/hr must divide by the covered time (SUM of interval windows),
	// not MAX-MIN of snapshot timestamps: that has a fencepost error (N
	// intervals of cost over an N-1 interval span) and yields NULL→0 for
	// single-snapshot groups.
	if !strings.Contains(receivedSQL, "SUM(interval_seconds)") {
		t.Errorf("avg_cost_per_hour should divide by SUM(interval_seconds), got: %s", receivedSQL)
	}
	if strings.Contains(receivedSQL, "TIMESTAMP_DIFF(MAX(timestamp), MIN(timestamp)") {
		t.Errorf("avg_cost_per_hour still uses timestamp-span denominator: %s", receivedSQL)
	}
	// Rows written before the cost_mode column existed are autopilot.
	if !strings.Contains(receivedSQL, "IFNULL(cost_mode, 'autopilot')") {
		t.Errorf("cost_mode should be normalized with IFNULL, got: %s", receivedSQL)
	}
	// The grouping must use the IFNULL expression, not the bare column name:
	// a bare `GROUP BY cost_mode` next to an identically-named SELECT alias
	// is at best ambiguous, and grouping on the raw column would keep legacy
	// NULL rows in a separate group that then displays as a duplicate
	// 'autopilot' row.
	if !strings.Contains(receivedSQL, "GROUP BY cluster_name, team, workload, subtype, namespace, IFNULL(cost_mode, 'autopilot'), timestamp") {
		t.Errorf("per-snapshot grouping should group by the IFNULL expression and timestamp, got: %s", receivedSQL)
	}
	// Record writes one row per is_spot value per snapshot. Summing
	// interval_seconds across those sibling rows would double-count the
	// covered time (halving $/hr) and distort the AVG columns, so rows must
	// first collapse per snapshot timestamp before the range aggregation.
	if !strings.Contains(receivedSQL, "WITH per_snapshot AS") || !strings.Contains(receivedSQL, "FROM per_snapshot") {
		t.Errorf("expected two-level aggregation via per_snapshot CTE, got: %s", receivedSQL)
	}
	if !strings.Contains(receivedSQL, "ANY_VALUE(interval_seconds)") {
		t.Errorf("interval_seconds must be taken once per snapshot, not summed across sibling rows, got: %s", receivedSQL)
	}
}

func TestQueryTimeSeriesNormalizesCostMode(t *testing.T) {
	var receivedSQL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedSQL = req.Query
		json.NewEncoder(w).Encode(queryResponse{JobComplete: true, TotalRows: "0"})
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	_, err := reader.QueryTimeSeries(context.Background(), time.Now().Add(-time.Hour), 300, QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(receivedSQL, "IFNULL(cost_mode, 'autopilot')") {
		t.Errorf("cost_mode should be normalized with IFNULL, got: %s", receivedSQL)
	}
	// Group by the IFNULL expression, not the bare (ambiguous) column name.
	if !strings.Contains(receivedSQL, "GROUP BY cluster_name, team, workload, subtype, namespace, IFNULL(cost_mode, 'autopilot'), bucket") {
		t.Errorf("time-series grouping should use the IFNULL expression, got: %s", receivedSQL)
	}
}

func TestQueryFollowsPagination(t *testing.T) {
	// Results beyond the first page must be fetched via pageToken, not
	// silently truncated.
	makeRow := func(team string, cost string) responseRow {
		return responseRow{F: []responseCell{
			{V: "c"}, {V: team}, {V: "w"}, {V: ""}, {V: "ns"}, {V: "autopilot"},
			{V: "false"}, {V: "1"}, {V: "1"}, {V: "1"},
			{V: cost}, {V: "1"}, {V: "1"}, {V: "1"}, {V: "0"},
			{V: nil}, {V: nil}, {V: nil},
		}}
	}

	var pageRequests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageRequests = append(pageRequests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost {
			// First page: one row + a pageToken pointing at the rest.
			json.NewEncoder(w).Encode(map[string]any{
				"jobComplete":  true,
				"totalRows":    "3",
				"pageToken":    "page2",
				"jobReference": map[string]string{"projectId": "proj", "jobId": "job123", "location": "US"},
				"rows":         []responseRow{makeRow("team-a", "1.0")},
			})
			return
		}

		// Paginated GET for subsequent pages.
		if !strings.Contains(r.URL.Path, "job123") {
			t.Errorf("expected jobId in path, got %s", r.URL.Path)
		}
		switch r.URL.Query().Get("pageToken") {
		case "page2":
			json.NewEncoder(w).Encode(map[string]any{
				"jobComplete": true,
				"pageToken":   "page3",
				"rows":        []responseRow{makeRow("team-b", "2.0")},
			})
		case "page3":
			json.NewEncoder(w).Encode(map[string]any{
				"jobComplete": true,
				"rows":        []responseRow{makeRow("team-c", "3.0")},
			})
		default:
			t.Errorf("unexpected pageToken %q", r.URL.Query().Get("pageToken"))
		}
	}))
	defer srv.Close()

	reader := NewReader("proj", "ds", "tbl", WithReaderBaseURL(srv.URL))
	rows, err := reader.QueryAggregatedCosts(context.Background(), time.Now().Add(-time.Hour), QueryFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows across pages, got %d (requests: %v)", len(rows), pageRequests)
	}
	if rows[0].Team != "team-a" || rows[1].Team != "team-b" || rows[2].Team != "team-c" {
		t.Errorf("rows out of order or missing: %+v", rows)
	}
}

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

func TestWriteSnapshots(t *testing.T) {
	var receivedBody insertAllRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		expectedPath := "/projects/my-project/datasets/autopilot_costs/tables/cost_snapshots/insertAll"
		if r.URL.Path != expectedPath {
			t.Errorf("path = %s, want %s", r.URL.Path, expectedPath)
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(insertAllResponse{})
	}))
	defer srv.Close()

	writer := NewWriter("my-project", "autopilot_costs", "cost_snapshots",
		WithWriterBaseURL(srv.URL))

	snapshots := []CostSnapshot{
		{
			Timestamp:       time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			ProjectID:       "my-project",
			Region:          "us-central1",
			ClusterName:     "prod-cluster",
			Namespace:       "default",
			Team:            "platform",
			Workload:        "web",
			PodCount:        3,
			CPURequestVCPU:  1.5,
			MemoryRequestGB: 3.0,
			CPUCost:         0.105,
			MemoryCost:      0.024,
			TotalCost:       0.129,
			IsSpot:          false,
			IntervalSeconds: 300,
		},
	}

	err := writer.Write(context.Background(), snapshots)
	if err != nil {
		t.Fatal(err)
	}

	if len(receivedBody.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(receivedBody.Rows))
	}

	row := receivedBody.Rows[0].JSON
	if row["team"] != "platform" {
		t.Errorf("team = %v, want platform", row["team"])
	}
	if row["workload"] != "web" {
		t.Errorf("workload = %v, want web", row["workload"])
	}
	if row["pod_count"] != float64(3) {
		t.Errorf("pod_count = %v, want 3", row["pod_count"])
	}
}

func TestWriteMultipleSnapshots(t *testing.T) {
	var receivedBody insertAllRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(insertAllResponse{})
	}))
	defer srv.Close()

	writer := NewWriter("proj", "ds", "tbl", WithWriterBaseURL(srv.URL))

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	snapshots := []CostSnapshot{
		{Timestamp: ts, ProjectID: "proj", Team: "alpha", Workload: "svc1", PodCount: 2},
		{Timestamp: ts, ProjectID: "proj", Team: "beta", Workload: "svc2", PodCount: 5, IsSpot: true},
		{Timestamp: ts, ProjectID: "proj", Team: "alpha", Workload: "svc3", PodCount: 1},
	}

	err := writer.Write(context.Background(), snapshots)
	if err != nil {
		t.Fatal(err)
	}

	if len(receivedBody.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(receivedBody.Rows))
	}

	// Verify all rows have unique insert IDs
	ids := make(map[string]bool)
	for _, row := range receivedBody.Rows {
		if ids[row.InsertID] {
			t.Errorf("duplicate insert ID: %s", row.InsertID)
		}
		ids[row.InsertID] = true
	}

	// Verify second row is the beta/svc2 one
	if receivedBody.Rows[1].JSON["team"] != "beta" {
		t.Errorf("second row team = %v, want beta", receivedBody.Rows[1].JSON["team"])
	}
}

func TestWriteEmpty(t *testing.T) {
	writer := NewWriter("proj", "ds", "tbl")
	err := writer.Write(context.Background(), nil)
	if err != nil {
		t.Fatal("expected no error for empty snapshots")
	}
}

func TestWriteEmptySlice(t *testing.T) {
	writer := NewWriter("proj", "ds", "tbl")
	err := writer.Write(context.Background(), []CostSnapshot{})
	if err != nil {
		t.Fatal("expected no error for empty slice")
	}
}

func TestWithWriterHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	w := NewWriter("proj", "ds", "tbl", WithWriterHTTPClient(customClient))
	if w.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestNewWriterDefaults(t *testing.T) {
	w := NewWriter("p", "d", "t")
	if w.project != "p" {
		t.Errorf("project = %s, want p", w.project)
	}
	if w.dataset != "d" {
		t.Errorf("dataset = %s, want d", w.dataset)
	}
	if w.table != "t" {
		t.Errorf("table = %s, want t", w.table)
	}
	if w.baseURL != bigqueryAPIBase {
		t.Errorf("baseURL = %s, want %s", w.baseURL, bigqueryAPIBase)
	}
	if w.httpClient == nil {
		t.Error("expected default HTTP client")
	}
}

func TestWriteAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"access denied"}`))
	}))
	defer srv.Close()

	writer := NewWriter("proj", "ds", "tbl", WithWriterBaseURL(srv.URL))
	err := writer.Write(context.Background(), []CostSnapshot{{
		Timestamp: time.Now(),
	}})

	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestWriteInsertErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := insertAllResponse{
			InsertErrors: []insertError{
				{Index: 0, Errors: []struct {
					Reason  string `json:"reason"`
					Message string `json:"message"`
				}{{Reason: "invalid", Message: "bad row"}}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	writer := NewWriter("proj", "ds", "tbl", WithWriterBaseURL(srv.URL))
	err := writer.Write(context.Background(), []CostSnapshot{{
		Timestamp: time.Now(),
	}})

	if err == nil {
		t.Fatal("expected error for insert errors")
	}

	// Verify error message contains details (no longer just goes to log)
	if !strings.Contains(err.Error(), "invalid") || !strings.Contains(err.Error(), "bad row") {
		t.Errorf("error should contain details, got: %v", err)
	}
}

func TestWriteSubtypeDifferentiatesInsertID(t *testing.T) {
	var receivedBody insertAllRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(insertAllResponse{})
	}))
	defer srv.Close()

	writer := NewWriter("proj", "ds", "tbl", WithWriterBaseURL(srv.URL))

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	// Two snapshots identical except for Subtype — must get different InsertIDs
	snapshots := []CostSnapshot{
		{Timestamp: ts, ProjectID: "proj", ClusterName: "c", Namespace: "ns",
			Team: "alpha", Workload: "svc1", Subtype: "extract", IsSpot: false},
		{Timestamp: ts, ProjectID: "proj", ClusterName: "c", Namespace: "ns",
			Team: "alpha", Workload: "svc1", Subtype: "transform", IsSpot: false},
	}

	err := writer.Write(context.Background(), snapshots)
	if err != nil {
		t.Fatal(err)
	}

	if len(receivedBody.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(receivedBody.Rows))
	}

	if receivedBody.Rows[0].InsertID == receivedBody.Rows[1].InsertID {
		t.Errorf("rows with different subtypes must have different InsertIDs, both got: %s",
			receivedBody.Rows[0].InsertID)
	}
}

func TestSnapshotToRow(t *testing.T) {
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	s := CostSnapshot{
		Timestamp:       ts,
		ProjectID:       "proj",
		Region:          "us-central1",
		ClusterName:     "cluster",
		Namespace:       "ns",
		Team:            "team1",
		Workload:        "wl1",
		Subtype:         "sub1",
		PodCount:        5,
		CPURequestVCPU:  2.5,
		MemoryRequestGB: 8.0,
		CPUCost:         0.175,
		MemoryCost:      0.032,
		TotalCost:       0.207,
		IsSpot:          true,
		IntervalSeconds: 300,
	}

	row := snapshotToRow(s)

	// Verify all 16 fields
	expectations := map[string]any{
		"timestamp":         "2025-01-15T12:00:00Z",
		"project_id":        "proj",
		"region":            "us-central1",
		"cluster_name":      "cluster",
		"namespace":         "ns",
		"team":              "team1",
		"workload":          "wl1",
		"subtype":           "sub1",
		"pod_count":         5,
		"cpu_request_vcpu":  2.5,
		"memory_request_gb": 8.0,
		"cpu_cost":          0.175,
		"memory_cost":       0.032,
		"total_cost":        0.207,
		"is_spot":           true,
		"interval_seconds":  int64(300),
	}

	if len(row) != len(expectations) {
		t.Errorf("expected %d fields, got %d", len(expectations), len(row))
	}

	for key, want := range expectations {
		got, ok := row[key]
		if !ok {
			t.Errorf("missing field: %s", key)
			continue
		}
		if got != want {
			t.Errorf("field %s = %v (%T), want %v (%T)", key, got, got, want, want)
		}
	}

	// Nullable fields should be absent when not set
	for _, key := range []string{"cost_mode", "cpu_utilization", "memory_utilization", "efficiency_score", "wasted_cost"} {
		if _, ok := row[key]; ok {
			t.Errorf("nullable field %s should be absent when not set", key)
		}
	}
}

func TestSnapshotToRowWithNullableFields(t *testing.T) {
	cpuUtil := 0.75
	memUtil := 0.50
	efficiency := 0.65
	wastedCost := 0.042

	s := CostSnapshot{
		Timestamp:         time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		ProjectID:         "proj",
		CostMode:          "autopilot",
		CPUUtilization:    &cpuUtil,
		MemoryUtilization: &memUtil,
		EfficiencyScore:   &efficiency,
		WastedCost:        &wastedCost,
	}

	row := snapshotToRow(s)

	if row["cost_mode"] != "autopilot" {
		t.Errorf("cost_mode = %v, want autopilot", row["cost_mode"])
	}
	if row["cpu_utilization"] != 0.75 {
		t.Errorf("cpu_utilization = %v, want 0.75", row["cpu_utilization"])
	}
	if row["memory_utilization"] != 0.50 {
		t.Errorf("memory_utilization = %v, want 0.50", row["memory_utilization"])
	}
	if row["efficiency_score"] != 0.65 {
		t.Errorf("efficiency_score = %v, want 0.65", row["efficiency_score"])
	}
	if row["wasted_cost"] != 0.042 {
		t.Errorf("wasted_cost = %v, want 0.042", row["wasted_cost"])
	}
}

func TestSnapshotToRowCostModeEmptyOmitted(t *testing.T) {
	s := CostSnapshot{
		Timestamp: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		CostMode:  "",
	}

	row := snapshotToRow(s)
	if _, ok := row["cost_mode"]; ok {
		t.Error("cost_mode should be omitted when empty")
	}
}

func TestInsertIDIncludesCostMode(t *testing.T) {
	var receivedBody insertAllRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(insertAllResponse{})
	}))
	defer srv.Close()

	writer := NewWriter("proj", "ds", "tbl", WithWriterBaseURL(srv.URL))

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	snapshots := []CostSnapshot{
		{Timestamp: ts, ProjectID: "proj", ClusterName: "c", Namespace: "ns",
			Team: "alpha", Workload: "svc1", CostMode: "autopilot"},
		{Timestamp: ts, ProjectID: "proj", ClusterName: "c", Namespace: "ns",
			Team: "alpha", Workload: "svc1", CostMode: "standard"},
	}

	err := writer.Write(context.Background(), snapshots)
	if err != nil {
		t.Fatal(err)
	}

	if receivedBody.Rows[0].InsertID == receivedBody.Rows[1].InsertID {
		t.Error("rows with different cost_mode must have different InsertIDs")
	}
}

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
}

package bigquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	if row["project_id"] != "proj" {
		t.Errorf("project_id = %v, want proj", row["project_id"])
	}
	if row["is_spot"] != true {
		t.Errorf("is_spot = %v, want true", row["is_spot"])
	}
	if row["pod_count"] != 5 {
		t.Errorf("pod_count = %v, want 5", row["pod_count"])
	}
	if row["timestamp"] != "2025-01-15T12:00:00Z" {
		t.Errorf("timestamp = %v, want 2025-01-15T12:00:00Z", row["timestamp"])
	}
}

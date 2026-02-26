package bigquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureDatasetCreatesNew(t *testing.T) {
	var receivedBody datasetRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		expectedPath := "/projects/my-project/datasets"
		if r.URL.Path != expectedPath {
			t.Errorf("path = %s, want %s", r.URL.Path, expectedPath)
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureDataset(context.Background(), "autopilot_costs", "US")
	if err != nil {
		t.Fatal(err)
	}

	if receivedBody.DatasetReference.DatasetID != "autopilot_costs" {
		t.Errorf("dataset ID = %s, want autopilot_costs", receivedBody.DatasetReference.DatasetID)
	}
	if receivedBody.Location != "US" {
		t.Errorf("location = %s, want US", receivedBody.Location)
	}
}

func TestEnsureDatasetAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"already exists"}`))
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureDataset(context.Background(), "autopilot_costs", "US")
	if err != nil {
		t.Fatalf("expected no error for existing dataset, got: %v", err)
	}
}

func TestEnsureTableCreatesNew(t *testing.T) {
	var receivedBody tableRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/projects/my-project/datasets/autopilot_costs/tables"
		if r.URL.Path != expectedPath {
			t.Errorf("path = %s, want %s", r.URL.Path, expectedPath)
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "autopilot_costs", "cost_snapshots")
	if err != nil {
		t.Fatal(err)
	}

	if receivedBody.TableReference.TableID != "cost_snapshots" {
		t.Errorf("table ID = %s, want cost_snapshots", receivedBody.TableReference.TableID)
	}

	if len(receivedBody.Schema.Fields) != len(TableSchema()) {
		t.Errorf("schema fields = %d, want %d", len(receivedBody.Schema.Fields), len(TableSchema()))
	}

	if receivedBody.TimePartitioning["type"] != "DAY" {
		t.Errorf("partitioning type = %s, want DAY", receivedBody.TimePartitioning["type"])
	}

	if receivedBody.Clustering == nil {
		t.Fatal("expected clustering config")
	}
	if len(receivedBody.Clustering.Fields) != 2 {
		t.Errorf("clustering fields = %d, want 2", len(receivedBody.Clustering.Fields))
	}
}

func TestEnsureTableAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "ds", "tbl")
	if err != nil {
		t.Fatalf("expected no error for existing table, got: %v", err)
	}
}

func TestEnsureDatasetAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer srv.Close()

	sc := NewSetupClient("proj", WithSetupBaseURL(srv.URL))
	err := sc.EnsureDataset(context.Background(), "ds", "US")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureTableAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer srv.Close()

	sc := NewSetupClient("proj", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "ds", "tbl")
	if err == nil {
		t.Fatal("expected error")
	}
}

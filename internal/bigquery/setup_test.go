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
	err := sc.EnsureDataset(context.Background(), "gke_costs", "US")
	if err != nil {
		t.Fatal(err)
	}

	if receivedBody.DatasetReference.DatasetID != "gke_costs" {
		t.Errorf("dataset ID = %s, want gke_costs", receivedBody.DatasetReference.DatasetID)
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
	err := sc.EnsureDataset(context.Background(), "gke_costs", "US")
	if err != nil {
		t.Fatalf("expected no error for existing dataset, got: %v", err)
	}
}

func TestEnsureTableCreatesNew(t *testing.T) {
	var receivedBody tableRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/projects/my-project/datasets/gke_costs/tables"
		if r.URL.Path != expectedPath {
			t.Errorf("path = %s, want %s", r.URL.Path, expectedPath)
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "gke_costs", "cost_snapshots")
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

func TestEnsureTableAlreadyExistsCompleteSchema(t *testing.T) {
	// Existing table already has every column — no PATCH should be issued.
	var patched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusConflict)
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"schema": tableSchemaWrapper{Fields: TableSchema()},
			})
		case http.MethodPatch:
			patched = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "ds", "tbl")
	if err != nil {
		t.Fatalf("expected no error for existing table, got: %v", err)
	}
	if patched {
		t.Error("no PATCH expected when the schema is already complete")
	}
}

func TestEnsureTableMigratesMissingColumns(t *testing.T) {
	// A table created by an older version lacks the newer NULLABLE columns
	// (cost_mode, utilization fields). EnsureTable must add them, otherwise
	// every subsequent insert that populates them fails.
	full := TableSchema()
	old := full[:16] // schema before cost_mode + utilization columns

	var patchedFields []FieldSchema
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusConflict)
		case http.MethodGet:
			if r.URL.Path != "/projects/my-project/datasets/ds/tables/tbl" {
				t.Errorf("unexpected GET path %s", r.URL.Path)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"schema": tableSchemaWrapper{Fields: old},
			})
		case http.MethodPatch:
			if r.URL.Path != "/projects/my-project/datasets/ds/tables/tbl" {
				t.Errorf("unexpected PATCH path %s", r.URL.Path)
			}
			var body struct {
				Schema tableSchemaWrapper `json:"schema"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			patchedFields = body.Schema.Fields
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "ds", "tbl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patchedFields) != len(full) {
		t.Fatalf("PATCH schema has %d fields, want %d (existing + missing)", len(patchedFields), len(full))
	}
	names := map[string]bool{}
	for _, f := range patchedFields {
		names[f.Name] = true
	}
	for _, want := range []string{"cost_mode", "cpu_utilization", "memory_utilization", "efficiency_score", "wasted_cost"} {
		if !names[want] {
			t.Errorf("PATCH schema missing column %s", want)
		}
	}
}

func TestEnsureTableMigrationRejectsRequiredAddition(t *testing.T) {
	// BigQuery only allows adding NULLABLE columns to an existing table; if a
	// REQUIRED column is missing the migration must fail loudly rather than
	// send a doomed PATCH.
	full := TableSchema()
	// Existing schema missing the REQUIRED interval_seconds (index 15) but
	// containing everything else.
	old := append(append([]FieldSchema{}, full[:15]...), full[16:]...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusConflict)
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"schema": tableSchemaWrapper{Fields: old},
			})
		case http.MethodPatch:
			t.Error("PATCH must not be attempted for REQUIRED additions")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	sc := NewSetupClient("my-project", WithSetupBaseURL(srv.URL))
	err := sc.EnsureTable(context.Background(), "ds", "tbl")
	if err == nil {
		t.Fatal("expected error for REQUIRED column migration")
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

func TestWithSetupHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	sc := NewSetupClient("proj", WithSetupHTTPClient(customClient))
	if sc.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestNewSetupClientDefaults(t *testing.T) {
	sc := NewSetupClient("my-project")
	if sc.project != "my-project" {
		t.Errorf("project = %s, want my-project", sc.project)
	}
	if sc.baseURL != bigqueryAPIBase {
		t.Errorf("baseURL = %s, want %s", sc.baseURL, bigqueryAPIBase)
	}
	if sc.httpClient == nil {
		t.Error("expected default HTTP client")
	}
}

func TestEnsureDatasetRequestBody(t *testing.T) {
	var receivedBody datasetRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	sc := NewSetupClient("proj-123", WithSetupBaseURL(srv.URL))
	err := sc.EnsureDataset(context.Background(), "my_dataset", "EU")
	if err != nil {
		t.Fatal(err)
	}

	if receivedBody.DatasetReference.ProjectID != "proj-123" {
		t.Errorf("project = %s, want proj-123", receivedBody.DatasetReference.ProjectID)
	}
	if receivedBody.DatasetReference.DatasetID != "my_dataset" {
		t.Errorf("dataset = %s, want my_dataset", receivedBody.DatasetReference.DatasetID)
	}
	if receivedBody.Location != "EU" {
		t.Errorf("location = %s, want EU", receivedBody.Location)
	}
}

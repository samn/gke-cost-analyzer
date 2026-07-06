package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SetupClient creates BigQuery datasets and tables.
type SetupClient struct {
	httpClient *http.Client
	project    string
	baseURL    string
}

// SetupOption configures a SetupClient.
type SetupOption func(*SetupClient)

// WithSetupHTTPClient sets a custom HTTP client.
func WithSetupHTTPClient(c *http.Client) SetupOption {
	return func(sc *SetupClient) { sc.httpClient = c }
}

// WithSetupBaseURL overrides the BigQuery API base URL (for testing).
func WithSetupBaseURL(url string) SetupOption {
	return func(sc *SetupClient) { sc.baseURL = url }
}

// NewSetupClient creates a new SetupClient.
func NewSetupClient(project string, opts ...SetupOption) *SetupClient {
	sc := &SetupClient{
		httpClient: newDefaultHTTPClient(),
		project:    project,
		baseURL:    bigqueryAPIBase,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// datasetRequest is the body for creating a dataset.
type datasetRequest struct {
	DatasetReference datasetReference `json:"datasetReference"`
	Location         string           `json:"location,omitempty"`
}

type datasetReference struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
}

// tableRequest is the body for creating a table.
type tableRequest struct {
	TableReference   tableReference     `json:"tableReference"`
	Schema           tableSchemaWrapper `json:"schema"`
	TimePartitioning map[string]string  `json:"timePartitioning,omitempty"`
	Clustering       *clusteringConfig  `json:"clustering,omitempty"`
}

type tableReference struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
	TableID   string `json:"tableId"`
}

type tableSchemaWrapper struct {
	Fields []FieldSchema `json:"fields"`
}

type clusteringConfig struct {
	Fields []string `json:"fields"`
}

// EnsureDataset creates the BigQuery dataset if it doesn't exist.
func (sc *SetupClient) EnsureDataset(ctx context.Context, dataset, location string) error {
	url := fmt.Sprintf("%s/projects/%s/datasets", sc.baseURL, sc.project)

	reqBody := datasetRequest{
		DatasetReference: datasetReference{
			ProjectID: sc.project,
			DatasetID: dataset,
		},
		Location: location,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling dataset request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating dataset request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	}
	defer resp.Body.Close()

	// 409 = already exists, which is fine
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create dataset returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// EnsureTable creates the BigQuery table if it doesn't exist.
func (sc *SetupClient) EnsureTable(ctx context.Context, dataset, table string) error {
	url := fmt.Sprintf("%s/projects/%s/datasets/%s/tables", sc.baseURL, sc.project, dataset)

	reqBody := tableRequest{
		TableReference: tableReference{
			ProjectID: sc.project,
			DatasetID: dataset,
			TableID:   table,
		},
		Schema:           tableSchemaWrapper{Fields: TableSchema()},
		TimePartitioning: TimePartitioning(),
		Clustering:       &clusteringConfig{Fields: Clustering()},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling table request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating table request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("creating table: %w", err)
	}
	defer resp.Body.Close()

	// 409 = already exists; migrate its schema if columns were added since.
	if resp.StatusCode == http.StatusConflict {
		return sc.migrateTableSchema(ctx, dataset, table)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create table returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// migrateTableSchema adds columns from TableSchema() that are missing from an
// existing table. Without this, a table created by an older version silently
// rejects every insert that populates newer columns. BigQuery only permits
// additive NULLABLE columns on existing tables; anything else is an error.
func (sc *SetupClient) migrateTableSchema(ctx context.Context, dataset, table string) error {
	tableURL := fmt.Sprintf("%s/projects/%s/datasets/%s/tables/%s", sc.baseURL, sc.project, dataset, table)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tableURL, nil)
	if err != nil {
		return fmt.Errorf("creating table get request: %w", err)
	}
	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching existing table: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("get table returned status %d: %s", resp.StatusCode, string(body))
	}

	var existing struct {
		Schema tableSchemaWrapper `json:"schema"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&existing); err != nil {
		return fmt.Errorf("decoding existing table: %w", err)
	}

	have := make(map[string]bool, len(existing.Schema.Fields))
	for _, f := range existing.Schema.Fields {
		have[f.Name] = true
	}

	var missing []FieldSchema
	for _, f := range TableSchema() {
		if have[f.Name] {
			continue
		}
		if f.Mode == "REQUIRED" {
			return fmt.Errorf("existing table %s.%s is missing REQUIRED column %q; BigQuery cannot add it in place — recreate the table or migrate manually", dataset, table, f.Name)
		}
		missing = append(missing, f)
	}
	if len(missing) == 0 {
		return nil
	}

	patchBody := struct {
		Schema tableSchemaWrapper `json:"schema"`
	}{Schema: tableSchemaWrapper{Fields: append(existing.Schema.Fields, missing...)}}

	data, err := json.Marshal(patchBody)
	if err != nil {
		return fmt.Errorf("marshaling schema patch: %w", err)
	}

	patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, tableURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating schema patch request: %w", err)
	}
	patchReq.Header.Set("Content-Type", "application/json")

	patchResp, err := sc.httpClient.Do(patchReq)
	if err != nil {
		return fmt.Errorf("patching table schema: %w", err)
	}
	defer patchResp.Body.Close()

	if patchResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(patchResp.Body)
		return fmt.Errorf("patch table schema returned status %d: %s", patchResp.StatusCode, string(body))
	}

	return nil
}

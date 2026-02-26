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
		httpClient: http.DefaultClient,
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

	// 409 = already exists, which is fine
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create table returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

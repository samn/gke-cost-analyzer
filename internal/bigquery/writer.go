package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const bigqueryAPIBase = "https://bigquery.googleapis.com/bigquery/v2"

// Writer inserts cost snapshot rows into BigQuery using the streaming insert API.
type Writer struct {
	httpClient *http.Client
	project    string
	dataset    string
	table      string
	baseURL    string
}

// WriterOption configures a Writer.
type WriterOption func(*Writer)

// WithWriterHTTPClient sets a custom HTTP client (for auth or testing).
func WithWriterHTTPClient(c *http.Client) WriterOption {
	return func(w *Writer) { w.httpClient = c }
}

// WithWriterBaseURL overrides the BigQuery API base URL (for testing).
func WithWriterBaseURL(url string) WriterOption {
	return func(w *Writer) { w.baseURL = url }
}

// NewWriter creates a BigQuery streaming insert writer.
func NewWriter(project, dataset, table string, opts ...WriterOption) *Writer {
	w := &Writer{
		httpClient: http.DefaultClient,
		project:    project,
		dataset:    dataset,
		table:      table,
		baseURL:    bigqueryAPIBase,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// insertAllRequest is the request body for the BigQuery streaming insert API.
type insertAllRequest struct {
	Rows []insertRow `json:"rows"`
}

type insertRow struct {
	InsertID string         `json:"insertId"`
	JSON     map[string]any `json:"json"`
}

// insertAllResponse is the response from the BigQuery streaming insert API.
type insertAllResponse struct {
	InsertErrors []insertError `json:"insertErrors,omitempty"`
}

type insertError struct {
	Index  int `json:"index"`
	Errors []struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Write inserts cost snapshots into BigQuery.
func (w *Writer) Write(ctx context.Context, snapshots []CostSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	rows := make([]insertRow, len(snapshots))
	for i, s := range snapshots {
		rows[i] = insertRow{
			InsertID: fmt.Sprintf("%s-%s-%s-%s-%s-%t-%d", s.ProjectID, s.ClusterName, s.Namespace, s.Team, s.Workload, s.IsSpot, s.Timestamp.UnixNano()),
			JSON:     snapshotToRow(s),
		}
	}

	reqBody := insertAllRequest{Rows: rows}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling insert request: %w", err)
	}

	url := fmt.Sprintf("%s/projects/%s/datasets/%s/tables/%s/insertAll",
		w.baseURL, w.project, w.dataset, w.table)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating insert request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending insert request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("BigQuery insert returned status %d: %s", resp.StatusCode, string(body))
	}

	var result insertAllResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding insert response: %w", err)
	}

	if len(result.InsertErrors) > 0 {
		var details []string
		for _, ie := range result.InsertErrors {
			for _, e := range ie.Errors {
				details = append(details, fmt.Sprintf("row %d: %s - %s", ie.Index, e.Reason, e.Message))
			}
		}
		return fmt.Errorf("BigQuery insert had %d row errors: %s", len(result.InsertErrors), strings.Join(details, "; "))
	}

	return nil
}

func snapshotToRow(s CostSnapshot) map[string]any {
	return map[string]any{
		"timestamp":         s.Timestamp.Format(time.RFC3339),
		"project_id":        s.ProjectID,
		"region":            s.Region,
		"cluster_name":      s.ClusterName,
		"namespace":         s.Namespace,
		"team":              s.Team,
		"workload":          s.Workload,
		"subtype":           s.Subtype,
		"pod_count":         s.PodCount,
		"cpu_request_vcpu":  s.CPURequestVCPU,
		"memory_request_gb": s.MemoryRequestGB,
		"cpu_cost":          s.CPUCost,
		"memory_cost":       s.MemoryCost,
		"total_cost":        s.TotalCost,
		"is_spot":           s.IsSpot,
		"interval_seconds":  s.IntervalSeconds,
	}
}

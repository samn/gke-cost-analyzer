package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Reader queries BigQuery for historical cost data.
type Reader struct {
	httpClient *http.Client
	project    string
	dataset    string
	table      string
	baseURL    string
}

// ReaderOption configures a Reader.
type ReaderOption func(*Reader)

// WithReaderHTTPClient sets a custom HTTP client (for auth or testing).
func WithReaderHTTPClient(c *http.Client) ReaderOption {
	return func(r *Reader) { r.httpClient = c }
}

// WithReaderBaseURL overrides the BigQuery API base URL (for testing).
func WithReaderBaseURL(url string) ReaderOption {
	return func(r *Reader) { r.baseURL = url }
}

// NewReader creates a BigQuery reader for querying cost data.
func NewReader(project, dataset, table string, opts ...ReaderOption) *Reader {
	r := &Reader{
		httpClient: http.DefaultClient,
		project:    project,
		dataset:    dataset,
		table:      table,
		baseURL:    bigqueryAPIBase,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// queryRequest is the body for POST /projects/{projectId}/queries.
type queryRequest struct {
	Query        string `json:"query"`
	UseLegacySQL bool   `json:"useLegacySql"`
	MaxResults   int    `json:"maxResults,omitempty"`
}

// queryResponse is the response from the BigQuery query API.
type queryResponse struct {
	Schema      responseSchema `json:"schema"`
	Rows        []responseRow  `json:"rows"`
	TotalRows   string         `json:"totalRows"`
	JobComplete bool           `json:"jobComplete"`
}

type responseSchema struct {
	Fields []responseField `json:"fields"`
}

type responseField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type responseRow struct {
	F []responseCell `json:"f"`
}

type responseCell struct {
	V any `json:"v"`
}

// query executes a SQL query and returns the raw response.
func (r *Reader) query(ctx context.Context, sql string) (*queryResponse, error) {
	reqBody := queryRequest{
		Query:        sql,
		UseLegacySQL: false,
		MaxResults:   10000,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling query request: %w", err)
	}

	url := fmt.Sprintf("%s/projects/%s/queries", r.baseURL, r.project)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating query request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending query request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("BigQuery query returned status %d: %s", resp.StatusCode, string(body))
	}

	var result queryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding query response: %w", err)
	}

	if !result.JobComplete {
		return nil, fmt.Errorf("BigQuery query did not complete synchronously; try a shorter time range")
	}

	return &result, nil
}

// tableRef returns the fully-qualified BigQuery table reference.
func (r *Reader) tableRef() string {
	return fmt.Sprintf("`%s.%s.%s`", r.project, r.dataset, r.table)
}

// buildFilterClause returns SQL WHERE conditions for optional filters.
func buildFilterClause(f QueryFilters) string {
	var clauses []string
	if f.ClusterName != "" {
		clauses = append(clauses, fmt.Sprintf("AND cluster_name = '%s'", escapeSQLString(f.ClusterName)))
	}
	if f.Namespace != "" {
		clauses = append(clauses, fmt.Sprintf("AND namespace = '%s'", escapeSQLString(f.Namespace)))
	}
	if f.Team != "" {
		clauses = append(clauses, fmt.Sprintf("AND team = '%s'", escapeSQLString(f.Team)))
	}
	return strings.Join(clauses, " ")
}

// escapeSQLString escapes single quotes in SQL string literals.
func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}

// QueryAggregatedCosts queries BigQuery for aggregated cost data since the given time.
func (r *Reader) QueryAggregatedCosts(ctx context.Context, since time.Time, filters QueryFilters) ([]HistoryCostRow, error) {
	seconds := int64(time.Since(since).Seconds())
	filterClause := buildFilterClause(filters)

	sql := fmt.Sprintf(`SELECT
  cluster_name,
  team, workload, subtype, namespace, cost_mode,
  LOGICAL_OR(is_spot) AS has_spot,
  AVG(pod_count) AS avg_pods,
  AVG(cpu_request_vcpu) AS avg_cpu_vcpu,
  AVG(memory_request_gb) AS avg_memory_gb,
  SUM(total_cost) AS total_cost,
  SUM(cpu_cost) AS total_cpu_cost,
  SUM(memory_cost) AS total_mem_cost,
  SAFE_DIVIDE(SUM(total_cost), TIMESTAMP_DIFF(MAX(timestamp), MIN(timestamp), SECOND) / 3600.0) AS avg_cost_per_hour,
  SUM(IFNULL(wasted_cost, 0)) AS total_wasted_cost,
  AVG(cpu_utilization) AS avg_cpu_util,
  AVG(memory_utilization) AS avg_mem_util,
  AVG(efficiency_score) AS avg_efficiency
FROM %s
WHERE timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL %d SECOND)
  %s
GROUP BY cluster_name, team, workload, subtype, namespace, cost_mode
ORDER BY total_cost DESC`,
		r.tableRef(), seconds, filterClause)

	resp, err := r.query(ctx, sql)
	if err != nil {
		return nil, err
	}

	return parseAggregatedRows(resp)
}

// QueryTimeSeries queries BigQuery for time-bucketed cost data for sparklines.
func (r *Reader) QueryTimeSeries(ctx context.Context, since time.Time, bucketSeconds int64, filters QueryFilters) ([]TimeSeriesPoint, error) {
	seconds := int64(time.Since(since).Seconds())
	filterClause := buildFilterClause(filters)

	sql := fmt.Sprintf(`SELECT
  cluster_name,
  team, workload, subtype, cost_mode,
  TIMESTAMP_SECONDS(DIV(UNIX_SECONDS(timestamp), %d) * %d) AS bucket,
  SUM(total_cost) AS bucket_cost
FROM %s
WHERE timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL %d SECOND)
  %s
GROUP BY cluster_name, team, workload, subtype, cost_mode, bucket
ORDER BY cluster_name, team, workload, subtype, cost_mode, bucket`,
		bucketSeconds, bucketSeconds, r.tableRef(), seconds, filterClause)

	resp, err := r.query(ctx, sql)
	if err != nil {
		return nil, err
	}

	return parseTimeSeriesRows(resp)
}

// parseAggregatedRows parses the BigQuery response into HistoryCostRow slices.
func parseAggregatedRows(resp *queryResponse) ([]HistoryCostRow, error) {
	var rows []HistoryCostRow
	for i, row := range resp.Rows {
		if len(row.F) < 18 {
			return nil, fmt.Errorf("row %d: expected at least 18 columns, got %d", i, len(row.F))
		}

		r := HistoryCostRow{
			ClusterName: cellString(row.F[0]),
			Team:        cellString(row.F[1]),
			Workload:    cellString(row.F[2]),
			Subtype:     cellString(row.F[3]),
			Namespace:   cellString(row.F[4]),
			CostMode:    cellString(row.F[5]),
		}

		r.HasSpot = cellString(row.F[6]) == "true"
		r.AvgPods = cellFloat(row.F[7])
		r.AvgCPUVCPU = cellFloat(row.F[8])
		r.AvgMemoryGB = cellFloat(row.F[9])
		r.TotalCost = cellFloat(row.F[10])
		r.TotalCPUCost = cellFloat(row.F[11])
		r.TotalMemCost = cellFloat(row.F[12])
		r.AvgCostPerHour = cellFloat(row.F[13])
		r.TotalWastedCost = cellFloat(row.F[14])
		r.AvgCPUUtil = cellFloatPtr(row.F[15])
		r.AvgMemUtil = cellFloatPtr(row.F[16])
		r.AvgEfficiency = cellFloatPtr(row.F[17])

		rows = append(rows, r)
	}
	return rows, nil
}

// parseTimeSeriesRows parses the BigQuery response into TimeSeriesPoint slices.
func parseTimeSeriesRows(resp *queryResponse) ([]TimeSeriesPoint, error) {
	var points []TimeSeriesPoint
	for i, row := range resp.Rows {
		if len(row.F) < 7 {
			return nil, fmt.Errorf("row %d: expected 7 columns, got %d", i, len(row.F))
		}

		p := TimeSeriesPoint{
			Key: WorkloadKey{
				ClusterName: cellString(row.F[0]),
				Team:        cellString(row.F[1]),
				Workload:    cellString(row.F[2]),
				Subtype:     cellString(row.F[3]),
				CostMode:    cellString(row.F[4]),
			},
		}

		// BigQuery returns TIMESTAMP as epoch seconds (as a string float).
		bucketStr := cellString(row.F[5])
		if bucketSec, err := strconv.ParseFloat(bucketStr, 64); err == nil {
			p.Bucket = time.Unix(int64(bucketSec), 0).UTC()
		}

		p.BucketCost = cellFloat(row.F[6])
		points = append(points, p)
	}
	return points, nil
}

// cellString extracts a string from a BigQuery response cell.
func cellString(c responseCell) string {
	if c.V == nil {
		return ""
	}
	s, ok := c.V.(string)
	if !ok {
		return fmt.Sprintf("%v", c.V)
	}
	return s
}

// cellFloat extracts a float64 from a BigQuery response cell.
func cellFloat(c responseCell) float64 {
	if c.V == nil {
		return 0
	}
	switch v := c.V.(type) {
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	case float64:
		return v
	default:
		return 0
	}
}

// cellFloatPtr extracts a *float64 from a BigQuery response cell (nil for NULL).
func cellFloatPtr(c responseCell) *float64 {
	if c.V == nil {
		return nil
	}
	f := cellFloat(c)
	return &f
}

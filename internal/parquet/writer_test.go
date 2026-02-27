package parquet

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
)

func testSnapshot(ts time.Time, team, workload string, podCount int) bigquery.CostSnapshot {
	return bigquery.CostSnapshot{
		Timestamp:       ts,
		ProjectID:       "test-project",
		Region:          "us-central1",
		ClusterName:     "test-cluster",
		Namespace:       "default",
		Team:            team,
		Workload:        workload,
		Subtype:         "",
		PodCount:        podCount,
		CPURequestVCPU:  1.5,
		MemoryRequestGB: 3.0,
		CPUCost:         0.105,
		MemoryCost:      0.024,
		TotalCost:       0.129,
		IsSpot:          false,
		IntervalSeconds: 300,
	}
}

func TestSnapshotToRowAndBack(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	snap := testSnapshot(ts, "platform", "web", 3)
	snap.IsSpot = true
	snap.Subtype = "api"

	row := SnapshotToRow(snap)

	if row.Timestamp != ts.UnixMicro() {
		t.Errorf("timestamp = %d, want %d", row.Timestamp, ts.UnixMicro())
	}
	if row.ProjectID != "test-project" {
		t.Errorf("project_id = %s, want test-project", row.ProjectID)
	}
	if row.PodCount != 3 {
		t.Errorf("pod_count = %d, want 3", row.PodCount)
	}
	if !row.IsSpot {
		t.Error("is_spot should be true")
	}
	if row.Subtype != "api" {
		t.Errorf("subtype = %s, want api", row.Subtype)
	}

	back := RowToSnapshot(row)
	if !back.Timestamp.Equal(ts) {
		t.Errorf("round-trip timestamp = %v, want %v", back.Timestamp, ts)
	}
	if back.Team != "platform" {
		t.Errorf("round-trip team = %s, want platform", back.Team)
	}
	if back.PodCount != 3 {
		t.Errorf("round-trip pod_count = %d, want 3", back.PodCount)
	}
	if !back.IsSpot {
		t.Error("round-trip is_spot should be true")
	}
}

func TestAppendToFileCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.parquet")

	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	snapshots := []bigquery.CostSnapshot{
		testSnapshot(ts, "platform", "web", 2),
		testSnapshot(ts, "ml", "training", 5),
	}

	if err := AppendToFile(path, snapshots); err != nil {
		t.Fatalf("AppendToFile: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].Team != "platform" {
		t.Errorf("row 0 team = %s, want platform", got[0].Team)
	}
	if got[1].Team != "ml" {
		t.Errorf("row 1 team = %s, want ml", got[1].Team)
	}
}

func TestAppendToFileAppendsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.parquet")

	ts1 := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	ts2 := time.Date(2025, 6, 15, 12, 5, 0, 0, time.UTC)

	// Write first batch
	batch1 := []bigquery.CostSnapshot{
		testSnapshot(ts1, "platform", "web", 2),
	}
	if err := AppendToFile(path, batch1); err != nil {
		t.Fatalf("first AppendToFile: %v", err)
	}

	// Append second batch
	batch2 := []bigquery.CostSnapshot{
		testSnapshot(ts2, "ml", "training", 5),
		testSnapshot(ts2, "data", "pipeline", 3),
	}
	if err := AppendToFile(path, batch2); err != nil {
		t.Fatalf("second AppendToFile: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}
	if got[0].Team != "platform" {
		t.Errorf("row 0 team = %s, want platform", got[0].Team)
	}
	if got[1].Team != "ml" {
		t.Errorf("row 1 team = %s, want ml", got[1].Team)
	}
	if got[2].Team != "data" {
		t.Errorf("row 2 team = %s, want data", got[2].Team)
	}
}

func TestAppendToFilePreservesAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.parquet")

	ts := time.Date(2025, 6, 15, 12, 30, 45, 0, time.UTC)
	snap := bigquery.CostSnapshot{
		Timestamp:       ts,
		ProjectID:       "my-project",
		Region:          "eu-west1",
		ClusterName:     "prod-cluster",
		Namespace:       "production",
		Team:            "backend",
		Workload:        "api-server",
		Subtype:         "grpc",
		PodCount:        10,
		CPURequestVCPU:  4.0,
		MemoryRequestGB: 8.0,
		CPUCost:         0.50,
		MemoryCost:      0.30,
		TotalCost:       0.80,
		IsSpot:          true,
		IntervalSeconds: 600,
	}

	if err := AppendToFile(path, []bigquery.CostSnapshot{snap}); err != nil {
		t.Fatalf("AppendToFile: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}

	g := got[0]
	if !g.Timestamp.Equal(ts) {
		t.Errorf("timestamp = %v, want %v", g.Timestamp, ts)
	}
	if g.ProjectID != "my-project" {
		t.Errorf("project_id = %s, want my-project", g.ProjectID)
	}
	if g.Region != "eu-west1" {
		t.Errorf("region = %s, want eu-west1", g.Region)
	}
	if g.ClusterName != "prod-cluster" {
		t.Errorf("cluster_name = %s, want prod-cluster", g.ClusterName)
	}
	if g.Namespace != "production" {
		t.Errorf("namespace = %s, want production", g.Namespace)
	}
	if g.Team != "backend" {
		t.Errorf("team = %s, want backend", g.Team)
	}
	if g.Workload != "api-server" {
		t.Errorf("workload = %s, want api-server", g.Workload)
	}
	if g.Subtype != "grpc" {
		t.Errorf("subtype = %s, want grpc", g.Subtype)
	}
	if g.PodCount != 10 {
		t.Errorf("pod_count = %d, want 10", g.PodCount)
	}
	if g.CPURequestVCPU != 4.0 {
		t.Errorf("cpu_request_vcpu = %f, want 4.0", g.CPURequestVCPU)
	}
	if g.MemoryRequestGB != 8.0 {
		t.Errorf("memory_request_gb = %f, want 8.0", g.MemoryRequestGB)
	}
	if g.CPUCost != 0.50 {
		t.Errorf("cpu_cost = %f, want 0.50", g.CPUCost)
	}
	if g.MemoryCost != 0.30 {
		t.Errorf("memory_cost = %f, want 0.30", g.MemoryCost)
	}
	if g.TotalCost != 0.80 {
		t.Errorf("total_cost = %f, want 0.80", g.TotalCost)
	}
	if !g.IsSpot {
		t.Error("is_spot should be true")
	}
	if g.IntervalSeconds != 600 {
		t.Errorf("interval_seconds = %d, want 600", g.IntervalSeconds)
	}
}

func TestReadFileNonExistent(t *testing.T) {
	_, err := ReadFile("/nonexistent/path.parquet")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestAppendToFileEmptySnapshots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.parquet")

	// Write initial data
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := AppendToFile(path, []bigquery.CostSnapshot{testSnapshot(ts, "a", "b", 1)}); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Append empty slice — should preserve existing data
	if err := AppendToFile(path, nil); err != nil {
		t.Fatalf("empty append: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row after empty append, got %d", len(got))
	}
}

func TestSchemaFieldCountInSync(t *testing.T) {
	bqFields := reflect.TypeOf(bigquery.CostSnapshot{}).NumField()
	pqFields := reflect.TypeOf(Row{}).NumField()
	if bqFields != pqFields {
		t.Errorf("BigQuery CostSnapshot has %d fields but Parquet Row has %d — schemas are out of sync", bqFields, pqFields)
	}
}

func TestAppendToFileCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.parquet")

	// Write garbage data
	if err := os.WriteFile(path, []byte("not a parquet file"), 0o644); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	err := AppendToFile(path, []bigquery.CostSnapshot{testSnapshot(ts, "a", "b", 1)})
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	pqwriter "github.com/samn/autopilot-cost-analyzer/internal/parquet"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

func TestAggregatedToSnapshot(t *testing.T) {
	sc := snapshotConfig{
		projectID:   "test-project",
		region:      "us-central1",
		clusterName: "test-cluster",
	}

	// Per-hour rates: these represent the hourly cost for the aggregation group.
	// The snapshot should store cost for the interval window (300s = 5min = 1/12 hour).
	agg := cost.AggregatedCost{
		Key: cost.GroupKey{
			Team:     "platform",
			Workload: "web",
			Subtype:  "api",
			IsSpot:   false,
		},
		Namespace:      "default",
		PodCount:       3,
		TotalCPUVCPU:   1.5,
		TotalMemGB:     3.0,
		CPUCostPerHour: 1.26,
		MemCostPerHour: 0.288,
		CostPerHour:    1.548,
	}

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	snap := aggregatedToSnapshot(agg, ts, sc, 300)

	if snap.ProjectID != "test-project" {
		t.Errorf("project = %s, want test-project", snap.ProjectID)
	}
	if snap.Region != "us-central1" {
		t.Errorf("region = %s, want us-central1", snap.Region)
	}
	if snap.ClusterName != "test-cluster" {
		t.Errorf("cluster = %s, want test-cluster", snap.ClusterName)
	}
	if snap.Team != "platform" {
		t.Errorf("team = %s, want platform", snap.Team)
	}
	if snap.Workload != "web" {
		t.Errorf("workload = %s, want web", snap.Workload)
	}
	if snap.Subtype != "api" {
		t.Errorf("subtype = %s, want api", snap.Subtype)
	}
	if snap.Namespace != "default" {
		t.Errorf("namespace = %s, want default", snap.Namespace)
	}
	if snap.PodCount != 3 {
		t.Errorf("pod count = %d, want 3", snap.PodCount)
	}
	if snap.CPURequestVCPU != 1.5 {
		t.Errorf("cpu request = %f, want 1.5", snap.CPURequestVCPU)
	}
	if snap.MemoryRequestGB != 3.0 {
		t.Errorf("mem request = %f, want 3.0", snap.MemoryRequestGB)
	}
	// 300s = 1/12 hour. Costs should be per-hour rate × interval hours.
	intervalHours := 300.0 / 3600.0
	wantCPUCost := 1.26 * intervalHours    // 0.105
	wantMemCost := 0.288 * intervalHours   // 0.024
	wantTotalCost := 1.548 * intervalHours // 0.129
	if snap.CPUCost != wantCPUCost {
		t.Errorf("cpu cost = %f, want %f", snap.CPUCost, wantCPUCost)
	}
	if snap.MemoryCost != wantMemCost {
		t.Errorf("mem cost = %f, want %f", snap.MemoryCost, wantMemCost)
	}
	if snap.TotalCost != wantTotalCost {
		t.Errorf("total cost = %f, want %f", snap.TotalCost, wantTotalCost)
	}
	if snap.IntervalSeconds != 300 {
		t.Errorf("interval = %d, want 300", snap.IntervalSeconds)
	}
	if snap.IsSpot {
		t.Error("should not be spot")
	}
}

func TestSnapshotCostSumsCorrectlyOverDay(t *testing.T) {
	// Verify the key invariant: SUM(total_cost) across a day's snapshots should
	// equal cost_per_hour × 24 for a pod running the entire day.
	sc := snapshotConfig{projectID: "p", region: "r", clusterName: "c"}

	costPerHour := 0.50 // $0.50/hr
	intervalSecs := int64(300)
	intervalHours := float64(intervalSecs) / 3600.0
	snapshotsPerDay := int(86400 / intervalSecs) // 288

	agg := cost.AggregatedCost{
		Key:            cost.GroupKey{Team: "t", Workload: "w"},
		CostPerHour:    costPerHour,
		CPUCostPerHour: 0.30,
		MemCostPerHour: 0.20,
	}

	var totalCostSum float64
	for i := 0; i < snapshotsPerDay; i++ {
		snap := aggregatedToSnapshot(agg, time.Now(), sc, intervalSecs)
		totalCostSum += snap.TotalCost
	}

	expectedDailyCost := costPerHour * 24.0 // $12.00
	// Each snapshot contributes costPerHour * intervalHours = 0.50 * (300/3600) ≈ 0.04167
	// 288 snapshots × 0.04167 = 12.00
	expectedFromIntervals := costPerHour * intervalHours * float64(snapshotsPerDay)
	if expectedFromIntervals != expectedDailyCost {
		t.Fatalf("sanity check failed: %f != %f", expectedFromIntervals, expectedDailyCost)
	}

	const epsilon = 1e-9
	if diff := totalCostSum - expectedDailyCost; diff > epsilon || diff < -epsilon {
		t.Errorf("SUM(total_cost) over day = %f, want %f (diff = %f)",
			totalCostSum, expectedDailyCost, diff)
	}
}

func TestAggregatedToSnapshotSpot(t *testing.T) {
	sc := snapshotConfig{
		projectID:   "proj",
		region:      "eu-west1",
		clusterName: "spot-cluster",
	}

	agg := cost.AggregatedCost{
		Key: cost.GroupKey{
			Team:     "ml",
			Workload: "training",
			IsSpot:   true,
		},
		Namespace:   "ml-ns",
		PodCount:    10,
		CostPerHour: 9.0,
	}

	ts := time.Now()
	snap := aggregatedToSnapshot(agg, ts, sc, 600)
	if !snap.IsSpot {
		t.Error("should be spot")
	}
	if snap.IntervalSeconds != 600 {
		t.Errorf("interval = %d, want 600", snap.IntervalSeconds)
	}
	// 600s = 1/6 hour. TotalCost = 9.0 * (600/3600) = 1.5
	wantCost := 9.0 * (600.0 / 3600.0)
	if snap.TotalCost != wantCost {
		t.Errorf("total cost = %f, want %f", snap.TotalCost, wantCost)
	}
}

func TestRecordSnapshot(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
			kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	var receivedRows int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Rows []json.RawMessage `json:"rows"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		receivedRows = len(body.Rows)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	writer := bigquery.NewWriter("proj", "ds", "tbl", bigquery.WithWriterBaseURL(srv.URL))
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	err := recordSnapshot(context.Background(), lister, calc, lc, writer, nil, sc, 300, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both pods have the same labels, so they aggregate into 1 group → 1 row
	if receivedRows != 1 {
		t.Errorf("expected 1 BQ row, got %d", receivedRows)
	}
}

func TestRecordSnapshotDryRun(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
			kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Pass nil writer for dry-run
	err := recordSnapshot(context.Background(), lister, calc, lc, nil, nil, sc, 300, "")

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("reading captured output: %v", err)
	}
	output := buf.String()

	// Verify JSON output is present
	if !strings.Contains(output, `"team":"platform"`) {
		t.Errorf("expected JSON with team field, got: %s", output)
	}
	if !strings.Contains(output, `"workload":"web"`) {
		t.Errorf("expected JSON with workload field, got: %s", output)
	}
	if !strings.Contains(output, "Would write 1 records (2 pods)") {
		t.Errorf("expected dry-run summary message, got: %s", output)
	}

	// Verify the JSON line is valid
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var snapshot bigquery.CostSnapshot
	if err := json.Unmarshal([]byte(lines[0]), &snapshot); err != nil {
		t.Fatalf("first line is not valid JSON: %v", err)
	}
	if snapshot.PodCount != 2 {
		t.Errorf("expected pod count 2, got %d", snapshot.PodCount)
	}
}

func TestRecordSnapshotListError(t *testing.T) {
	lister := &mockPodLister{
		err: context.DeadlineExceeded,
	}

	pt := pricing.FromPrices(nil)
	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	writer := bigquery.NewWriter("proj", "ds", "tbl")
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	err := recordSnapshot(context.Background(), lister, calc, lc, writer, nil, sc, 300, "")
	if err == nil {
		t.Fatal("expected error from list failure")
	}
	if !strings.Contains(err.Error(), "listing pods") {
		t.Errorf("error should mention listing pods, got: %v", err)
	}
}

func TestRecordRequiresRegion(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = ""
	project = "proj"
	clusterName = "cluster"
	recordInterval = 5 * time.Minute

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --region is missing")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error should mention --region, got: %v", err)
	}
}

func TestRecordRequiresProject(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	project = ""
	clusterName = "cluster"
	recordInterval = 5 * time.Minute

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --project is missing")
	}
	if !strings.Contains(err.Error(), "--project") {
		t.Errorf("error should mention --project, got: %v", err)
	}
}

func TestRecordRequiresClusterName(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	project = "proj"
	clusterName = ""
	recordInterval = 5 * time.Minute

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --cluster-name is missing")
	}
	if !strings.Contains(err.Error(), "--cluster-name") {
		t.Errorf("error should mention --cluster-name, got: %v", err)
	}
}

func TestRecordSnapshotEmptyPods(t *testing.T) {
	lister := &mockPodLister{pods: nil}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
	})

	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	// Capture stdout for dry-run
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := recordSnapshot(context.Background(), lister, calc, lc, nil, nil, sc, 300, "")

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error for empty pods: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("reading captured output: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "Would write 0 records (0 pods)") {
		t.Errorf("expected dry-run summary for 0 pods, got: %s", output)
	}
}

func TestRecordSnapshotMultipleGroups(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
			kube.NewTestPodInfo("worker-1", "batch", 1000, 1024, startTime, true,
				map[string]string{"team": "data", "app": "etl"}),
		},
	}

	var receivedRows int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Rows []json.RawMessage `json:"rows"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		receivedRows = len(body.Rows)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.00001},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	writer := bigquery.NewWriter("proj", "ds", "tbl", bigquery.WithWriterBaseURL(srv.URL))
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	err := recordSnapshot(context.Background(), lister, calc, lc, writer, nil, sc, 300, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Different teams/workloads/spot → 2 groups → 2 rows
	if receivedRows != 2 {
		t.Errorf("expected 2 BQ rows, got %d", receivedRows)
	}
}

func TestRecordSnapshotWriteError(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"access denied"}`))
	}))
	defer srv.Close()

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	writer := bigquery.NewWriter("proj", "ds", "tbl", bigquery.WithWriterBaseURL(srv.URL))
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	err := recordSnapshot(context.Background(), lister, calc, lc, writer, nil, sc, 300, "")
	if err == nil {
		t.Fatal("expected error from BigQuery write failure")
	}
	if !strings.Contains(err.Error(), "writing to BigQuery") {
		t.Errorf("error should mention BigQuery write, got: %v", err)
	}
}

func TestRecordRejectsNegativeInterval(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	project = "proj"
	clusterName = "cluster"
	recordInterval = -5 * time.Minute

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error for negative interval")
	}
	if !strings.Contains(err.Error(), "--interval") {
		t.Errorf("error should mention --interval, got: %v", err)
	}
}

func TestRecordRejectsZeroInterval(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	project = "proj"
	clusterName = "cluster"
	recordInterval = 0

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error for zero interval")
	}
	if !strings.Contains(err.Error(), "--interval") {
		t.Errorf("error should mention --interval, got: %v", err)
	}
}

func TestRecordOutputFileRequiresDryRun(t *testing.T) {
	saved := region
	savedProject := project
	savedCluster := clusterName
	savedInterval := recordInterval
	savedDryRun := dryRun
	savedOutputFile := outputFile
	defer func() {
		region = saved
		project = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
		dryRun = savedDryRun
		outputFile = savedOutputFile
	}()
	region = "us-central1"
	project = "proj"
	clusterName = "cluster"
	recordInterval = 5 * time.Minute
	dryRun = false
	outputFile = "/tmp/test.parquet"

	err := runRecord(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --output-file used without --dry-run")
	}
	if !strings.Contains(err.Error(), "--output-file requires --dry-run") {
		t.Errorf("error should mention --output-file requires --dry-run, got: %v", err)
	}
}

func TestRecordSnapshotParquetOutput(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
			kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	dir := t.TempDir()
	pqFile := dir + "/snapshots.parquet"

	// Pass nil writer (dry-run) with parquet file
	err := recordSnapshot(context.Background(), lister, calc, lc, nil, nil, sc, 300, pqFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back and verify
	got, err := pqwriter.ReadFile(pqFile)
	if err != nil {
		t.Fatalf("reading parquet: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Team != "platform" {
		t.Errorf("team = %s, want platform", got[0].Team)
	}
	if got[0].Workload != "web" {
		t.Errorf("workload = %s, want web", got[0].Workload)
	}
	if got[0].PodCount != 2 {
		t.Errorf("pod_count = %d, want 2", got[0].PodCount)
	}
}

func TestRecordSnapshotParquetAppends(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	dir := t.TempDir()
	pqFile := dir + "/snapshots.parquet"

	// First snapshot
	err := recordSnapshot(context.Background(), lister, calc, lc, nil, nil, sc, 300, pqFile)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// Second snapshot
	err = recordSnapshot(context.Background(), lister, calc, lc, nil, nil, sc, 300, pqFile)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	got, err := pqwriter.ReadFile(pqFile)
	if err != nil {
		t.Fatalf("reading parquet: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows after two appends, got %d", len(got))
	}
}

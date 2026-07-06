package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
	"github.com/samn/gke-cost-analyzer/internal/cost"
	"github.com/samn/gke-cost-analyzer/internal/kube"
	pqwriter "github.com/samn/gke-cost-analyzer/internal/parquet"
	"github.com/samn/gke-cost-analyzer/internal/pricing"
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
			Namespace: "default",
			Team:      "platform",
			Workload:  "web",
			Subtype:   "api",
			IsSpot:    false,
		},
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

func TestAggregatedToSnapshotWastedCostNormalized(t *testing.T) {
	sc := snapshotConfig{
		projectID:   "test-project",
		region:      "us-central1",
		clusterName: "test-cluster",
	}

	agg := cost.AggregatedCost{
		Key:               cost.GroupKey{Team: "platform", Workload: "web"},
		CostPerHour:       2.0,
		CPUCostPerHour:    1.2,
		MemCostPerHour:    0.8,
		HasUtilization:    true,
		CPUUtilization:    0.5,
		MemUtilization:    0.5,
		EfficiencyScore:   0.5,
		WastedCostPerHour: 1.0, // 2.0 * (1 - 0.5)
	}

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	snap := aggregatedToSnapshot(agg, ts, sc, 300)

	// WastedCost should be normalized to the interval: 1.0 * (300/3600) = 0.08333...
	intervalHours := 300.0 / 3600.0
	wantWasted := 1.0 * intervalHours
	if snap.WastedCost == nil {
		t.Fatal("WastedCost should not be nil when HasUtilization is true")
	}
	if *snap.WastedCost != wantWasted {
		t.Errorf("WastedCost = %f, want %f", *snap.WastedCost, wantWasted)
	}

	// Also verify it's NOT the per-hour rate
	if *snap.WastedCost == agg.WastedCostPerHour {
		t.Error("WastedCost should be interval-normalized, not the per-hour rate")
	}
}

func TestWastedCostSumsCorrectlyOverDay(t *testing.T) {
	// Verify SUM(wasted_cost) across a day's snapshots equals
	// wasted_cost_per_hour × 24 for a consistent workload.
	sc := snapshotConfig{projectID: "p", region: "r", clusterName: "c"}

	wastedPerHour := 0.40
	intervalSecs := int64(300)
	snapshotsPerDay := int(86400 / intervalSecs) // 288

	agg := cost.AggregatedCost{
		Key:               cost.GroupKey{Team: "t", Workload: "w"},
		CostPerHour:       1.0,
		CPUCostPerHour:    0.6,
		MemCostPerHour:    0.4,
		HasUtilization:    true,
		CPUUtilization:    0.6,
		MemUtilization:    0.6,
		EfficiencyScore:   0.6,
		WastedCostPerHour: wastedPerHour,
	}

	var wastedSum float64
	for i := 0; i < snapshotsPerDay; i++ {
		snap := aggregatedToSnapshot(agg, time.Now(), sc, intervalSecs)
		wastedSum += *snap.WastedCost
	}

	expectedDailyWaste := wastedPerHour * 24.0

	const epsilon = 1e-9
	if diff := wastedSum - expectedDailyWaste; diff > epsilon || diff < -epsilon {
		t.Errorf("SUM(wasted_cost) over day = %f, want %f (diff = %f)",
			wastedSum, expectedDailyWaste, diff)
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
			Namespace: "ml-ns",
			Team:      "ml",
			Workload:  "training",
			IsSpot:    true,
		},
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

	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, writer, nil, sc, 300, "")
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
	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, nil, nil, sc, 300, "")

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

	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, writer, nil, sc, 300, "")
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

	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, nil, nil, sc, 300, "")

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

	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, writer, nil, sc, 300, "")
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

	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, writer, nil, sc, 300, "")
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
	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, nil, nil, sc, 300, pqFile)
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

func TestAggregatedToSnapshotCostMode(t *testing.T) {
	sc := snapshotConfig{projectID: "proj", region: "r", clusterName: "c"}

	agg := cost.AggregatedCost{
		Key:      cost.GroupKey{Team: "t", Workload: "w", CostMode: "standard"},
		CostMode: "standard",
	}

	snap := aggregatedToSnapshot(agg, time.Now(), sc, 300)
	if snap.CostMode != "standard" {
		t.Errorf("CostMode = %s, want standard", snap.CostMode)
	}

	// Autopilot mode
	agg.CostMode = "autopilot"
	snap = aggregatedToSnapshot(agg, time.Now(), sc, 300)
	if snap.CostMode != "autopilot" {
		t.Errorf("CostMode = %s, want autopilot", snap.CostMode)
	}
}

func TestAggregatedToSnapshotUtilizationFields(t *testing.T) {
	sc := snapshotConfig{projectID: "proj", region: "r", clusterName: "c"}

	// With utilization data
	agg := cost.AggregatedCost{
		Key:               cost.GroupKey{Team: "t", Workload: "w"},
		CostPerHour:       1.0,
		CPUCostPerHour:    0.6,
		MemCostPerHour:    0.4,
		HasUtilization:    true,
		CPUUtilization:    0.75,
		MemUtilization:    0.50,
		EfficiencyScore:   0.65,
		WastedCostPerHour: 0.35,
	}

	snap := aggregatedToSnapshot(agg, time.Now(), sc, 3600) // 1 hour interval

	if snap.CPUUtilization == nil || *snap.CPUUtilization != 0.75 {
		t.Errorf("CPUUtilization = %v, want 0.75", snap.CPUUtilization)
	}
	if snap.MemoryUtilization == nil || *snap.MemoryUtilization != 0.50 {
		t.Errorf("MemoryUtilization = %v, want 0.50", snap.MemoryUtilization)
	}
	if snap.EfficiencyScore == nil || *snap.EfficiencyScore != 0.65 {
		t.Errorf("EfficiencyScore = %v, want 0.65", snap.EfficiencyScore)
	}
	// WastedCost = WastedCostPerHour × intervalHours = 0.35 × 1.0 = 0.35
	if snap.WastedCost == nil || *snap.WastedCost != 0.35 {
		t.Errorf("WastedCost = %v, want 0.35", snap.WastedCost)
	}

	// Without utilization data — all nullable fields should be nil
	agg.HasUtilization = false
	snap = aggregatedToSnapshot(agg, time.Now(), sc, 3600)

	if snap.CPUUtilization != nil {
		t.Error("CPUUtilization should be nil when HasUtilization is false")
	}
	if snap.MemoryUtilization != nil {
		t.Error("MemoryUtilization should be nil when HasUtilization is false")
	}
	if snap.EfficiencyScore != nil {
		t.Error("EfficiencyScore should be nil when HasUtilization is false")
	}
	if snap.WastedCost != nil {
		t.Error("WastedCost should be nil when HasUtilization is false")
	}
}

func TestNeedsStandard(t *testing.T) {
	saved := mode
	defer func() { mode = saved }()

	tests := []struct {
		mode string
		want bool
	}{
		{"standard", true},
		{"all", true},
		{"autopilot", false},
	}

	for _, tt := range tests {
		mode = tt.mode
		if got := needsStandard(); got != tt.want {
			t.Errorf("needsStandard() with mode=%q = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestNeedsAutopilot(t *testing.T) {
	saved := mode
	defer func() { mode = saved }()

	tests := []struct {
		mode string
		want bool
	}{
		{"autopilot", true},
		{"all", true},
		{"standard", false},
	}

	for _, tt := range tests {
		mode = tt.mode
		if got := needsAutopilot(); got != tt.want {
			t.Errorf("needsAutopilot() with mode=%q = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestClusterMode(t *testing.T) {
	saved := mode
	defer func() { mode = saved }()

	mode = "autopilot"
	if got := clusterMode(); got != kube.ClusterModeAutopilot {
		t.Errorf("clusterMode() = %v, want ClusterModeAutopilot", got)
	}

	mode = "standard"
	if got := clusterMode(); got != kube.ClusterModeStandard {
		t.Errorf("clusterMode() = %v, want ClusterModeStandard", got)
	}

	mode = "all"
	if got := clusterMode(); got != kube.ClusterModeAll {
		t.Errorf("clusterMode() = %v, want ClusterModeAll", got)
	}

	mode = "something-else"
	if got := clusterMode(); got != kube.ClusterModeAll {
		t.Errorf("clusterMode() with unknown mode = %v, want ClusterModeAll", got)
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
	err := recordSnapshot(context.Background(), lister, calc, nil, nil, lc, nil, nil, sc, 300, pqFile)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// Second snapshot
	err = recordSnapshot(context.Background(), lister, calc, nil, nil, lc, nil, nil, sc, 300, pqFile)
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

func TestListNamespace(t *testing.T) {
	savedMode := mode
	savedNS := namespace
	defer func() { mode = savedMode; namespace = savedNS }()

	// Standard-mode attribution needs the full pod set per node, so the API
	// listing must be cluster-wide and the namespace applied post-calculation.
	mode = "all"
	namespace = "team-ns"
	apiNS, postNS := listNamespace()
	if apiNS != "" || postNS != "team-ns" {
		t.Errorf("mode=all ns=team-ns: got api=%q post=%q, want api=\"\" post=\"team-ns\"", apiNS, postNS)
	}

	mode = "standard"
	apiNS, postNS = listNamespace()
	if apiNS != "" || postNS != "team-ns" {
		t.Errorf("mode=standard ns=team-ns: got api=%q post=%q, want api=\"\" post=\"team-ns\"", apiNS, postNS)
	}

	// Autopilot costs are per-pod, so API-side filtering is safe and cheaper.
	mode = "autopilot"
	apiNS, postNS = listNamespace()
	if apiNS != "team-ns" || postNS != "" {
		t.Errorf("mode=autopilot ns=team-ns: got api=%q post=%q, want api=\"team-ns\" post=\"\"", apiNS, postNS)
	}

	// No namespace flag: no filtering anywhere.
	mode = "all"
	namespace = ""
	apiNS, postNS = listNamespace()
	if apiNS != "" || postNS != "" {
		t.Errorf("no ns: got api=%q post=%q, want empty", apiNS, postNS)
	}
}

func TestRecordSnapshotNamespaceFilterPreservesShares(t *testing.T) {
	// A --namespace filter must not inflate standard-mode cost shares: the
	// share denominator comes from ALL pods on the node, and filtering to the
	// target namespace happens after cost calculation.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			// Cluster-wide listing returns pods from both namespaces.
			kube.NewTestPodInfoOnNode("target-pod", "target", 1000, 4000, startTime, false,
				map[string]string{"team": "a", "app": "w1"}, "gke-node-1"),
			kube.NewTestPodInfoOnNode("other-pod", "other", 3000, 12000, startTime, false,
				map[string]string{"team": "b", "app": "w2"}, "gke-node-1"),
		},
	}

	cpt := pricing.FromComputePrices([]pricing.ComputePrice{
		{Region: "us-central1", MachineFamily: "n2", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.03},
		{Region: "us-central1", MachineFamily: "n2", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})
	stdCalc := cost.NewStandardCalculator("us-central1", cpt, func() time.Time { return now })
	stdCalc.SetNodes([]kube.NodeInfo{{
		Name: "gke-node-1", MachineFamily: "n2", VCPU: 4, MemoryGB: 16,
	}})

	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	sc := snapshotConfig{
		projectID: "proj", region: "us-central1", clusterName: "cluster",
		filterNamespace: "target",
	}

	dir := t.TempDir()
	pqFile := dir + "/ns-filter.parquet"

	err := recordSnapshot(context.Background(), lister, nil, stdCalc, nil, lc, nil, nil, sc, 3600, pqFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := pqwriter.ReadFile(pqFile)
	if err != nil {
		t.Fatalf("reading parquet: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row (target namespace only), got %d", len(got))
	}
	if got[0].Namespace != "target" {
		t.Errorf("namespace = %s, want target", got[0].Namespace)
	}

	// Node cost/hr: cpu 4×0.03=0.12, mem 16×0.004=0.064.
	// target-pod requests 1/4 of CPU (1 of 4 requested vCPU) and 1/4 of
	// memory (4 of 16 requested GB): 0.25×0.12 + 0.25×0.064 = 0.046/hr.
	// With a 3600s interval, total_cost = 0.046. If shares had been computed
	// from the filtered set, the pod would absorb the whole 0.184/hr.
	want := 0.25*0.12 + 0.25*0.064
	if diff := got[0].TotalCost - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total cost = %f, want %f (share from full pod set)", got[0].TotalCost, want)
	}
}

func TestShutdownSignalsIncludeSIGTERM(t *testing.T) {
	// Kubernetes sends SIGTERM on pod termination; a daemon that only traps
	// SIGINT never shuts down gracefully in-cluster.
	foundTerm, foundInt := false, false
	for _, s := range shutdownSignals {
		if s == syscall.SIGTERM {
			foundTerm = true
		}
		if s == os.Interrupt {
			foundInt = true
		}
	}
	if !foundTerm {
		t.Error("shutdownSignals must include syscall.SIGTERM")
	}
	if !foundInt {
		t.Error("shutdownSignals must include os.Interrupt")
	}
}

func TestSnapshotIntervalSecs(t *testing.T) {
	nominal := 5 * time.Minute
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	// First snapshot (no previous): use the nominal interval.
	if got := snapshotIntervalSecs(time.Time{}, now, nominal); got != 300 {
		t.Errorf("first snapshot interval = %d, want 300", got)
	}

	// Steady state: actual elapsed time, so missed/slow ticks don't
	// permanently undercount cost.
	last := now.Add(-7 * time.Minute) // one tick was missed
	if got := snapshotIntervalSecs(last, now, nominal); got != 420 {
		t.Errorf("elapsed interval = %d, want 420", got)
	}

	// Clock weirdness (now before last) falls back to nominal.
	if got := snapshotIntervalSecs(now.Add(time.Minute), now, nominal); got != 300 {
		t.Errorf("negative elapsed should fall back to nominal, got %d", got)
	}
}

func TestSnapshotTimeout(t *testing.T) {
	// A short --interval must not starve legitimate snapshots: the timeout
	// gets a floor so a snapshot that takes longer than a small interval can
	// still complete (otherwise lastSnapshot never advances and the daemon
	// records nothing forever).
	if got := snapshotTimeout(15 * time.Second); got != 2*time.Minute {
		t.Errorf("snapshotTimeout(15s) = %v, want the 2m floor", got)
	}
	if got := snapshotTimeout(5 * time.Minute); got != 5*time.Minute {
		t.Errorf("snapshotTimeout(5m) = %v, want 5m", got)
	}
}

func TestRefreshPricesNilCalculators(t *testing.T) {
	// With no active calculators the refresh is a no-op (no cache or catalog
	// access) and returns the inputs unchanged.
	ap, std, err := refreshPrices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ap != nil || std != nil {
		t.Errorf("expected nil calculators back, got %v / %v", ap, std)
	}
}

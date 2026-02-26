package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

func TestAggregatedToSnapshot(t *testing.T) {
	sc := snapshotConfig{
		projectID:   "test-project",
		region:      "us-central1",
		clusterName: "test-cluster",
	}

	agg := cost.AggregatedCost{
		Key: cost.GroupKey{
			Team:     "platform",
			Workload: "web",
			Subtype:  "api",
			IsSpot:   false,
		},
		Namespace:    "default",
		PodCount:     3,
		TotalCPUVCPU: 1.5,
		TotalMemGB:   3.0,
		CPUCost:      0.105,
		MemCost:      0.024,
		TotalCost:    0.129,
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
	if snap.CPUCost != 0.105 {
		t.Errorf("cpu cost = %f, want 0.105", snap.CPUCost)
	}
	if snap.MemoryCost != 0.024 {
		t.Errorf("mem cost = %f, want 0.024", snap.MemoryCost)
	}
	if snap.TotalCost != 0.129 {
		t.Errorf("total cost = %f, want 0.129", snap.TotalCost)
	}
	if snap.IntervalSeconds != 300 {
		t.Errorf("interval = %d, want 300", snap.IntervalSeconds)
	}
	if snap.IsSpot {
		t.Error("should not be spot")
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
		Namespace: "ml-ns",
		PodCount:  10,
		TotalCost: 1.5,
	}

	ts := time.Now()
	snap := aggregatedToSnapshot(agg, ts, sc, 600)
	if !snap.IsSpot {
		t.Error("should be spot")
	}
	if snap.IntervalSeconds != 600 {
		t.Errorf("interval = %d, want 600", snap.IntervalSeconds)
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	writer := bigquery.NewWriter("proj", "ds", "tbl", bigquery.WithWriterBaseURL(srv.URL))
	sc := snapshotConfig{projectID: "proj", region: "us-central1", clusterName: "cluster"}

	err := recordSnapshot(context.Background(), lister, calc, lc, writer, sc, 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both pods have the same labels, so they aggregate into 1 group → 1 row
	if receivedRows != 1 {
		t.Errorf("expected 1 BQ row, got %d", receivedRows)
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

	err := recordSnapshot(context.Background(), lister, calc, lc, writer, sc, 300)
	if err == nil {
		t.Fatal("expected error from list failure")
	}
	if !strings.Contains(err.Error(), "listing pods") {
		t.Errorf("error should mention listing pods, got: %v", err)
	}
}

func TestRecordRequiresRegion(t *testing.T) {
	saved := region
	savedProject := bqProject
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		bqProject = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = ""
	bqProject = "proj"
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
	savedProject := bqProject
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		bqProject = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	bqProject = ""
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
	savedProject := bqProject
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		bqProject = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	bqProject = "proj"
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

func TestRecordRejectsZeroInterval(t *testing.T) {
	saved := region
	savedProject := bqProject
	savedCluster := clusterName
	savedInterval := recordInterval
	defer func() {
		region = saved
		bqProject = savedProject
		clusterName = savedCluster
		recordInterval = savedInterval
	}()
	region = "us-central1"
	bqProject = "proj"
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

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

// mockPodLister implements the podLister interface for testing.
type mockPodLister struct {
	pods []kube.PodInfo
	err  error
}

func (m *mockPodLister) ListPods(_ context.Context) ([]kube.PodInfo, error) {
	return m.pods, m.err
}

func TestRenderTable(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	pods := []kube.PodInfo{
		kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
			map[string]string{"team": "platform", "app": "web"}),
		kube.NewTestPodInfo("web-2", "default", 500, 512, startTime, false,
			map[string]string{"team": "platform", "app": "web"}),
		kube.NewTestPodInfo("worker-1", "batch", 1000, 1024, startTime, true,
			map[string]string{"team": "data", "app": "etl"}),
	}

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.01},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	costs := calc.CalculateAll(pods)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	aggs := cost.Aggregate(costs, lc)

	var buf bytes.Buffer
	renderTable(&buf, aggs)
	output := buf.String()

	// Verify header is present
	if !strings.Contains(output, "TEAM") {
		t.Error("missing TEAM header")
	}
	if !strings.Contains(output, "WORKLOAD") {
		t.Error("missing WORKLOAD header")
	}
	if !strings.Contains(output, "$/HR") {
		t.Error("missing $/HR header")
	}
	if !strings.Contains(output, "SPOT") {
		t.Error("missing SPOT header")
	}

	// Verify team names appear
	if !strings.Contains(output, "platform") {
		t.Error("missing platform team")
	}
	if !strings.Contains(output, "data") {
		t.Error("missing data team")
	}

	// Verify SPOT column renders "yes" for spot pods
	if !strings.Contains(output, "yes") {
		t.Error("missing 'yes' in SPOT column for spot pods")
	}

	// Verify TOTAL row
	if !strings.Contains(output, "TOTAL") {
		t.Error("missing TOTAL row")
	}

	// Verify cost values are present (non-zero dollar amounts)
	if !strings.Contains(output, "$") {
		t.Error("missing dollar amounts in output")
	}
}

func TestRenderTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderTable(&buf, nil)
	output := buf.String()

	// Should still have header and total
	if !strings.Contains(output, "TEAM") {
		t.Error("missing header for empty table")
	}
	if !strings.Contains(output, "TOTAL") {
		t.Error("missing TOTAL for empty table")
	}
}

func TestRenderTableSortOrder(t *testing.T) {
	// Create aggregations in reverse order to verify sorting
	aggs := []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "zeta", Workload: "api"}, PodCount: 1, CostPerHour: 0.01},
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3, CostPerHour: 0.03},
	}

	var buf bytes.Buffer
	renderTable(&buf, aggs)
	output := buf.String()

	// renderTable does not sort (displayCosts does), but verify all appear
	if !strings.Contains(output, "zeta") {
		t.Error("missing zeta team")
	}
	if !strings.Contains(output, "alpha") {
		t.Error("missing alpha team")
	}
}

func TestDisplayCosts(t *testing.T) {
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

	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.Spot, UnitPrice: 0.01},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.Spot, UnitPrice: 0.0012},
	})

	calc := cost.NewCalculator("us-central1", pt, func() time.Time { return now })
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}

	err := displayCosts(context.Background(), lister, calc, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDisplayCostsListError(t *testing.T) {
	lister := &mockPodLister{
		err: context.DeadlineExceeded,
	}

	pt := pricing.FromPrices(nil)
	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}

	err := displayCosts(context.Background(), lister, calc, lc)
	if err == nil {
		t.Fatal("expected error from list failure")
	}
	if !strings.Contains(err.Error(), "listing pods") {
		t.Errorf("error should mention listing pods, got: %v", err)
	}
}

func TestWatchRequiresRegion(t *testing.T) {
	saved := region
	defer func() { region = saved }()
	region = ""

	err := runWatch(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --region is missing")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error should mention --region, got: %v", err)
	}
}

func TestWatchRejectsZeroInterval(t *testing.T) {
	saved := region
	savedInterval := watchInterval
	defer func() {
		region = saved
		watchInterval = savedInterval
	}()
	region = "us-central1"
	watchInterval = 0

	err := runWatch(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error for zero interval")
	}
	if !strings.Contains(err.Error(), "--interval") {
		t.Errorf("error should mention --interval, got: %v", err)
	}
}

func TestWatchRejectsNegativeInterval(t *testing.T) {
	saved := region
	savedInterval := watchInterval
	defer func() {
		region = saved
		watchInterval = savedInterval
	}()
	region = "us-central1"
	watchInterval = -5 * time.Second

	err := runWatch(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error for negative interval")
	}
	if !strings.Contains(err.Error(), "--interval") {
		t.Errorf("error should mention --interval, got: %v", err)
	}
}

func TestOrDefault(t *testing.T) {
	if orDefault("hello", "-") != "hello" {
		t.Error("expected hello")
	}
	if orDefault("", "-") != "-" {
		t.Error("expected -")
	}
}

package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

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

	// Verify team names appear
	if !strings.Contains(output, "platform") {
		t.Error("missing platform team")
	}
	if !strings.Contains(output, "data") {
		t.Error("missing data team")
	}

	// Verify TOTAL row
	if !strings.Contains(output, "TOTAL") {
		t.Error("missing TOTAL row")
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

func TestOrDefault(t *testing.T) {
	if orDefault("hello", "-") != "hello" {
		t.Error("expected hello")
	}
	if orDefault("", "-") != "-" {
		t.Error("expected -")
	}
}

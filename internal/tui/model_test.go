package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
)

type mockPodLister struct {
	pods []kube.PodInfo
	err  error
}

func (m *mockPodLister) ListPods(_ context.Context) ([]kube.PodInfo, error) {
	return m.pods, m.err
}

func testModel(lister PodLister) Model {
	ctx, cancel := context.WithCancel(context.Background())
	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})
	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	return NewModel(ctx, cancel, lister, calc, lc, 5*time.Second)
}

func TestModelInitialView(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	view := m.View()
	if !strings.Contains(view, "Loading") {
		t.Errorf("initial view should show loading, got: %s", view)
	}
}

func TestModelCostDataUpdate(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}
	m := testModel(lister)

	// Simulate receiving cost data
	msg := costDataMsg{
		aggs: []cost.AggregatedCost{
			{Key: cost.GroupKey{Team: "platform", Workload: "web"}, PodCount: 1, CostPerHour: 0.02},
		},
		podCount: 1,
	}

	updated, _ := m.Update(msg)
	view := updated.View()

	if strings.Contains(view, "Loading") {
		t.Error("view should not show loading after data received")
	}
	if !strings.Contains(view, "platform") {
		t.Error("view should contain team name")
	}
	if !strings.Contains(view, "1 pods") {
		t.Error("view should contain pod count")
	}
}

func TestModelErrorUpdate(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	// First give it some data so lastUpdate is set
	m.lastUpdate = time.Now()

	msg := errMsg{err: context.DeadlineExceeded}
	updated, _ := m.Update(msg)
	view := updated.View()

	if !strings.Contains(view, "error") {
		t.Errorf("view should show error, got: %s", view)
	}
}

func TestModelQuitOnQ(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command")
	}

	// Execute the command to verify it's a quit
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestModelQuitOnCtrlC(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command")
	}

	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestModelFetchCosts(t *testing.T) {
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
	m := testModel(lister)

	msg := m.fetchCosts()
	dataMsg, ok := msg.(costDataMsg)
	if !ok {
		t.Fatalf("expected costDataMsg, got %T", msg)
	}
	if dataMsg.podCount != 2 {
		t.Errorf("expected 2 pods, got %d", dataMsg.podCount)
	}
	if len(dataMsg.aggs) == 0 {
		t.Error("expected non-empty aggregations")
	}
}

func TestModelFetchCostsError(t *testing.T) {
	lister := &mockPodLister{err: context.DeadlineExceeded}
	m := testModel(lister)

	msg := m.fetchCosts()
	errMsg, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if errMsg.err == nil {
		t.Error("expected non-nil error")
	}
	if !strings.Contains(errMsg.err.Error(), "listing pods") {
		t.Errorf("error should mention listing pods, got: %v", errMsg.err)
	}
}

func TestModelFetchCostsSortsResults(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("z-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "zeta", "app": "api"}),
			kube.NewTestPodInfo("a-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "alpha", "app": "web"}),
			kube.NewTestPodInfo("a-2", "default", 500, 512, startTime, false,
				map[string]string{"team": "alpha", "app": "api"}),
		},
	}
	m := testModel(lister)

	msg := m.fetchCosts()
	dataMsg := msg.(costDataMsg)

	// Verify sorted by team, then workload
	if len(dataMsg.aggs) < 3 {
		t.Fatalf("expected at least 3 groups, got %d", len(dataMsg.aggs))
	}
	if dataMsg.aggs[0].Key.Team != "alpha" || dataMsg.aggs[0].Key.Workload != "api" {
		t.Errorf("first group should be alpha/api, got %s/%s", dataMsg.aggs[0].Key.Team, dataMsg.aggs[0].Key.Workload)
	}
	if dataMsg.aggs[1].Key.Team != "alpha" || dataMsg.aggs[1].Key.Workload != "web" {
		t.Errorf("second group should be alpha/web, got %s/%s", dataMsg.aggs[1].Key.Team, dataMsg.aggs[1].Key.Workload)
	}
	if dataMsg.aggs[2].Key.Team != "zeta" {
		t.Errorf("third group should be zeta, got %s", dataMsg.aggs[2].Key.Team)
	}
}

func TestModelInit(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}
}

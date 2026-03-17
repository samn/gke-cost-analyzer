package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/samn/autopilot-cost-analyzer/internal/prometheus"
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
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})
	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	return NewModel(ctx, cancel, lister, calc, nil, nil, lc, 5*time.Second, nil, "", false)
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

func TestModelViewShowsElapsedTime(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.startedAt = time.Now().Add(-5 * time.Minute)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()

	view := m.View()
	if !strings.Contains(view, "watching for 5m") {
		t.Errorf("expected 'watching for 5m' in view, got:\n%s", view)
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

func TestModelViewSortsResults(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	// Simulate receiving unsorted data
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "zeta", Workload: "api"}, PodCount: 1, CostPerHour: 0.01},
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3, CostPerHour: 0.03},
	}
	m.rebuildDisplay()

	view := m.View()

	// Default sort is team ascending, so alpha should appear before zeta
	alphaIdx := strings.Index(view, "alpha")
	zetaIdx := strings.Index(view, "zeta")
	if alphaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("expected both alpha and zeta in view:\n%s", view)
	}
	if alphaIdx > zetaIdx {
		t.Errorf("expected alpha before zeta with default sort, got alpha@%d zeta@%d", alphaIdx, zetaIdx)
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

func TestModelKeyPressSortToggle(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	// Default is team ascending
	if m.sortCfg.Column != SortByTeam || !m.sortCfg.Asc {
		t.Fatal("expected default sort to be team ascending")
	}

	// Press "3" -> PODS (no subtype in testModel)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m2 := updated.(Model)
	if m2.sortCfg.Column != SortByPods {
		t.Errorf("expected SortByPods, got %v", m2.sortCfg.Column)
	}
	if !m2.sortCfg.Asc {
		t.Error("expected ascending when switching to new column")
	}

	// Press "3" again -> toggle to descending
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m3 := updated.(Model)
	if m3.sortCfg.Column != SortByPods {
		t.Errorf("expected SortByPods, got %v", m3.sortCfg.Column)
	}
	if m3.sortCfg.Asc {
		t.Error("expected descending after toggle")
	}

	// Press "3" once more -> toggle back to ascending
	updated, _ = m3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m4 := updated.(Model)
	if m4.sortCfg.Asc != true {
		t.Error("expected ascending after second toggle")
	}
}

func TestModelKeyPressInvalidKey(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)

	// Press "8" which is invalid without subtype (only 1-7 valid)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'8'}})
	m2 := updated.(Model)
	// Sort config should remain unchanged
	if m2.sortCfg.Column != SortByTeam || !m2.sortCfg.Asc {
		t.Error("sort config should not change for invalid key")
	}
}

func TestModelHelpText(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()

	view := m.View()

	if !strings.Contains(view, "Sort:") {
		t.Errorf("expected help text with Sort: prefix in view:\n%s", view)
	}
	if !strings.Contains(view, "q=Quit") {
		t.Errorf("expected q=Quit in help text:\n%s", view)
	}
	if !strings.Contains(view, "Navigate") {
		t.Errorf("expected Navigate in help text:\n%s", view)
	}
	if !strings.Contains(view, "Expand/Collapse") {
		t.Errorf("expected Expand/Collapse in help text:\n%s", view)
	}
	// Without subtype, should not mention Subtype
	if strings.Contains(view, "Subtype") {
		t.Errorf("should not mention Subtype without subtype label:\n%s", view)
	}
}

func testModelWithSubtype(lister PodLister) Model {
	ctx, cancel := context.WithCancel(context.Background())
	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})
	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app", SubtypeLabel: "subtype"}
	return NewModel(ctx, cancel, lister, calc, nil, nil, lc, 5*time.Second, nil, "", false)
}

func TestModelHelpTextWithSubtype(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithSubtype(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web", Subtype: "grpc"}, PodCount: 1},
	}
	m.rebuildDisplay()

	view := m.View()

	if !strings.Contains(view, "3=Subtype") {
		t.Errorf("expected 3=Subtype in help text with subtype:\n%s", view)
	}
	if !strings.Contains(view, "8=Cost") {
		t.Errorf("expected 8=Cost in help text with subtype:\n%s", view)
	}
}

func TestModelSortKeyWithSubtype(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithSubtype(lister)

	// Press "3" -> SUBTYPE (with subtype)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m2 := updated.(Model)
	if m2.sortCfg.Column != SortBySubtype {
		t.Errorf("expected SortBySubtype with subtype, got %v", m2.sortCfg.Column)
	}

	// Press "8" -> COST (valid with subtype)
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'8'}})
	m3 := updated.(Model)
	if m3.sortCfg.Column != SortByCost {
		t.Errorf("expected SortByCost, got %v", m3.sortCfg.Column)
	}
}

func TestModelViewReflectsSortOrder(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1, CostPerHour: 0.01},
		{Key: cost.GroupKey{Team: "zeta", Workload: "api"}, PodCount: 5, CostPerHour: 0.05},
	}

	// Sort by pods descending
	m.sortCfg = SortConfig{Column: SortByPods, Asc: false}
	m.rebuildDisplay()
	view := m.View()

	// zeta (5 pods) should appear before alpha (1 pod)
	zetaIdx := strings.Index(view, "zeta")
	alphaIdx := strings.Index(view, "alpha")
	if zetaIdx < 0 || alphaIdx < 0 {
		t.Fatalf("expected both teams in view:\n%s", view)
	}
	if zetaIdx > alphaIdx {
		t.Errorf("expected zeta before alpha when sorting by pods desc")
	}
}

func TestModelSortIndicatorInView(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()

	// Default sort is team ascending
	view := m.View()
	if !strings.Contains(view, "TEAM ^") {
		t.Errorf("expected 'TEAM ^' sort indicator in view:\n%s", view)
	}
}

// testModelWithPrometheus creates a model with a real Prometheus client
// pointing at the given test server URL.
func testModelWithPrometheus(lister PodLister, promURL string) Model {
	return testModelWithPrometheusProject(lister, promURL, "test-project")
}

func testModelWithPrometheusProject(lister PodLister, promURL string, promProject string) Model {
	ctx, cancel := context.WithCancel(context.Background())
	pt := pricing.FromPrices([]pricing.Price{
		{Region: "us-central1", ResourceType: pricing.CPU, Tier: pricing.OnDemand, UnitPrice: 0.000035},
		{Region: "us-central1", ResourceType: pricing.Memory, Tier: pricing.OnDemand, UnitPrice: 0.004},
	})
	calc := cost.NewCalculator("us-central1", pt, nil)
	lc := cost.LabelConfig{TeamLabel: "team", WorkloadLabel: "app"}
	client := prometheus.NewClient(promURL)
	return NewModel(ctx, cancel, lister, calc, nil, nil, lc, 5*time.Second, client, promProject, false)
}

func TestModelShowUtilizationWhenPromClientSet(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheus(lister, "http://unused")
	if !m.showUtilization {
		t.Error("showUtilization should be true when promClient is set")
	}
}

func TestModelViewShowsPrometheusError(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheus(lister, "http://unused")
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()
	m.promErr = fmt.Errorf("connection refused")

	view := m.View()
	if !strings.Contains(view, "prometheus test-project error: connection refused") {
		t.Errorf("expected prometheus error with project in view, got:\n%s", view)
	}
}

func TestModelViewShowsNoUtilizationData(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheus(lister, "http://unused")
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()
	m.promErr = nil
	m.utilPodCount = 0

	view := m.View()
	if !strings.Contains(view, "prometheus test-project: no utilization data") {
		t.Errorf("expected 'no utilization data' with project in view, got:\n%s", view)
	}
}

func TestModelViewShowsUtilizationCount(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheus(lister, "http://unused")
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 3, HasUtilization: true},
	}
	m.rebuildDisplay()
	m.promErr = nil
	m.utilPodCount = 5

	view := m.View()
	if !strings.Contains(view, "utilization test-project: 5 pods") {
		t.Errorf("expected 'utilization: 5 pods' with project in view, got:\n%s", view)
	}
}

func TestModelViewShowsNoProjectWhenEmpty(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheusProject(lister, "http://unused", "")
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()
	m.promErr = nil
	m.utilPodCount = 0

	view := m.View()
	// Without a project, should show "prometheus:" without a project name
	if !strings.Contains(view, "(prometheus: no utilization data)") {
		t.Errorf("expected '(prometheus: no utilization data)' without project tag, got:\n%s", view)
	}
}

func TestModelViewHelpTextWithUtilization(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheus(lister, "http://unused")
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()

	view := m.View()
	if !strings.Contains(view, "8=CPU%") {
		t.Errorf("expected 8=CPU%% in help text with utilization:\n%s", view)
	}
	if !strings.Contains(view, "9=Waste") {
		t.Errorf("expected 9=Waste in help text with utilization:\n%s", view)
	}
}

// promResponseJSON is a helper to build a Prometheus API response.
type promResponseJSON struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func TestModelFetchCostsWithPrometheus(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")

		resp := promResponseJSON{}
		resp.Status = "success"
		resp.Data.ResultType = "vector"
		if strings.Contains(query, "cpu_usage_seconds_total") {
			resp.Data.Result = []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			}{
				{
					Metric: map[string]string{"namespace": "default", "pod": "web-1"},
					Value:  []any{1234567890.0, "0.25"},
				},
			}
		} else {
			resp.Data.Result = []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			}{
				{
					Metric: map[string]string{"namespace": "default", "pod": "web-1"},
					Value:  []any{1234567890.0, "268435456"},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	m := testModelWithPrometheus(lister, srv.URL)
	msg := m.fetchCosts()
	dataMsg, ok := msg.(costDataMsg)
	if !ok {
		t.Fatalf("expected costDataMsg, got %T", msg)
	}
	if dataMsg.podCount != 1 {
		t.Errorf("expected 1 pod, got %d", dataMsg.podCount)
	}
	if dataMsg.promErr != nil {
		t.Errorf("expected no prometheus error, got: %v", dataMsg.promErr)
	}
	if dataMsg.utilPodCount != 1 {
		t.Errorf("expected utilPodCount=1, got %d", dataMsg.utilPodCount)
	}
	// Verify aggregation has utilization data
	if len(dataMsg.aggs) == 0 {
		t.Fatal("expected non-empty aggregations")
	}
	if !dataMsg.aggs[0].HasUtilization {
		t.Error("expected HasUtilization=true for aggregated group")
	}
}

func TestModelFetchCostsPrometheusError(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-1 * time.Hour)

	lister := &mockPodLister{
		pods: []kube.PodInfo{
			kube.NewTestPodInfo("web-1", "default", 500, 512, startTime, false,
				map[string]string{"team": "platform", "app": "web"}),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	m := testModelWithPrometheus(lister, srv.URL)
	msg := m.fetchCosts()
	dataMsg, ok := msg.(costDataMsg)
	if !ok {
		t.Fatalf("expected costDataMsg, got %T", msg)
	}
	// Pod data should still be present
	if dataMsg.podCount != 1 {
		t.Errorf("expected 1 pod even with prometheus error, got %d", dataMsg.podCount)
	}
	// Prometheus error should be populated
	if dataMsg.promErr == nil {
		t.Error("expected prometheus error to be set")
	}
	if !strings.Contains(dataMsg.promErr.Error(), "503") {
		t.Errorf("expected error to mention 503, got: %v", dataMsg.promErr)
	}
	// Utilization should be 0
	if dataMsg.utilPodCount != 0 {
		t.Errorf("expected utilPodCount=0 on error, got %d", dataMsg.utilPodCount)
	}
}

func TestModelUpdateSetsPromStatus(t *testing.T) {
	lister := &mockPodLister{}
	m := testModelWithPrometheus(lister, "http://unused")

	// Simulate receiving cost data with prometheus error
	promError := fmt.Errorf("connection refused")
	msg := costDataMsg{
		aggs: []cost.AggregatedCost{
			{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		},
		podCount:     1,
		promErr:      promError,
		utilPodCount: 0,
	}

	updated, _ := m.Update(msg)
	m2 := updated.(Model)

	if m2.promErr != promError {
		t.Errorf("expected promErr to be set, got %v", m2.promErr)
	}
	if m2.utilPodCount != 0 {
		t.Errorf("expected utilPodCount=0, got %d", m2.utilPodCount)
	}

	// Now simulate a successful update
	successMsg := costDataMsg{
		aggs: []cost.AggregatedCost{
			{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1, HasUtilization: true},
		},
		podCount:     1,
		promErr:      nil,
		utilPodCount: 3,
	}

	updated, _ = m2.Update(successMsg)
	m3 := updated.(Model)

	if m3.promErr != nil {
		t.Errorf("expected promErr to be nil after success, got %v", m3.promErr)
	}
	if m3.utilPodCount != 3 {
		t.Errorf("expected utilPodCount=3, got %d", m3.utilPodCount)
	}
}

func TestModelCursorNavigation(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "gamma", Workload: "worker"}, PodCount: 3},
	}
	m.rebuildDisplay()

	// Initial cursor at 0
	if m.cursor != 0 {
		t.Errorf("expected initial cursor at 0, got %d", m.cursor)
	}

	// Move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Errorf("expected cursor at 1 after down, got %d", m2.cursor)
	}

	// Move down again
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyDown})
	m3 := updated.(Model)
	if m3.cursor != 2 {
		t.Errorf("expected cursor at 2 after second down, got %d", m3.cursor)
	}

	// Move down at bottom — should not go past last row
	updated, _ = m3.Update(tea.KeyMsg{Type: tea.KeyDown})
	m4 := updated.(Model)
	if m4.cursor != 2 {
		t.Errorf("expected cursor clamped at 2, got %d", m4.cursor)
	}

	// Move up
	updated, _ = m4.Update(tea.KeyMsg{Type: tea.KeyUp})
	m5 := updated.(Model)
	if m5.cursor != 1 {
		t.Errorf("expected cursor at 1 after up, got %d", m5.cursor)
	}

	// Move up to top
	updated, _ = m5.Update(tea.KeyMsg{Type: tea.KeyUp})
	m6 := updated.(Model)
	if m6.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", m6.cursor)
	}

	// Move up at top — should not go below 0
	updated, _ = m6.Update(tea.KeyMsg{Type: tea.KeyUp})
	m7 := updated.(Model)
	if m7.cursor != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", m7.cursor)
	}
}

func TestModelCursorNavigationJK(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "beta", Workload: "api"}, PodCount: 2},
	}
	m.rebuildDisplay()

	// j moves down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Errorf("expected cursor at 1 after j, got %d", m2.cursor)
	}

	// k moves up
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m3 := updated.(Model)
	if m3.cursor != 0 {
		t.Errorf("expected cursor at 0 after k, got %d", m3.cursor)
	}
}

func TestModelExpandCollapse(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3},
		{Key: cost.GroupKey{Team: "beta", Workload: "worker"}, PodCount: 1},
	}
	m.rebuildDisplay()

	// Initially 2 rows (collapsed teams)
	if len(m.displayRows) != 2 {
		t.Fatalf("expected 2 display rows, got %d", len(m.displayRows))
	}

	// Press Enter on alpha (cursor=0) to expand
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	// Should now have 4 rows: alpha header + 2 workloads + beta header
	if len(m2.displayRows) != 4 {
		t.Fatalf("expected 4 display rows after expand, got %d", len(m2.displayRows))
	}
	if !m2.expandedTeams["alpha"] {
		t.Error("alpha should be expanded")
	}

	// View should show workload names
	view := m2.View()
	if !strings.Contains(view, "web") {
		t.Errorf("expected 'web' workload in expanded view:\n%s", view)
	}
	if !strings.Contains(view, "api") {
		t.Errorf("expected 'api' workload in expanded view:\n%s", view)
	}
	// Expanded arrow should be visible
	if !strings.Contains(view, "▼") {
		t.Errorf("expected expanded arrow ▼:\n%s", view)
	}

	// Press Enter again on alpha to collapse
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := updated.(Model)
	if len(m3.displayRows) != 2 {
		t.Fatalf("expected 2 display rows after collapse, got %d", len(m3.displayRows))
	}
	if m3.expandedTeams["alpha"] {
		t.Error("alpha should be collapsed")
	}
}

func TestModelExpandFromWorkloadRow(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3},
	}
	m.rebuildDisplay()

	// Expand alpha
	m.expandedTeams["alpha"] = true
	m.rebuildDisplay()

	// Move cursor to a workload row (index 1)
	m.cursor = 1
	if m.displayRows[1].Kind != rowWorkloadDetail {
		t.Fatal("expected cursor on workload detail row")
	}

	// Press Enter on workload row should collapse the parent team
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)
	if m2.expandedTeams["alpha"] {
		t.Error("pressing Enter on workload row should collapse parent team")
	}
}

func TestModelToggleExpandAll(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3},
		{Key: cost.GroupKey{Team: "beta", Workload: "worker"}, PodCount: 1},
	}
	m.rebuildDisplay()

	// Press 'a' to expand all
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m2 := updated.(Model)

	if !m2.expandedTeams["alpha"] || !m2.expandedTeams["beta"] {
		t.Error("all teams should be expanded after 'a'")
	}
	// alpha: 1 header + 2 workloads, beta: 1 header + 1 workload = 5
	if len(m2.displayRows) != 5 {
		t.Errorf("expected 5 display rows when all expanded, got %d", len(m2.displayRows))
	}

	// Press 'a' again to collapse all
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m3 := updated.(Model)

	if m3.expandedTeams["alpha"] || m3.expandedTeams["beta"] {
		t.Error("all teams should be collapsed after second 'a'")
	}
	if len(m3.displayRows) != 2 {
		t.Errorf("expected 2 display rows when all collapsed, got %d", len(m3.displayRows))
	}
}

func TestModelToggleGroupMode(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 2, CostPerHour: 0.02, TotalCost: 0.10},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 3, CostPerHour: 0.03, TotalCost: 0.15},
		{Key: cost.GroupKey{Team: "beta", Workload: "worker"}, PodCount: 1, CostPerHour: 0.01, TotalCost: 0.05},
	}
	m.rebuildDisplay()

	// Default is grouped mode
	if !m.grouped {
		t.Fatal("expected grouped mode by default")
	}
	// Should have 2 team summary rows (collapsed)
	if len(m.displayRows) != 2 {
		t.Fatalf("expected 2 display rows in grouped mode, got %d", len(m.displayRows))
	}

	// Press 'g' to switch to flat mode
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m2 := updated.(Model)

	if m2.grouped {
		t.Error("expected flat mode after pressing g")
	}
	// Should have 3 flat workload rows
	if len(m2.displayRows) != 3 {
		t.Fatalf("expected 3 display rows in flat mode, got %d", len(m2.displayRows))
	}
	// All rows should be rowFlat
	for i, dr := range m2.displayRows {
		if dr.Kind != rowFlat {
			t.Errorf("expected rowFlat at index %d, got %v", i, dr.Kind)
		}
	}

	// View should show both team and workload columns
	view := m2.View()
	if !strings.Contains(view, "alpha") {
		t.Errorf("expected team 'alpha' in flat view:\n%s", view)
	}
	if !strings.Contains(view, "web") {
		t.Errorf("expected workload 'web' in flat view:\n%s", view)
	}
	if !strings.Contains(view, "beta") {
		t.Errorf("expected team 'beta' in flat view:\n%s", view)
	}

	// Press 'g' again to switch back to grouped mode
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m3 := updated.(Model)

	if !m3.grouped {
		t.Error("expected grouped mode after pressing g again")
	}
	if len(m3.displayRows) != 2 {
		t.Fatalf("expected 2 display rows back in grouped mode, got %d", len(m3.displayRows))
	}
}

func TestModelFlatModeSortsByCost(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "cheap"}, PodCount: 1, CostPerHour: 0.01},
		{Key: cost.GroupKey{Team: "beta", Workload: "expensive"}, PodCount: 1, CostPerHour: 0.10},
		{Key: cost.GroupKey{Team: "alpha", Workload: "medium"}, PodCount: 1, CostPerHour: 0.05},
	}

	// Switch to flat mode and sort by cost descending
	m.grouped = false
	m.sortCfg = SortConfig{Column: SortByCostPerHour, Asc: false}
	m.rebuildDisplay()

	if len(m.displayRows) != 3 {
		t.Fatalf("expected 3 display rows, got %d", len(m.displayRows))
	}
	// Should be sorted: expensive, medium, cheap (regardless of team)
	if m.displayRows[0].Agg.Key.Workload != "expensive" {
		t.Errorf("expected expensive first, got %s", m.displayRows[0].Agg.Key.Workload)
	}
	if m.displayRows[1].Agg.Key.Workload != "medium" {
		t.Errorf("expected medium second, got %s", m.displayRows[1].Agg.Key.Workload)
	}
	if m.displayRows[2].Agg.Key.Workload != "cheap" {
		t.Errorf("expected cheap third, got %s", m.displayRows[2].Agg.Key.Workload)
	}
}

func TestModelFlatModeHelpText(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.grouped = false
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()

	view := m.View()
	if !strings.Contains(view, "g=Grouped") {
		t.Errorf("expected 'g=Grouped' in flat mode help text:\n%s", view)
	}
	if strings.Contains(view, "Expand/Collapse") {
		t.Errorf("should not show Expand/Collapse in flat mode:\n%s", view)
	}
}

func TestModelGroupedModeHelpText(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
	}
	m.rebuildDisplay()

	view := m.View()
	if !strings.Contains(view, "g=Flat") {
		t.Errorf("expected 'g=Flat' in grouped mode help text:\n%s", view)
	}
	if !strings.Contains(view, "Expand/Collapse") {
		t.Errorf("expected Expand/Collapse in grouped mode help:\n%s", view)
	}
}

func TestModelFlatModeExpandNoOp(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.grouped = false
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 2},
	}
	m.rebuildDisplay()

	// Enter in flat mode should be a no-op
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)
	if len(m2.displayRows) != 2 {
		t.Errorf("expected display rows unchanged after enter in flat mode, got %d", len(m2.displayRows))
	}

	// 'a' in flat mode should also be a no-op
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m3 := updated.(Model)
	if len(m3.displayRows) != 2 {
		t.Errorf("expected display rows unchanged after 'a' in flat mode, got %d", len(m3.displayRows))
	}
}

func TestModelExpandSpaceKey(t *testing.T) {
	lister := &mockPodLister{}
	m := testModel(lister)
	m.lastUpdate = time.Now()
	m.aggs = []cost.AggregatedCost{
		{Key: cost.GroupKey{Team: "alpha", Workload: "web"}, PodCount: 1},
		{Key: cost.GroupKey{Team: "alpha", Workload: "api"}, PodCount: 2},
	}
	m.rebuildDisplay()

	// Space should also expand
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m2 := updated.(Model)
	if !m2.expandedTeams["alpha"] {
		t.Error("space key should expand team")
	}
}

// Package tui implements the bubbletea-based terminal UI for the watch command.
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/prometheus"
)

// PodLister abstracts Kubernetes pod listing for testability.
type PodLister interface {
	ListPods(ctx context.Context) ([]kube.PodInfo, error)
}

// costDataMsg carries refreshed cost data to the model.
type costDataMsg struct {
	aggs         []cost.AggregatedCost
	podCount     int
	promErr      error // non-nil if Prometheus fetch failed
	utilPodCount int   // number of pods with utilization data
}

// errMsg carries errors from async operations.
type errMsg struct{ err error }

// tickMsg triggers a data refresh.
type tickMsg struct{}

// Model is the bubbletea model for the watch TUI.
type Model struct {
	aggs       []cost.AggregatedCost
	podCount   int
	err        error
	lastUpdate time.Time

	sortCfg         SortConfig
	showSubtype     bool
	showUtilization bool

	// Prometheus status (displayed in header when utilization is enabled).
	promErr      error  // last Prometheus fetch error (nil = OK)
	utilPodCount int    // number of pods with utilization data
	promProject  string // GCP project queried for Prometheus metrics

	lister     PodLister
	calc       *cost.Calculator
	lc         cost.LabelConfig
	interval   time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	promClient *prometheus.Client
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, cancel context.CancelFunc, lister PodLister, calc *cost.Calculator, lc cost.LabelConfig, interval time.Duration, promClient *prometheus.Client, promProject string) Model {
	return Model{
		lister:          lister,
		calc:            calc,
		lc:              lc,
		interval:        interval,
		ctx:             ctx,
		cancel:          cancel,
		sortCfg:         DefaultSort(),
		showSubtype:     lc.SubtypeLabel != "",
		showUtilization: promClient != nil,
		promClient:      promClient,
		promProject:     promProject,
	}
}

// Init starts the first data fetch.
func (m Model) Init() tea.Cmd {
	return m.fetchCosts
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
			key := rune(msg.String()[0])
			if col, ok := ColumnForKey(key, m.showSubtype, m.showUtilization); ok {
				if col == m.sortCfg.Column {
					m.sortCfg.Asc = !m.sortCfg.Asc
				} else {
					m.sortCfg = SortConfig{Column: col, Asc: true}
				}
			}
			return m, nil
		}

	case costDataMsg:
		m.aggs = msg.aggs
		m.podCount = msg.podCount
		m.err = nil
		m.promErr = msg.promErr
		m.utilPodCount = msg.utilPodCount
		m.lastUpdate = time.Now()
		return m, m.scheduleTick

	case errMsg:
		m.err = msg.err
		return m, m.scheduleTick

	case tickMsg:
		return m, m.fetchCosts
	}

	return m, nil
}

// View renders the TUI.
func (m Model) View() string {
	if m.lastUpdate.IsZero() && m.err == nil {
		return "Loading...\n"
	}

	header := fmt.Sprintf("Autopilot Cost Analyzer — %s — %d pods",
		m.lastUpdate.Format("15:04:05"), m.podCount)

	if m.err != nil {
		header += fmt.Sprintf("  (error: %v)", m.err)
	}

	if m.showUtilization {
		projectTag := ""
		if m.promProject != "" {
			projectTag = " " + m.promProject
		}
		switch {
		case m.promErr != nil:
			header += fmt.Sprintf("  (prometheus%s error: %v)", projectTag, m.promErr)
		case m.utilPodCount == 0:
			header += fmt.Sprintf("  (prometheus%s: no utilization data)", projectTag)
		default:
			header += fmt.Sprintf("  (utilization%s: %d pods)", projectTag, m.utilPodCount)
		}
	}

	// Sort a copy so we don't mutate the stored data.
	sorted := make([]cost.AggregatedCost, len(m.aggs))
	copy(sorted, m.aggs)
	SortAggs(sorted, m.sortCfg)

	help := m.helpText()

	return header + "\n\n" + RenderTable(sorted, m.showSubtype, m.showUtilization, m.sortCfg) + "\n\n" + help + "\n"
}

// fetchCosts fetches pod data and calculates costs.
func (m Model) fetchCosts() tea.Msg {
	pods, err := m.lister.ListPods(m.ctx)
	if err != nil {
		return errMsg{fmt.Errorf("listing pods: %w", err)}
	}

	var usage map[prometheus.PodKey]prometheus.PodUsage
	var promErr error
	if m.promClient != nil {
		usage, promErr = m.promClient.FetchUsage(m.ctx)
	}

	costs := m.calc.CalculateAll(pods)
	aggs := cost.AggregateWithUtilization(costs, m.lc, usage)

	return costDataMsg{
		aggs:         aggs,
		podCount:     len(pods),
		promErr:      promErr,
		utilPodCount: len(usage),
	}
}

// helpText returns the footer help line showing sort key mappings.
func (m Model) helpText() string {
	if m.showSubtype && m.showUtilization {
		return "Sort: 1=Team 2=Workload 3=Subtype 4=Pods 5=CPU 6=Mem 7=$/hr 8=Cost 9=CPU% 0=Waste · q=Quit"
	}
	if m.showSubtype {
		return "Sort: 1=Team 2=Workload 3=Subtype 4=Pods 5=CPU 6=Mem 7=$/hr 8=Cost · q=Quit"
	}
	if m.showUtilization {
		return "Sort: 1=Team 2=Workload 3=Pods 4=CPU 5=Mem 6=$/hr 7=Cost 8=CPU% 9=Waste · q=Quit"
	}
	return "Sort: 1=Team 2=Workload 3=Pods 4=CPU 5=Mem 6=$/hr 7=Cost · q=Quit"
}

// scheduleTick waits for the interval then sends a tick.
func (m Model) scheduleTick() tea.Msg {
	time.Sleep(m.interval)
	return tickMsg{}
}

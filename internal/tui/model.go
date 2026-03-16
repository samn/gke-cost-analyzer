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

// NodeLister abstracts Kubernetes node listing for testability.
type NodeLister = kube.NodeLister

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
	showMode        bool // show MODE column (when in "all" mode)

	// Prometheus status (displayed in header when utilization is enabled).
	promErr      error  // last Prometheus fetch error (nil = OK)
	utilPodCount int    // number of pods with utilization data
	promProject  string // GCP project queried for Prometheus metrics

	lister        PodLister
	autopilotCalc *cost.Calculator
	standardCalc  *cost.StandardCalculator
	nodeLister    *NodeLister
	lc            cost.LabelConfig
	interval      time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	promClient    *prometheus.Client
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, cancel context.CancelFunc, lister PodLister, autopilotCalc *cost.Calculator, standardCalc *cost.StandardCalculator, nodeLister *NodeLister, lc cost.LabelConfig, interval time.Duration, promClient *prometheus.Client, promProject string, showMode bool) Model {
	return Model{
		lister:          lister,
		autopilotCalc:   autopilotCalc,
		standardCalc:    standardCalc,
		nodeLister:      nodeLister,
		lc:              lc,
		interval:        interval,
		ctx:             ctx,
		cancel:          cancel,
		sortCfg:         DefaultSort(),
		showSubtype:     lc.SubtypeLabel != "",
		showUtilization: promClient != nil,
		showMode:        showMode,
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
			if col, ok := ColumnForKey(key, m.showSubtype, m.showUtilization, m.showMode); ok {
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

	header := fmt.Sprintf("GKE Cost Analyzer — %s — %d pods",
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

	return header + "\n\n" + RenderTable(sorted, m.showSubtype, m.showUtilization, m.showMode, m.sortCfg) + "\n\n" + help + "\n"
}

// fetchCosts fetches pod data and calculates costs.
func (m Model) fetchCosts() tea.Msg {
	pods, err := m.lister.ListPods(m.ctx)
	if err != nil {
		return errMsg{fmt.Errorf("listing pods: %w", err)}
	}

	// Refresh nodes for standard calculator
	if m.nodeLister != nil && m.standardCalc != nil {
		nodes, err := m.nodeLister.ListNodes(m.ctx)
		if err != nil {
			return errMsg{fmt.Errorf("listing nodes: %w", err)}
		}
		m.standardCalc.SetNodes(nodes)
	}

	var usage map[prometheus.PodKey]prometheus.PodUsage
	var promErr error
	if m.promClient != nil {
		usage, promErr = m.promClient.FetchUsage(m.ctx)
	}

	// Calculate costs — partition pods by type if both calculators are set
	allCosts := calculateCosts(pods, m.autopilotCalc, m.standardCalc)

	aggs := cost.AggregateWithUtilization(allCosts, m.lc, usage)

	return costDataMsg{
		aggs:         aggs,
		podCount:     len(pods),
		promErr:      promErr,
		utilPodCount: len(usage),
	}
}

// helpText returns the footer help line showing sort key mappings.
func (m Model) helpText() string {
	cols := []string{"Team", "Workload"}
	if m.showSubtype {
		cols = append(cols, "Subtype")
	}
	if m.showMode {
		cols = append(cols, "Mode")
	}
	cols = append(cols, "Pods", "CPU", "Mem", "$/hr", "Cost")
	if m.showUtilization {
		cols = append(cols, "CPU%", "Waste")
	}

	help := "Sort:"
	for i, c := range cols {
		key := i + 1
		if key == 10 {
			key = 0
		}
		help += fmt.Sprintf(" %d=%s", key, c)
	}
	help += " · q=Quit"
	return help
}

// calculateCosts computes pod costs using the appropriate calculator(s).
func calculateCosts(pods []kube.PodInfo, autopilotCalc *cost.Calculator, standardCalc *cost.StandardCalculator) []cost.PodCost {
	switch {
	case autopilotCalc != nil && standardCalc != nil:
		var autopilotPods, standardPods []kube.PodInfo
		for _, p := range pods {
			if p.IsAutopilot {
				autopilotPods = append(autopilotPods, p)
			} else {
				standardPods = append(standardPods, p)
			}
		}
		allCosts := autopilotCalc.CalculateAll(autopilotPods)
		return append(allCosts, standardCalc.CalculateAll(standardPods)...)
	case autopilotCalc != nil:
		return autopilotCalc.CalculateAll(pods)
	case standardCalc != nil:
		return standardCalc.CalculateAll(pods)
	default:
		return nil
	}
}

// scheduleTick waits for the interval then sends a tick.
func (m Model) scheduleTick() tea.Msg {
	time.Sleep(m.interval)
	return tickMsg{}
}

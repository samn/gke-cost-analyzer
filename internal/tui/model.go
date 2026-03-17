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
type NodeLister interface {
	ListNodes(ctx context.Context) ([]kube.NodeInfo, error)
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
	startedAt  time.Time

	sortCfg         SortConfig
	showSubtype     bool
	showUtilization bool
	showMode        bool // show MODE column (when in "all" mode)

	// Team drill-down state.
	grouped       bool            // true = team-grouped view, false = flat workload view
	expandedTeams map[string]bool // which teams are expanded (grouped mode only)
	cursor        int             // selected display row index
	displayRows   []DisplayRow    // current display rows (recomputed on data/expand changes)
	teamGroups    []TeamGroup     // current team groups (recomputed on data changes)

	// Prometheus status (displayed in header when utilization is enabled).
	promErr      error  // last Prometheus fetch error (nil = OK)
	utilPodCount int    // number of pods with utilization data
	promProject  string // GCP project queried for Prometheus metrics

	lister        PodLister
	autopilotCalc *cost.Calculator
	standardCalc  *cost.StandardCalculator
	nodeLister    NodeLister
	lc            cost.LabelConfig
	interval      time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	promClient    *prometheus.Client
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, cancel context.CancelFunc, lister PodLister, autopilotCalc *cost.Calculator, standardCalc *cost.StandardCalculator, nodeLister NodeLister, lc cost.LabelConfig, interval time.Duration, promClient *prometheus.Client, promProject string, showMode bool) Model {
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
		startedAt:       time.Now(),
		grouped:         true,
		expandedTeams:   make(map[string]bool),
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
				m.rebuildDisplay()
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.displayRows)-1 {
				m.cursor++
			}
			return m, nil
		case "enter", " ":
			if m.grouped {
				m.toggleExpand()
			}
			return m, nil
		case "a":
			if m.grouped {
				m.toggleExpandAll()
			}
			return m, nil
		case "g":
			m.grouped = !m.grouped
			m.rebuildDisplay()
			m.clampCursor()
			return m, nil
		}

	case costDataMsg:
		m.aggs = msg.aggs
		m.podCount = msg.podCount
		m.err = nil
		m.promErr = msg.promErr
		m.utilPodCount = msg.utilPodCount
		m.lastUpdate = time.Now()
		m.rebuildDisplay()
		return m, m.scheduleTick

	case errMsg:
		m.err = msg.err
		return m, m.scheduleTick

	case tickMsg:
		return m, m.fetchCosts
	}

	return m, nil
}

// toggleExpand expands or collapses the team at the cursor.
func (m *Model) toggleExpand() {
	if m.cursor < 0 || m.cursor >= len(m.displayRows) {
		return
	}
	dr := m.displayRows[m.cursor]
	var teamName string
	switch dr.Kind {
	case rowTeamSummary:
		teamName = dr.TeamName
	case rowWorkloadDetail:
		// Find the parent team by walking backward.
		for i := m.cursor - 1; i >= 0; i-- {
			if m.displayRows[i].Kind == rowTeamSummary {
				teamName = m.displayRows[i].TeamName
				break
			}
		}
	}
	if teamName == "" {
		return
	}
	m.expandedTeams[teamName] = !m.expandedTeams[teamName]
	m.displayRows = buildDisplayRows(m.teamGroups, m.expandedTeams)
	m.clampCursor()
}

// toggleExpandAll expands all teams if any are collapsed, or collapses all if all are expanded.
func (m *Model) toggleExpandAll() {
	allExpanded := true
	for _, g := range m.teamGroups {
		if !m.expandedTeams[g.Team] {
			allExpanded = false
			break
		}
	}
	for _, g := range m.teamGroups {
		m.expandedTeams[g.Team] = !allExpanded
	}
	m.displayRows = buildDisplayRows(m.teamGroups, m.expandedTeams)
	m.clampCursor()
}

// rebuildDisplay re-sorts, re-groups, and rebuilds display rows from current aggs.
func (m *Model) rebuildDisplay() {
	sorted := make([]cost.AggregatedCost, len(m.aggs))
	copy(sorted, m.aggs)
	SortAggs(sorted, m.sortCfg)

	if m.grouped {
		m.teamGroups = groupByTeam(sorted)
		SortTeamGroups(m.teamGroups, m.sortCfg)
		m.displayRows = buildDisplayRows(m.teamGroups, m.expandedTeams)
	} else {
		m.teamGroups = nil
		m.displayRows = buildFlatDisplayRows(sorted)
	}
}

// clampCursor ensures the cursor is within the valid display row range.
func (m *Model) clampCursor() {
	if m.cursor >= len(m.displayRows) {
		m.cursor = len(m.displayRows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// View renders the TUI.
func (m Model) View() string {
	if m.lastUpdate.IsZero() && m.err == nil {
		return "Loading...\n"
	}

	elapsed := time.Since(m.startedAt).Truncate(time.Second)
	header := fmt.Sprintf("GKE Cost Analyzer — %s — %d pods — watching for %s",
		m.lastUpdate.Format("15:04:05"), m.podCount, elapsed)

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

	help := m.helpText()

	return header + "\n\n" + RenderTable(m.displayRows, m.showSubtype, m.showUtilization, m.showMode, m.sortCfg, m.cursor) + "\n\n" + help + "\n"
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
	allCosts := cost.PartitionAndCalculate(pods, m.autopilotCalc, m.standardCalc)

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
	vis := ColumnVisibility{Subtype: m.showSubtype, Mode: m.showMode, Utilization: m.showUtilization}
	defs := visibleColumns(vis)

	help := "Sort:"
	keyIdx := 0
	for _, d := range defs {
		if !d.sortable {
			continue
		}
		name := d.helpName
		if name == "" {
			name = d.header
		}
		key := keyIdx + 1
		if key == 10 {
			key = 0
		}
		help += fmt.Sprintf(" %d=%s", key, name)
		keyIdx++
	}
	help += " · ↑↓=Navigate"
	if m.grouped {
		help += " Enter=Expand/Collapse a=Toggle All"
	}
	if m.grouped {
		help += " g=Flat"
	} else {
		help += " g=Grouped"
	}
	help += " · q=Quit"
	return help
}

// scheduleTick waits for the interval then sends a tick.
func (m Model) scheduleTick() tea.Msg {
	time.Sleep(m.interval)
	return tickMsg{}
}

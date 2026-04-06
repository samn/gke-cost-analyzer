package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sync/errgroup"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
)

// HistoryDataFetcher abstracts BigQuery reading for testability.
type HistoryDataFetcher interface {
	QueryAggregatedCosts(ctx context.Context, since time.Time, filters bigquery.QueryFilters) ([]bigquery.HistoryCostRow, error)
	QueryTimeSeries(ctx context.Context, since time.Time, bucketSeconds int64, filters bigquery.QueryFilters) ([]bigquery.TimeSeriesPoint, error)
}

// historyDataMsg carries fetched history data to the model.
type historyDataMsg struct {
	rows   []bigquery.HistoryCostRow
	series []bigquery.TimeSeriesPoint
}

// historyErrMsg carries errors from async operations.
type historyErrMsg struct{ err error }

// HistoryModel is the BubbleTea model for the history command TUI.
type HistoryModel struct {
	rows       []bigquery.HistoryCostRow
	sparklines map[bigquery.WorkloadKey]string
	err        error
	loading    bool

	sortCfg HistorySortConfig
	vis     ColumnVisibility

	grouped       bool
	expandedTeams map[string]bool
	cursor        int
	displayRows   []HistoryDisplayRow
	teamGroups    []HistoryTeamGroup

	timeRange     time.Duration
	totalCost     float64
	workloadCount int

	fetcher    HistoryDataFetcher
	filters    bigquery.QueryFilters
	bucketSecs int64
	since      time.Time
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewHistoryModel creates a new history TUI model.
func NewHistoryModel(ctx context.Context, cancel context.CancelFunc, fetcher HistoryDataFetcher, timeRange time.Duration, bucketSecs int64, filters bigquery.QueryFilters, vis ColumnVisibility) HistoryModel {
	return HistoryModel{
		loading:       true,
		fetcher:       fetcher,
		timeRange:     timeRange,
		bucketSecs:    bucketSecs,
		filters:       filters,
		since:         time.Now().Add(-timeRange),
		ctx:           ctx,
		cancel:        cancel,
		sortCfg:       DefaultHistorySort(),
		vis:           vis,
		grouped:       true,
		expandedTeams: make(map[string]bool),
	}
}

// Init starts the data fetch.
func (m HistoryModel) Init() tea.Cmd {
	return m.fetchHistory
}

// Update handles messages.
func (m HistoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
			key := rune(msg.String()[0])
			if col, ok := HistoryColumnForKey(key, m.vis); ok {
				if col == m.sortCfg.Column {
					m.sortCfg.Asc = !m.sortCfg.Asc
				} else {
					m.sortCfg = HistorySortConfig{Column: col, Asc: true}
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
		case "enter", "space":
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

	case historyDataMsg:
		m.rows = msg.rows
		m.loading = false

		// Check if any rows have utilization data
		for _, r := range m.rows {
			if r.AvgCPUUtil != nil {
				m.vis.Utilization = true
				break
			}
		}

		// Build sparklines
		grouped := bigquery.BuildSparklines(msg.series)
		m.sparklines = make(map[bigquery.WorkloadKey]string, len(grouped))
		for key, values := range grouped {
			m.sparklines[key] = Sparkline(values)
		}

		// Compute summary stats
		m.workloadCount = len(m.rows)
		m.totalCost = 0
		for _, r := range m.rows {
			m.totalCost += r.TotalCost
		}

		m.rebuildDisplay()
		return m, nil

	case historyErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil
	}

	return m, nil
}

// View renders the history TUI.
func (m HistoryModel) View() tea.View {
	if m.loading {
		v := tea.NewView("Querying BigQuery...\n")
		v.AltScreen = true
		return v
	}

	if m.err != nil {
		v := tea.NewView(fmt.Sprintf("Error: %v\n", m.err))
		v.AltScreen = true
		return v
	}

	if len(m.rows) == 0 {
		v := tea.NewView(fmt.Sprintf("No cost data found for the last %s\n", formatDuration(m.timeRange)))
		v.AltScreen = true
		return v
	}

	header := fmt.Sprintf("GKE Cost Analyzer — History (%s) — $%.2f total — %d workloads",
		formatDuration(m.timeRange), m.totalCost, m.workloadCount)
	if !m.vis.Cluster && m.filters.ClusterName != "" {
		header += fmt.Sprintf(" — cluster: %s", m.filters.ClusterName)
	}

	help := m.helpText()
	result := header + "\n\n" + RenderHistoryTable(m.displayRows, m.vis, m.sortCfg, m.cursor, m.sparklines) + "\n\n" + help + "\n"

	v := tea.NewView(result)
	v.AltScreen = true
	return v
}

// fetchHistory fetches data from BigQuery.
func (m HistoryModel) fetchHistory() tea.Msg {
	g, ctx := errgroup.WithContext(m.ctx)

	var rows []bigquery.HistoryCostRow
	var series []bigquery.TimeSeriesPoint

	g.Go(func() error {
		var err error
		rows, err = m.fetcher.QueryAggregatedCosts(ctx, m.since, m.filters)
		return err
	})

	g.Go(func() error {
		var err error
		series, err = m.fetcher.QueryTimeSeries(ctx, m.since, m.bucketSecs, m.filters)
		return err
	})

	if err := g.Wait(); err != nil {
		return historyErrMsg{err}
	}
	return historyDataMsg{rows: rows, series: series}
}

func (m *HistoryModel) rebuildDisplay() {
	sorted := make([]bigquery.HistoryCostRow, len(m.rows))
	copy(sorted, m.rows)
	SortHistoryRows(sorted, m.sortCfg)

	if m.grouped {
		m.teamGroups = GroupHistoryByTeam(sorted)
		SortHistoryTeamGroups(m.teamGroups, m.sortCfg)
		m.displayRows = buildHistoryDisplayRows(m.teamGroups, m.expandedTeams)
	} else {
		m.teamGroups = nil
		m.displayRows = buildFlatHistoryDisplayRows(sorted)
	}
}

func (m *HistoryModel) clampCursor() {
	if m.cursor >= len(m.displayRows) {
		m.cursor = len(m.displayRows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *HistoryModel) toggleExpand() {
	if m.cursor < 0 || m.cursor >= len(m.displayRows) {
		return
	}
	dr := m.displayRows[m.cursor]
	var teamName string
	switch dr.Kind {
	case rowTeamSummary:
		teamName = dr.TeamName
	case rowWorkloadDetail:
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
	m.displayRows = buildHistoryDisplayRows(m.teamGroups, m.expandedTeams)
	m.clampCursor()
}

func (m *HistoryModel) toggleExpandAll() {
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
	m.displayRows = buildHistoryDisplayRows(m.teamGroups, m.expandedTeams)
	m.clampCursor()
}

func (m HistoryModel) helpText() string {
	defs := historyVisibleColumns(m.vis)

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

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	hours := d.Hours()
	switch {
	case hours < 24:
		return fmt.Sprintf("%.0fh", hours)
	case hours < 168:
		days := hours / 24
		if days == float64(int(days)) {
			return fmt.Sprintf("%.0fd", days)
		}
		return fmt.Sprintf("%.1fd", days)
	default:
		weeks := hours / 168
		if weeks == float64(int(weeks)) {
			return fmt.Sprintf("%.0fw", weeks)
		}
		return fmt.Sprintf("%.1fw", weeks)
	}
}

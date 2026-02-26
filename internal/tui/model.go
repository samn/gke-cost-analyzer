// Package tui implements the bubbletea-based terminal UI for the watch command.
package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
)

// PodLister abstracts Kubernetes pod listing for testability.
type PodLister interface {
	ListPods(ctx context.Context) ([]kube.PodInfo, error)
}

// costDataMsg carries refreshed cost data to the model.
type costDataMsg struct {
	aggs     []cost.AggregatedCost
	podCount int
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

	lister   PodLister
	calc     *cost.Calculator
	lc       cost.LabelConfig
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, cancel context.CancelFunc, lister PodLister, calc *cost.Calculator, lc cost.LabelConfig, interval time.Duration) Model {
	return Model{
		lister:   lister,
		calc:     calc,
		lc:       lc,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
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
		}

	case costDataMsg:
		m.aggs = msg.aggs
		m.podCount = msg.podCount
		m.err = nil
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

	return header + "\n\n" + RenderTable(m.aggs) + "\n\nPress q to quit.\n"
}

// fetchCosts fetches pod data and calculates costs.
func (m Model) fetchCosts() tea.Msg {
	pods, err := m.lister.ListPods(m.ctx)
	if err != nil {
		return errMsg{fmt.Errorf("listing pods: %w", err)}
	}

	costs := m.calc.CalculateAll(pods)
	aggs := cost.Aggregate(costs, m.lc)

	sort.Slice(aggs, func(i, j int) bool {
		if aggs[i].Key.Team != aggs[j].Key.Team {
			return aggs[i].Key.Team < aggs[j].Key.Team
		}
		if aggs[i].Key.Workload != aggs[j].Key.Workload {
			return aggs[i].Key.Workload < aggs[j].Key.Workload
		}
		return aggs[i].Key.Subtype < aggs[j].Key.Subtype
	})

	return costDataMsg{aggs: aggs, podCount: len(pods)}
}

// scheduleTick waits for the interval then sends a tick.
func (m Model) scheduleTick() tea.Msg {
	time.Sleep(m.interval)
	return tickMsg{}
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/samn/gke-cost-analyzer/internal/trend"
)

var (
	eventHeaderStyle = lipgloss.NewStyle().Bold(true).Faint(true)
	eventUpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	eventDownStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	eventLifeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
)

// RenderEventLog renders the event log panel. Shows the most recent maxLines
// events, most recent at the bottom.
func RenderEventLog(events []trend.Event, now time.Time, maxLines int) string {
	return RenderEventLogScrolled(events, now, maxLines, 0)
}

// RenderEventLogScrolled renders the event log panel with a scrollback
// offset: 0 shows the most recent events, offset N shifts the window N
// events into the past (clamped to the available history).
func RenderEventLogScrolled(events []trend.Event, now time.Time, maxLines, scrollOffset int) string {
	if len(events) == 0 {
		return eventHeaderStyle.Render("--- Events (waiting for data) ---")
	}

	total := len(events)
	maxOffset := total - maxLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if scrollOffset > maxOffset {
		scrollOffset = maxOffset
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}

	endIdx := total - scrollOffset
	startIdx := endIdx - maxLines
	if startIdx < 0 {
		startIdx = 0
	}

	headerText := "--- Events ---"
	if scrollOffset > 0 {
		headerText = fmt.Sprintf("--- Events (%d older, ]=newer) ---", scrollOffset)
	}
	header := eventHeaderStyle.Render(headerText)

	var lines []string
	lines = append(lines, header)

	for i := startIdx; i < endIdx; i++ {
		line := trend.FormatEvent(events[i], now)
		styled := styleEventLine(events[i], line)
		lines = append(lines, styled)
	}

	return strings.Join(lines, "\n")
}

// styleEventLine applies color to an event line based on event kind.
func styleEventLine(e trend.Event, line string) string {
	switch e.Kind {
	case trend.EventAberration:
		if e.PctChange >= 0 {
			return eventUpStyle.Render(line)
		}
		return eventDownStyle.Render(line)
	case trend.EventAppeared, trend.EventDisappeared:
		return eventLifeStyle.Render(line)
	default:
		return line
	}
}

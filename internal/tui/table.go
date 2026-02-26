package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle   = lipgloss.NewStyle().Padding(0, 1)

	// Right-align numeric columns (PODS=3, CPU REQ=4, MEM REQ=5, $/HR=6).
	numericStyle = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
)

// RenderTable renders the aggregated costs as a formatted table string.
func RenderTable(aggs []cost.AggregatedCost) string {
	rows := make([][]string, 0, len(aggs)+1)

	var totalCostPerHour float64
	for _, a := range aggs {
		spot := ""
		if a.Key.IsSpot {
			spot = "yes"
		}
		rows = append(rows, []string{
			orDefault(a.Key.Team, "-"),
			orDefault(a.Key.Workload, "-"),
			orDefault(a.Key.Subtype, "-"),
			fmt.Sprintf("%d", a.PodCount),
			fmt.Sprintf("%.2f", a.TotalCPUVCPU),
			fmt.Sprintf("%.1f GB", a.TotalMemGB),
			fmt.Sprintf("$%.4f", a.CostPerHour),
			spot,
		})
		totalCostPerHour += a.CostPerHour
	}

	// Total row
	rows = append(rows, []string{
		"TOTAL", "", "", "", "", "",
		fmt.Sprintf("$%.4f", totalCostPerHour), "",
	})

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderRow(false).
		Headers("TEAM", "WORKLOAD", "SUBTYPE", "PODS", "CPU REQ", "MEM REQ", "$/HR", "SPOT").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			// Right-align numeric columns
			if col >= 3 && col <= 6 {
				return numericStyle
			}
			return cellStyle
		})

	return t.String()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

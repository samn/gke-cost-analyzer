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

	// Right-align numeric columns.
	numericStyle = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)
)

// sortIndicator returns the header text with a sort direction arrow if this
// column is the active sort column.
func sortIndicator(header string, col SortColumn, cfg SortConfig) string {
	if col != cfg.Column {
		return header
	}
	if cfg.Asc {
		return header + " ^"
	}
	return header + " v"
}

// RenderTable renders the aggregated costs as a formatted table string.
// When showSubtype is true, a SUBTYPE column is included.
// When showUtilization is true, CPU%, MEM%, and WASTE columns are included.
// The sortCfg controls which column header receives a sort indicator arrow.
func RenderTable(aggs []cost.AggregatedCost, showSubtype, showUtilization bool, sortCfg SortConfig) string {
	rows := make([][]string, 0, len(aggs)+1)

	var totalCostPerHour, totalCost, totalWaste float64
	for _, a := range aggs {
		spot := ""
		if a.Key.IsSpot {
			spot = "yes"
		}
		row := []string{
			orDefault(a.Key.Team, "-"),
			orDefault(a.Key.Workload, "-"),
		}
		if showSubtype {
			row = append(row, orDefault(a.Key.Subtype, "-"))
		}
		row = append(row,
			fmt.Sprintf("%d", a.PodCount),
			fmt.Sprintf("%.2f", a.TotalCPUVCPU),
			fmt.Sprintf("%.1f GB", a.TotalMemGB),
			fmt.Sprintf("$%.4f", a.CostPerHour),
			fmt.Sprintf("$%.4f", a.TotalCost),
			spot,
		)
		if showUtilization {
			if a.HasUtilization {
				row = append(row,
					fmt.Sprintf("%.0f%%", a.CPUUtilization*100),
					fmt.Sprintf("%.0f%%", a.MemUtilization*100),
					fmt.Sprintf("$%.4f", a.WastedCostPerHour),
				)
			} else {
				row = append(row, "-", "-", "-")
			}
		}
		rows = append(rows, row)
		totalCostPerHour += a.CostPerHour
		totalCost += a.TotalCost
		totalWaste += a.WastedCostPerHour
	}

	// Total row
	totalRow := []string{"TOTAL", ""}
	if showSubtype {
		totalRow = append(totalRow, "")
	}
	totalRow = append(totalRow, "", "", "",
		fmt.Sprintf("$%.4f", totalCostPerHour),
		fmt.Sprintf("$%.4f", totalCost),
		"",
	)
	if showUtilization {
		totalRow = append(totalRow, "", "",
			fmt.Sprintf("$%.4f", totalWaste),
		)
	}
	rows = append(rows, totalRow)

	headers := []string{
		sortIndicator("TEAM", SortByTeam, sortCfg),
		sortIndicator("WORKLOAD", SortByWorkload, sortCfg),
	}
	if showSubtype {
		headers = append(headers, sortIndicator("SUBTYPE", SortBySubtype, sortCfg))
	}
	headers = append(headers,
		sortIndicator("PODS", SortByPods, sortCfg),
		sortIndicator("CPU REQ", SortByCPU, sortCfg),
		sortIndicator("MEM REQ", SortByMem, sortCfg),
		sortIndicator("$/HR", SortByCostPerHour, sortCfg),
		sortIndicator("COST", SortByCost, sortCfg),
		"SPOT",
	)
	if showUtilization {
		headers = append(headers,
			sortIndicator("CPU%", SortByCPUUtil, sortCfg),
			"MEM%",
			sortIndicator("WASTE", SortByWaste, sortCfg),
		)
	}

	// First numeric column index depends on whether SUBTYPE is shown.
	numericStart := 2
	if showSubtype {
		numericStart = 3
	}
	// Base numeric columns: PODS, CPU REQ, MEM REQ, $/HR, COST
	numericEnd := numericStart + 4
	// SPOT column is at numericEnd+1, then utilization columns follow
	utilStart := numericEnd + 2 // after SPOT
	utilEnd := utilStart + 2    // CPU%, MEM%, WASTE

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderRow(false).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if col >= numericStart && col <= numericEnd {
				return numericStyle
			}
			if showUtilization && col >= utilStart && col <= utilEnd {
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

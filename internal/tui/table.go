package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle   = lipgloss.NewStyle().Padding(0, 1)

	// Right-align numeric columns.
	numericStyle = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right)

	// Styles for selected (cursor) row.
	selectedStyle        = lipgloss.NewStyle().Padding(0, 1).Bold(true).Reverse(true)
	selectedNumericStyle = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right).Bold(true).Reverse(true)

	// Style for the separator row (thin line between data and total).
	separatorCellStyle = lipgloss.NewStyle().Padding(0, 0)

	// Style for the total row (bold to stand out).
	totalStyle        = lipgloss.NewStyle().Padding(0, 1).Bold(true)
	totalNumericStyle = lipgloss.NewStyle().Padding(0, 1).Align(lipgloss.Right).Bold(true)
)

// rowKind tags a display row as a team summary or workload detail.
type rowKind int

const (
	rowTeamSummary    rowKind = iota // collapsed or expanded team header
	rowWorkloadDetail                // workload under an expanded team
	rowFlat                          // flat mode: individual workload, not nested under a team
)

// DisplayRow represents one row in the rendered table.
type DisplayRow struct {
	Kind          rowKind
	TeamName      string
	Expanded      bool // only meaningful for rowTeamSummary
	WorkloadCount int  // number of workloads in team (for summary rows)
	Agg           cost.AggregatedCost
}

// buildDisplayRows creates display rows from team groups and expanded state.
func buildDisplayRows(groups []TeamGroup, expanded map[string]bool) []DisplayRow {
	var rows []DisplayRow
	for _, g := range groups {
		isExpanded := expanded[g.Team]
		rows = append(rows, DisplayRow{
			Kind:          rowTeamSummary,
			TeamName:      g.Team,
			Expanded:      isExpanded,
			WorkloadCount: len(g.Workloads),
			Agg:           g.Summary,
		})
		if isExpanded {
			for _, w := range g.Workloads {
				rows = append(rows, DisplayRow{
					Kind: rowWorkloadDetail,
					Agg:  w,
				})
			}
		}
	}
	return rows
}

// buildFlatDisplayRows creates flat display rows from sorted aggs (no team grouping).
func buildFlatDisplayRows(aggs []cost.AggregatedCost) []DisplayRow {
	rows := make([]DisplayRow, len(aggs))
	for i, a := range aggs {
		rows[i] = DisplayRow{
			Kind: rowFlat,
			Agg:  a,
		}
	}
	return rows
}

// sortIndicator returns the header text with a sort direction arrow if this
// column is the active sort column.
func sortIndicator(header string, col SortColumn, sortable bool, cfg SortConfig) string {
	if !sortable || col != cfg.Column {
		return header
	}
	if cfg.Asc {
		return header + " ^"
	}
	return header + " v"
}

// costModeShort returns a short display string for the cost mode.
func costModeShort(mode string) string {
	switch mode {
	case "autopilot":
		return "AP"
	case "standard":
		return "STD"
	default:
		return mode
	}
}

// RenderTable renders the aggregated costs as a formatted table string with
// team rollup and drill-down support.
// displayRows are the pre-built rows (team summaries + expanded workload details).
// cursor is the index of the currently selected display row (-1 for no selection).
func RenderTable(displayRows []DisplayRow, showSubtype, showUtilization, showMode bool, sortCfg SortConfig, cursor int) string {
	vis := ColumnVisibility{Subtype: showSubtype, Mode: showMode, Utilization: showUtilization}
	defs := visibleColumns(vis)

	rows := make([][]string, 0, len(displayRows)+2) // +2 for separator + total

	var totalPods int
	var totalCPU, totalMem float64
	var totalCostPerHour, totalCost, totalWaste float64

	for _, dr := range displayRows {
		var row []string
		switch dr.Kind {
		case rowTeamSummary:
			arrow := "▶"
			if dr.Expanded {
				arrow = "▼"
			}
			row = []string{
				dr.TeamName,
				fmt.Sprintf("%d workloads %s", dr.WorkloadCount, arrow),
			}
			if showSubtype {
				row = append(row, "")
			}
			if showMode {
				row = append(row, "")
			}
			a := dr.Agg
			row = append(row,
				fmt.Sprintf("%d", a.PodCount),
				fmt.Sprintf("%.2f", a.TotalCPUVCPU),
				fmt.Sprintf("%.1f GB", a.TotalMemGB),
				fmt.Sprintf("$%.4f", a.CostPerHour),
				fmt.Sprintf("$%.4f", a.TotalCost),
				"", // spot: mixed at team level
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
			// Accumulate totals from team summaries only (not workload details).
			totalPods += a.PodCount
			totalCPU += a.TotalCPUVCPU
			totalMem += a.TotalMemGB
			totalCostPerHour += a.CostPerHour
			totalCost += a.TotalCost
			totalWaste += a.WastedCostPerHour

		case rowWorkloadDetail:
			row = buildWorkloadRow(dr.Agg, "", showSubtype, showMode, showUtilization)

		case rowFlat:
			a := dr.Agg
			row = buildWorkloadRow(a, orDefault(a.Key.Team, "-"), showSubtype, showMode, showUtilization)
			// Accumulate totals from flat rows.
			totalPods += a.PodCount
			totalCPU += a.TotalCPUVCPU
			totalMem += a.TotalMemGB
			totalCostPerHour += a.CostPerHour
			totalCost += a.TotalCost
			totalWaste += a.WastedCostPerHour
		}
		rows = append(rows, row)
	}

	// Separator row: horizontal line characters sized to each column header.
	sepRow := make([]string, len(defs))
	for i, d := range defs {
		sepRow[i] = strings.Repeat("─", len(d.header)+2) // +2 for padding equivalent
	}
	separatorIdx := len(rows) // 0-based index of separator row
	rows = append(rows, sepRow)

	// Total row
	totalRow := []string{"TOTAL", ""}
	if showSubtype {
		totalRow = append(totalRow, "")
	}
	if showMode {
		totalRow = append(totalRow, "")
	}
	totalRow = append(totalRow,
		fmt.Sprintf("%d", totalPods),
		fmt.Sprintf("%.2f", totalCPU),
		fmt.Sprintf("%.1f GB", totalMem),
		fmt.Sprintf("$%.4f", totalCostPerHour),
		fmt.Sprintf("$%.4f", totalCost),
		"",
	)
	if showUtilization {
		totalRow = append(totalRow, "", "",
			fmt.Sprintf("$%.4f", totalWaste),
		)
	}
	totalIdx := len(rows) // 0-based index of total row
	rows = append(rows, totalRow)

	// Build headers from column definitions.
	headers := make([]string, len(defs))
	for i, d := range defs {
		headers[i] = sortIndicator(d.header, d.sortCol, d.sortable, sortCfg)
	}

	// Build a set of numeric column indices from definitions.
	numericCols := make(map[int]bool)
	for i, d := range defs {
		if d.numeric {
			numericCols[i] = true
		}
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderRow(false).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			// Separator row: no padding so dashes fill the cell.
			if row == separatorIdx {
				return separatorCellStyle
			}
			// Total row: bold.
			if row == totalIdx {
				if numericCols[col] {
					return totalNumericStyle
				}
				return totalStyle
			}
			// Highlight the cursor row (both are 0-based).
			if cursor >= 0 && row == cursor {
				if numericCols[col] {
					return selectedNumericStyle
				}
				return selectedStyle
			}
			if numericCols[col] {
				return numericStyle
			}
			return cellStyle
		})

	return t.String()
}

// buildWorkloadRow creates a table row for a single workload aggregate.
// teamCol is the value for the TEAM column (empty string for nested workload details).
func buildWorkloadRow(a cost.AggregatedCost, teamCol string, showSubtype, showMode, showUtilization bool) []string {
	spot := ""
	if a.Key.IsSpot {
		spot = "yes"
	}
	row := []string{
		teamCol,
		orDefault(a.Key.Workload, "-"),
	}
	if showSubtype {
		row = append(row, orDefault(a.Key.Subtype, "-"))
	}
	if showMode {
		row = append(row, costModeShort(a.CostMode))
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
	return row
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

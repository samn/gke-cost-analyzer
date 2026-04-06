package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
)

// HistoryDisplayRow represents one row in the history table.
type HistoryDisplayRow struct {
	Kind          rowKind
	TeamName      string
	Expanded      bool
	WorkloadCount int
	Row           bigquery.HistoryCostRow
}

// buildHistoryDisplayRows creates display rows from team groups and expanded state.
func buildHistoryDisplayRows(groups []HistoryTeamGroup, expanded map[string]bool) []HistoryDisplayRow {
	var rows []HistoryDisplayRow
	for _, g := range groups {
		isExpanded := expanded[g.Team]
		rows = append(rows, HistoryDisplayRow{
			Kind:          rowTeamSummary,
			TeamName:      g.Team,
			Expanded:      isExpanded,
			WorkloadCount: len(g.Workloads),
			Row:           g.Summary,
		})
		if isExpanded {
			for _, w := range g.Workloads {
				rows = append(rows, HistoryDisplayRow{
					Kind: rowWorkloadDetail,
					Row:  w,
				})
			}
		}
	}
	return rows
}

// buildFlatHistoryDisplayRows creates flat display rows (no team grouping).
func buildFlatHistoryDisplayRows(rows []bigquery.HistoryCostRow) []HistoryDisplayRow {
	out := make([]HistoryDisplayRow, len(rows))
	for i, r := range rows {
		out[i] = HistoryDisplayRow{
			Kind: rowFlat,
			Row:  r,
		}
	}
	return out
}

// RenderHistoryTable renders the history cost data as a formatted table string.
func RenderHistoryTable(displayRows []HistoryDisplayRow, showSubtype, showUtilization, showMode bool, sortCfg HistorySortConfig, cursor int, sparklines map[bigquery.WorkloadKey]string) string {
	vis := ColumnVisibility{Subtype: showSubtype, Mode: showMode, Utilization: showUtilization}
	defs := historyVisibleColumns(vis)

	rows := make([][]string, 0, len(displayRows)+2)

	var totalAvgPods, totalAvgCPU, totalAvgMem float64
	var totalCostPerHour, totalCost, totalWaste float64

	for _, dr := range displayRows {
		var row []string
		switch dr.Kind {
		case rowTeamSummary:
			arrow := "▶"
			if dr.Expanded {
				arrow = "▼"
			}
			r := dr.Row
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
			row = append(row,
				fmt.Sprintf("%.1f", r.AvgPods),
				fmt.Sprintf("%.2f", r.AvgCPUVCPU),
				fmt.Sprintf("%.1f GB", r.AvgMemoryGB),
				fmt.Sprintf("$%.4f", r.AvgCostPerHour),
				fmt.Sprintf("$%.4f", r.TotalCost),
				"", // trend: empty at team level
				"", // spot: mixed at team level
			)
			if showUtilization {
				row = append(row, "", "", fmt.Sprintf("$%.4f", r.TotalWastedCost))
			}
			totalAvgPods += r.AvgPods
			totalAvgCPU += r.AvgCPUVCPU
			totalAvgMem += r.AvgMemoryGB
			totalCostPerHour += r.AvgCostPerHour
			totalCost += r.TotalCost
			totalWaste += r.TotalWastedCost

		case rowWorkloadDetail:
			row = buildHistoryWorkloadRow(dr.Row, "", showSubtype, showMode, showUtilization, sparklines)

		case rowFlat:
			r := dr.Row
			row = buildHistoryWorkloadRow(r, orDefault(r.Team, "-"), showSubtype, showMode, showUtilization, sparklines)
			totalAvgPods += r.AvgPods
			totalAvgCPU += r.AvgCPUVCPU
			totalAvgMem += r.AvgMemoryGB
			totalCostPerHour += r.AvgCostPerHour
			totalCost += r.TotalCost
			totalWaste += r.TotalWastedCost
		}
		rows = append(rows, row)
	}

	// Separator row
	sepRow := make([]string, len(defs))
	for i, d := range defs {
		sepRow[i] = strings.Repeat("─", len(d.header)+2)
	}
	separatorIdx := len(rows)
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
		fmt.Sprintf("%.1f", totalAvgPods),
		fmt.Sprintf("%.2f", totalAvgCPU),
		fmt.Sprintf("%.1f GB", totalAvgMem),
		fmt.Sprintf("$%.4f", totalCostPerHour),
		fmt.Sprintf("$%.4f", totalCost),
		"", // trend
		"", // spot
	)
	if showUtilization {
		totalRow = append(totalRow, "", "", fmt.Sprintf("$%.4f", totalWaste))
	}
	totalIdx := len(rows)
	rows = append(rows, totalRow)

	// Headers
	headers := make([]string, len(defs))
	for i, d := range defs {
		headers[i] = historySortIndicator(d.header, d.sortCol, d.sortable, sortCfg)
	}

	// Numeric columns
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
			if row == separatorIdx {
				return separatorCellStyle
			}
			if row == totalIdx {
				if numericCols[col] {
					return totalNumericStyle
				}
				return totalStyle
			}
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

// buildHistoryWorkloadRow creates a table row for a single history workload.
func buildHistoryWorkloadRow(r bigquery.HistoryCostRow, teamCol string, showSubtype, showMode, showUtilization bool, sparklines map[bigquery.WorkloadKey]string) []string {
	spot := ""
	if r.HasSpot {
		spot = "yes"
	}
	row := []string{
		teamCol,
		orDefault(r.Workload, "-"),
	}
	if showSubtype {
		row = append(row, orDefault(r.Subtype, "-"))
	}
	if showMode {
		row = append(row, costModeShort(r.CostMode))
	}

	key := bigquery.KeyFromRow(r)
	spark := ""
	if sparklines != nil {
		spark = sparklines[key]
	}

	row = append(row,
		fmt.Sprintf("%.1f", r.AvgPods),
		fmt.Sprintf("%.2f", r.AvgCPUVCPU),
		fmt.Sprintf("%.1f GB", r.AvgMemoryGB),
		fmt.Sprintf("$%.4f", r.AvgCostPerHour),
		fmt.Sprintf("$%.4f", r.TotalCost),
		spark,
		spot,
	)
	if showUtilization {
		if r.AvgCPUUtil != nil {
			row = append(row,
				fmt.Sprintf("%.0f%%", *r.AvgCPUUtil*100),
				fmt.Sprintf("%.0f%%", derefFloat(r.AvgMemUtil)*100),
				fmt.Sprintf("$%.4f", r.TotalWastedCost),
			)
		} else {
			row = append(row, "-", "-", "-")
		}
	}
	return row
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

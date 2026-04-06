package tui

import (
	"sort"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
)

// HistSortColumn identifies which column to sort by in the history view.
type HistSortColumn int

const (
	HistSortByCluster HistSortColumn = iota
	HistSortByTeam
	HistSortByWorkload
	HistSortBySubtype
	HistSortByMode
	HistSortByAvgPods
	HistSortByAvgCPU
	HistSortByAvgMem
	HistSortByAvgCostPerHour
	HistSortByTotalCost
	HistSortByWaste
)

// historyColumnDef describes a single column in the history table.
type historyColumnDef struct {
	header   string
	sortCol  HistSortColumn
	sortable bool
	numeric  bool
	helpName string
}

// historyVisibleColumns returns the column definitions for the history table.
func historyVisibleColumns(vis ColumnVisibility) []historyColumnDef {
	var cols []historyColumnDef
	if vis.Cluster {
		cols = append(cols, historyColumnDef{header: "CLUSTER", sortCol: HistSortByCluster, sortable: true, helpName: "Cluster"})
	}
	cols = append(cols,
		historyColumnDef{header: "TEAM", sortCol: HistSortByTeam, sortable: true, helpName: "Team"},
		historyColumnDef{header: "WORKLOAD", sortCol: HistSortByWorkload, sortable: true, helpName: "Workload"},
	)
	if vis.Subtype {
		cols = append(cols, historyColumnDef{header: "SUBTYPE", sortCol: HistSortBySubtype, sortable: true, helpName: "Subtype"})
	}
	if vis.Mode {
		cols = append(cols, historyColumnDef{header: "MODE", sortCol: HistSortByMode, sortable: true, helpName: "Mode"})
	}
	cols = append(cols,
		historyColumnDef{header: "AVG PODS", sortCol: HistSortByAvgPods, sortable: true, numeric: true, helpName: "Pods"},
		historyColumnDef{header: "AVG CPU", sortCol: HistSortByAvgCPU, sortable: true, numeric: true, helpName: "CPU"},
		historyColumnDef{header: "AVG MEM", sortCol: HistSortByAvgMem, sortable: true, numeric: true, helpName: "Mem"},
		historyColumnDef{header: "AVG $/HR", sortCol: HistSortByAvgCostPerHour, sortable: true, numeric: true, helpName: "$/hr"},
		historyColumnDef{header: "TOTAL", sortCol: HistSortByTotalCost, sortable: true, numeric: true, helpName: "Total"},
		historyColumnDef{header: "TREND", sortable: false},
		historyColumnDef{header: "SPOT", sortable: false},
	)
	if vis.Utilization {
		cols = append(cols,
			historyColumnDef{header: "CPU%", sortable: false, numeric: true},
			historyColumnDef{header: "MEM%", sortable: false, numeric: true},
			historyColumnDef{header: "WASTE", sortCol: HistSortByWaste, sortable: true, numeric: true, helpName: "Waste"},
		)
	}
	return cols
}

// HistorySortConfig holds the current sort column and direction for history.
type HistorySortConfig struct {
	Column HistSortColumn
	Asc    bool
}

// DefaultHistorySort returns the default sort (total cost descending).
func DefaultHistorySort() HistorySortConfig {
	return HistorySortConfig{Column: HistSortByTotalCost, Asc: false}
}

// HistoryTeamGroup represents a team with its workloads and summary in the history view.
type HistoryTeamGroup struct {
	Team      string
	Workloads []bigquery.HistoryCostRow
	Summary   bigquery.HistoryCostRow
}

// SortHistoryRows sorts history rows in place according to the given config.
func SortHistoryRows(rows []bigquery.HistoryCostRow, cfg HistorySortConfig) {
	sort.SliceStable(rows, func(i, j int) bool {
		cmp := histCompareByColumn(rows[i], rows[j], cfg.Column)
		if cmp != 0 {
			if cfg.Asc {
				return cmp < 0
			}
			return cmp > 0
		}
		// Secondary sort: cluster → team → workload ascending
		if rows[i].ClusterName != rows[j].ClusterName {
			return rows[i].ClusterName < rows[j].ClusterName
		}
		if rows[i].Team != rows[j].Team {
			return rows[i].Team < rows[j].Team
		}
		return rows[i].Workload < rows[j].Workload
	})
}

func histCompareByColumn(a, b bigquery.HistoryCostRow, col HistSortColumn) int {
	switch col {
	case HistSortByCluster:
		return compareStr(a.ClusterName, b.ClusterName)
	case HistSortByTeam:
		return compareStr(a.Team, b.Team)
	case HistSortByWorkload:
		return compareStr(a.Workload, b.Workload)
	case HistSortBySubtype:
		return compareStr(a.Subtype, b.Subtype)
	case HistSortByMode:
		return compareStr(a.CostMode, b.CostMode)
	case HistSortByAvgPods:
		return compareFloat(a.AvgPods, b.AvgPods)
	case HistSortByAvgCPU:
		return compareFloat(a.AvgCPUVCPU, b.AvgCPUVCPU)
	case HistSortByAvgMem:
		return compareFloat(a.AvgMemoryGB, b.AvgMemoryGB)
	case HistSortByAvgCostPerHour:
		return compareFloat(a.AvgCostPerHour, b.AvgCostPerHour)
	case HistSortByTotalCost:
		return compareFloat(a.TotalCost, b.TotalCost)
	case HistSortByWaste:
		return compareFloat(a.TotalWastedCost, b.TotalWastedCost)
	default:
		return 0
	}
}

// GroupHistoryByTeam groups history rows by team name.
func GroupHistoryByTeam(rows []bigquery.HistoryCostRow) []HistoryTeamGroup {
	teamMap := make(map[string]*HistoryTeamGroup)
	var teamOrder []string

	for _, r := range rows {
		team := r.Team
		if team == "" {
			team = "-"
		}
		tg, ok := teamMap[team]
		if !ok {
			tg = &HistoryTeamGroup{Team: team}
			teamMap[team] = tg
			teamOrder = append(teamOrder, team)
		}
		tg.Workloads = append(tg.Workloads, r)
		tg.Summary.AvgPods += r.AvgPods
		tg.Summary.AvgCPUVCPU += r.AvgCPUVCPU
		tg.Summary.AvgMemoryGB += r.AvgMemoryGB
		tg.Summary.TotalCost += r.TotalCost
		tg.Summary.TotalCPUCost += r.TotalCPUCost
		tg.Summary.TotalMemCost += r.TotalMemCost
		tg.Summary.AvgCostPerHour += r.AvgCostPerHour
		tg.Summary.TotalWastedCost += r.TotalWastedCost
	}

	groups := make([]HistoryTeamGroup, 0, len(teamOrder))
	for _, name := range teamOrder {
		tg := teamMap[name]
		tg.Summary.Team = tg.Team
		groups = append(groups, *tg)
	}
	return groups
}

// SortHistoryTeamGroups sorts team groups by their summaries, and workloads within each group.
func SortHistoryTeamGroups(groups []HistoryTeamGroup, cfg HistorySortConfig) {
	sort.SliceStable(groups, func(i, j int) bool {
		cmp := histCompareByColumn(groups[i].Summary, groups[j].Summary, cfg.Column)
		if cmp != 0 {
			if cfg.Asc {
				return cmp < 0
			}
			return cmp > 0
		}
		return groups[i].Team < groups[j].Team
	})
	for i := range groups {
		SortHistoryRows(groups[i].Workloads, cfg)
	}
}

// HistoryColumnForKey maps a number key press to a history sort column.
func HistoryColumnForKey(key rune, vis ColumnVisibility) (HistSortColumn, bool) {
	defs := historyVisibleColumns(vis)

	var cols []HistSortColumn
	for _, d := range defs {
		if d.sortable {
			cols = append(cols, d.sortCol)
		}
	}

	idx := int(key-'1') % 10
	if key == '0' {
		idx = 9
	}
	if idx < 0 || idx >= len(cols) {
		return 0, false
	}
	return cols[idx], true
}

// historySortIndicator returns the header text with a sort direction arrow.
func historySortIndicator(header string, col HistSortColumn, sortable bool, cfg HistorySortConfig) string {
	if !sortable || col != cfg.Column {
		return header
	}
	if cfg.Asc {
		return header + " ^"
	}
	return header + " v"
}

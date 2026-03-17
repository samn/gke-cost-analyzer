package tui

import (
	"sort"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
)

// TeamGroup represents a team with its aggregated workloads and summary.
type TeamGroup struct {
	Team      string
	Workloads []cost.AggregatedCost
	Summary   cost.AggregatedCost // rolled-up totals across all workloads
}

// SortColumn identifies which column to sort by.
type SortColumn int

const (
	SortByTeam SortColumn = iota
	SortByWorkload
	SortBySubtype
	SortByMode
	SortByPods
	SortByCPU
	SortByMem
	SortByCostPerHour
	SortByCost
	SortByCPUUtil
	SortByWaste
)

// columnDef describes a single table column.
type columnDef struct {
	header   string     // display header text
	sortCol  SortColumn // sort column constant (used for key mapping and sort indicators)
	sortable bool       // whether this column supports sorting
	numeric  bool       // whether to right-align
	helpName string     // short name for help footer (empty = use header)
}

// ColumnVisibility controls which optional columns are shown.
type ColumnVisibility struct {
	Subtype     bool
	Mode        bool
	Utilization bool
}

// visibleColumns returns the ordered column definitions for the current visibility settings.
// This is the single source of truth for column ordering — table.go, sort.go, and model.go
// all derive their column lists from this function.
func visibleColumns(vis ColumnVisibility) []columnDef {
	cols := []columnDef{
		{header: "TEAM", sortCol: SortByTeam, sortable: true, helpName: "Team"},
		{header: "WORKLOAD", sortCol: SortByWorkload, sortable: true, helpName: "Workload"},
	}
	if vis.Subtype {
		cols = append(cols, columnDef{header: "SUBTYPE", sortCol: SortBySubtype, sortable: true, helpName: "Subtype"})
	}
	if vis.Mode {
		cols = append(cols, columnDef{header: "MODE", sortCol: SortByMode, sortable: true, helpName: "Mode"})
	}
	cols = append(cols,
		columnDef{header: "PODS", sortCol: SortByPods, sortable: true, numeric: true, helpName: "Pods"},
		columnDef{header: "CPU REQ", sortCol: SortByCPU, sortable: true, numeric: true, helpName: "CPU"},
		columnDef{header: "MEM REQ", sortCol: SortByMem, sortable: true, numeric: true, helpName: "Mem"},
		columnDef{header: "$/HR", sortCol: SortByCostPerHour, sortable: true, numeric: true, helpName: "$/hr"},
		columnDef{header: "COST", sortCol: SortByCost, sortable: true, numeric: true, helpName: "Cost"},
		columnDef{header: "SPOT", sortable: false},
	)
	if vis.Utilization {
		cols = append(cols,
			columnDef{header: "CPU%", sortCol: SortByCPUUtil, sortable: true, numeric: true, helpName: "CPU%"},
			columnDef{header: "MEM%", sortable: false, numeric: true},
			columnDef{header: "WASTE", sortCol: SortByWaste, sortable: true, numeric: true, helpName: "Waste"},
		)
	}
	return cols
}

// SortConfig holds the current sort column and direction.
type SortConfig struct {
	Column SortColumn
	Asc    bool
}

// DefaultSort returns the default sort configuration (team ascending).
func DefaultSort() SortConfig {
	return SortConfig{Column: SortByTeam, Asc: true}
}

// SortAggs sorts aggregated costs in place according to the given config.
// Ties are broken by team → workload → subtype ascending.
func SortAggs(aggs []cost.AggregatedCost, cfg SortConfig) {
	sort.SliceStable(aggs, func(i, j int) bool {
		cmp := compareByColumn(aggs[i], aggs[j], cfg.Column)
		if cmp != 0 {
			if cfg.Asc {
				return cmp < 0
			}
			return cmp > 0
		}
		// Secondary sort: team → workload → subtype ascending
		if aggs[i].Key.Team != aggs[j].Key.Team {
			return aggs[i].Key.Team < aggs[j].Key.Team
		}
		if aggs[i].Key.Workload != aggs[j].Key.Workload {
			return aggs[i].Key.Workload < aggs[j].Key.Workload
		}
		return aggs[i].Key.Subtype < aggs[j].Key.Subtype
	})
}

// compareByColumn returns -1, 0, or 1 comparing a and b on the given column.
func compareByColumn(a, b cost.AggregatedCost, col SortColumn) int {
	switch col {
	case SortByTeam:
		return compareStr(a.Key.Team, b.Key.Team)
	case SortByWorkload:
		return compareStr(a.Key.Workload, b.Key.Workload)
	case SortBySubtype:
		return compareStr(a.Key.Subtype, b.Key.Subtype)
	case SortByMode:
		return compareStr(a.CostMode, b.CostMode)
	case SortByPods:
		return compareInt(a.PodCount, b.PodCount)
	case SortByCPU:
		return compareFloat(a.TotalCPUVCPU, b.TotalCPUVCPU)
	case SortByMem:
		return compareFloat(a.TotalMemGB, b.TotalMemGB)
	case SortByCostPerHour:
		return compareFloat(a.CostPerHour, b.CostPerHour)
	case SortByCost:
		return compareFloat(a.TotalCost, b.TotalCost)
	case SortByCPUUtil:
		return compareFloat(a.CPUUtilization, b.CPUUtilization)
	case SortByWaste:
		return compareFloat(a.WastedCostPerHour, b.WastedCostPerHour)
	default:
		return 0
	}
}

func compareStr(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// groupByTeam groups sorted aggregated costs by team name.
// The order of teams follows first-appearance in the input slice.
// Workloads within each team preserve their input order.
func groupByTeam(aggs []cost.AggregatedCost) []TeamGroup {
	teamMap := make(map[string]*TeamGroup)
	var teamOrder []string

	for _, a := range aggs {
		team := a.Key.Team
		if team == "" {
			team = "-"
		}
		tg, ok := teamMap[team]
		if !ok {
			tg = &TeamGroup{Team: team}
			teamMap[team] = tg
			teamOrder = append(teamOrder, team)
		}
		tg.Workloads = append(tg.Workloads, a)
		tg.Summary.PodCount += a.PodCount
		tg.Summary.TotalCPUVCPU += a.TotalCPUVCPU
		tg.Summary.TotalMemGB += a.TotalMemGB
		tg.Summary.CostPerHour += a.CostPerHour
		tg.Summary.TotalCost += a.TotalCost
		tg.Summary.CPUCostPerHour += a.CPUCostPerHour
		tg.Summary.MemCostPerHour += a.MemCostPerHour
		tg.Summary.WastedCostPerHour += a.WastedCostPerHour
		if a.HasUtilization {
			tg.Summary.HasUtilization = true
		}
	}

	groups := make([]TeamGroup, 0, len(teamOrder))
	for _, name := range teamOrder {
		tg := teamMap[name]
		tg.Summary.Key.Team = tg.Team
		// Compute weighted utilization for the team summary.
		if tg.Summary.HasUtilization && tg.Summary.TotalCPUVCPU > 0 {
			var cpuUsed, cpuReqWithUsage, memUsed, memReqWithUsage float64
			for _, w := range tg.Workloads {
				if w.HasUtilization {
					cpuUsed += w.CPUUtilization * w.TotalCPUVCPU
					cpuReqWithUsage += w.TotalCPUVCPU
					memUsed += w.MemUtilization * w.TotalMemGB
					memReqWithUsage += w.TotalMemGB
				}
			}
			if cpuReqWithUsage > 0 {
				tg.Summary.CPUUtilization = cpuUsed / cpuReqWithUsage
			}
			if memReqWithUsage > 0 {
				tg.Summary.MemUtilization = memUsed / memReqWithUsage
			}
		}
		groups = append(groups, *tg)
	}
	return groups
}

// SortTeamGroups sorts team groups by comparing their summaries using the given config.
// Within each group, workloads are also sorted by the same config.
func SortTeamGroups(groups []TeamGroup, cfg SortConfig) {
	sort.SliceStable(groups, func(i, j int) bool {
		cmp := compareByColumn(groups[i].Summary, groups[j].Summary, cfg.Column)
		if cmp != 0 {
			if cfg.Asc {
				return cmp < 0
			}
			return cmp > 0
		}
		return groups[i].Team < groups[j].Team
	})
	for i := range groups {
		SortAggs(groups[i].Workloads, cfg)
	}
}

// ColumnForKey maps a number key press to a sort column.
// Returns the column and true if the key is valid, or false otherwise.
func ColumnForKey(key rune, showSubtype, showUtilization, showMode bool) (SortColumn, bool) {
	vis := ColumnVisibility{Subtype: showSubtype, Mode: showMode, Utilization: showUtilization}
	defs := visibleColumns(vis)

	// Build sortable column list
	var cols []SortColumn
	for _, d := range defs {
		if d.sortable {
			cols = append(cols, d.sortCol)
		}
	}

	// Keys '1'-'9' map to indices 0-8, '0' maps to index 9.
	idx := int(key-'1') % 10
	if key == '0' {
		idx = 9
	}
	if idx < 0 || idx >= len(cols) {
		return 0, false
	}
	return cols[idx], true
}

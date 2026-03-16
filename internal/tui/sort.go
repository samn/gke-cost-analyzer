package tui

import (
	"sort"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
)

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

// ColumnForKey maps a number key press to a sort column.
// Returns the column and true if the key is valid, or false otherwise.
func ColumnForKey(key rune, showSubtype, showUtilization, showMode bool) (SortColumn, bool) {
	cols := []SortColumn{SortByTeam, SortByWorkload}
	if showSubtype {
		cols = append(cols, SortBySubtype)
	}
	if showMode {
		cols = append(cols, SortByMode)
	}
	cols = append(cols, SortByPods, SortByCPU, SortByMem, SortByCostPerHour, SortByCost)
	if showUtilization {
		cols = append(cols, SortByCPUUtil, SortByWaste)
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

# Team Rollup & Drill-down in Monitor Mode

## Goal
- Default view groups rows by team (rolled up), showing team-level totals
- Users can expand a team to see individual workload rows
- Horizontal separator before the TOTAL row
- TOTAL row includes pod count, CPU REQ, MEM REQ

## Design

### Data Model
- `TeamGroup`: team name + workloads slice + summary aggregate
- `DisplayRow`: tagged union (teamSummary | workloadDetail) with aggregate data
- `groupByTeam()`: groups sorted aggs into TeamGroups
- `buildDisplayRows()`: creates display rows from groups + expanded state

### Model State
- `expandedTeams map[string]bool` — which teams are expanded
- `cursor int` — selected row index (into display rows, not total)
- Recompute display rows on data update or expand/collapse toggle

### Key Bindings
- Up/Down (↑↓ or j/k): move cursor
- Enter: expand/collapse selected team
- a: toggle expand all / collapse all
- Existing number keys: sort columns
- q: quit

### Rendering
- Team summary row: TEAM | "N workloads ▶/▼" | pods | cpu | mem | $/hr | cost | spot
- Workload detail row: (empty) | workload-name | pods | cpu | mem | $/hr | cost | spot
- Selected row highlighted via Reverse style
- Last data row underlined to separate from TOTAL
- TOTAL row includes total pods, CPU REQ, MEM REQ

### Sorting
- Teams sorted by team-level aggregate (e.g., total cost, total pods)
- Within expanded teams, workloads sorted by same criterion
- Secondary sort tie-breaking preserved (team → workload → subtype)

### Files Modified
- `internal/tui/sort.go` — TeamGroup type, SortTeamGroups
- `internal/tui/table.go` — DisplayRow, groupByTeam, buildDisplayRows, RenderTable refactor
- `internal/tui/model.go` — expandedTeams, cursor, key handling
- Tests updated for all changes

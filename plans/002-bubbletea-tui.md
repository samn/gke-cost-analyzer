# Plan 002: Bubbletea TUI for Watch Command

## Problem

The `watch` command's table output using `text/tabwriter` has alignment issues —
column headers don't line up with values, making it hard to read.

## Solution

Replace the raw terminal output loop with a [bubbletea](https://github.com/charmbracelet/bubbletea)
TUI application, using [lipgloss/table](https://github.com/charmbracelet/lipgloss)
for properly aligned, bordered table rendering.

## Changes

### New files

- `internal/tui/model.go` — Bubbletea `Model` implementing `Init`, `Update`, `View`
  - Manages periodic data fetching via tick messages
  - Handles quit on `q` / `Ctrl+C`
  - Shows loading state, error state, and cost table
- `internal/tui/table.go` — `RenderTable()` using `lipgloss/table`
  - Proper column alignment (right-aligned for numeric columns)
  - Box-drawing borders
  - Header styling (bold)
  - TOTAL row at the bottom
- `internal/tui/model_test.go` — Tests for model lifecycle
- `internal/tui/table_test.go` — Tests for table rendering

### Modified files

- `cmd/watch.go` — Replaced manual ticker loop with `tea.NewProgram(model, tea.WithAltScreen())`
- `cmd/common.go` — Moved shared `podLister` interface here (used by both watch and record)
- `cmd/watch_test.go` — Removed tests for deleted functions; validation tests remain
- `cmd/testing_helpers_test.go` — Extracted `mockPodLister` shared across test files

### Dependencies added

- `github.com/charmbracelet/bubbletea` v1.3.10
- `github.com/charmbracelet/lipgloss` v1.1.0

## Architecture

```
cmd/watch.go  →  tui.NewModel(...)  →  tea.NewProgram(model).Run()
                     │
                     ├── Init()    → fetchCosts (first load)
                     ├── Update()  → handle tick/data/error/key messages
                     └── View()    → RenderTable(aggs) via lipgloss/table
```

The bubbletea framework is now in place for future TUI enhancements (scrolling,
filtering, interactive selection, etc.).

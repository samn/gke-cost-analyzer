# Plan: Upgrade Bubbletea to v2

## Summary

Upgrade `github.com/charmbracelet/bubbletea` v1 to `charm.land/bubbletea/v2` and
`github.com/charmbracelet/lipgloss` v1 to `charm.land/lipgloss/v2`. Update all
other dependencies to latest.

## Changes Required

### Import Paths
- `github.com/charmbracelet/bubbletea` → `charm.land/bubbletea/v2`
- `github.com/charmbracelet/lipgloss` → `charm.land/lipgloss/v2`
- `github.com/charmbracelet/lipgloss/table` → `charm.land/lipgloss/v2/table`

### API Changes

1. **View() signature**: `View() string` → `View() tea.View`, use `tea.NewView(s)`.
2. **AltScreen**: Remove `tea.WithAltScreen()` from `tea.NewProgram`, set `view.AltScreen = true` in `View()`.
3. **KeyMsg → KeyPressMsg**: `tea.KeyMsg` → `tea.KeyPressMsg` in Update switch.
4. **Space key**: `case " ":` → `case "space":` (already using `" "` in the code).
5. **Key construction in tests**: `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}` → `tea.KeyPressMsg{Code: 'q', Text: "q"}`.
6. **Special keys in tests**: `tea.KeyMsg{Type: tea.KeyDown}` → `tea.KeyPressMsg{Code: tea.KeyDown}`, etc.

### Files to Update
- `internal/tui/model.go` — imports, View(), Update() key handling
- `internal/tui/model_test.go` — imports, key message construction
- `internal/tui/table.go` — lipgloss import path
- `internal/tui/events.go` — lipgloss import path
- `cmd/watch.go` — import path, remove WithAltScreen
- `go.mod` / `go.sum` — dependency update

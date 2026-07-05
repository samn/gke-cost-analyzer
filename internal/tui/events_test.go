package tui

import (
	"strings"
	"testing"
	"time"

	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/samn/gke-cost-analyzer/internal/cost"
	"github.com/samn/gke-cost-analyzer/internal/trend"
)

func makeEvent(kind trend.EventKind, team, workload string, pctChange float64, ago time.Duration) trend.Event {
	return trend.Event{
		Time:      time.Now().Add(-ago),
		Key:       cost.GroupKey{Team: team, Workload: workload},
		Kind:      kind,
		PrevCost:  1.0,
		NewCost:   1.0 + pctChange/100,
		PctChange: pctChange,
	}
}

func TestRenderEventLog_Empty(t *testing.T) {
	got := RenderEventLog(nil, time.Now(), 5)
	if !strings.Contains(got, "waiting for data") {
		t.Errorf("empty log should show waiting message, got: %s", got)
	}
}

func TestRenderEventLog_ShowsEvents(t *testing.T) {
	now := time.Now()
	events := []trend.Event{
		makeEvent(trend.EventAppeared, "platform", "web", 0, 2*time.Minute),
		makeEvent(trend.EventAberration, "platform", "web", 45, 30*time.Second),
	}
	got := RenderEventLog(events, now, 5)
	if !strings.Contains(got, "Events") {
		t.Errorf("should show header, got: %s", got)
	}
	if !strings.Contains(got, "platform/web") {
		t.Errorf("should show workload, got: %s", got)
	}
}

func TestRenderEventLog_MaxLines(t *testing.T) {
	now := time.Now()
	var events []trend.Event
	for i := 0; i < 20; i++ {
		events = append(events, makeEvent(
			trend.EventAberration, "team", "workload",
			float64(i*5), time.Duration(20-i)*time.Second,
		))
	}

	got := RenderEventLog(events, now, 5)
	// Should have header + 5 event lines = 6 lines total.
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Errorf("expected 6 lines (header + 5 events), got %d", len(lines))
	}
}

func TestRenderEventLog_ShowsMostRecent(t *testing.T) {
	now := time.Now()
	events := []trend.Event{
		makeEvent(trend.EventAberration, "team", "old", 10, 5*time.Minute),
		makeEvent(trend.EventAberration, "team", "new", 20, 5*time.Second),
	}

	got := RenderEventLog(events, now, 1)
	// With maxLines=1, should show only the most recent event.
	if !strings.Contains(got, "team/new") {
		t.Errorf("should show most recent event, got: %s", got)
	}
}

func TestRenderEventLogScrolled(t *testing.T) {
	now := time.Now()
	var events []trend.Event
	for i := 0; i < 10; i++ {
		events = append(events, makeEvent(trend.EventAppeared, "t", fmt.Sprintf("w%d", i), 0, time.Minute))
	}

	// Unscrolled: latest events (w8, w9) visible, oldest not.
	got := RenderEventLogScrolled(events, now, 2, 0)
	if !strings.Contains(got, "w9") || strings.Contains(got, "w0") {
		t.Errorf("offset 0 should show the tail, got: %s", got)
	}

	// Scrolled back 8: shows w0, w1.
	got = RenderEventLogScrolled(events, now, 2, 8)
	if !strings.Contains(got, "t/w0") || !strings.Contains(got, "t/w1") {
		t.Errorf("offset 8 should show the head, got: %s", got)
	}
	if strings.Contains(got, "w9") {
		t.Errorf("offset 8 should not show the tail, got: %s", got)
	}

	// Scrolled view indicates there is newer content.
	if !strings.Contains(got, "older") {
		t.Errorf("scrolled view should indicate scrollback, got: %s", got)
	}

	// Offset beyond history clamps to the head.
	got = RenderEventLogScrolled(events, now, 2, 100)
	if !strings.Contains(got, "t/w0") {
		t.Errorf("excess offset should clamp to head, got: %s", got)
	}
}

func TestEventScrollKeys(t *testing.T) {
	// '[' scrolls back through the event log, ']' scrolls forward; both are
	// clamped and only active while the event panel is visible.
	cfg := trend.DefaultConfig()
	m := testModel(&mockPodLister{})
	m.tracker = trend.NewTracker(cfg)
	m.showEvents = true

	// Seed 10 events (appearances) via tracker updates.
	now := time.Now()
	m.tracker.Update(nil, now)
	for i := 0; i < 10; i++ {
		m.tracker.Update([]cost.AggregatedCost{
			{Key: cost.GroupKey{Team: "t", Workload: fmt.Sprintf("w%d", i)}, CostPerHour: 1},
		}, now.Add(time.Duration(i)*time.Second))
	}

	m2 := pressKey(t, m, "[")
	if m2.eventScroll != 1 {
		t.Errorf("eventScroll after '[' = %d, want 1", m2.eventScroll)
	}
	m3 := pressKey(t, m2, "]")
	if m3.eventScroll != 0 {
		t.Errorf("eventScroll after ']' = %d, want 0", m3.eventScroll)
	}
	// ']' at the tail stays clamped at 0.
	m4 := pressKey(t, m3, "]")
	if m4.eventScroll != 0 {
		t.Errorf("eventScroll clamp failed, got %d", m4.eventScroll)
	}
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	return updated.(Model)
}

package trend

import (
	"fmt"
	"time"
)

// FormatEvent returns a single-line display string for an event.
func FormatEvent(e Event, now time.Time) string {
	ago := FormatTimeAgo(e.Time, now)
	workload := e.Key.Workload
	if e.Key.Team != "" {
		workload = e.Key.Team + "/" + workload
	}
	if workload == "" {
		workload = "(unknown)"
	}

	switch e.Kind {
	case EventAberration:
		// %.0f rounds; only sign a change that displays as non-zero.
		dir := ""
		if e.PctChange >= 0.5 {
			dir = "+"
		}
		pct := e.PctChange
		if pct > -0.5 && pct < 0.5 {
			pct = 0
		}
		return fmt.Sprintf("%-8s %-30s cost %s%.0f%% ($%.4f -> $%.4f)",
			ago, workload, dir, pct, e.PrevCost, e.NewCost)
	case EventAppeared:
		return fmt.Sprintf("%-8s %-30s appeared ($%.4f/hr)", ago, workload, e.NewCost)
	case EventDisappeared:
		return fmt.Sprintf("%-8s %-30s disappeared (was $%.4f/hr)", ago, workload, e.PrevCost)
	default:
		return fmt.Sprintf("%-8s %-30s unknown event", ago, workload)
	}
}

// FormatTimeAgo returns a human-readable relative time string.
func FormatTimeAgo(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}

	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

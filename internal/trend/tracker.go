// Package trend implements cost aberration detection using EWMA (Exponential
// Weighted Moving Average) with Z-score thresholds. It tracks per-workload
// cost trends and emits events when costs deviate significantly from their
// recent baseline, while tolerating normal cyclical patterns like autoscaling.
package trend

import (
	"math"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/cost"
)

// Config holds tunable parameters for trend detection.
type Config struct {
	Alpha          float64 // EWMA smoothing factor (0–1), default 0.3
	Threshold      float64 // Z-score threshold for aberration, default 3.0
	MinSamples     int     // Minimum observations before flagging, default 5
	MinCostPerHour float64 // Ignore workloads below this $/hr, default 0.001
	MaxEvents      int     // Maximum events to retain, default 100
}

// DefaultConfig returns sensible defaults for trend detection.
func DefaultConfig() Config {
	return Config{
		Alpha:          0.3,
		Threshold:      3.0,
		MinSamples:     5,
		MinCostPerHour: 0.001,
		MaxEvents:      100,
	}
}

// EventKind classifies the type of trend event.
type EventKind int

const (
	EventAberration  EventKind = iota // cost deviated significantly from trend
	EventAppeared                     // workload appeared (new GroupKey)
	EventDisappeared                  // workload disappeared (GroupKey no longer present)
)

// Event represents a detected cost change or lifecycle event.
type Event struct {
	Time      time.Time
	Key       cost.GroupKey
	Kind      EventKind
	PrevCost  float64 // EWMA at time of detection (for aberrations)
	NewCost   float64 // observed CostPerHour
	PctChange float64 // percentage change from EWMA
	ZScore    float64 // how many stddevs away
}

// seriesState holds EWMA state for one workload GroupKey.
type seriesState struct {
	ewma     float64
	ewmaVar  float64
	samples  int
	lastCost float64
	lastSeen time.Time
}

// Tracker maintains per-workload trend state and produces events.
type Tracker struct {
	config    Config
	series    map[cost.GroupKey]*seriesState
	events    []Event
	firstCall bool // true if Update has never been called
}

// NewTracker creates a new Tracker with the given config.
func NewTracker(cfg Config) *Tracker {
	return &Tracker{
		config:    cfg,
		series:    make(map[cost.GroupKey]*seriesState),
		firstCall: true,
	}
}

// Update processes a new set of aggregated costs and returns any newly
// generated events. On the first call, all workloads are recorded as the
// initial baseline without emitting events.
func (t *Tracker) Update(aggs []cost.AggregatedCost, now time.Time) []Event {
	var newEvents []Event

	// Build set of current keys.
	currentKeys := make(map[cost.GroupKey]bool, len(aggs))
	for _, a := range aggs {
		currentKeys[a.Key] = true
	}

	// Process each aggregated cost.
	for _, a := range aggs {
		s, exists := t.series[a.Key]
		if !exists {
			// New workload.
			t.series[a.Key] = &seriesState{
				ewma:     a.CostPerHour,
				ewmaVar:  0,
				samples:  1,
				lastCost: a.CostPerHour,
				lastSeen: now,
			}
			if !t.firstCall {
				newEvents = append(newEvents, Event{
					Time:    now,
					Key:     a.Key,
					Kind:    EventAppeared,
					NewCost: a.CostPerHour,
				})
			}
			continue
		}

		// Compute z-score against the OLD EWMA state before updating,
		// so the current observation doesn't inflate the variance.
		alpha := t.config.Alpha
		diff := a.CostPerHour - s.ewma
		oldEWMA := s.ewma
		oldVar := s.ewmaVar
		s.samples++

		// Check for aberration using pre-update state. Threshold <= 0
		// disables detection. The cost floor ignores noise-level workloads,
		// but compares against the larger of baseline and current cost so a
		// big workload crashing to near-zero is still flagged.
		if t.config.Threshold > 0 && s.samples >= t.config.MinSamples &&
			math.Max(oldEWMA, a.CostPerHour) >= t.config.MinCostPerHour {
			stddev := math.Sqrt(oldVar)
			// Use a floor of 1% of the EWMA so that perfectly stable
			// workloads still detect sudden jumps (variance=0 otherwise).
			minStddev := oldEWMA * 0.01
			if stddev < minStddev {
				stddev = minStddev
			}
			if stddev > 1e-10 {
				zscore := diff / stddev
				if math.Abs(zscore) > t.config.Threshold {
					var pctChange float64
					if oldEWMA > 0 {
						pctChange = (a.CostPerHour - oldEWMA) / oldEWMA * 100
					}
					newEvents = append(newEvents, Event{
						Time:      now,
						Key:       a.Key,
						Kind:      EventAberration,
						PrevCost:  oldEWMA,
						NewCost:   a.CostPerHour,
						PctChange: pctChange,
						ZScore:    zscore,
					})
				}
			}
		}

		// Update EWMA state after the check.
		s.ewma = alpha*a.CostPerHour + (1-alpha)*s.ewma
		s.ewmaVar = (1 - alpha) * (s.ewmaVar + alpha*diff*diff)
		s.lastCost = a.CostPerHour
		s.lastSeen = now
	}

	// Detect disappeared workloads.
	if !t.firstCall {
		for key := range t.series {
			if !currentKeys[key] {
				s := t.series[key]
				newEvents = append(newEvents, Event{
					Time:     now,
					Key:      key,
					Kind:     EventDisappeared,
					PrevCost: s.lastCost,
				})
				delete(t.series, key)
			}
		}
	}

	t.firstCall = false
	t.events = append(t.events, newEvents...)

	// Trim to max events.
	if len(t.events) > t.config.MaxEvents {
		t.events = t.events[len(t.events)-t.config.MaxEvents:]
	}

	return newEvents
}

// Events returns all retained events (oldest first).
func (t *Tracker) Events() []Event {
	return t.events
}

// ActiveAberrations returns the most recent aberration event per GroupKey
// that occurred since the given time. Used by the TUI for row highlighting.
func (t *Tracker) ActiveAberrations(since time.Time) map[cost.GroupKey]Event {
	result := make(map[cost.GroupKey]Event)
	for _, e := range t.events {
		if e.Kind == EventAberration && !e.Time.Before(since) {
			result[e.Key] = e
		}
	}
	return result
}

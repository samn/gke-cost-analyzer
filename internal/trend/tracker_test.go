package trend

import (
	"testing"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/cost"
)

func makeAgg(team, workload string, costPerHour float64) cost.AggregatedCost {
	return cost.AggregatedCost{
		Key:         cost.GroupKey{Team: team, Workload: workload},
		CostPerHour: costPerHour,
	}
}

func TestStableWorkload_NoAberrations(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	for i := 0; i < 20; i++ {
		events := tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
		}, now.Add(time.Duration(i)*10*time.Second))

		if i > 0 { // skip first call (no events)
			for _, e := range events {
				if e.Kind == EventAberration {
					t.Errorf("unexpected aberration at tick %d: %+v", i, e)
				}
			}
		}
	}
}

func TestSuddenSpike_DetectsAberration(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// 10 stable observations.
	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Sudden spike to 3x.
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 3.0),
	}, now.Add(100*time.Second))

	found := false
	for _, e := range events {
		if e.Kind == EventAberration && e.Key.Team == "platform" && e.Key.Workload == "web" {
			found = true
			if e.PctChange <= 0 {
				t.Errorf("expected positive pctChange, got %f", e.PctChange)
			}
			if e.ZScore <= 0 {
				t.Errorf("expected positive zScore, got %f", e.ZScore)
			}
		}
	}
	if !found {
		t.Error("expected aberration event for sudden spike, got none")
	}
}

func TestGradualRamp_NoAberration(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// Gradually increase cost by 5% per tick for 30 ticks.
	costVal := 1.0
	for i := 0; i < 30; i++ {
		events := tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "api", costVal),
		}, now.Add(time.Duration(i)*10*time.Second))

		for _, e := range events {
			if e.Kind == EventAberration {
				t.Errorf("unexpected aberration at tick %d (cost=%.4f): %+v", i, costVal, e)
			}
		}
		costVal *= 1.05
	}
}

func TestWorkloadAppearance(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// First call: no events (initial baseline).
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
	}, now)
	if len(events) != 0 {
		t.Errorf("expected no events on first call, got %d", len(events))
	}

	// Second call with new workload: should emit Appeared.
	events = tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
		makeAgg("platform", "api", 0.5),
	}, now.Add(10*time.Second))

	found := false
	for _, e := range events {
		if e.Kind == EventAppeared && e.Key.Workload == "api" {
			found = true
		}
	}
	if !found {
		t.Error("expected Appeared event for new workload 'api'")
	}
}

func TestWorkloadDisappearance(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// First call with two workloads.
	tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
		makeAgg("platform", "api", 0.5),
	}, now)

	// Second call: api disappears.
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
	}, now.Add(10*time.Second))

	found := false
	for _, e := range events {
		if e.Kind == EventDisappeared && e.Key.Workload == "api" {
			found = true
			if e.PrevCost != 0.5 {
				t.Errorf("expected PrevCost=0.5, got %f", e.PrevCost)
			}
		}
	}
	if !found {
		t.Error("expected Disappeared event for workload 'api'")
	}
}

func TestWarmupPeriod_NoAberrations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 5
	tracker := NewTracker(cfg)
	now := time.Now()

	// First 4 stable, then a spike at sample 5 (total samples = 5, but the
	// spike is the 5th observation so samples becomes 5 after update).
	for i := 0; i < 4; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Spike at tick 4 (5th observation, samples will be 5 after update).
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 10.0),
	}, now.Add(40*time.Second))

	// With MinSamples=5, the 5th sample should trigger detection since
	// samples reaches 5 after the update.
	found := false
	for _, e := range events {
		if e.Kind == EventAberration {
			found = true
		}
	}
	if !found {
		t.Error("expected aberration at sample 5 (MinSamples=5)")
	}
}

func TestWarmupPeriod_TooEarly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 5
	tracker := NewTracker(cfg)
	now := time.Now()

	// Only 3 stable observations, then a spike.
	for i := 0; i < 3; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Spike at tick 3 (4th observation, samples = 4 < MinSamples=5).
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 10.0),
	}, now.Add(30*time.Second))

	for _, e := range events {
		if e.Kind == EventAberration {
			t.Error("should not emit aberration during warmup period")
		}
	}
}

func TestSmallWorkload_Ignored(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinCostPerHour = 0.01
	tracker := NewTracker(cfg)
	now := time.Now()

	// 10 stable at tiny cost.
	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "tiny", 0.001),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// 5x spike on a tiny workload — should be ignored (still below MinCostPerHour).
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "tiny", 0.005),
	}, now.Add(100*time.Second))

	for _, e := range events {
		if e.Kind == EventAberration {
			t.Error("should not flag aberration for workload below MinCostPerHour threshold")
		}
	}
}

func TestEventTrimming(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxEvents = 5
	cfg.MinSamples = 1
	cfg.Threshold = 0.1 // very sensitive, will trigger on any change
	tracker := NewTracker(cfg)
	now := time.Now()

	// First call: baseline.
	tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
	}, now)

	// Generate many events by oscillating cost wildly.
	for i := 1; i <= 20; i++ {
		costVal := 1.0
		if i%2 == 0 {
			costVal = 5.0
		}
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", costVal),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	if len(tracker.Events()) > cfg.MaxEvents {
		t.Errorf("events should be trimmed to %d, got %d", cfg.MaxEvents, len(tracker.Events()))
	}
}

func TestCyclicalPattern_Adapts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSamples = 3
	tracker := NewTracker(cfg)
	now := time.Now()

	// Alternating pattern: 1.0, 1.5, 1.0, 1.5 ... for 30 ticks.
	// After EWMA adapts, this should stop triggering aberrations.
	aberrationCount := 0
	for i := 0; i < 30; i++ {
		costVal := 1.0
		if i%2 == 1 {
			costVal = 1.5
		}
		events := tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", costVal),
		}, now.Add(time.Duration(i)*10*time.Second))

		for _, e := range events {
			if e.Kind == EventAberration {
				aberrationCount++
			}
		}
	}

	// The first few oscillations might trigger, but later ones should not.
	// With alpha=0.3 and threshold=3.0, the EWMA variance should absorb the
	// pattern within ~10 observations.
	if aberrationCount > 5 {
		t.Errorf("cyclical pattern should be tolerated after adaptation, got %d aberrations", aberrationCount)
	}
}

func TestActiveAberrations(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// Build baseline.
	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Spike.
	spikeTime := now.Add(100 * time.Second)
	tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 5.0),
	}, spikeTime)

	// Active since before spike.
	active := tracker.ActiveAberrations(spikeTime.Add(-time.Second))
	key := cost.GroupKey{Team: "platform", Workload: "web"}
	if _, ok := active[key]; !ok {
		t.Error("expected active aberration for platform/web")
	}

	// Active since after spike — should be empty.
	active = tracker.ActiveAberrations(spikeTime.Add(time.Second))
	if _, ok := active[key]; ok {
		t.Error("expected no active aberration after spike time")
	}
}

func TestSuddenDrop_DetectsAberration(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// 10 stable observations at high cost.
	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 5.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Sudden drop to 1/5.
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
	}, now.Add(100*time.Second))

	found := false
	for _, e := range events {
		if e.Kind == EventAberration && e.Key.Workload == "web" {
			found = true
			if e.PctChange >= 0 {
				t.Errorf("expected negative pctChange for drop, got %f", e.PctChange)
			}
			if e.ZScore >= 0 {
				t.Errorf("expected negative zScore for drop, got %f", e.ZScore)
			}
		}
	}
	if !found {
		t.Error("expected aberration event for sudden drop")
	}
}

func TestMultipleWorkloads_Independent(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	// Build baseline for two workloads.
	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
			makeAgg("data", "pipeline", 2.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Spike only web.
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 5.0),
		makeAgg("data", "pipeline", 2.0),
	}, now.Add(100*time.Second))

	webAberration := false
	pipelineAberration := false
	for _, e := range events {
		if e.Kind == EventAberration {
			if e.Key.Workload == "web" {
				webAberration = true
			}
			if e.Key.Workload == "pipeline" {
				pipelineAberration = true
			}
		}
	}
	if !webAberration {
		t.Error("expected aberration for web")
	}
	if pipelineAberration {
		t.Error("should not flag aberration for stable pipeline")
	}
}

func TestFirstCall_NoEvents(t *testing.T) {
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 1.0),
		makeAgg("data", "pipeline", 2.0),
	}, now)

	if len(events) != 0 {
		t.Errorf("first call should produce no events, got %d", len(events))
	}
}

func TestZeroThreshold_DisablesDetection(t *testing.T) {
	// Threshold 0 means "disabled" per the flag contract. Without an internal
	// guard, |z| > 0 fires on nearly every sample.
	cfg := DefaultConfig()
	cfg.Threshold = 0
	tracker := NewTracker(cfg)
	now := time.Now()

	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 1.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// A 10x spike must not produce aberration events when disabled.
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 10.0),
	}, now.Add(200*time.Second))
	for _, e := range events {
		if e.Kind == EventAberration {
			t.Errorf("threshold=0 must disable detection, got %+v", e)
		}
	}
}

func TestDropToNearZero_DetectsAberration(t *testing.T) {
	// MinCostPerHour exists to ignore noise on tiny workloads, but it must
	// not mask a large workload crashing to near-zero cost.
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "web", 5.0),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	// Crash to below MinCostPerHour (0.001 default).
	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "web", 0.0005),
	}, now.Add(100*time.Second))

	found := false
	for _, e := range events {
		if e.Kind == EventAberration && e.ZScore < 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected aberration for a large workload dropping to near-zero cost")
	}
}

func TestTinyWorkloadStaysIgnored(t *testing.T) {
	// Workloads whose baseline AND current cost are below MinCostPerHour
	// remain ignored even when their relative change is large.
	tracker := NewTracker(DefaultConfig())
	now := time.Now()

	for i := 0; i < 10; i++ {
		tracker.Update([]cost.AggregatedCost{
			makeAgg("platform", "tiny", 0.0002),
		}, now.Add(time.Duration(i)*10*time.Second))
	}

	events := tracker.Update([]cost.AggregatedCost{
		makeAgg("platform", "tiny", 0.0008), // 4x, but still noise-level
	}, now.Add(100*time.Second))
	for _, e := range events {
		if e.Kind == EventAberration {
			t.Errorf("noise-level workload should stay ignored, got %+v", e)
		}
	}
}

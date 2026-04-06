package tui

import "testing"

func TestSparklineEmpty(t *testing.T) {
	if got := Sparkline(nil); got != "" {
		t.Errorf("Sparkline(nil) = %q, want empty", got)
	}
	if got := Sparkline([]float64{}); got != "" {
		t.Errorf("Sparkline([]) = %q, want empty", got)
	}
}

func TestSparklineSingleValue(t *testing.T) {
	got := Sparkline([]float64{42})
	if got != "▁" {
		t.Errorf("Sparkline([42]) = %q, want %q", got, "▁")
	}
}

func TestSparklineAscending(t *testing.T) {
	values := []float64{0, 1, 2, 3, 4, 5, 6, 7}
	got := Sparkline(values)
	want := "▁▂▃▄▅▆▇█"
	if got != want {
		t.Errorf("Sparkline(ascending) = %q, want %q", got, want)
	}
}

func TestSparklineDescending(t *testing.T) {
	values := []float64{7, 6, 5, 4, 3, 2, 1, 0}
	got := Sparkline(values)
	want := "█▇▆▅▄▃▂▁"
	if got != want {
		t.Errorf("Sparkline(descending) = %q, want %q", got, want)
	}
}

func TestSparklineAllSame(t *testing.T) {
	values := []float64{5, 5, 5, 5}
	got := Sparkline(values)
	want := "▁▁▁▁"
	if got != want {
		t.Errorf("Sparkline(all-same) = %q, want %q", got, want)
	}
}

func TestSparklineAllZero(t *testing.T) {
	values := []float64{0, 0, 0}
	got := Sparkline(values)
	want := "▁▁▁"
	if got != want {
		t.Errorf("Sparkline(all-zero) = %q, want %q", got, want)
	}
}

func TestSparklineMixed(t *testing.T) {
	// min=1, max=9, span=8
	// 1→▁, 5→▄, 9→█, 1→▁, 5→▄
	values := []float64{1, 5, 9, 1, 5}
	got := Sparkline(values)
	if len([]rune(got)) != 5 {
		t.Errorf("expected 5 characters, got %d: %q", len([]rune(got)), got)
	}
	runes := []rune(got)
	if runes[0] != '▁' {
		t.Errorf("first char = %c, want ▁", runes[0])
	}
	if runes[2] != '█' {
		t.Errorf("third char = %c, want █", runes[2])
	}
	// Symmetry: positions 0==3, 1==4
	if runes[0] != runes[3] {
		t.Errorf("expected symmetry: positions 0 and 3 differ: %c vs %c", runes[0], runes[3])
	}
	if runes[1] != runes[4] {
		t.Errorf("expected symmetry: positions 1 and 4 differ: %c vs %c", runes[1], runes[4])
	}
}

func TestSparklineTwoValues(t *testing.T) {
	got := Sparkline([]float64{0, 100})
	want := "▁█"
	if got != want {
		t.Errorf("Sparkline([0,100]) = %q, want %q", got, want)
	}
}

package tui

import "strings"

// sparkBlocks are the Unicode block characters used for sparklines, ordered from lowest to highest.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders a slice of float64 values as a Unicode sparkline string.
// Empty input returns an empty string. All-equal values render as the lowest bar.
func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}

	minVal, maxVal := values[0], values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	span := maxVal - minVal

	var buf strings.Builder
	buf.Grow(len(values) * 3) // UTF-8 block chars are 3 bytes
	for _, v := range values {
		idx := 0
		if span > 0 {
			idx = int((v - minVal) / span * float64(len(sparkBlocks)-1))
			if idx >= len(sparkBlocks) {
				idx = len(sparkBlocks) - 1
			}
		}
		buf.WriteRune(sparkBlocks[idx])
	}
	return buf.String()
}

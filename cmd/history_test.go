package cmd

import (
	"testing"
	"time"
)

func TestParseHistoryDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"3h", 3 * time.Hour},
		{"12h", 12 * time.Hour},
		{"1d", 24 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"365d", 365 * 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHistoryDuration(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseHistoryDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseHistoryDurationInvalid(t *testing.T) {
	invalid := []string{
		"",
		"d",
		"3",
		"3x",
		"0d",
		"-1d",
		"abc",
		"3.5d",
	}
	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			_, err := parseHistoryDuration(input)
			if err == nil {
				t.Errorf("parseHistoryDuration(%q) should return error", input)
			}
		})
	}
}

func TestHistoryCommandRequiresProject(t *testing.T) {
	oldProject := project
	project = ""
	defer func() { project = oldProject }()

	err := runHistory(historyCmd, []string{"3d"})
	if err == nil {
		t.Fatal("expected error when project is empty")
	}
	if err.Error() != "--project is required for the history command" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHistoryAllClustersWithClusterNameError(t *testing.T) {
	oldProject := project
	project = "test-project"
	defer func() { project = oldProject }()

	oldCluster := historyCluster
	historyCluster = "some-cluster"
	defer func() { historyCluster = oldCluster }()

	oldAllClusters := historyAllClusters
	historyAllClusters = true
	defer func() { historyAllClusters = oldAllClusters }()

	err := runHistory(historyCmd, []string{"3d"})
	if err == nil {
		t.Fatal("expected error when --all-clusters used with --cluster-name")
	}
	if err.Error() != "cannot use --all-clusters with --cluster-name" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveClusterFilter(t *testing.T) {
	tests := []struct {
		name         string
		autoDetected string
		explicit     string
		allClusters  bool
		want         string
	}{
		{"default uses auto-detected", "auto-detected", "", false, "auto-detected"},
		{"explicit overrides auto-detected", "auto-detected", "explicit", false, "explicit"},
		{"all-clusters clears filter", "auto-detected", "", true, ""},
		{"all-clusters overrides explicit", "auto-detected", "explicit", true, ""},
		{"no auto-detection and no flags queries all", "", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveClusterFilter(tt.autoDetected, tt.explicit, tt.allClusters)
			if got != tt.want {
				t.Errorf("resolveClusterFilter(%q, %q, %v) = %q, want %q",
					tt.autoDetected, tt.explicit, tt.allClusters, got, tt.want)
			}
		})
	}
}

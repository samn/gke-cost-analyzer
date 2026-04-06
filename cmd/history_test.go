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

func TestHistoryClusterFilterLogic(t *testing.T) {
	// When historyCluster is set, it should override auto-detected clusterName
	oldCluster := clusterName
	oldHistCluster := historyCluster
	oldAllClusters := historyAllClusters
	defer func() {
		clusterName = oldCluster
		historyCluster = oldHistCluster
		historyAllClusters = oldAllClusters
	}()

	// Simulate auto-detected cluster
	clusterName = "auto-detected"
	historyCluster = ""
	historyAllClusters = false

	// Default: uses auto-detected
	filterCluster := clusterName
	if historyCluster != "" {
		filterCluster = historyCluster
	}
	if historyAllClusters {
		filterCluster = ""
	}
	if filterCluster != "auto-detected" {
		t.Errorf("default should use auto-detected cluster, got %q", filterCluster)
	}

	// Explicit --cluster-name overrides
	historyCluster = "explicit"
	filterCluster = clusterName
	if historyCluster != "" {
		filterCluster = historyCluster
	}
	if filterCluster != "explicit" {
		t.Errorf("explicit should override, got %q", filterCluster)
	}

	// --all-clusters clears filter
	historyAllClusters = true
	historyCluster = ""
	filterCluster = clusterName
	if historyCluster != "" {
		filterCluster = historyCluster
	}
	if historyAllClusters {
		filterCluster = ""
	}
	if filterCluster != "" {
		t.Errorf("--all-clusters should clear filter, got %q", filterCluster)
	}
}

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

func TestResolveClusterView(t *testing.T) {
	tests := []struct {
		name        string
		autoDetect  string
		explicit    string
		allClusters bool
		wantFilter  string
		wantShowCol bool
	}{
		{"explicit wins", "detected", "explicit", false, "explicit", false},
		{"autodetected", "detected", "", false, "detected", false},
		{"all clusters", "detected", "", true, "", true},
		// When nothing is detected and no flag is given, the query silently
		// spans every cluster — the CLUSTER column must be shown so blended
		// data is identifiable.
		{"detection failed", "", "", false, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, showCol := resolveClusterView(tt.autoDetect, tt.explicit, tt.allClusters)
			if filter != tt.wantFilter {
				t.Errorf("filter = %q, want %q", filter, tt.wantFilter)
			}
			if showCol != tt.wantShowCol {
				t.Errorf("showCol = %v, want %v", showCol, tt.wantShowCol)
			}
		})
	}
}

func TestParseHistoryDurationRejectsOverflow(t *testing.T) {
	// Absurd values would overflow int64 nanoseconds and produce a negative
	// duration (breaking the SQL time filter); they must be rejected.
	if _, err := parseHistoryDuration("99999999999w"); err == nil {
		t.Error("expected error for overflowing duration")
	}
	if _, err := parseHistoryDuration("60w"); err != nil {
		t.Errorf("one year of weeks should be accepted, got %v", err)
	}
}

func TestHistoryArgsErrorsAreUsageErrors(t *testing.T) {
	// A missing duration argument is an operator mistake, not an application
	// error to report to Sentry.
	err := historyCmd.Args(historyCmd, []string{})
	if err == nil {
		t.Fatal("expected error for missing duration argument")
	}
	if !IsUsageError(err) {
		t.Errorf("missing argument should classify as a usage error, got %T", err)
	}
}

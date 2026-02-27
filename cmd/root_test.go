package cmd

import "testing"

func TestExecute(t *testing.T) {
	// Verify the root command can be executed without error
	// when called with no subcommands (prints help)
	rootCmd.SetArgs([]string{})
	if err := Execute(); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
}

func TestLabelConfig(t *testing.T) {
	saved := teamLabel
	savedWL := workloadLabel
	savedST := subtypeLabel
	defer func() {
		teamLabel = saved
		workloadLabel = savedWL
		subtypeLabel = savedST
	}()

	teamLabel = "my-team"
	workloadLabel = "my-app"
	subtypeLabel = "my-sub"

	lc := labelConfig()
	if lc.TeamLabel != "my-team" {
		t.Errorf("team label = %s, want my-team", lc.TeamLabel)
	}
	if lc.WorkloadLabel != "my-app" {
		t.Errorf("workload label = %s, want my-app", lc.WorkloadLabel)
	}
	if lc.SubtypeLabel != "my-sub" {
		t.Errorf("subtype label = %s, want my-sub", lc.SubtypeLabel)
	}
}

func TestLabelConfigDefaults(t *testing.T) {
	saved := teamLabel
	savedWL := workloadLabel
	savedST := subtypeLabel
	defer func() {
		teamLabel = saved
		workloadLabel = savedWL
		subtypeLabel = savedST
	}()

	// Simulate defaults from root.go init()
	teamLabel = "team"
	workloadLabel = "app"
	subtypeLabel = ""

	lc := labelConfig()
	if lc.TeamLabel != "team" {
		t.Errorf("team label = %s, want team", lc.TeamLabel)
	}
	if lc.WorkloadLabel != "app" {
		t.Errorf("workload label = %s, want app", lc.WorkloadLabel)
	}
	if lc.SubtypeLabel != "" {
		t.Errorf("subtype label = %s, want empty", lc.SubtypeLabel)
	}
}

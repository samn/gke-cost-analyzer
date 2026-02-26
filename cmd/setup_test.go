package cmd

import "testing"

func TestSetupRequiresProject(t *testing.T) {
	// Reset flags
	setupProject = ""

	rootCmd.SetArgs([]string{"setup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --project is missing")
	}
}

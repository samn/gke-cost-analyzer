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

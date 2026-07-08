package cmd

import (
	"strings"
	"testing"
)

func TestSetupRequiresProject(t *testing.T) {
	defer resetProjectState()()

	err := runSetup(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when no BigQuery project is available")
	}
	if !IsUsageError(err) || !strings.Contains(err.Error(), "no BigQuery project") {
		t.Errorf("error should mention the missing BigQuery project, got: %v", err)
	}
}

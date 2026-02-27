package cmd

import (
	"strings"
	"testing"
)

func TestSetupRequiresProject(t *testing.T) {
	saved := project
	defer func() { project = saved }()
	project = ""

	err := runSetup(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --project is missing")
	}
	if !strings.Contains(err.Error(), "--project") {
		t.Errorf("error should mention --project, got: %v", err)
	}
}

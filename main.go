// Package main is the entry point for the autopilot-cost-analyzer CLI.
package main

import (
	"fmt"
	"os"

	"github.com/samn/autopilot-cost-analyzer/cmd"
	appSentry "github.com/samn/autopilot-cost-analyzer/internal/sentry"
)

func main() {
	cleanup, err := appSentry.Init(cmd.Version())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize Sentry: %v\n", err)
	}
	defer cleanup()
	defer appSentry.RecoverAndCapture()

	if err := cmd.Execute(); err != nil {
		appSentry.CaptureError(err)
		os.Exit(1)
	}
}

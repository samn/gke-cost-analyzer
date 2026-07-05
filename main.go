// Package main is the entry point for the gke-cost-analyzer CLI.
package main

import (
	"fmt"
	"os"

	"github.com/samn/gke-cost-analyzer/cmd"
	appSentry "github.com/samn/gke-cost-analyzer/internal/sentry"
)

func main() {
	os.Exit(run())
}

func run() int {
	cleanup, err := appSentry.Init(cmd.Version())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize Sentry: %v\n", err)
	}
	defer cleanup()
	defer appSentry.RecoverAndCapture()

	if err := cmd.Execute(); err != nil {
		// Operator input mistakes (missing flags, bad arguments) are not
		// application errors — don't report them to Sentry.
		if !cmd.IsUsageError(err) {
			appSentry.CaptureError(err)
		}
		return 1
	}
	return 0
}

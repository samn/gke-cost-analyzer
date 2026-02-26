// Package main is the entry point for the autopilot-cost-analyzer CLI.
package main

import (
	"os"

	"github.com/samn/autopilot-cost-analyzer/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

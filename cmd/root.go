// Package cmd contains the CLI command definitions.
package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "autopilot-cost-analyzer",
	Short: "Analyze costs of GKE Autopilot workloads",
	Long:  "A CLI tool to monitor and analyze costs of GKE Autopilot workloads, with support for BigQuery export.",
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

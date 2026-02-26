// Package cmd contains the CLI command definitions.
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	teamLabel     string
	workloadLabel string
	subtypeLabel  string
	namespace     string
	region        string
)

var rootCmd = &cobra.Command{
	Use:   "autopilot-cost-analyzer",
	Short: "Analyze costs of GKE Autopilot workloads",
	Long:  "A CLI tool to monitor and analyze costs of GKE Autopilot workloads, with support for BigQuery export.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&teamLabel, "team-label", "team", "Pod label to use for team grouping")
	rootCmd.PersistentFlags().StringVar(&workloadLabel, "workload-label", "app", "Pod label to use for workload grouping")
	rootCmd.PersistentFlags().StringVar(&subtypeLabel, "subtype-label", "", "Pod label to use for subtype grouping (optional)")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Kubernetes namespace to filter (empty = all)")
	rootCmd.PersistentFlags().StringVar(&region, "region", "", "GCP region for pricing (required)")
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

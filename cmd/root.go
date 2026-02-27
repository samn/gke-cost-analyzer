// Package cmd contains the CLI command definitions.
package cmd

import (
	"fmt"

	"github.com/samn/autopilot-cost-analyzer/internal/envdefaults"
	"github.com/spf13/cobra"
)

var (
	teamLabel     string
	workloadLabel string
	subtypeLabel  string
	namespace     string
	region        string
)

// newDetector is overridable for testing.
var newDetector = func() *envdefaults.Detector {
	return envdefaults.NewDetector()
}

var rootCmd = &cobra.Command{
	Use:   "autopilot-cost-analyzer",
	Short: "Analyze costs of GKE Autopilot workloads",
	Long:  "A CLI tool to monitor and analyze costs of GKE Autopilot workloads, with support for BigQuery export.",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		d := newDetector()
		applyDefaults(d, cmd)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&teamLabel, "team-label", "team", "Pod label to use for team grouping")
	rootCmd.PersistentFlags().StringVar(&workloadLabel, "workload-label", "app", "Pod label to use for workload grouping")
	rootCmd.PersistentFlags().StringVar(&subtypeLabel, "subtype-label", "", "Pod label to use for subtype grouping (optional)")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Kubernetes namespace to filter (empty = all)")
	rootCmd.PersistentFlags().StringVar(&region, "region", "", "GCP region for pricing (auto-detected from environment)")
}

// applyDefaults fills in missing flag values from environment detection.
// Explicit CLI flags (non-empty values) are never overwritten.
func applyDefaults(d *envdefaults.Detector, cmd *cobra.Command) {
	defaults := d.Detect()

	var applied []string

	if region == "" && defaults.Region != "" {
		region = defaults.Region
		applied = append(applied, fmt.Sprintf("region=%s", defaults.Region))
	}
	if bqProject == "" && defaults.ProjectID != "" {
		bqProject = defaults.ProjectID
		applied = append(applied, fmt.Sprintf("project=%s", defaults.ProjectID))
	}
	if setupProject == "" && defaults.ProjectID != "" {
		setupProject = defaults.ProjectID
	}
	if clusterName == "" && defaults.ClusterName != "" {
		clusterName = defaults.ClusterName
		applied = append(applied, fmt.Sprintf("cluster-name=%s", defaults.ClusterName))
	}

	if len(applied) > 0 {
		fmt.Printf("Detected defaults: %s\n", joinStrings(applied))
	}
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

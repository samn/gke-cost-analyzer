// Package cmd contains the CLI command definitions.
package cmd

import (
	"fmt"

	"github.com/samn/gke-cost-analyzer/internal/envdefaults"
	"github.com/spf13/cobra"
)

var (
	teamLabel           string
	workloadLabel       string
	subtypeLabel        string
	namespace           string
	region              string
	bigqueryProjectID   string
	prometheusProjectID string
	excludeNamespaces   []string
	prometheusURL       string
	mode                string

	// detectedProject is the GCP project inferred from the environment (GCE
	// metadata or kubeconfig). It is the default project for both BigQuery and
	// Prometheus when the use-specific flags are not set.
	detectedProject string
)

// newDetector is overridable for testing.
var newDetector = func() *envdefaults.Detector {
	return envdefaults.NewDetector()
}

var rootCmd = &cobra.Command{
	Use:   "gke-cost-analyzer",
	Short: "Analyze costs of GKE workloads",
	Long:  "A CLI tool to monitor and analyze costs of GKE workloads (Autopilot and standard), with support for real-time display and BigQuery export.",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// version needs no cluster/project context; skip detection so it
		// never blocks on metadata-server timeouts off-GCP.
		if cmd.Name() != "version" {
			d := newDetector()
			applyDefaults(d, cmd)
		}
		if err := validateMode(); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&teamLabel, "team-label", "team", "Pod label to use for team grouping")
	rootCmd.PersistentFlags().StringVar(&workloadLabel, "workload-label", "app", "Pod label to use for workload grouping")
	rootCmd.PersistentFlags().StringVar(&subtypeLabel, "subtype-label", "", "Pod label to use for subtype grouping (optional)")
	rootCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Kubernetes namespace to filter (empty = all)")
	rootCmd.PersistentFlags().StringVar(&region, "region", "", "GCP region for pricing (auto-detected from environment)")
	rootCmd.PersistentFlags().StringSliceVar(&excludeNamespaces, "exclude-namespaces", []string{"kube-system", "gmp-system"}, "Namespaces to exclude from pod listing (comma-separated)")
	rootCmd.PersistentFlags().StringVar(&prometheusURL, "prometheus-url", "", "Prometheus API base URL (defaults to GCP Managed Prometheus when a project is available)")
	rootCmd.PersistentFlags().StringVar(&mode, "mode", "all", "Cost calculation mode: autopilot, standard, or all")
}

// applyDefaults fills in missing flag values from environment detection and
// records the detected project (for BigQuery/Prometheus defaults and, in
// record, the project_id attribution). Explicit CLI flags (non-empty values)
// are never overwritten.
//
// Detection always runs (except for version, which is skipped by the caller):
// the detected project is a distinct value with no backing flag, and record
// needs it for accurate per-cluster project_id attribution even when every
// flag is set. The three metadata lookups run concurrently, so the cost is a
// single ~1s timeout off-GCP.
func applyDefaults(d *envdefaults.Detector, cmd *cobra.Command) {
	defaults := d.Detect()
	detectedProject = defaults.ProjectID

	var applied []string

	if region == "" && defaults.Region != "" {
		region = defaults.Region
		applied = append(applied, fmt.Sprintf("region=%s", defaults.Region))
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

func validateMode() error {
	switch mode {
	case "autopilot", "standard", "all":
		return nil
	default:
		return usageErrorf("--mode must be one of: autopilot, standard, all (got %q)", mode)
	}
}

// Execute runs the root command.
func Execute() error {
	// Flag-parse failures are operator input mistakes, not application errors.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err}
	})
	return rootCmd.Execute()
}

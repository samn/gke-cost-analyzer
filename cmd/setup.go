package cmd

import (
	"fmt"

	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
	"github.com/spf13/cobra"
)

var (
	setupProject  string
	setupDataset  string
	setupTable    string
	setupLocation string
)

func init() {
	setupCmd.Flags().StringVar(&setupProject, "project", "", "GCP project ID (auto-detected from environment)")
	setupCmd.Flags().StringVar(&setupDataset, "dataset", "autopilot_costs", "BigQuery dataset name")
	setupCmd.Flags().StringVar(&setupTable, "table", "cost_snapshots", "BigQuery table name")
	setupCmd.Flags().StringVar(&setupLocation, "location", "US", "BigQuery dataset location")
	rootCmd.AddCommand(setupCmd)
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create BigQuery dataset and table for cost snapshots",
	Long:  "Create the BigQuery dataset and table (if they don't exist) needed by the record command.",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, _ []string) error {
	if setupProject == "" {
		return fmt.Errorf("--project is required")
	}

	ctx := cmd.Context()

	httpClient, err := authHTTPClient(ctx)
	if err != nil {
		return fmt.Errorf("creating authenticated client: %w", err)
	}

	sc := bigquery.NewSetupClient(setupProject,
		bigquery.WithSetupHTTPClient(httpClient))

	fmt.Printf("Creating dataset %s.%s (location: %s)...\n", setupProject, setupDataset, setupLocation)
	if err := sc.EnsureDataset(ctx, setupDataset, setupLocation); err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	}
	fmt.Println("Dataset ready.")

	fmt.Printf("Creating table %s.%s.%s...\n", setupProject, setupDataset, setupTable)
	if err := sc.EnsureTable(ctx, setupDataset, setupTable); err != nil {
		return fmt.Errorf("creating table: %w", err)
	}
	fmt.Println("Table ready.")

	fmt.Println("\nSetup complete! You can now run:")
	fmt.Printf("  autopilot-cost-analyzer record --project %s --region <REGION> --cluster-name <CLUSTER>\n", setupProject)
	return nil
}

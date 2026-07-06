package cmd

import (
	"fmt"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
	"github.com/spf13/cobra"
)

var (
	setupDataset  string
	setupTable    string
	setupLocation string
)

func init() {
	setupCmd.Flags().StringVar(&setupDataset, "dataset", "gke_costs", "BigQuery dataset name")
	setupCmd.Flags().StringVar(&setupTable, "table", "cost_snapshots", "BigQuery table name")
	setupCmd.Flags().StringVar(&setupLocation, "location", "US", "BigQuery dataset location")
	rootCmd.AddCommand(setupCmd)
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create or migrate the BigQuery dataset and table for cost snapshots",
	Long:  "Create the BigQuery dataset and table needed by the record command. If the table already exists, its schema is migrated: columns added in newer versions (all NULLABLE) are patched in.",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, _ []string) error {
	if project == "" {
		return usageErrorf("--project is required")
	}

	ctx := cmd.Context()

	httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/bigquery")
	if err != nil {
		return fmt.Errorf("creating authenticated client: %w", err)
	}

	sc := bigquery.NewSetupClient(project,
		bigquery.WithSetupHTTPClient(httpClient))

	fmt.Printf("Creating dataset %s.%s (location: %s)...\n", project, setupDataset, setupLocation)
	if err := sc.EnsureDataset(ctx, setupDataset, setupLocation); err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	}
	fmt.Println("Dataset ready.")

	fmt.Printf("Creating table %s.%s.%s...\n", project, setupDataset, setupTable)
	if err := sc.EnsureTable(ctx, setupDataset, setupTable); err != nil {
		return fmt.Errorf("creating table: %w", err)
	}
	fmt.Println("Table ready.")

	fmt.Println("\nSetup complete! You can now run:")
	fmt.Printf("  gke-cost-analyzer record --project %s --region <REGION> --cluster-name <CLUSTER>\n", project)
	return nil
}

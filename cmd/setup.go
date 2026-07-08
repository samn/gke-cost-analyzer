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
	setupCmd.Flags().StringVar(&bigqueryProjectID, "bigquery-project-id", "", "GCP project ID owning the BigQuery dataset (defaults to the auto-detected environment project; overridden by a fully-qualified --table)")
	setupCmd.Flags().StringVar(&setupDataset, "dataset", "gke_costs", "BigQuery dataset name")
	setupCmd.Flags().StringVar(&setupTable, "table", "cost_snapshots", "BigQuery table name (accepts dataset.table or project.dataset.table)")
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
	bqProject, dataset, table, err := parseTableRef(setupTable, bigQueryProject(), setupDataset)
	if err != nil {
		return err
	}
	if bqProject == "" {
		return usageErrorf("no BigQuery project: set --bigquery-project-id, use a fully-qualified --table (project.dataset.table), or run where the project is auto-detected")
	}

	ctx := cmd.Context()

	httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/bigquery")
	if err != nil {
		return fmt.Errorf("creating authenticated client: %w", err)
	}

	sc := bigquery.NewSetupClient(bqProject,
		bigquery.WithSetupHTTPClient(httpClient))

	fmt.Printf("Creating dataset %s.%s (location: %s)...\n", bqProject, dataset, setupLocation)
	if err := sc.EnsureDataset(ctx, dataset, setupLocation); err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	}
	fmt.Println("Dataset ready.")

	fmt.Printf("Creating table %s.%s.%s...\n", bqProject, dataset, table)
	if err := sc.EnsureTable(ctx, dataset, table); err != nil {
		return fmt.Errorf("creating table: %w", err)
	}
	fmt.Println("Table ready.")

	fmt.Println("\nSetup complete! You can now run:")
	fmt.Printf("  gke-cost-analyzer record --bigquery-project-id %s --region <REGION> --cluster-name <CLUSTER>\n", bqProject)
	return nil
}

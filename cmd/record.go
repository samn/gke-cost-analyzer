package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	recordInterval time.Duration
	bqProject      string
	bqDataset      string
	bqTable        string
	clusterName    string
)

func init() {
	recordCmd.Flags().DurationVar(&recordInterval, "interval", 5*time.Minute, "Snapshot interval")
	recordCmd.Flags().StringVar(&bqProject, "project", "", "GCP project ID for BigQuery (required)")
	recordCmd.Flags().StringVar(&bqDataset, "dataset", "autopilot_costs", "BigQuery dataset name")
	recordCmd.Flags().StringVar(&bqTable, "table", "cost_snapshots", "BigQuery table name")
	recordCmd.Flags().StringVar(&clusterName, "cluster-name", "", "GKE cluster name (required)")
	rootCmd.AddCommand(recordCmd)
}

var recordCmd = &cobra.Command{
	Use:   "record",
	Short: "Record GKE Autopilot workload costs to BigQuery",
	Long:  "Run as a daemon, periodically snapshot pod costs and write aggregated records to BigQuery.",
	RunE:  runRecord,
}

func runRecord(cmd *cobra.Command, _ []string) error {
	if region == "" {
		return fmt.Errorf("--region is required")
	}
	if bqProject == "" {
		return fmt.Errorf("--project is required")
	}
	if clusterName == "" {
		return fmt.Errorf("--cluster-name is required")
	}
	if recordInterval <= 0 {
		return fmt.Errorf("--interval must be positive")
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	fmt.Println("Loading prices...")
	prices, err := loadPrices(ctx)
	if err != nil {
		return err
	}
	pt := pricing.FromPrices(prices)

	fmt.Println("Connecting to Kubernetes cluster...")
	lister, err := newPodLister()
	if err != nil {
		return fmt.Errorf("connecting to cluster: %w", err)
	}

	// Create authenticated HTTP client for BigQuery
	httpClient, err := authHTTPClient(ctx)
	if err != nil {
		return fmt.Errorf("creating authenticated client: %w", err)
	}

	writer := bigquery.NewWriter(bqProject, bqDataset, bqTable,
		bigquery.WithWriterHTTPClient(httpClient))

	calc := cost.NewCalculator(region, pt, nil)
	lc := labelConfig()
	intervalSecs := int64(recordInterval.Seconds())
	sc := snapshotConfig{
		projectID:   bqProject,
		region:      region,
		clusterName: clusterName,
	}

	fmt.Printf("Recording costs every %s to %s.%s.%s\n",
		recordInterval, bqProject, bqDataset, bqTable)

	ticker := time.NewTicker(recordInterval)
	defer ticker.Stop()

	// Run once immediately
	if err := recordSnapshot(ctx, lister, calc, lc, writer, sc, intervalSecs); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nStopped.")
			return nil
		case <-ticker.C:
			if err := recordSnapshot(ctx, lister, calc, lc, writer, sc, intervalSecs); err != nil {
				fmt.Fprintf(os.Stderr, "Error recording snapshot: %v\n", err)
			}
		}
	}
}

// snapshotConfig holds the metadata needed to convert aggregated costs to BigQuery snapshots.
type snapshotConfig struct {
	projectID   string
	region      string
	clusterName string
}

func recordSnapshot(ctx context.Context, lister podLister, calc *cost.Calculator, lc cost.LabelConfig, writer *bigquery.Writer, sc snapshotConfig, intervalSecs int64) error {
	pods, err := lister.ListPods(ctx)
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	costs := calc.CalculateAll(pods)
	aggs := cost.Aggregate(costs, lc)
	now := time.Now()

	snapshots := make([]bigquery.CostSnapshot, len(aggs))
	for i, a := range aggs {
		snapshots[i] = aggregatedToSnapshot(a, now, sc, intervalSecs)
	}

	if err := writer.Write(ctx, snapshots); err != nil {
		return fmt.Errorf("writing to BigQuery: %w", err)
	}

	fmt.Printf("[%s] Wrote %d records (%d pods)\n",
		now.Format("15:04:05"), len(snapshots), len(pods))
	return nil
}

func aggregatedToSnapshot(a cost.AggregatedCost, ts time.Time, sc snapshotConfig, intervalSecs int64) bigquery.CostSnapshot {
	return bigquery.CostSnapshot{
		Timestamp:       ts,
		ProjectID:       sc.projectID,
		Region:          sc.region,
		ClusterName:     sc.clusterName,
		Namespace:       a.Namespace,
		Team:            a.Key.Team,
		Workload:        a.Key.Workload,
		Subtype:         a.Key.Subtype,
		PodCount:        a.PodCount,
		CPURequestVCPU:  a.TotalCPUVCPU,
		MemoryRequestGB: a.TotalMemGB,
		CPUCost:         a.CPUCost,
		MemoryCost:      a.MemCost,
		TotalCost:       a.TotalCost,
		IsSpot:          a.Key.IsSpot,
		IntervalSeconds: intervalSecs,
	}
}

func authHTTPClient(ctx context.Context) (*http.Client, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/bigquery")
	if err != nil {
		return nil, fmt.Errorf("getting default credentials: %w", err)
	}
	return oauth2.NewClient(ctx, ts), nil
}

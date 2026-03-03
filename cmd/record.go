package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/bigquery"
	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	pqwriter "github.com/samn/autopilot-cost-analyzer/internal/parquet"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/samn/autopilot-cost-analyzer/internal/prometheus"
	"github.com/spf13/cobra"
)

var (
	recordInterval time.Duration
	bqDataset      string
	bqTable        string
	clusterName    string
	dryRun         bool
	outputFile     string
)

func init() {
	recordCmd.Flags().DurationVar(&recordInterval, "interval", 5*time.Minute, "Snapshot interval")
	recordCmd.Flags().StringVar(&bqDataset, "dataset", "autopilot_costs", "BigQuery dataset name")
	recordCmd.Flags().StringVar(&bqTable, "table", "cost_snapshots", "BigQuery table name")
	recordCmd.Flags().StringVar(&clusterName, "cluster-name", "", "GKE cluster name (auto-detected from environment)")
	recordCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log rows that would be written without writing to BigQuery")
	recordCmd.Flags().StringVar(&outputFile, "output-file", "", "Append dry-run snapshots to a local Parquet file (requires --dry-run)")
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
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	if clusterName == "" {
		return fmt.Errorf("--cluster-name is required")
	}
	if recordInterval <= 0 {
		return fmt.Errorf("--interval must be positive")
	}
	if outputFile != "" && !dryRun {
		return fmt.Errorf("--output-file requires --dry-run")
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

	promClient, err := newPromClient(ctx)
	if err != nil {
		return err
	}

	var writer *bigquery.Writer
	if dryRun {
		if outputFile != "" {
			fmt.Printf("Dry-run mode: appending rows to %s\n", outputFile)
		} else {
			fmt.Println("Dry-run mode: rows will be logged to stdout, not written to BigQuery")
		}
	} else {
		// Create authenticated HTTP client for BigQuery
		httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/bigquery")
		if err != nil {
			return fmt.Errorf("creating authenticated client: %w", err)
		}
		writer = bigquery.NewWriter(project, bqDataset, bqTable,
			bigquery.WithWriterHTTPClient(httpClient))
		fmt.Printf("Recording costs every %s to %s.%s.%s\n",
			recordInterval, project, bqDataset, bqTable)
	}

	calc := cost.NewCalculator(region, pt, nil)
	lc := labelConfig()
	intervalSecs := int64(recordInterval.Seconds())
	sc := snapshotConfig{
		projectID:   project,
		region:      region,
		clusterName: clusterName,
	}

	ticker := time.NewTicker(recordInterval)
	defer ticker.Stop()

	// Run once immediately
	if err := recordSnapshot(ctx, lister, calc, lc, writer, promClient, sc, intervalSecs, outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nStopped.")
			return nil
		case <-ticker.C:
			if err := recordSnapshot(ctx, lister, calc, lc, writer, promClient, sc, intervalSecs, outputFile); err != nil {
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

func recordSnapshot(ctx context.Context, lister podLister, calc *cost.Calculator, lc cost.LabelConfig, writer *bigquery.Writer, promClient *prometheus.Client, sc snapshotConfig, intervalSecs int64, parquetFile string) error {
	// Capture timestamp before listing pods so it reflects the start of the
	// snapshot window, not the end of processing.
	now := time.Now()

	pods, err := lister.ListPods(ctx)
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	// Fetch utilization data from Prometheus if configured.
	var usage map[prometheus.PodKey]prometheus.PodUsage
	if promClient != nil {
		usage, err = promClient.FetchUsage(ctx)
		if err != nil {
			// Log warning but continue without utilization data.
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch utilization metrics: %v\n", err)
		}
	}

	costs := calc.CalculateAll(pods)
	aggs := cost.AggregateWithUtilization(costs, lc, usage)

	snapshots := make([]bigquery.CostSnapshot, len(aggs))
	for i, a := range aggs {
		snapshots[i] = aggregatedToSnapshot(a, now, sc, intervalSecs)
	}

	if writer == nil {
		if parquetFile != "" {
			if err := pqwriter.AppendToFile(parquetFile, snapshots); err != nil {
				return fmt.Errorf("writing to parquet: %w", err)
			}
			fmt.Printf("[%s] Appended %d records (%d pods) to %s\n",
				now.Format("15:04:05"), len(snapshots), len(pods), parquetFile)
		} else {
			for _, s := range snapshots {
				data, err := json.Marshal(s)
				if err != nil {
					return fmt.Errorf("marshaling snapshot: %w", err)
				}
				fmt.Println(string(data))
			}
			fmt.Printf("[%s] Would write %d records (%d pods)\n",
				now.Format("15:04:05"), len(snapshots), len(pods))
		}
		return nil
	}

	if err := writer.Write(ctx, snapshots); err != nil {
		return fmt.Errorf("writing to BigQuery: %w", err)
	}

	fmt.Printf("[%s] Wrote %d records (%d pods)\n",
		now.Format("15:04:05"), len(snapshots), len(pods))
	return nil
}

func aggregatedToSnapshot(a cost.AggregatedCost, ts time.Time, sc snapshotConfig, intervalSecs int64) bigquery.CostSnapshot {
	// Compute cost for this interval window only, not the cumulative lifetime
	// cost. Using the per-hour rate × interval hours ensures that
	// SUM(total_cost) over a day equals the actual daily cost.
	intervalHours := float64(intervalSecs) / 3600.0
	snap := bigquery.CostSnapshot{
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
		CPUCost:         a.CPUCostPerHour * intervalHours,
		MemoryCost:      a.MemCostPerHour * intervalHours,
		TotalCost:       a.CostPerHour * intervalHours,
		IsSpot:          a.Key.IsSpot,
		IntervalSeconds: intervalSecs,
	}
	if a.HasUtilization {
		snap.CPUUtilization = &a.CPUUtilization
		snap.MemoryUtilization = &a.MemUtilization
		snap.EfficiencyScore = &a.EfficiencyScore
		wastedCost := a.WastedCostPerHour * intervalHours
		snap.WastedCost = &wastedCost
	}
	return snap
}

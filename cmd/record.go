package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
	"github.com/samn/gke-cost-analyzer/internal/cost"
	"github.com/samn/gke-cost-analyzer/internal/kube"
	pqwriter "github.com/samn/gke-cost-analyzer/internal/parquet"
	"github.com/samn/gke-cost-analyzer/internal/pricing"
	"github.com/samn/gke-cost-analyzer/internal/prometheus"
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
	recordCmd.Flags().StringVar(&bqDataset, "dataset", "gke_costs", "BigQuery dataset name")
	recordCmd.Flags().StringVar(&bqTable, "table", "cost_snapshots", "BigQuery table name")
	recordCmd.Flags().StringVar(&clusterName, "cluster-name", "", "GKE cluster name (auto-detected from environment)")
	recordCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log rows that would be written without writing to BigQuery")
	recordCmd.Flags().StringVar(&outputFile, "output-file", "", "Append dry-run snapshots to a local Parquet file (requires --dry-run)")
	rootCmd.AddCommand(recordCmd)
}

var recordCmd = &cobra.Command{
	Use:   "record",
	Short: "Record GKE workload costs to BigQuery",
	Long:  "Run as a daemon, periodically snapshot pod costs and write aggregated records to BigQuery.",
	RunE:  runRecord,
}

func runRecord(cmd *cobra.Command, _ []string) error {
	if region == "" {
		return usageErrorf("--region is required")
	}
	if project == "" {
		return usageErrorf("--project is required")
	}
	if clusterName == "" {
		return usageErrorf("--cluster-name is required")
	}
	if recordInterval <= 0 {
		return usageErrorf("--interval must be positive")
	}
	if outputFile != "" && !dryRun {
		return usageErrorf("--output-file requires --dry-run")
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), shutdownSignals...)
	defer cancel()

	var autopilotCalc *cost.Calculator
	if needsAutopilot() {
		fmt.Println("Loading Autopilot prices...")
		prices, err := loadPrices(ctx)
		if err != nil {
			return err
		}
		pt := pricing.FromPrices(prices)
		autopilotCalc = cost.NewCalculator(region, pt, nil)
	}

	var standardCalc *cost.StandardCalculator
	var nodeLister *kube.NodeLister
	if needsStandard() {
		fmt.Println("Loading Compute Engine prices...")
		computePrices, err := loadComputePrices(ctx)
		if err != nil {
			return err
		}
		cpt := pricing.FromComputePrices(computePrices)
		standardCalc = cost.NewStandardCalculator(region, cpt, nil)

		nl, err := newNodeLister()
		if err != nil {
			return fmt.Errorf("connecting to cluster for node listing: %w", err)
		}
		nodeLister = nl
	}

	fmt.Println("Connecting to Kubernetes cluster...")
	apiNS, _ := listNamespace()
	lister, err := newPodLister(apiNS)
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

	lc := labelConfig()
	_, postFilterNS := listNamespace()
	sc := snapshotConfig{
		projectID:       project,
		region:          region,
		clusterName:     clusterName,
		filterNamespace: postFilterNS,
	}

	ticker := time.NewTicker(recordInterval)
	defer ticker.Stop()

	// takeSnapshot bounds each snapshot so a hung backend can't wedge the
	// loop, records cost over the actual elapsed time since the last
	// successful snapshot, and refreshes prices when the cache TTL lapses
	// (a long-running daemon should not use launch-time prices forever).
	var lastSnapshot time.Time
	pricesLoadedAt := time.Now()
	takeSnapshot := func() {
		if time.Since(pricesLoadedAt) > pricing.DefaultCacheTTL {
			// Bound the refresh too: the paginated catalog fetch must not
			// stall the loop indefinitely on a degraded billing API.
			refreshCtx, cancelRefresh := context.WithTimeout(ctx, priceRefreshTimeout)
			ap, std, err := refreshPrices(refreshCtx, autopilotCalc, standardCalc)
			cancelRefresh()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: refreshing prices failed, keeping previous prices: %v\n", err)
			} else {
				autopilotCalc, standardCalc = ap, std
			}
			// Even on failure, wait a full TTL before retrying to avoid
			// hammering the billing API every tick.
			pricesLoadedAt = time.Now()
		}

		now := time.Now()
		intervalSecs := snapshotIntervalSecs(lastSnapshot, now, recordInterval)
		snapCtx, cancelSnap := context.WithTimeout(ctx, snapshotTimeout(recordInterval))
		err := recordSnapshot(snapCtx, lister, autopilotCalc, standardCalc, nodeLister, lc, writer, promClient, sc, intervalSecs, outputFile)
		cancelSnap()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error recording snapshot: %v\n", err)
			return
		}
		lastSnapshot = now
	}

	// Run once immediately
	takeSnapshot()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nStopped.")
			return nil
		case <-ticker.C:
			takeSnapshot()
		}
	}
}

// snapshotIntervalSecs returns the cost window for the next snapshot: the
// actual elapsed time since the last successful snapshot, so missed or slow
// ticks don't permanently undercount cost. The first snapshot (and any clock
// anomaly) uses the nominal interval.
func snapshotIntervalSecs(lastSnapshot, now time.Time, nominal time.Duration) int64 {
	if lastSnapshot.IsZero() {
		return int64(nominal.Seconds())
	}
	elapsed := now.Sub(lastSnapshot)
	if elapsed <= 0 {
		return int64(nominal.Seconds())
	}
	return int64(elapsed.Seconds())
}

// minSnapshotTimeout floors the per-snapshot deadline: with a short
// --interval, a legitimately long snapshot (large cluster, slow backends)
// must still be able to complete, or the daemon would cancel every attempt
// and record nothing forever.
const minSnapshotTimeout = 2 * time.Minute

// priceRefreshTimeout bounds the daily paginated billing-catalog refetch.
const priceRefreshTimeout = 10 * time.Minute

// snapshotTimeout returns the deadline for one snapshot cycle.
func snapshotTimeout(interval time.Duration) time.Duration {
	return max(interval, minSnapshotTimeout)
}

// refreshPrices reloads pricing (via cache or API) and returns fresh
// calculators for the ones that were active. On error the caller keeps its
// existing calculators.
func refreshPrices(ctx context.Context, autopilotCalc *cost.Calculator, standardCalc *cost.StandardCalculator) (*cost.Calculator, *cost.StandardCalculator, error) {
	if autopilotCalc != nil {
		prices, err := loadPrices(ctx)
		if err != nil {
			return nil, nil, err
		}
		autopilotCalc = cost.NewCalculator(region, pricing.FromPrices(prices), nil)
	}
	if standardCalc != nil {
		computePrices, err := loadComputePrices(ctx)
		if err != nil {
			return nil, nil, err
		}
		standardCalc = cost.NewStandardCalculator(region, pricing.FromComputePrices(computePrices), nil)
	}
	return autopilotCalc, standardCalc, nil
}

// snapshotConfig holds the metadata needed to convert aggregated costs to BigQuery snapshots.
type snapshotConfig struct {
	projectID   string
	region      string
	clusterName string
	// filterNamespace narrows recorded groups to one namespace after cost
	// calculation (so standard-mode share denominators use the full pod set).
	filterNamespace string
}

func recordSnapshot(ctx context.Context, lister podLister, autopilotCalc *cost.Calculator, standardCalc *cost.StandardCalculator, nodeLister *kube.NodeLister, lc cost.LabelConfig, writer *bigquery.Writer, promClient *prometheus.Client, sc snapshotConfig, intervalSecs int64, parquetFile string) error {
	// Capture timestamp before listing pods so it reflects the start of the
	// snapshot window, not the end of processing.
	now := time.Now()

	pods, err := lister.ListPods(ctx)
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	// Refresh nodes for standard calculator
	if nodeLister != nil && standardCalc != nil {
		nodes, err := nodeLister.ListNodes(ctx)
		if err != nil {
			return fmt.Errorf("listing nodes: %w", err)
		}
		standardCalc.SetNodes(nodes)
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

	// Calculate costs — partition pods by type if both calculators are set
	allCosts := cost.PartitionAndCalculate(pods, autopilotCalc, standardCalc)
	allCosts = cost.FilterByNamespace(allCosts, sc.filterNamespace)

	aggs := cost.AggregateWithUtilization(allCosts, lc, usage)

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
				now.Format("15:04:05"), len(snapshots), len(allCosts), parquetFile)
		} else {
			for _, s := range snapshots {
				data, err := json.Marshal(s)
				if err != nil {
					return fmt.Errorf("marshaling snapshot: %w", err)
				}
				fmt.Println(string(data))
			}
			fmt.Printf("[%s] Would write %d records (%d pods)\n",
				now.Format("15:04:05"), len(snapshots), len(allCosts))
		}
		return nil
	}

	if err := writer.Write(ctx, snapshots); err != nil {
		return fmt.Errorf("writing to BigQuery: %w", err)
	}

	fmt.Printf("[%s] Wrote %d records (%d pods)\n",
		now.Format("15:04:05"), len(snapshots), len(allCosts))
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
		Namespace:       a.Key.Namespace,
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
		CostMode:        a.CostMode,
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

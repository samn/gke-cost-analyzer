package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/samn/gke-cost-analyzer/internal/bigquery"
	"github.com/samn/gke-cost-analyzer/internal/tui"
)

var (
	historyDataset     string
	historyTable       string
	historyTeam        string
	historyCluster     string
	historyAllClusters bool
)

func init() {
	historyCmd.Flags().StringVar(&historyDataset, "dataset", "gke_costs", "BigQuery dataset name")
	historyCmd.Flags().StringVar(&historyTable, "table", "cost_snapshots", "BigQuery table name")
	historyCmd.Flags().StringVar(&historyTeam, "team", "", "Filter by team name")
	historyCmd.Flags().StringVar(&historyCluster, "cluster-name", "", "Filter by cluster name (defaults to auto-detected cluster)")
	historyCmd.Flags().BoolVar(&historyAllClusters, "all-clusters", false, "Show costs from all clusters")
	rootCmd.AddCommand(historyCmd)
}

var historyCmd = &cobra.Command{
	Use:   "history <duration>",
	Short: "View historical cost data from BigQuery",
	Long:  "Query BigQuery for historical cost snapshots and display aggregated data with trend sparklines.\n\nDuration format: 3h (hours), 3d (days), 1w (weeks). Maximum 5 years.",
	Args:  usageArgs(cobra.ExactArgs(1)),
	RunE:  runHistory,
}

func runHistory(cmd *cobra.Command, args []string) error {
	if project == "" {
		return usageErrorf("--project is required for the history command")
	}

	if historyAllClusters && historyCluster != "" {
		return usageErrorf("cannot use --all-clusters with --cluster-name")
	}

	duration, err := parseHistoryDuration(args[0])
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), shutdownSignals...)
	defer cancel()

	fmt.Println("Authenticating with BigQuery...")
	httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/bigquery.readonly")
	if err != nil {
		return fmt.Errorf("creating authenticated client: %w", err)
	}

	reader := bigquery.NewReader(project, historyDataset, historyTable,
		bigquery.WithReaderHTTPClient(httpClient))

	bucketSecs := bigquery.BucketSeconds(duration)

	clusterFilter, showClusterCol := resolveClusterView(clusterName, historyCluster, historyAllClusters)
	if clusterFilter == "" && !historyAllClusters {
		fmt.Fprintln(os.Stderr, "Warning: no cluster detected and no --cluster-name given; showing data from all clusters")
	}

	filters := bigquery.QueryFilters{
		ClusterName: clusterFilter,
		Namespace:   namespace,
		Team:        historyTeam,
	}

	lc := labelConfig()
	vis := tui.ColumnVisibility{
		Cluster: showClusterCol,
		Subtype: lc.SubtypeLabel != "",
		Mode:    mode == "all",
	}

	model := tui.NewHistoryModel(ctx, cancel, reader, duration, bucketSecs, filters, vis)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

// resolveClusterView determines the effective cluster filter for the history
// command and whether the CLUSTER column should be displayed. --all-clusters
// clears the filter; --cluster-name overrides auto-detection; otherwise the
// auto-detected cluster is used. Whenever the query spans multiple clusters
// (explicitly or because detection failed) the CLUSTER column is shown so
// blended data is identifiable.
func resolveClusterView(autoDetected, explicit string, allClusters bool) (filter string, showClusterCol bool) {
	if allClusters {
		return "", true
	}
	if explicit != "" {
		return explicit, false
	}
	return autoDetected, autoDetected == ""
}

// maxHistoryDuration caps the queryable time range. Beyond guarding the
// int64-nanosecond overflow (which would produce a negative duration and a
// broken SQL time filter), five years is far past any useful retention.
const maxHistoryDuration = 5 * 365 * 24 * time.Hour

// parseHistoryDuration parses a duration string like "3d", "1w", "12h".
// Supports h (hours), d (days), w (weeks).
func parseHistoryDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, usageErrorf("invalid duration %q: use format like 3h, 3d, or 1w", s)
	}
	numStr := s[:len(s)-1]
	unit := s[len(s)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil || num <= 0 {
		return 0, usageErrorf("invalid duration %q: number must be a positive integer", s)
	}
	var hoursPerUnit int
	switch unit {
	case 'h':
		hoursPerUnit = 1
	case 'd':
		hoursPerUnit = 24
	case 'w':
		hoursPerUnit = 7 * 24
	default:
		return 0, usageErrorf("invalid duration unit %q in %q: use h (hours), d (days), or w (weeks)", string(unit), s)
	}
	// Check before multiplying so huge values can't overflow int64 nanoseconds.
	if num > int(maxHistoryDuration/time.Hour)/hoursPerUnit {
		return 0, usageErrorf("invalid duration %q: maximum is 5 years", s)
	}
	return time.Duration(num) * time.Duration(hoursPerUnit) * time.Hour, nil
}

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
	Long:  "Query BigQuery for historical cost snapshots and display aggregated data with trend sparklines.\n\nDuration format: 3h (hours), 3d (days), 1w (weeks).",
	Args:  cobra.ExactArgs(1),
	RunE:  runHistory,
}

func runHistory(cmd *cobra.Command, args []string) error {
	if project == "" {
		return fmt.Errorf("--project is required for the history command")
	}

	if historyAllClusters && historyCluster != "" {
		return fmt.Errorf("cannot use --all-clusters with --cluster-name")
	}

	duration, err := parseHistoryDuration(args[0])
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	fmt.Println("Authenticating with BigQuery...")
	httpClient, err := gcpHTTPClientFn(ctx, "https://www.googleapis.com/auth/bigquery.readonly")
	if err != nil {
		return fmt.Errorf("creating authenticated client: %w", err)
	}

	reader := bigquery.NewReader(project, historyDataset, historyTable,
		bigquery.WithReaderHTTPClient(httpClient))

	bucketSecs := bigquery.BucketSeconds(duration)

	filters := bigquery.QueryFilters{
		ClusterName: resolveClusterFilter(clusterName, historyCluster, historyAllClusters),
		Namespace:   namespace,
		Team:        historyTeam,
	}

	lc := labelConfig()
	showCluster := historyAllClusters
	showSubtype := lc.SubtypeLabel != ""
	showMode := mode == "all"

	model := tui.NewHistoryModel(ctx, cancel, reader, duration, bucketSecs, filters, showCluster, showSubtype, showMode)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

// resolveClusterFilter determines the effective cluster filter for the history
// command. --all-clusters clears the filter; --cluster-name overrides
// auto-detection; otherwise the auto-detected cluster is used.
func resolveClusterFilter(autoDetected, explicit string, allClusters bool) string {
	if allClusters {
		return ""
	}
	if explicit != "" {
		return explicit
	}
	return autoDetected
}

// parseHistoryDuration parses a duration string like "3d", "1w", "12h".
// Supports h (hours), d (days), w (weeks).
func parseHistoryDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration %q: use format like 3h, 3d, or 1w", s)
	}
	numStr := s[:len(s)-1]
	unit := s[len(s)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil || num <= 0 {
		return 0, fmt.Errorf("invalid duration %q: number must be a positive integer", s)
	}
	switch unit {
	case 'h':
		return time.Duration(num) * time.Hour, nil
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit %q in %q: use h (hours), d (days), or w (weeks)", string(unit), s)
	}
}

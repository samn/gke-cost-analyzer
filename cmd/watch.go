package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/kube"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/spf13/cobra"
)

var watchInterval time.Duration

func init() {
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 10*time.Second, "Refresh interval")
	rootCmd.AddCommand(watchCmd)
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch GKE Autopilot workload costs in real-time",
	Long:  "Periodically fetch pod data, calculate costs, and display an aggregated cost table.",
	RunE:  runWatch,
}

// podLister is an interface for listing pods, enabling testing without a real cluster.
type podLister interface {
	ListPods(ctx context.Context) ([]kube.PodInfo, error)
}

func runWatch(cmd *cobra.Command, _ []string) error {
	if region == "" {
		return fmt.Errorf("--region is required")
	}
	if watchInterval <= 0 {
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

	calc := cost.NewCalculator(region, pt, nil)
	lc := labelConfig()

	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()

	// Run once immediately
	if err := displayCosts(ctx, lister, calc, lc); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nStopped.")
			return nil
		case <-ticker.C:
			if err := displayCosts(ctx, lister, calc, lc); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		}
	}
}

func displayCosts(ctx context.Context, lister podLister, calc *cost.Calculator, lc cost.LabelConfig) error {
	pods, err := lister.ListPods(ctx)
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	costs := calc.CalculateAll(pods)
	aggs := cost.Aggregate(costs, lc)

	// Sort by team, workload, subtype
	sort.Slice(aggs, func(i, j int) bool {
		if aggs[i].Key.Team != aggs[j].Key.Team {
			return aggs[i].Key.Team < aggs[j].Key.Team
		}
		if aggs[i].Key.Workload != aggs[j].Key.Workload {
			return aggs[i].Key.Workload < aggs[j].Key.Workload
		}
		return aggs[i].Key.Subtype < aggs[j].Key.Subtype
	})

	// Clear screen
	fmt.Print("\033[2J\033[H")
	fmt.Printf("Autopilot Cost Analyzer — %s — %d pods\n\n",
		time.Now().Format("15:04:05"), len(pods))

	renderTable(os.Stdout, aggs)
	return nil
}

func renderTable(w io.Writer, aggs []cost.AggregatedCost) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TEAM\tWORKLOAD\tSUBTYPE\tPODS\tCPU REQ\tMEM REQ\t$/HR\tSPOT")
	fmt.Fprintln(tw, strings.Repeat("-", 80))

	var totalCostPerHour float64
	for _, a := range aggs {
		spot := ""
		if a.Key.IsSpot {
			spot = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%.2f\t%.1f GB\t$%.4f\t%s\n",
			orDefault(a.Key.Team, "-"),
			orDefault(a.Key.Workload, "-"),
			orDefault(a.Key.Subtype, "-"),
			a.PodCount,
			a.TotalCPUVCPU,
			a.TotalMemGB,
			a.CostPerHour,
			spot,
		)
		totalCostPerHour += a.CostPerHour
	}

	fmt.Fprintln(tw, strings.Repeat("-", 80))
	fmt.Fprintf(tw, "TOTAL\t\t\t\t\t\t$%.4f\t\n", totalCostPerHour)
	tw.Flush()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

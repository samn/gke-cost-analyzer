package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/samn/autopilot-cost-analyzer/internal/cost"
	"github.com/samn/autopilot-cost-analyzer/internal/pricing"
	"github.com/samn/autopilot-cost-analyzer/internal/tui"
)

var watchInterval time.Duration

func init() {
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 10*time.Second, "Refresh interval")
	rootCmd.AddCommand(watchCmd)
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch GKE workload costs in real-time",
	Long:  "Periodically fetch pod data, calculate costs, and display an aggregated cost table.",
	RunE:  runWatch,
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
	var nodeLister tui.NodeLister
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
	lister, err := newPodLister()
	if err != nil {
		return fmt.Errorf("connecting to cluster: %w", err)
	}

	promClient, err := newPromClient(ctx)
	if err != nil {
		return err
	}

	lc := labelConfig()
	showMode := mode == "all"

	model := tui.NewModel(ctx, cancel, lister, autopilotCalc, standardCalc, nodeLister, lc, watchInterval, promClient, project, showMode)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

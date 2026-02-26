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
	Short: "Watch GKE Autopilot workload costs in real-time",
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

	model := tui.NewModel(ctx, cancel, lister, calc, lc, watchInterval)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

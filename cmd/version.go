package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// These variables are set at build time via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "autopilot-cost-analyzer %s (commit: %s, built: %s)\n", version, commit, date)
	},
}

// Version returns the build version string.
func Version() string {
	return version
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

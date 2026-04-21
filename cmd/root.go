package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "confluence-to-outline",
	Short: "CLI tool to convert confluence pages to outline documents",
	Long:  `Command line tool to migrate confluence pages with attachments and tree structure to outline documents.`,
}

func Execute() {
	rootCmd.PersistentFlags().String("log", "info", "Logging level")
	rootCmd.PersistentFlags().Int("outline-rate-limit", 1000, "Max Outline API requests per --outline-rate-window. Set to 0 to disable throttling. Matches Outline's RATE_LIMITER_REQUESTS default.")
	rootCmd.PersistentFlags().Int("outline-rate-window", 60, "Window in seconds for --outline-rate-limit. Matches Outline's RATE_LIMITER_DURATION_WINDOW default.")
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
}

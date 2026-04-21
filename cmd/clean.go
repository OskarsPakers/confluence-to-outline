package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"zzdats.lv/confluence-to-outline/outline"
)

// findChangesCmd represents the findCommited command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Delete all documents in collection",
	Long:  `Finds and deletes all documents within the collection.`,
	Run: func(cmd *cobra.Command, args []string) {
		lvl := new(slog.LevelVar)
		levelString := cmd.Flag("log").Value.String()
		lvl.UnmarshalText([]byte(levelString))
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: lvl,
		}))
		collection, err := cmd.Flags().GetString("collection")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting collection flag: %v\n", err)
			os.Exit(1)
		}
		logger.Info("Cleaning collection", "collection", collection)

		rateLimit, err := outlineRateLimitFromFlags(cmd)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}

		client, err := outline.GetClient(logger, rateLimit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating Outline client: %v\n", err)
			os.Exit(1)
		}

		err = client.CleanCollection(collection) //TODO Fix mystery 403 authorization_error
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error cleaning collection: %v\n", err)
			os.Exit(1)
		}

	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.PersistentFlags().String("collection", "", "Collection id or to clean")
	cleanCmd.MarkPersistentFlagRequired("collection")
}

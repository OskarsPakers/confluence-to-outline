package cmd

import (
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
			panic(err)
		}
		logger.Info("Cleaning collection", "collection", collection)

		client, err := outline.GetClient(logger)
		if err != nil {
			panic(err)
		}

		err = client.CleanCollection(collection) //TODO Fix mystery 403 authorization_error
		if err != nil {
			panic(err)
		}

	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.PersistentFlags().String("collection", "", "Collection id or to clean")
	cleanCmd.MarkPersistentFlagRequired("collection")
}

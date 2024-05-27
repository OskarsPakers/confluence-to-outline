package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zzdats.lv/confluence-to-outline/outline"
)

// findChangesCmd represents the findCommited command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Delete all documents in collection",
	Long:  `Finds and deletes all documents within the collection.`,
	Run: func(cmd *cobra.Command, args []string) {
		collection, err := cmd.Flags().GetString("collection")
		if err != nil {
			panic(err)
		}

		fmt.Printf("Cleaning collection %s.\n", collection)

		client, err := outline.GetClient()
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

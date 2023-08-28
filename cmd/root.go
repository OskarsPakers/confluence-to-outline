/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
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
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
}

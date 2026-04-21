package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"zzdats.lv/confluence-to-outline/outline"
)

func outlineRateLimitFromFlags(cmd *cobra.Command) (outline.RateLimit, error) {
	requests, err := cmd.Flags().GetInt("outline-rate-limit")
	if err != nil {
		return outline.RateLimit{}, fmt.Errorf("Error getting --outline-rate-limit flag: %w", err)
	}
	windowSeconds, err := cmd.Flags().GetInt("outline-rate-window")
	if err != nil {
		return outline.RateLimit{}, fmt.Errorf("Error getting --outline-rate-window flag: %w", err)
	}
	return outline.RateLimit{
		Requests: requests,
		Window:   time.Duration(windowSeconds) * time.Second,
	}, nil
}

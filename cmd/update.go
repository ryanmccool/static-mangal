package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/selfupdate"
	"github.com/spf13/cobra"
)

var (
	updateCheck = selfupdate.Check
	updateApply = selfupdate.Apply
)

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().Bool("check", false, "check for an available update without changing the installation")
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for or install a newer release",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		config := selfupdate.Config{
			CurrentVersion: constant.Version,
			Distribution:   constant.Distribution,
		}
		check, err := cmd.Flags().GetBool("check")
		if err != nil {
			return err
		}
		if check {
			return runUpdateCheck(cmd.Context(), cmd, config)
		}
		return runUpdateApply(cmd.Context(), cmd, config)
	},
}

func runUpdateCheck(ctx context.Context, cmd *cobra.Command, config selfupdate.Config) error {
	result, err := updateCheck(ctx, config)
	if errors.Is(err, selfupdate.ErrLocalAhead) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Current version: %s\nLatest version: %s\nRefusing downgrade: the local version is newer.\n", result.CurrentVersion, result.LatestVersion)
		return err
	}
	if err != nil {
		return err
	}
	if result.UpdateAvailable {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Current version: %s\nUpdate available: %s\n", result.CurrentVersion, result.LatestVersion)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Current version: %s\nNo update available.\n", result.CurrentVersion)
	return nil
}

func runUpdateApply(ctx context.Context, cmd *cobra.Command, config selfupdate.Config) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updating static-mangal from version %s...\n", config.CurrentVersion)
	result, err := updateApply(ctx, config)
	if err != nil {
		var committedWarning *selfupdate.CommittedUpdateError
		if errors.As(err, &committedWarning) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated static-mangal from %s to %s.\nWarning: update committed, but cleanup or durability was not fully confirmed: %v\nRestart static-mangal to use the new version.\n", result.CurrentVersion, result.LatestVersion, committedWarning.WarningErr)
		}
		return err
	}
	if !result.UpdateAvailable {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No update available.")
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated static-mangal from %s to %s.\nRestart static-mangal to use the new version.\n", result.CurrentVersion, result.LatestVersion)
	return nil
}

package cmd

import (
	"context"
	"fmt"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

const githubRepoSlug = "giantswarm/klaus"

func newSelfUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "self-update",
		Short: "Update klaus to the latest version",
		Long: `Checks for the latest release of klaus on GitHub and
updates the current binary if a newer version is found.`,
		RunE: runSelfUpdate,
	}
}

func runSelfUpdate(cmd *cobra.Command, args []string) error {
	currentVersion := rootCmd.Version
	if currentVersion == "" || currentVersion == "dev" {
		return fmt.Errorf("cannot self-update a development version")
	}

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Println("Checking for updates...")

	updater, err := selfupdate.NewUpdater(selfupdate.Config{})
	if err != nil {
		return fmt.Errorf("failed to create updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.ParseSlug(githubRepoSlug))
	if err != nil {
		return fmt.Errorf("error detecting latest version: %w", err)
	}
	if !found {
		return fmt.Errorf("latest release for %s could not be found", githubRepoSlug)
	}

	if !latest.GreaterThan(currentVersion) {
		fmt.Println("Current version is the latest.")
		return nil
	}

	fmt.Printf("Found newer version: %s (published at %s)\n", latest.Version(), latest.PublishedAt)
	fmt.Printf("Release notes:\n%s\n", latest.ReleaseNotes)

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("could not locate executable path: %w", err)
	}

	fmt.Printf("Updating %s to version %s...\n", exe, latest.Version())

	if err := updater.UpdateTo(context.Background(), latest, exe); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Printf("Successfully updated to version %s\n", latest.Version())
	return nil
}

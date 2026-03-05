package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klaus/pkg/project"
)

var serveCmd *cobra.Command

var rootCmd = &cobra.Command{
	Use:   "klaus",
	Short: "AI agent orchestrator for Kubernetes",
	Long: `klaus is a Go wrapper around claude-code to orchestrate AI agents within Kubernetes.
It provides agent lifecycle management, plugin/MCP tool integration, subagent
coordination, and skill-based task routing.

When run without subcommands, it starts the server (equivalent to 'klaus serve').`,
	SilenceUsage: true,
	// Default to the serve command when no subcommand is provided.
	RunE: func(cmd *cobra.Command, args []string) error {
		return serveCmd.RunE(serveCmd, args)
	},
}

// SetBuildInfo propagates build-time metadata to the root command and pkg/project.
func SetBuildInfo(version, commit, date string) {
	rootCmd.Version = version
	project.SetBuildInfo(version, commit, date)
}

// Execute runs the root command and exits on error.
func Execute() {
	rootCmd.SetVersionTemplate(`{{printf "klaus version %s\n" .Version}}`)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	serveCmd = newServeCmd()
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newSelfUpdateCmd())
	rootCmd.AddCommand(serveCmd)
}

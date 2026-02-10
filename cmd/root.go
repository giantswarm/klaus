package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klaus/pkg/project"
)

// serveCmd is stored so the root command can delegate to it.
var serveCmd *cobra.Command

// rootCmd represents the base command for the klaus application.
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

// SetVersion sets the version for the root command and propagates it to pkg/project.
// This function is called from the main package to inject the application version at build time.
func SetVersion(v string) {
	rootCmd.Version = v
	project.SetVersion(v)
}

// Execute is the main entry point for the CLI application.
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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd creates the Cobra command for displaying the application version.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number of klaus",
		Long:  `All software has versions. This is klaus's.`,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "klaus version %s\n", rootCmd.Version)
		},
	}
}

package main

import (
	"github.com/giantswarm/klaus/cmd"
)

// version will be set by goreleaser during build.
var version = "dev"

func main() {
	// Set the version from build-time variable.
	cmd.SetVersion(version)

	// Execute the root command.
	cmd.Execute()
}

package main

import (
	"github.com/giantswarm/klaus/cmd"
)

// Build-time variables set via ldflags (goreleaser & Dockerfile).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cmd.SetBuildInfo(version, commit, date)
	cmd.Execute()
}

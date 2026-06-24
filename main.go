package main

import (
	"github.com/giantswarm/klaus/cmd"
)

// Build-time variables set via ldflags. Container builds inject build metadata
// directly into pkg/project via architect-orb's go-build job, so the defaults
// here stay empty to avoid clobbering those values.
var (
	version string
	commit  string
	date    string
)

func main() {
	cmd.SetBuildInfo(version, commit, date)
	cmd.Execute()
}

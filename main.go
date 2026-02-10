package main

import (
	"github.com/giantswarm/klaus/cmd"
)

var version = "dev" // set by goreleaser

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}

package project

// Populated at link time via `-X` ldflags by architect-orb's go-build job
// (container images), which sets gitSHA and buildTimestamp directly; version
// stays at its default because no tag is plumbed through.
var (
	buildTimestamp string
	gitSHA         string
	version        = "dev"
)

const (
	// Name is the name of this project.
	Name = "klaus"

	// Description is a short description of this project.
	Description = "A Go wrapper around claude-code to orchestrate AI agents within Kubernetes."
)

// BuildTimestamp returns the build timestamp set at compile time.
func BuildTimestamp() string {
	return buildTimestamp
}

// GitSHA returns the git SHA set at compile time.
func GitSHA() string {
	return gitSHA
}

// SetBuildInfo overrides the build-time metadata. This is called by the cmd
// package to propagate ldflags set on main (via the Dockerfile build).
func SetBuildInfo(v, commit, date string) {
	if v != "" {
		version = v
	}
	if commit != "" {
		gitSHA = commit
	}
	if date != "" {
		buildTimestamp = date
	}
}

// Version returns the best human-readable build identifier available: the
// release tag if one was injected, otherwise the commit SHA, otherwise "dev".
func Version() string {
	if version != "dev" && version != "" {
		return version
	}
	if gitSHA != "" {
		return gitSHA
	}
	return "dev"
}

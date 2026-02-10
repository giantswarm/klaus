package project

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
// package to propagate ldflags set on main (via goreleaser or Dockerfile).
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

// Version returns the application version set at compile time.
func Version() string {
	return version
}

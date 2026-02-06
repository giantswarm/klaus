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

// Version returns the application version set at compile time.
func Version() string {
	return version
}

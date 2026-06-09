package a2a

import (
	"os"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"

	"github.com/giantswarm/klaus/pkg/project"
)

const (
	envAgentName        = "KLAUS_AGENT_NAME"
	envAgentDescription = "KLAUS_AGENT_DESCRIPTION"
	envAgentVersion     = "KLAUS_AGENT_VERSION"
	envAgentURL         = "KLAUS_AGENT_URL"

	mimeTextPlain = "text/plain"
)

// AgentCard returns an A2A AgentCard populated from environment variables,
// with sensible defaults derived from the build-time project metadata.
//
// Environment variables:
//   - KLAUS_AGENT_NAME         (default: project.Name)
//   - KLAUS_AGENT_DESCRIPTION  (default: "Klaus AI agent")
//   - KLAUS_AGENT_VERSION      (default: project.Version())
//   - KLAUS_AGENT_URL          (default: "http://localhost:8080/a2a")
func AgentCard() *a2a.AgentCard {
	name := envOrDefault(envAgentName, project.Name)
	description := envOrDefault(envAgentDescription, "Klaus AI agent")
	version := envOrDefault(envAgentVersion, project.Version())
	url := envOrDefault(envAgentURL, "http://localhost:8080/a2a")

	return &a2a.AgentCard{
		Name:        name,
		Description: description,
		Version:     version,
		SupportedInterfaces: []*a2a.AgentInterface{
			{
				URL:             url,
				ProtocolBinding: a2a.TransportProtocolJSONRPC,
				// kagent-controller speaks the v0.3 JSON-RPC wire format.
				ProtocolVersion: a2av0.Version,
			},
		},
		DefaultInputModes:  []string{mimeTextPlain},
		DefaultOutputModes: []string{mimeTextPlain},
		Capabilities: a2a.AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
		},
		Skills: []a2a.AgentSkill{
			{
				ID:          "coding",
				Name:        "Software Engineering",
				Description: "Read, write, and refactor code across any language; run tests; navigate large codebases; propose and apply multi-file changes.",
				Tags:        []string{"coding", "refactoring", "testing", "debugging"},
			},
			{
				ID:          "analysis",
				Name:        "Code & Data Analysis",
				Description: "Explain complex code, trace execution paths, analyse logs, review pull requests, and summarise technical documents.",
				Tags:        []string{"analysis", "review", "explanation", "documentation"},
			},
			{
				ID:          "claude-code",
				Name:        name,
				Description: description,
				Tags:        []string{"claude", "ai", "agent"},
			},
		},
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

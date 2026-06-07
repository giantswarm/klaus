package a2a

import (
	"os"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/giantswarm/klaus/pkg/project"
)

// agentCardEnvs lists the environment variables that configure the agent card.
// Each corresponds to a field in the returned AgentCard.
const (
	envAgentName        = "AGENT_NAME"
	envAgentDescription = "AGENT_DESCRIPTION"
	envAgentVersion     = "AGENT_VERSION"
	envAgentURL         = "AGENT_URL"
)

// AgentCard returns an A2A AgentCard populated from environment variables,
// with sensible defaults derived from the build-time project metadata.
//
// Environment variables:
//   - AGENT_NAME         (default: project.Name)
//   - AGENT_DESCRIPTION  (default: "Klaus AI agent")
//   - AGENT_VERSION      (default: project.Version())
//   - AGENT_URL          (default: "http://localhost:8080/a2a")
func AgentCard() *a2a.AgentCard {
	name := envOrDefault(envAgentName, project.Name)
	description := envOrDefault(envAgentDescription, "Klaus AI agent")
	version := envOrDefault(envAgentVersion, project.Version())
	url := envOrDefault(envAgentURL, "http://localhost:8080/a2a")

	return &a2a.AgentCard{
		Name:               name,
		Description:        description,
		Version:            version,
		URL:                url,
		ProtocolVersion:    string(a2a.Version),
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities: a2a.AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
		},
		Skills: []a2a.AgentSkill{
			{
				ID:          "claude-code",
				Name:        name,
				Description: description,
				Tags:        []string{"coding", "ai", "claude"},
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

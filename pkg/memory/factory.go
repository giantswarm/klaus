package memory

import "os"

// New returns a Store backed by the kagent memory API.
// Returns NoOp when MEMORY_ENABLED is not set, endpoint is empty,
// or KLAUS_EMBEDDING_MODEL is unset (vectors cannot be generated).
func New(endpoint, agentName string) Store {
	if !parseBoolEnv("MEMORY_ENABLED") {
		return NoOp{}
	}
	if endpoint == "" {
		return NoOp{}
	}
	embedder := newEmbeddingClientFromEnv()
	if !embedder.enabled() {
		return NoOp{}
	}
	return newKagentStore(endpoint, agentName, embedder)
}

func parseBoolEnv(key string) bool {
	switch os.Getenv(key) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

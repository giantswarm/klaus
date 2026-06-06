package memory

import "os"

// New returns a memory Client configured from environment variables.
//
// Returns NoOp when:
//   - KAGENT_MEMORY_ENDPOINT is unset (no store configured)
//   - KLAUD_EMBEDDING_MODEL is unset (vectors cannot be generated)
//
// When both are set, returns a KagentClient that stores and searches
// conversation memory via the kagent pgvector API.
func New() Client {
	endpoint := os.Getenv("KAGENT_MEMORY_ENDPOINT")
	if endpoint == "" {
		return NoOp{}
	}
	client := newKagentClientFromEnv(endpoint)
	if !client.embedder.enabled() {
		return NoOp{}
	}
	return client
}

package claude

import (
	"context"
)

// Prompter defines the interface for sending prompts to a Claude agent.
// Both single-shot (Process) and persistent (PersistentProcess) modes
// implement this interface.
type Prompter interface {
	// Run spawns or sends a prompt and returns a channel of stream-json messages.
	// The channel is closed when the response is complete.
	Run(ctx context.Context, prompt string) (<-chan StreamMessage, error)

	// RunWithOptions is like Run but accepts per-invocation overrides.
	RunWithOptions(ctx context.Context, prompt string, opts *RunOptions) (<-chan StreamMessage, error)

	// RunSyncWithOptions sends a prompt with per-run overrides and blocks until completion.
	RunSyncWithOptions(ctx context.Context, prompt string, opts *RunOptions) (string, []StreamMessage, error)

	// Submit starts a prompt non-blocking: it calls RunWithOptions, spawns a
	// background goroutine to drain the message channel and store results,
	// then returns immediately. The ctx should be a server-scoped context
	// (not an MCP request context) so the drain goroutine outlives the caller.
	// Use Status() to check progress and retrieve the result when complete.
	Submit(ctx context.Context, prompt string, opts *RunOptions) error

	// Status returns the current status information. When idle after a
	// completed Submit run, the Result field contains the agent's output.
	Status() StatusInfo

	// Stop stops the current operation or subprocess.
	Stop() error

	// Done returns a channel that is closed when the current run completes.
	Done() <-chan struct{}

	// ResultDetail returns the full untruncated result and detailed metadata
	// from the last completed run. Intended for debugging and troubleshooting.
	ResultDetail() ResultDetailInfo

	// MarshalStatus returns the status as JSON.
	MarshalStatus() ([]byte, error)
}

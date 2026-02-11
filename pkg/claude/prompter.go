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

	// Status returns the current status information.
	Status() StatusInfo

	// Stop stops the current operation or subprocess.
	Stop() error

	// Done returns a channel that is closed when the current run completes.
	Done() <-chan struct{}

	// MarshalStatus returns the status as JSON.
	MarshalStatus() ([]byte, error)
}

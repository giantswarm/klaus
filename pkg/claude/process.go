package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/giantswarm/klaus/pkg/metrics"
)

// RunOptions allows overriding base Options on a per-invocation basis.
// Zero values are ignored (the base Options value is used instead).
type RunOptions struct {
	// SessionID overrides Options.SessionID for this run.
	SessionID string
	// Resume overrides Options.Resume for this run.
	Resume string
	// ContinueSession overrides Options.ContinueSession for this run.
	ContinueSession bool
	// ForkSession overrides Options.ForkSession for this run.
	ForkSession bool
	// ActiveAgent overrides Options.ActiveAgent for this run.
	ActiveAgent string
	// JSONSchema overrides Options.JSONSchema for this run.
	JSONSchema string
	// MaxBudgetUSD overrides Options.MaxBudgetUSD for this run.
	MaxBudgetUSD float64
	// Effort overrides Options.Effort for this run.
	Effort string
}

// ignoredFields returns the names of fields that have non-zero values.
// This is used to log which per-invocation overrides cannot be applied
// in persistent mode.
func (ro *RunOptions) ignoredFields() []string {
	if ro == nil {
		return nil
	}
	var fields []string
	if ro.SessionID != "" {
		fields = append(fields, "session_id")
	}
	if ro.Resume != "" {
		fields = append(fields, "resume")
	}
	if ro.ContinueSession {
		fields = append(fields, "continue")
	}
	if ro.ForkSession {
		fields = append(fields, "fork_session")
	}
	if ro.ActiveAgent != "" {
		fields = append(fields, "agent")
	}
	if ro.JSONSchema != "" {
		fields = append(fields, "json_schema")
	}
	if ro.MaxBudgetUSD > 0 {
		fields = append(fields, "max_budget_usd")
	}
	if ro.Effort != "" {
		fields = append(fields, "effort")
	}
	return fields
}

// Process manages a Claude CLI subprocess lifecycle and streams its output.
type Process struct {
	opts Options

	mu            sync.RWMutex
	cmd           *exec.Cmd
	status        ProcessStatus
	sessionID     string
	lastError     string
	totalCost     float64
	messageCount  int
	toolCallCount int
	lastMessage   string
	lastToolName  string

	// result stores the output of the last completed Submit run,
	// allowing callers to retrieve results asynchronously.
	result resultState

	done      chan struct{}
	runCancel context.CancelFunc // cancels the stdout-reading goroutine
}

// NewProcess returns a Process ready to run. The Done channel is pre-closed
// to indicate no subprocess is active.
func NewProcess(opts Options) *Process {
	done := make(chan struct{})
	close(done) // Pre-closed: "not running" == "already done".
	return &Process{
		opts:   opts,
		status: ProcessStatusIdle,
		done:   done,
	}
}

// mergedOpts returns a copy of the base options with per-run overrides applied.
func (p *Process) mergedOpts(ro *RunOptions) Options {
	opts := p.opts
	if ro == nil {
		return opts
	}
	if ro.SessionID != "" {
		opts.SessionID = ro.SessionID
	}
	if ro.Resume != "" {
		opts.Resume = ro.Resume
	}
	if ro.ContinueSession {
		opts.ContinueSession = true
	}
	if ro.ForkSession {
		opts.ForkSession = true
	}
	if ro.ActiveAgent != "" {
		opts.ActiveAgent = ro.ActiveAgent
	}
	if ro.JSONSchema != "" {
		opts.JSONSchema = ro.JSONSchema
	}
	if ro.MaxBudgetUSD > 0 {
		opts.MaxBudgetUSD = ro.MaxBudgetUSD
	}
	if ro.Effort != "" {
		opts.Effort = ro.Effort
	}
	return opts
}

// Run spawns a claude subprocess for the given prompt and returns a channel
// of stream-json messages. The channel is closed when the process exits.
func (p *Process) Run(ctx context.Context, prompt string) (<-chan StreamMessage, error) {
	return p.RunWithOptions(ctx, prompt, nil)
}

// RunWithOptions spawns a claude subprocess with per-run option overrides.
func (p *Process) RunWithOptions(ctx context.Context, prompt string, runOpts *RunOptions) (<-chan StreamMessage, error) {
	p.mu.Lock()
	if p.status == ProcessStatusBusy {
		p.mu.Unlock()
		return nil, fmt.Errorf("claude process is already busy")
	}
	p.status = ProcessStatusStarting
	p.lastError = ""
	p.messageCount = 0
	p.toolCallCount = 0
	p.lastMessage = ""
	p.lastToolName = ""

	// Create a new done channel for this run while holding the lock,
	// ensuring no race between concurrent callers.
	done := make(chan struct{})
	p.done = done
	p.mu.Unlock()

	opts := p.mergedOpts(runOpts)
	args := opts.args()
	args = append(args, "--", prompt)

	// Use exec.Command (not CommandContext) so that cancellation goes through
	// Stop() which sends SIGTERM for graceful shutdown, rather than the
	// immediate SIGKILL that CommandContext would send.
	cmd := exec.Command("claude", args...) //nolint:gosec // args are controlled
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		close(done)
		p.setError(fmt.Sprintf("failed to create stdout pipe: %v", err))
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		close(done)
		p.setError(fmt.Sprintf("failed to create stderr pipe: %v", err))
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		close(done)
		p.setError(fmt.Sprintf("failed to start claude: %v", err))
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Create a context for the stdout-reading goroutine so it can be
	// cancelled by Stop() without blocking on a full output channel.
	runCtx, runCancel := context.WithCancel(context.Background())

	p.mu.Lock()
	p.cmd = cmd
	p.status = ProcessStatusBusy
	p.runCancel = runCancel
	p.mu.Unlock()
	metrics.SetProcessStatus(string(ProcessStatusBusy))

	out := make(chan StreamMessage, 100)

	// Wait for the stderr reader to finish before calling cmd.Wait(),
	// avoiding potential data loss from premature pipe closure.
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)

	// Read stderr in background for logging.
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[claude stderr] %s", scanner.Text())
		}
	}()

	// Read stdout stream-json messages.
	// Use the local done variable captured at creation time so that
	// closing it cannot race with a subsequent Run call.
	go func() {
		defer close(out)
		defer func() {
			// Wait for stderr reader to drain before calling Wait.
			stderrWg.Wait()
			waitErr := cmd.Wait()

			p.mu.Lock()
			p.cmd = nil
			if waitErr != nil && p.status != ProcessStatusStopped {
				p.status = ProcessStatusError
				p.lastError = waitErr.Error()
			} else if p.status == ProcessStatusBusy {
				p.status = ProcessStatusIdle
			}
			status := p.status
			close(done)
			p.mu.Unlock()
			metrics.SetProcessStatus(string(status))
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			msg, parseErr := ParseStreamMessage(line)
			if parseErr != nil {
				log.Printf("[claude] failed to parse stream message: %v (line: %s)", parseErr, string(line))
				continue
			}

			p.mu.Lock()
			p.messageCount++
			if msg.Type == MessageTypeSystem && msg.SessionID != "" {
				p.sessionID = msg.SessionID
			}
			if msg.Type == MessageTypeAssistant {
				if msg.Subtype == SubtypeText && msg.Text != "" {
					p.lastMessage = Truncate(msg.Text, 200)
				}
				if msg.Subtype == SubtypeToolUse {
					p.toolCallCount++
					p.lastToolName = msg.ToolName
				}
			}
			if msg.Type == MessageTypeResult {
				p.totalCost = msg.TotalCost
			}
			p.mu.Unlock()

			// Record Prometheus metrics.
			metrics.MessagesTotal.WithLabelValues(string(msg.Type)).Inc()
			if msg.Type == MessageTypeAssistant && msg.Subtype == SubtypeToolUse {
				metrics.ToolCallsTotal.WithLabelValues(msg.ToolName).Inc()
			}
			if msg.Type == MessageTypeResult && msg.TotalCost > 0 {
				metrics.SessionCostUSDTotal.Add(msg.TotalCost)
			}

			// Use select to prevent blocking if the consumer stops reading
			// (e.g., after context cancellation in the MCP handler).
			select {
			case out <- msg:
			case <-runCtx.Done():
				return
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			log.Printf("[claude] stdout scanner error: %v", scanErr)
		}
	}()

	return out, nil
}

// RunSync runs a prompt and blocks until completion, returning the result text
// and all messages. It respects context cancellation.
func (p *Process) RunSync(ctx context.Context, prompt string) (string, []StreamMessage, error) {
	return p.RunSyncWithOptions(ctx, prompt, nil)
}

// RunSyncWithOptions runs a prompt with per-run overrides and blocks until completion.
func (p *Process) RunSyncWithOptions(ctx context.Context, prompt string, runOpts *RunOptions) (string, []StreamMessage, error) {
	ch, err := p.RunWithOptions(ctx, prompt, runOpts)
	if err != nil {
		return "", nil, err
	}

	var messages []StreamMessage

loop:
	for {
		select {
		case <-ctx.Done():
			_ = p.Stop()
			return "", messages, ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				break loop
			}
			messages = append(messages, msg)
		}
	}

	return CollectResultText(messages), messages, nil
}

// Stop sends SIGTERM and waits up to 10s before SIGKILL.
func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	if cmd == nil || cmd.Process == nil {
		p.mu.Unlock()
		return nil
	}
	p.status = ProcessStatusStopped
	// Cancel the stdout-reading goroutine so it doesn't block on a full
	// output channel while we wait for the process to exit.
	if p.runCancel != nil {
		p.runCancel()
	}
	p.mu.Unlock()
	metrics.SetProcessStatus(string(ProcessStatusStopped))

	// Send SIGTERM first.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("[claude] SIGTERM failed (process may have already exited): %v", err)
		return nil
	}

	// Wait up to 10 seconds for graceful shutdown.
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		// Force kill.
		log.Printf("[claude] process did not exit after SIGTERM, sending SIGKILL")
		return cmd.Process.Kill()
	}
}

// Submit starts a prompt non-blocking. It calls RunWithOptions, spawns a
// background goroutine to drain the message channel and store results, then
// returns immediately. The ctx should be a server-scoped context so the drain
// goroutine outlives the MCP request. Previous results are preserved if the
// run fails to start (e.g. process is already busy).
func (p *Process) Submit(ctx context.Context, prompt string, opts *RunOptions) error {
	return submitAsync(ctx, prompt, opts, p.RunWithOptions, func(rs resultState) {
		p.mu.Lock()
		p.result = rs
		// When the drain goroutine finishes collecting the run output,
		// transition from idle to completed so callers can distinguish
		// "finished with results" from "never ran" (idle).
		if rs.completed && p.status == ProcessStatusIdle {
			p.status = ProcessStatusCompleted
		}
		p.mu.Unlock()
	})
}

func (p *Process) Status() StatusInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	info := StatusInfo{
		Status:        p.status,
		SessionID:     p.sessionID,
		ErrorMessage:  p.lastError,
		TotalCost:     p.totalCost,
		MessageCount:  p.messageCount,
		ToolCallCount: p.toolCallCount,
		LastMessage:   p.lastMessage,
		LastToolName:  p.lastToolName,
	}

	if p.status == ProcessStatusCompleted {
		info.Result = Truncate(p.result.text, maxStatusResultLen)
	}

	return info
}

// ResultDetail returns the full untruncated result and detailed metadata from
// the last completed Submit run. Intended for debugging and troubleshooting.
func (p *Process) ResultDetail() ResultDetailInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return ResultDetailInfo{
		ResultText:   p.result.text,
		Messages:     p.result.messages,
		MessageCount: len(p.result.messages),
		TotalCost:    p.totalCost,
		SessionID:    p.sessionID,
		Status:       p.status,
		ErrorMessage: p.lastError,
	}
}

// Done returns a channel closed when the current run completes.
func (p *Process) Done() <-chan struct{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.done
}

func (p *Process) MarshalStatus() ([]byte, error) {
	return json.Marshal(p.Status())
}

func (p *Process) setError(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = ProcessStatusError
	p.lastError = msg
	metrics.SetProcessStatus(string(ProcessStatusError))
}

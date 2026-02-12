package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// PersistentProcess maintains a long-running Claude subprocess for multi-turn
// conversations using bidirectional stream-json. Instead of spawning a new
// subprocess per prompt, it writes user messages to stdin and reads responses
// from stdout, providing conversation continuity and lower latency.
type PersistentProcess struct {
	opts Options

	mu            sync.RWMutex
	cmd           *exec.Cmd
	stdin         io.WriteCloser
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

	// responseCh receives stream-json messages during an active prompt.
	// It is set by Send and cleared when the response is complete.
	responseCh chan StreamMessage

	// done is closed when the current prompt response is complete.
	done chan struct{}

	// processDone is closed when the subprocess exits entirely.
	processDone chan struct{}

	// cancel cancels the stdout reader goroutine.
	cancel context.CancelFunc

	// autoRestart enables the background watchdog that restarts the
	// subprocess when it crashes unexpectedly.
	autoRestart bool

	// watchdogCtx controls the lifetime of the watchdog goroutine.
	watchdogCtx    context.Context
	watchdogCancel context.CancelFunc
}

// NewPersistentProcess returns a PersistentProcess. Call Start() to launch
// the subprocess before sending prompts. The process includes a background
// watchdog that automatically restarts the subprocess if it crashes.
func NewPersistentProcess(opts Options) *PersistentProcess {
	done := make(chan struct{})
	close(done) // Pre-closed: no active prompt.
	processDone := make(chan struct{})
	close(processDone) // Pre-closed: not started.
	return &PersistentProcess{
		opts:        opts,
		status:      ProcessStatusIdle,
		done:        done,
		processDone: processDone,
		autoRestart: true,
	}
}

// Start launches the persistent Claude subprocess. It must be called before
// sending prompts. The subprocess runs until Stop() is called or it exits.
func (p *PersistentProcess) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return fmt.Errorf("persistent process already running")
	}

	p.status = ProcessStatusStarting

	args := p.opts.PersistentArgs()

	cmd := exec.Command("claude", args...) //nolint:gosec // args are controlled
	if p.opts.WorkDir != "" {
		cmd.Dir = p.opts.WorkDir
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		p.status = ProcessStatusError
		p.lastError = fmt.Sprintf("failed to create stdin pipe: %v", err)
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.status = ProcessStatusError
		p.lastError = fmt.Sprintf("failed to create stdout pipe: %v", err)
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.status = ProcessStatusError
		p.lastError = fmt.Sprintf("failed to create stderr pipe: %v", err)
		return err
	}

	if err := cmd.Start(); err != nil {
		p.status = ProcessStatusError
		p.lastError = fmt.Sprintf("failed to start claude: %v", err)
		return fmt.Errorf("failed to start claude: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdinPipe
	p.status = ProcessStatusIdle

	processDone := make(chan struct{})
	p.processDone = processDone

	// Persistent mode subprocess lifetime is managed explicitly via Stop(),
	// not by any single request context.
	readerCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// Read stderr in background.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[claude persistent stderr] %s", scanner.Text())
		}
	}()

	// Read stdout stream-json messages and dispatch to active response channel.
	go p.readLoop(readerCtx, stdout, cmd, processDone)

	// Start the background watchdog that auto-restarts on unexpected exit.
	if p.autoRestart {
		p.startWatchdog(context.Background(), processDone)
	}

	log.Println("[claude] persistent subprocess started")
	return nil
}

// restartBackoff is the minimum time to wait between restart attempts to
// prevent tight restart loops when the subprocess fails immediately.
const restartBackoff = 2 * time.Second

// startWatchdog launches a background goroutine that watches for unexpected
// subprocess exits and restarts the process automatically. It cancels any
// previously running watchdog to avoid duplicates.
func (p *PersistentProcess) startWatchdog(ctx context.Context, processDone chan struct{}) {
	// Cancel any existing watchdog before starting a new one.
	if p.watchdogCancel != nil {
		p.watchdogCancel()
	}

	watchCtx, watchCancel := context.WithCancel(ctx)
	p.watchdogCtx = watchCtx
	p.watchdogCancel = watchCancel

	go func() {
		select {
		case <-watchCtx.Done():
			return
		case <-processDone:
		}

		p.mu.RLock()
		status := p.status
		p.mu.RUnlock()

		// Only restart if the process exited unexpectedly (not via Stop).
		if status == ProcessStatusStopped {
			return
		}

		log.Printf("[claude] persistent subprocess exited unexpectedly (status=%s), restarting in %v", status, restartBackoff)

		select {
		case <-watchCtx.Done():
			return
		case <-time.After(restartBackoff):
		}

		if err := p.Start(watchCtx); err != nil {
			log.Printf("[claude] failed to restart persistent subprocess: %v", err)
		}
	}()
}

// readLoop continuously reads stream-json messages from stdout and dispatches
// them to the active response channel. It detects result messages to mark the
// end of a response cycle.
func (p *PersistentProcess) readLoop(ctx context.Context, stdout io.ReadCloser, cmd *exec.Cmd, processDone chan struct{}) {
	defer func() {
		waitErr := cmd.Wait()
		p.mu.Lock()
		p.cmd = nil
		p.stdin = nil
		if waitErr != nil && p.status != ProcessStatusStopped {
			p.status = ProcessStatusError
			p.lastError = waitErr.Error()
		} else if p.status != ProcessStatusStopped {
			p.status = ProcessStatusIdle
		}
		// Close any pending response channel.
		if p.responseCh != nil {
			close(p.responseCh)
			p.responseCh = nil
		}
		// Close the done channel for any active prompt.
		select {
		case <-p.done:
			// Already closed.
		default:
			close(p.done)
		}
		close(processDone)
		p.mu.Unlock()
		log.Println("[claude] persistent subprocess exited")
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		msg, parseErr := ParseStreamMessage(line)
		if parseErr != nil {
			log.Printf("[claude persistent] failed to parse stream message: %v (line: %s)", parseErr, string(line))
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

		// Dispatch to the active response channel if one exists.
		ch := p.responseCh
		p.mu.Unlock()

		if ch != nil {
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}

		// A result message marks the end of the current response cycle.
		if msg.Type == MessageTypeResult {
			p.mu.Lock()
			if p.responseCh != nil {
				close(p.responseCh)
				p.responseCh = nil
			}
			if p.status == ProcessStatusBusy {
				p.status = ProcessStatusIdle
			}
			// Signal done for this prompt.
			select {
			case <-p.done:
				// Already closed.
			default:
				close(p.done)
			}
			p.mu.Unlock()
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		log.Printf("[claude persistent] stdout scanner error: %v", scanErr)
	}
}

// stdinMessageContent represents the nested message payload in the
// stream-json input format expected by claude-code v2.1+.
type stdinMessageContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// stdinMessage is the JSON structure written to the subprocess stdin
// in stream-json input mode. claude-code v2.1+ expects a "message"
// object with "role" and "content" fields instead of a flat "text" field.
type stdinMessage struct {
	Type    string              `json:"type"`
	Message stdinMessageContent `json:"message"`
}

// Run sends a prompt to the persistent subprocess and returns a channel
// of response messages. RunOptions are partially supported -- only fields
// relevant to multi-turn conversation (like ActiveAgent) take effect;
// subprocess-level flags (Model, PermissionMode, etc.) are set at Start time.
func (p *PersistentProcess) Run(ctx context.Context, prompt string) (<-chan StreamMessage, error) {
	return p.RunWithOptions(ctx, prompt, nil)
}

// RunWithOptions sends a prompt with optional per-invocation overrides.
// In persistent mode, per-invocation overrides cannot be applied since the
// subprocess flags were set at Start time. If non-empty overrides are provided,
// a warning is logged listing which fields were ignored.
func (p *PersistentProcess) RunWithOptions(ctx context.Context, prompt string, runOpts *RunOptions) (<-chan StreamMessage, error) {
	if runOpts != nil {
		if ignored := runOpts.ignoredFields(); len(ignored) > 0 {
			log.Printf("[claude persistent] WARNING: per-invocation overrides ignored in persistent mode: %s", strings.Join(ignored, ", "))
		}
	}

	p.mu.Lock()

	if p.cmd == nil {
		p.mu.Unlock()
		// Auto-start if not yet started.
		if err := p.Start(context.Background()); err != nil {
			return nil, err
		}
		p.mu.Lock()
	}

	if p.status == ProcessStatusBusy {
		p.mu.Unlock()
		return nil, fmt.Errorf("claude process is already busy")
	}

	p.status = ProcessStatusBusy
	p.lastError = ""
	p.messageCount = 0
	p.toolCallCount = 0
	p.lastMessage = ""
	p.lastToolName = ""

	// Create response channel and done channel for this prompt.
	ch := make(chan StreamMessage, 100)
	p.responseCh = ch
	done := make(chan struct{})
	p.done = done

	stdin := p.stdin
	p.mu.Unlock()

	// Write the user message to stdin as stream-json.
	msg := stdinMessage{
		Type: "user",
		Message: stdinMessageContent{
			Role:    "user",
			Content: prompt,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		p.setError(fmt.Sprintf("failed to marshal stdin message: %v", err))
		close(ch)
		close(done)
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	// Append newline delimiter for stream-json protocol.
	data = append(data, '\n')

	if _, err := stdin.Write(data); err != nil {
		p.setError(fmt.Sprintf("failed to write to stdin: %v", err))
		close(ch)
		close(done)
		return nil, fmt.Errorf("failed to write to stdin: %w", err)
	}

	return ch, nil
}

// RunSyncWithOptions sends a prompt and blocks until the response is complete.
func (p *PersistentProcess) RunSyncWithOptions(ctx context.Context, prompt string, runOpts *RunOptions) (string, []StreamMessage, error) {
	ch, err := p.RunWithOptions(ctx, prompt, runOpts)
	if err != nil {
		return "", nil, err
	}

	var messages []StreamMessage

loop:
	for {
		select {
		case <-ctx.Done():
			// For persistent process, we don't stop the whole subprocess,
			// just cancel the current prompt wait.
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

// Stop sends SIGTERM to the persistent subprocess and waits for it to exit.
// It also cancels the background watchdog to prevent auto-restart.
func (p *PersistentProcess) Stop() error {
	// Cancel the watchdog first to prevent restart during shutdown.
	if p.watchdogCancel != nil {
		p.watchdogCancel()
	}

	p.mu.Lock()
	cmd := p.cmd
	processDone := p.processDone
	if cmd == nil || cmd.Process == nil {
		p.mu.Unlock()
		return nil
	}
	p.status = ProcessStatusStopped
	if p.cancel != nil {
		p.cancel()
	}
	// Close stdin to signal EOF.
	if p.stdin != nil {
		_ = p.stdin.Close()
		p.stdin = nil
	}
	p.mu.Unlock()

	// Send SIGTERM first.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("[claude persistent] SIGTERM failed (process may have already exited): %v", err)
		return nil
	}

	// Wait up to 10 seconds for graceful shutdown.
	select {
	case <-processDone:
		return nil
	case <-time.After(10 * time.Second):
		log.Printf("[claude persistent] process did not exit after SIGTERM, sending SIGKILL")
		return cmd.Process.Kill()
	}
}

// Submit starts a prompt non-blocking. It calls RunWithOptions, spawns a
// background goroutine to drain the message channel and store results, then
// returns immediately. The ctx should be a server-scoped context so the drain
// goroutine outlives the MCP request. Previous results are preserved if the
// run fails to start (e.g. process is already busy).
func (p *PersistentProcess) Submit(ctx context.Context, prompt string, opts *RunOptions) error {
	return submitAsync(ctx, prompt, opts, p.RunWithOptions, func(rs resultState) {
		p.mu.Lock()
		p.result = rs
		p.mu.Unlock()
	})
}

// Status returns the current status information.
func (p *PersistentProcess) Status() StatusInfo {
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

	if p.status == ProcessStatusIdle && p.result.text != "" {
		info.Result = Truncate(p.result.text, maxStatusResultLen)
	}

	return info
}

// ResultDetail returns the full untruncated result and detailed metadata from
// the last completed Submit run. Intended for debugging and troubleshooting.
func (p *PersistentProcess) ResultDetail() ResultDetailInfo {
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

// Done returns a channel closed when the current prompt response is complete.
func (p *PersistentProcess) Done() <-chan struct{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.done
}

// MarshalStatus returns the status as JSON.
func (p *PersistentProcess) MarshalStatus() ([]byte, error) {
	return json.Marshal(p.Status())
}

func (p *PersistentProcess) setError(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = ProcessStatusError
	p.lastError = msg
}

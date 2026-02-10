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
)

// Process manages a Claude CLI subprocess lifecycle and streams its output.
type Process struct {
	opts Options

	mu        sync.RWMutex
	cmd       *exec.Cmd
	status    ProcessStatus
	sessionID string
	lastError string
	totalCost float64

	done chan struct{}
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

// Run spawns a claude subprocess for the given prompt and returns a channel
// of stream-json messages. The channel is closed when the process exits.
func (p *Process) Run(ctx context.Context, prompt string) (<-chan StreamMessage, error) {
	p.mu.Lock()
	if p.status == ProcessStatusBusy {
		p.mu.Unlock()
		return nil, fmt.Errorf("claude process is already busy")
	}
	p.status = ProcessStatusStarting
	p.lastError = ""

	// Create a new done channel for this run while holding the lock,
	// ensuring no race between concurrent callers.
	done := make(chan struct{})
	p.done = done
	p.mu.Unlock()

	args := p.opts.args()
	args = append(args, "--", prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	if p.opts.WorkDir != "" {
		cmd.Dir = p.opts.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.setError(fmt.Sprintf("failed to create stdout pipe: %v", err))
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.setError(fmt.Sprintf("failed to create stderr pipe: %v", err))
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		p.setError(fmt.Sprintf("failed to start claude: %v", err))
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	p.mu.Lock()
	p.cmd = cmd
	p.status = ProcessStatusBusy
	p.mu.Unlock()

	out := make(chan StreamMessage, 100)

	// Use a WaitGroup to ensure both pipe readers finish before cmd.Wait(),
	// avoiding potential data loss from premature pipe closure.
	var pipeWg sync.WaitGroup
	pipeWg.Add(2)

	// Read stderr in background for logging.
	go func() {
		defer pipeWg.Done()
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
			// Ensure both pipe readers are fully drained before Wait.
			pipeWg.Wait()
			waitErr := cmd.Wait()

			p.mu.Lock()
			p.cmd = nil
			if waitErr != nil && p.status != ProcessStatusStopped {
				p.status = ProcessStatusError
				p.lastError = waitErr.Error()
			} else if p.status == ProcessStatusBusy {
				p.status = ProcessStatusIdle
			}
			close(done)
			p.mu.Unlock()
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
			if msg.Type == MessageTypeSystem && msg.SessionID != "" {
				p.sessionID = msg.SessionID
			}
			if msg.Type == MessageTypeResult {
				p.totalCost = msg.TotalCost
			}
			p.mu.Unlock()

			out <- msg
		}

		if scanErr := scanner.Err(); scanErr != nil {
			log.Printf("[claude] stdout scanner error: %v", scanErr)
		}

		pipeWg.Done()
	}()

	return out, nil
}

// RunSync runs a prompt and blocks until completion, returning the result text
// and all messages. It respects context cancellation.
func (p *Process) RunSync(ctx context.Context, prompt string) (string, []StreamMessage, error) {
	ch, err := p.Run(ctx, prompt)
	if err != nil {
		return "", nil, err
	}

	var messages []StreamMessage
	var resultText string

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
			if msg.Type == MessageTypeResult {
				resultText = msg.Result
			}
		}
	}

	if resultText == "" {
		// Collect text from assistant messages as fallback.
		for _, msg := range messages {
			if msg.Type == MessageTypeAssistant && msg.Subtype == SubtypeText {
				resultText += msg.Text
			}
		}
	}

	return resultText, messages, nil
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
	p.mu.Unlock()

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

func (p *Process) Status() StatusInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return StatusInfo{
		Status:       p.status,
		SessionID:    p.sessionID,
		ErrorMessage: p.lastError,
		TotalCost:    p.totalCost,
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
}

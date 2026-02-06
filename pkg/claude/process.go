package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process manages a Claude CLI subprocess.
type Process struct {
	opts Options

	mu        sync.RWMutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	status    ProcessStatus
	sessionID string
	lastError string
	totalCost float64

	messages chan StreamMessage
	done     chan struct{}
}

// NewProcess creates a new Claude process manager with the given options.
func NewProcess(opts Options) *Process {
	return &Process{
		opts:     opts,
		status:   ProcessStatusStopped,
		messages: make(chan StreamMessage, 100),
		done:     make(chan struct{}),
	}
}

// Run sends a prompt to Claude in one-shot mode and streams results back.
// It spawns a new claude subprocess for each invocation.
// The returned channel receives all stream-json messages and is closed when
// the process completes.
func (p *Process) Run(ctx context.Context, prompt string) (<-chan StreamMessage, error) {
	p.mu.Lock()
	if p.status == ProcessStatusBusy {
		p.mu.Unlock()
		return nil, fmt.Errorf("claude process is already busy")
	}
	p.status = ProcessStatusStarting
	p.lastError = ""
	p.mu.Unlock()

	args := p.opts.args()
	args = append(args, prompt)

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
	p.done = make(chan struct{})
	p.mu.Unlock()

	out := make(chan StreamMessage, 100)

	// Read stderr in background for logging.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[claude stderr] %s", scanner.Text())
		}
	}()

	// Read stdout stream-json messages.
	go func() {
		defer close(out)
		defer func() {
			// Wait for process to finish.
			waitErr := cmd.Wait()

			p.mu.Lock()
			p.cmd = nil
			if waitErr != nil && p.status != ProcessStatusStopped {
				p.status = ProcessStatusError
				p.lastError = waitErr.Error()
			} else if p.status == ProcessStatusBusy {
				p.status = ProcessStatusIdle
			}
			close(p.done)
			p.mu.Unlock()
		}()

		scanner := bufio.NewScanner(stdout)
		// Claude can produce long lines; increase buffer.
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

			// Track session state.
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
	}()

	return out, nil
}

// RunSync sends a prompt and collects the full response synchronously.
// It returns the final result text and any messages received during execution.
func (p *Process) RunSync(ctx context.Context, prompt string) (string, []StreamMessage, error) {
	ch, err := p.Run(ctx, prompt)
	if err != nil {
		return "", nil, err
	}

	var messages []StreamMessage
	var resultText string

	for msg := range ch {
		messages = append(messages, msg)
		if msg.Type == MessageTypeResult {
			resultText = msg.Result
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

// Stop terminates the running Claude subprocess gracefully.
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
		// Process may have already exited.
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

// Status returns the current process status information.
func (p *Process) Status() StatusInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return StatusInfo{
		Status:    p.status,
		SessionID: p.sessionID,
		Error:     p.lastError,
		TotalCost: p.totalCost,
	}
}

// Done returns a channel that is closed when the current run completes.
func (p *Process) Done() <-chan struct{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.done
}

// MarshalStatus returns the status as JSON bytes.
func (p *Process) MarshalStatus() ([]byte, error) {
	return json.Marshal(p.Status())
}

func (p *Process) setError(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = ProcessStatusError
	p.lastError = msg
}

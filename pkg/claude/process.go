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
// in persistent mode. ContinueSession is excluded because persistent mode
// inherently continues the conversation -- the flag is expected, not ignored.
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
	costSeen      bool // true once any cost data has been observed
	messageCount  int
	toolCallCount int
	toolCalls     map[string]int
	modelUsage    map[string]int
	prURLs        []string
	toolUseIDs    map[string]string // toolUseID -> toolName
	errorCount    int
	tokenUsage    TokenUsage
	lastMessage   string
	lastToolName  string
	subagents     *subagentTracker
	liveMessages  []StreamMessage

	// result stores the output of the last completed Submit run,
	// allowing callers to retrieve results asynchronously.
	result resultState

	// resultStore persists results to disk so they survive restarts.
	// Nil means no persistence.
	resultStore *ResultStore

	done      chan struct{}
	runCancel context.CancelFunc // cancels the stdout-reading goroutine
}

// NewProcess returns a Process ready to run. The Done channel is pre-closed
// to indicate no subprocess is active.
func NewProcess(opts Options) *Process {
	done := make(chan struct{})
	close(done) // Pre-closed: "not running" == "already done".
	return &Process{
		opts:        opts,
		status:      ProcessStatusIdle,
		subagents:   newSubagentTracker(),
		done:        done,
		resultStore: NewResultStore(resultStoreDir(opts)),
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
		// --continue requires the previous session to exist on disk,
		// so disable --no-session-persistence when continuing (#171).
		opts.NoSessionPersistence = false
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
		return nil, ErrBusy
	}
	p.status = ProcessStatusStarting
	p.lastError = ""
	// Preserve liveMessages and messageCount across turns so that
	// the MCP messages tool returns the full conversation history
	// and message_count accumulates rather than resetting (#171).
	p.toolCallCount = 0
	p.toolCalls = make(map[string]int)
	p.modelUsage = make(map[string]int)
	p.prURLs = nil
	p.toolUseIDs = make(map[string]string)
	p.errorCount = 0
	p.tokenUsage = TokenUsage{}
	p.totalCost = 0
	p.costSeen = false
	p.lastMessage = ""
	p.lastToolName = ""
	p.subagents.reset()

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

			if msg.Type == MessageTypeStreamEvent {
				select {
				case out <- msg:
				case <-runCtx.Done():
					return
				}
				continue
			}

			p.mu.Lock()
			p.messageCount++
			p.liveMessages = append(p.liveMessages, msg)
			if msg.Type == MessageTypeSystem && msg.SessionID != "" {
				p.sessionID = msg.SessionID
			}
			if msg.Type == MessageTypeAssistant {
				if model := extractModel(msg); model != "" {
					p.modelUsage[model]++
				}
				if msg.Subtype == SubtypeText && msg.Text != "" {
					p.lastMessage = Truncate(msg.Text, 200)
				}
				if msg.Subtype == SubtypeToolUse {
					p.toolCallCount++
					p.lastToolName = msg.ToolName
					p.toolCalls[msg.ToolName]++
					if msg.ToolID != "" {
						p.toolUseIDs[msg.ToolID] = msg.ToolName
					}
					p.subagents.handleToolUse(msg)
				} else {
					p.subagents.handleMessage(msg)
				}
			} else {
				p.subagents.handleMessage(msg)
			}
			// Extract PR URLs and count errors from tool_result content blocks.
			if blocks := ExtractToolResults(msg); len(blocks) > 0 {
				for _, block := range blocks {
					// Only extract PR URLs from Bash tool results to avoid
					// false positives from file content (Read, Write, etc.).
					if block.ToolUseID == "" || isBashTool(p.toolUseIDs[block.ToolUseID]) {
						p.prURLs = appendUnique(p.prURLs, extractPRURLs(block.Content)...)
					}
					if block.IsError {
						p.errorCount++
					}
				}
			}
			// Aggregate token usage from assistant messages.
			if msg.Usage != nil {
				p.tokenUsage.InputTokens += msg.Usage.InputTokens
				p.tokenUsage.OutputTokens += msg.Usage.OutputTokens
				p.tokenUsage.CacheCreationInputTokens += msg.Usage.CacheCreationInputTokens
				p.tokenUsage.CacheReadInputTokens += msg.Usage.CacheReadInputTokens
			}
			// Track cost from result messages (total_cost_usd) and also
			// accumulate per-message cost_usd from any message type to
			// avoid reporting $0.00 when total_cost_usd is missing (#62).
			// Note: StreamMessage.TotalCost remains float64 (not *float64)
			// because it represents the raw wire protocol value from the
			// Claude CLI; the > 0 guard is appropriate since the CLI does
			// not emit total_cost_usd: 0.0 for zero-cost operations.
			var costToRecord float64
			if msg.Type == MessageTypeResult && msg.TotalCost > 0 {
				p.totalCost = msg.TotalCost
				p.costSeen = true
				costToRecord = msg.TotalCost
			} else if msg.Cost > 0 {
				p.totalCost += msg.Cost
				p.costSeen = true
			}
			// When the result message lacks total_cost_usd, use the
			// accumulated per-message cost for Prometheus.
			if msg.Type == MessageTypeResult && costToRecord == 0 {
				costToRecord = p.totalCost
			}
			p.mu.Unlock()

			// Record Prometheus metrics.
			metrics.RecordStreamMessage(string(msg.Type), string(msg.Subtype), msg.ToolName)
			if msg.Type == MessageTypeResult {
				metrics.RecordCost(costToRecord)
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
		status := p.status
		sessionID := p.sessionID
		totalCost := p.totalCost
		costSeen := p.costSeen
		lastError := p.lastError
		tu := p.tokenUsage
		store := p.resultStore
		p.mu.Unlock()

		var costPtr *float64
		if costSeen {
			costPtr = Float64Ptr(totalCost)
		}
		var tuPtr *TokenUsage
		if tu != (TokenUsage{}) {
			tuPtr = &tu
		}
		persistResult(store, rs, status, sessionID, costPtr, lastError, tuPtr)
	})
}

func (p *Process) Status() StatusInfo {
	p.mu.RLock()
	info := StatusInfo{
		Status:        p.status,
		SessionID:     p.sessionID,
		ErrorMessage:  p.lastError,
		MessageCount:  p.messageCount,
		ToolCallCount: p.toolCallCount,
		ToolCalls:     copyToolCalls(p.toolCalls),
		LastMessage:   p.lastMessage,
		LastToolName:  p.lastToolName,
		SubagentCalls: copySubagentCalls(p.subagents.calls()),
		ModelUsage:    copyToolCalls(p.modelUsage),
		ErrorCount:    p.errorCount,
	}

	if p.costSeen {
		info.TotalCost = Float64Ptr(p.totalCost)
	}

	tu := p.tokenUsage // copy value
	if tu != (TokenUsage{}) {
		info.TokenUsage = &tu
	}

	if p.status == ProcessStatusCompleted {
		info.Result = Truncate(p.result.text, maxStatusResultLen)
	}

	store := p.resultStore
	p.mu.RUnlock()

	// Fall back to disk when in-memory result is empty and a store exists.
	// Skip disk I/O when the process is actively running to avoid unnecessary reads.
	if info.Result == "" && store != nil && info.Status != ProcessStatusBusy && info.Status != ProcessStatusStarting {
		if pr, err := store.Load(); err == nil && pr != nil && pr.ResultText != "" {
			info.Result = Truncate(pr.ResultText, maxStatusResultLen)
			// Backfill status metadata from persisted result if the
			// in-memory state has been cleared (e.g. after restart).
			if info.SessionID == "" {
				info.SessionID = pr.SessionID
			}
			if info.TotalCost == nil && pr.TotalCost != nil {
				info.TotalCost = pr.TotalCost
			}
			if info.ErrorMessage == "" {
				info.ErrorMessage = pr.ErrorMessage
			}
			if info.TokenUsage == nil && pr.TokenUsage != nil {
				tu := *pr.TokenUsage
				info.TokenUsage = &tu
			}
			if info.ModelUsage == nil && pr.ModelUsage != nil {
				info.ModelUsage = copyToolCalls(pr.ModelUsage)
			}
			if info.ErrorCount == 0 && pr.ErrorCount != 0 {
				info.ErrorCount = pr.ErrorCount
			}
		}
	}

	return info
}

// ResultDetail returns the full untruncated result and detailed metadata from
// the last completed Submit run. Intended for debugging and troubleshooting.
// Falls back to the persisted result on disk when the in-memory state is empty.
func (p *Process) ResultDetail() ResultDetailInfo {
	p.mu.RLock()
	detail := ResultDetailInfo{
		ResultText:    p.result.text,
		Messages:      p.result.messages,
		MessageCount:  p.messageCount,
		ToolCalls:     copyToolCalls(p.toolCalls),
		SubagentCalls: copySubagentCalls(p.subagents.calls()),
		ModelUsage:    copyToolCalls(p.modelUsage),
		PRURLs:        copyStringSlice(p.prURLs),
		ErrorCount:    p.errorCount,
		SessionID:     p.sessionID,
		Status:        p.status,
		ErrorMessage:  p.lastError,
	}
	if p.costSeen {
		detail.TotalCost = Float64Ptr(p.totalCost)
	}
	tu := p.tokenUsage
	if tu != (TokenUsage{}) {
		detail.TokenUsage = &tu
	}
	store := p.resultStore
	p.mu.RUnlock()

	// Fall back to disk when in-memory result is empty.
	if detail.ResultText == "" && store != nil {
		if pr, err := store.Load(); err == nil && pr != nil {
			return pr.ToResultDetailInfo()
		}
	}

	return detail
}

// Messages returns the current conversation messages. liveMessages accumulates
// across turns, so it is preferred over result.messages (which only contains
// the last Submit run). Falls back to persisted state on disk when empty.
func (p *Process) Messages() MessagesInfo {
	p.mu.RLock()
	status := p.status
	live := copyStreamMessages(p.liveMessages)
	resMessages := copyStreamMessages(p.result.messages)
	store := p.resultStore
	p.mu.RUnlock()

	// Prefer liveMessages (accumulated across turns) over result.messages
	// (single turn from Submit) for full conversation history (#171).
	if len(live) > 0 {
		return MessagesInfo{
			Status:   status,
			Messages: SummarizeMessages(live),
		}
	}

	if len(resMessages) > 0 {
		return MessagesInfo{
			Status:   status,
			Messages: SummarizeMessages(resMessages),
		}
	}

	// Fall back to persisted result on disk.
	if store != nil {
		if pr, err := store.Load(); err == nil && pr != nil && len(pr.Messages) > 0 {
			return MessagesInfo{
				Status:   status,
				Messages: SummarizeMessages(pr.Messages),
			}
		}
	}

	return MessagesInfo{Status: status}
}

// RawMessages returns the raw stream-json messages from the current or last
// completed run. offset skips the first N messages; types filters by message
// type (empty means all types). The Total field always reflects the full
// unfiltered message count. liveMessages is preferred as it accumulates
// across turns (#171).
func (p *Process) RawMessages(offset int, types []string) RawMessagesInfo {
	p.mu.RLock()
	status := p.status
	live := copyStreamMessages(p.liveMessages)
	resMessages := copyStreamMessages(p.result.messages)
	store := p.resultStore
	p.mu.RUnlock()

	// Prefer liveMessages (accumulated across turns) for full history.
	if len(live) > 0 {
		return collectRawMessages(status, live, offset, types)
	}

	if len(resMessages) > 0 {
		return collectRawMessages(status, resMessages, offset, types)
	}

	// Fall back to persisted result on disk.
	if store != nil {
		if pr, err := store.Load(); err == nil && pr != nil && len(pr.Messages) > 0 {
			return collectRawMessages(status, pr.Messages, offset, types)
		}
	}

	return RawMessagesInfo{Status: status, Messages: []json.RawMessage{}}
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

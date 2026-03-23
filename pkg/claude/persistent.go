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

	"github.com/giantswarm/klaus/pkg/metrics"
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
	costSeen      bool
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

	// responseCh receives stream-json messages during an active prompt.
	// It is set by Send and cleared when the response is complete.
	responseCh chan StreamMessage

	// sawContent tracks whether any content (stream_event or assistant)
	// was forwarded in the current prompt cycle. Used to distinguish
	// intermediate result messages (e.g. slash command dispatch) from
	// final results.
	sawContent bool

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
		subagents:   newSubagentTracker(),
		toolUseIDs:  make(map[string]string),
		done:        done,
		processDone: processDone,
		autoRestart: true,
		resultStore: NewResultStore(resultStoreDir(opts)),
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
	metrics.SetProcessStatus(string(ProcessStatusIdle))

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
		metrics.ProcessRestartsTotal.Inc()

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
		status := p.status
		close(processDone)
		p.mu.Unlock()
		metrics.SetProcessStatus(string(status))
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

		// stream_event messages are ephemeral deltas for real-time streaming.
		// Forward to consumers but skip internal bookkeeping.
		if msg.Type == MessageTypeStreamEvent {
			p.mu.Lock()
			ch := p.responseCh
			if ch != nil {
				p.sawContent = true
			}
			p.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				case <-ctx.Done():
					return
				}
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
			p.sawContent = true
			if model := ExtractModel(msg); model != "" {
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
		// Compute the cost delta before overwriting the running total so we
		// only add the incremental cost to the Prometheus counter.
		// Also accumulate per-message cost_usd when total_cost_usd is
		// missing to avoid reporting $0.00 (#62).
		var costDelta float64
		if msg.Type == MessageTypeResult && msg.TotalCost > 0 {
			costDelta = msg.TotalCost - p.totalCost
			p.totalCost = msg.TotalCost
			p.costSeen = true
		} else if msg.Cost > 0 {
			costDelta = msg.Cost
			p.totalCost += msg.Cost
			p.costSeen = true
		}

		// Dispatch to the active response channel if one exists.
		ch := p.responseCh
		p.mu.Unlock()

		// Record Prometheus metrics.
		metrics.RecordStreamMessage(string(msg.Type), string(msg.Subtype), msg.ToolName)
		if msg.Type == MessageTypeResult {
			metrics.RecordCost(costDelta)
		}

		if ch != nil {
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}

		// A result message marks the end of the current response cycle,
		// unless it's an intermediate result from a slash command dispatch.
		// LLM-invoking slash commands (e.g. /refine) emit a result for the
		// dispatch (0 tokens, no content seen) before starting a new LLM turn.
		// We keep the channel open for intermediate results so the LLM output
		// from the subsequent turn reaches the consumer.
		if msg.Type == MessageTypeResult {
			isFinal := p.sawContent || msg.IsError ||
				msg.Result != "" ||
				(msg.Usage != nil && (msg.Usage.InputTokens > 0 || msg.Usage.OutputTokens > 0))
			p.mu.Lock()
			if p.responseCh != nil && isFinal {
				close(p.responseCh)
				p.responseCh = nil
				p.sawContent = false
			}
			if isFinal {
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
			}
			status := p.status
			p.mu.Unlock()
			metrics.SetProcessStatus(string(status))
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
		return nil, ErrBusy
	}

	p.status = ProcessStatusBusy
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

	// Inject a synthetic user message so the prompt appears in liveMessages.
	// The Claude CLI only emits assistant/system/result on stdout; the user's
	// input prompt is never echoed back, so we record it here (#179).
	p.liveMessages = append(p.liveMessages, syntheticUserMessage(prompt))
	p.messageCount++

	// Create response channel and done channel for this prompt.
	ch := make(chan StreamMessage, 100)
	p.responseCh = ch
	p.sawContent = false
	done := make(chan struct{})
	p.done = done

	stdin := p.stdin
	p.mu.Unlock()
	metrics.SetProcessStatus(string(ProcessStatusBusy))

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
	metrics.SetProcessStatus(string(ProcessStatusStopped))

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

// Status returns the current status information.
func (p *PersistentProcess) Status() StatusInfo {
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

	tu := p.tokenUsage
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
// the last completed Submit run. Falls back to the persisted result on disk
// when the in-memory state is empty.
func (p *PersistentProcess) ResultDetail() ResultDetailInfo {
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
func (p *PersistentProcess) Messages() MessagesInfo {
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
func (p *PersistentProcess) RawMessages(offset int, types []string) RawMessagesInfo {
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

	if store != nil {
		if pr, err := store.Load(); err == nil && pr != nil && len(pr.Messages) > 0 {
			return collectRawMessages(status, pr.Messages, offset, types)
		}
	}

	return RawMessagesInfo{Status: status, Messages: []json.RawMessage{}}
}

// OpenAIMessages returns conversation messages in OpenAI Chat Completions
// compatible format. System and result messages are extracted into metadata.
// offset skips the first N converted messages (after consolidation).
func (p *PersistentProcess) OpenAIMessages(offset int) OpenAIMessagesInfo {
	p.mu.RLock()
	status := p.status
	live := copyStreamMessages(p.liveMessages)
	resMessages := copyStreamMessages(p.result.messages)
	store := p.resultStore
	p.mu.RUnlock()

	if len(live) > 0 {
		return collectOpenAIMessages(status, live, offset)
	}
	if len(resMessages) > 0 {
		return collectOpenAIMessages(status, resMessages, offset)
	}
	if store != nil {
		if pr, err := store.Load(); err == nil && pr != nil && len(pr.Messages) > 0 {
			return collectOpenAIMessages(status, pr.Messages, offset)
		}
	}
	return OpenAIMessagesInfo{Messages: []OpenAIMessage{}, Metadata: OpenAIMetadata{}}
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
	metrics.SetProcessStatus(string(ProcessStatusError))
}

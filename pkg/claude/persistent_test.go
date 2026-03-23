package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNewPersistentProcess_InitialState(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	status := process.Status()
	if status.Status != ProcessStatusIdle {
		t.Errorf("expected initial status %q, got %q", ProcessStatusIdle, status.Status)
	}
	if status.SessionID != "" {
		t.Errorf("expected empty session ID, got %q", status.SessionID)
	}
	if status.ErrorMessage != "" {
		t.Errorf("expected empty error message, got %q", status.ErrorMessage)
	}
	if status.MessageCount != 0 {
		t.Errorf("expected 0 message count, got %d", status.MessageCount)
	}
	if status.ToolCallCount != 0 {
		t.Errorf("expected 0 tool call count, got %d", status.ToolCallCount)
	}
	if status.ToolCalls != nil {
		t.Errorf("expected nil tool calls, got %v", status.ToolCalls)
	}
	if status.LastMessage != "" {
		t.Errorf("expected empty last message, got %q", status.LastMessage)
	}
	if status.LastToolName != "" {
		t.Errorf("expected empty last tool name, got %q", status.LastToolName)
	}
}

func TestNewPersistentProcess_DonePreClosed(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Done channel should be immediately readable (pre-closed).
	select {
	case <-process.Done():
		// Expected.
	default:
		t.Error("expected Done() to be immediately readable for new process")
	}
}

func TestPersistentProcess_MarshalStatus(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	data, err := process.MarshalStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var info StatusInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if info.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, info.Status)
	}
}

func TestPersistentProcess_StatusToolCalls(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.toolCallCount = 4
	process.toolCalls = map[string]int{
		"Bash": 2,
		"Read": 1,
		"Glob": 1,
	}
	process.mu.Unlock()

	status := process.Status()
	if status.ToolCallCount != 4 {
		t.Errorf("expected tool_call_count 4, got %d", status.ToolCallCount)
	}
	if len(status.ToolCalls) != 3 {
		t.Fatalf("expected 3 tool entries, got %d", len(status.ToolCalls))
	}
	if status.ToolCalls["Bash"] != 2 {
		t.Errorf("expected Bash count 2, got %d", status.ToolCalls["Bash"])
	}
	if status.ToolCalls["Read"] != 1 {
		t.Errorf("expected Read count 1, got %d", status.ToolCalls["Read"])
	}
	if status.ToolCalls["Glob"] != 1 {
		t.Errorf("expected Glob count 1, got %d", status.ToolCalls["Glob"])
	}

	// Verify the returned map is a copy.
	status.ToolCalls["Bash"] = 999
	status2 := process.Status()
	if status2.ToolCalls["Bash"] != 2 {
		t.Errorf("expected internal map to be unchanged, got Bash=%d", status2.ToolCalls["Bash"])
	}
}

func TestPersistentProcess_ResultDetailToolCalls(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	process.mu.Lock()
	process.status = ProcessStatusCompleted
	process.result = resultState{text: "done", completed: true}
	process.toolCalls = map[string]int{
		"Bash": 5,
		"Task": 1,
	}
	process.mu.Unlock()

	detail := process.ResultDetail()
	if len(detail.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool entries, got %d", len(detail.ToolCalls))
	}
	if detail.ToolCalls["Bash"] != 5 {
		t.Errorf("expected Bash count 5, got %d", detail.ToolCalls["Bash"])
	}
	if detail.ToolCalls["Task"] != 1 {
		t.Errorf("expected Task count 1, got %d", detail.ToolCalls["Task"])
	}
}

func TestPersistentProcess_StopWhenNotRunning(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Stop on an idle process should be a no-op.
	if err := process.Stop(); err != nil {
		t.Errorf("unexpected error stopping idle process: %v", err)
	}
}

func TestPersistentProcess_ImplementsPrompter(t *testing.T) {
	// Compile-time check that PersistentProcess implements Prompter.
	var _ Prompter = (*PersistentProcess)(nil)
}

func TestPersistentProcess_ResultDetail_InitialState(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())
	detail := process.ResultDetail()

	if detail.ResultText != "" {
		t.Errorf("expected empty result text, got %q", detail.ResultText)
	}
	if detail.Messages != nil {
		t.Errorf("expected nil messages, got %v", detail.Messages)
	}
	if detail.MessageCount != 0 {
		t.Errorf("expected 0 message count, got %d", detail.MessageCount)
	}
	if detail.Status != ProcessStatusIdle {
		t.Errorf("expected status %q, got %q", ProcessStatusIdle, detail.Status)
	}
}

func TestPersistentProcess_ResultDetail_MessageCountWhileBusy(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Simulate a busy process that has received messages but has no
	// completed result yet (result.messages is empty).
	process.mu.Lock()
	process.status = ProcessStatusBusy
	process.messageCount = 5
	process.mu.Unlock()

	detail := process.ResultDetail()
	if detail.MessageCount != 5 {
		t.Errorf("expected MessageCount 5 from live counter while busy, got %d", detail.MessageCount)
	}
	if detail.Status != ProcessStatusBusy {
		t.Errorf("expected status %q, got %q", ProcessStatusBusy, detail.Status)
	}
}

func TestPersistentProcess_StatusNoResultWhenBusy(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	process.mu.Lock()
	process.result = resultState{text: "old result", completed: true}
	process.status = ProcessStatusBusy
	process.mu.Unlock()

	status := process.Status()
	if status.Result != "" {
		t.Errorf("expected empty result when busy, got %q", status.Result)
	}
}

func TestPersistentProcess_StatusNoResultWhenIdle(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// idle with no completed result should not show result.
	status := process.Status()
	if status.Result != "" {
		t.Errorf("expected empty result when idle, got %q", status.Result)
	}
}

func TestPersistentProcess_StatusShowsResultWhenCompleted(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	process.mu.Lock()
	process.result = resultState{text: "completed result", completed: true}
	process.status = ProcessStatusCompleted
	process.mu.Unlock()

	status := process.Status()
	if status.Status != ProcessStatusCompleted {
		t.Errorf("expected status %q, got %q", ProcessStatusCompleted, status.Status)
	}
	if status.Result != "completed result" {
		t.Errorf("expected result %q, got %q", "completed result", status.Result)
	}
}

func TestPersistentProcess_StatusTruncatesLongResult(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Create a result longer than maxStatusResultLen.
	long := make([]rune, maxStatusResultLen+100)
	for i := range long {
		long[i] = 'x'
	}
	process.mu.Lock()
	process.result = resultState{text: string(long), completed: true}
	process.status = ProcessStatusCompleted
	process.mu.Unlock()

	status := process.Status()
	expected := string(long[:maxStatusResultLen]) + "..."
	if status.Result != expected {
		t.Errorf("expected truncated result of length %d, got length %d",
			len([]rune(expected)), len([]rune(status.Result)))
	}

	detail := process.ResultDetail()
	if detail.ResultText != string(long) {
		t.Error("expected ResultDetail to return full untruncated text")
	}
}

func TestPersistentProcess_PersistentArgs(t *testing.T) {
	opts := Options{
		Model:              "claude-sonnet-4-20250514",
		SystemPrompt:       "Be helpful.",
		AppendSystemPrompt: "Be concise.",
		AllowedTools:       []string{"read", "write"},
		DisallowedTools:    []string{"exec"},
		Tools:              []string{"Bash", "Edit"},
		MaxTurns:           5,
		MCPConfigPath:      "/etc/mcp.json",
		StrictMCPConfig:    true,
		PermissionMode:     "bypassPermissions",
		MaxBudgetUSD:       10.50,
		Effort:             "high",
		FallbackModel:      "sonnet",
		JSONSchema:         `{"type":"object"}`,
		SettingsFile:       "/etc/settings.json",
		SettingSources:     "user",
		PluginDirs:         []string{"/plugins/a"},
		Agents: map[string]AgentConfig{
			"reviewer": {Description: "Reviews", Prompt: "Review"},
		},
		ActiveAgent:          "reviewer",
		NoSessionPersistence: true,
	}

	args := opts.PersistentArgs()

	// Persistent mode uses --input-format stream-json with --replay-user-messages.
	assertContainsSequence(t, args, "--input-format", "stream-json")
	assertContainsSequence(t, args, "--output-format", "stream-json")
	assertContains(t, args, "--replay-user-messages")
	assertContains(t, args, "--print")
	assertContains(t, args, "--verbose")

	// Standard options should be present.
	assertContainsSequence(t, args, "--model", "claude-sonnet-4-20250514")
	assertContainsSequence(t, args, "--system-prompt", "Be helpful.")
	assertContainsSequence(t, args, "--append-system-prompt", "Be concise.")
	assertContainsSequence(t, args, "--max-turns", "5")
	assertContainsSequence(t, args, "--permission-mode", "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContainsSequence(t, args, "--mcp-config", "/etc/mcp.json")
	assertContains(t, args, "--strict-mcp-config")
	assertContainsSequence(t, args, "--allowedTools", "read,write")
	assertContainsSequence(t, args, "--disallowedTools", "exec")
	assertContainsSequence(t, args, "--tools", "Bash,Edit")
	assertContainsSequence(t, args, "--max-budget-usd", "10.50")
	assertContainsSequence(t, args, "--effort", "high")
	assertContainsSequence(t, args, "--fallback-model", "sonnet")
	assertContainsSequence(t, args, "--json-schema", `{"type":"object"}`)
	assertContainsSequence(t, args, "--settings", "/etc/settings.json")
	assertContainsSequence(t, args, "--setting-sources", "user")
	assertContainsSequence(t, args, "--agent", "reviewer")
	assertContains(t, args, "--agents")
	assertContains(t, args, "--include-partial-messages")
	assertContains(t, args, "--no-session-persistence")
	assertContainsSequence(t, args, "--plugin-dir", "/plugins/a")

	// Session management flags should NOT be in persistent args
	// (they are per-subprocess and persistent mode maintains one subprocess).
	assertNotContains(t, args, "--session-id")
	assertNotContains(t, args, "--resume")
	assertNotContains(t, args, "--continue")
	assertNotContains(t, args, "--fork-session")
}

func TestPersistentProcess_PersistentArgs_Minimal(t *testing.T) {
	args := DefaultOptions().PersistentArgs()

	assertContainsSequence(t, args, "--input-format", "stream-json")
	assertContainsSequence(t, args, "--output-format", "stream-json")
	assertContains(t, args, "--replay-user-messages")
	assertContains(t, args, "--print")
	assertContains(t, args, "--verbose")
	assertContains(t, args, "--permission-mode")
	assertContains(t, args, "bypassPermissions")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContains(t, args, "--no-session-persistence")

	assertNotContains(t, args, "--model")
	assertNotContains(t, args, "--max-budget-usd")
	assertNotContains(t, args, "--effort")
}

func TestStdinMessage_JSON(t *testing.T) {
	msg := stdinMessage{
		Type: "user",
		Message: stdinMessageContent{
			Role:    "user",
			Content: "hello world",
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"type":"user","message":{"role":"user","content":"hello world"}}`
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

func TestRunOptions_IgnoredFields(t *testing.T) {
	t.Run("nil returns empty", func(t *testing.T) {
		var ro *RunOptions
		if fields := ro.ignoredFields(); len(fields) != 0 {
			t.Errorf("expected empty, got %v", fields)
		}
	})

	t.Run("zero value returns empty", func(t *testing.T) {
		ro := &RunOptions{}
		if fields := ro.ignoredFields(); len(fields) != 0 {
			t.Errorf("expected empty, got %v", fields)
		}
	})

	t.Run("non-zero fields listed", func(t *testing.T) {
		ro := &RunOptions{
			SessionID:    "sess-1",
			ActiveAgent:  "reviewer",
			MaxBudgetUSD: 5.0,
		}
		fields := ro.ignoredFields()
		if len(fields) != 3 {
			t.Errorf("expected 3 fields, got %d: %v", len(fields), fields)
		}
	})

	t.Run("ContinueSession excluded from ignored", func(t *testing.T) {
		ro := &RunOptions{ContinueSession: true}
		fields := ro.ignoredFields()
		if len(fields) != 0 {
			t.Errorf("ContinueSession should not appear in ignored fields, got %v", fields)
		}
	})
}

func TestPersistentProcess_MessagesAccumulateAcrossTurns(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Simulate first turn: 2 messages.
	process.mu.Lock()
	process.messageCount = 2
	process.liveMessages = []StreamMessage{
		{Type: MessageTypeSystem, SessionID: "sess-1"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "Hello"},
	}
	process.status = ProcessStatusIdle
	process.mu.Unlock()

	msgs := process.Messages()
	if len(msgs.Messages) < 1 {
		t.Fatalf("expected messages from first turn, got %d", len(msgs.Messages))
	}

	// Simulate second turn: messages should accumulate (not reset).
	process.mu.Lock()
	process.messageCount += 2
	process.liveMessages = append(process.liveMessages,
		StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "World"},
		StreamMessage{Type: MessageTypeResult, Result: "done"},
	)
	process.status = ProcessStatusIdle
	process.mu.Unlock()

	msgs = process.Messages()
	// Should contain messages from both turns.
	if process.messageCount != 4 {
		t.Errorf("expected messageCount 4, got %d", process.messageCount)
	}

	// liveMessages should have all 4 entries.
	process.mu.RLock()
	liveCount := len(process.liveMessages)
	process.mu.RUnlock()
	if liveCount != 4 {
		t.Errorf("expected 4 liveMessages, got %d", liveCount)
	}
}

func TestPersistentProcess_LiveMessagesPreferredOverResult(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Set both liveMessages (multi-turn history) and result.messages (single turn).
	process.mu.Lock()
	process.liveMessages = []StreamMessage{
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "turn 1"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "turn 2"},
	}
	process.result = resultState{
		messages:  []StreamMessage{{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "turn 2"}},
		completed: true,
	}
	process.status = ProcessStatusCompleted
	process.mu.Unlock()

	msgs := process.Messages()
	// Should prefer liveMessages (2 entries) over result.messages (1 entry).
	found := 0
	for _, m := range msgs.Messages {
		if m.Content == "turn 1" || m.Content == "turn 2" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 messages from liveMessages, found %d in %v", found, msgs.Messages)
	}
}

// TestPersistentProcess_CrashDuringActivePrompt verifies that when the
// subprocess exits unexpectedly while a prompt is active, the user receives
// an error message in the chat stream and diagnostics are preserved.
func TestPersistentProcess_CrashDuringActivePrompt(t *testing.T) {
	// Build a tiny helper binary that:
	// 1. Writes a valid stream-json assistant message to stdout
	// 2. Writes a line to stderr
	// 3. Exits with code 1 (simulating a crash)
	// This simulates a Claude subprocess that crashes mid-response.
	helperSrc := `
package main

import (
	"fmt"
	"os"
)

func main() {
	// Read stdin to avoid broken pipe (the test will write to it).
	buf := make([]byte, 4096)
	os.Stdin.Read(buf)

	// Emit one assistant text message.
	fmt.Println(` + "`" + `{"type":"assistant","subtype":"text","text":"partial response"}` + "`" + `)

	// Write to stderr for diagnostics capture.
	fmt.Fprintln(os.Stderr, "FATAL: something went wrong")

	// Exit with error code to simulate crash.
	os.Exit(1)
}
`
	tmpDir := t.TempDir()
	srcPath := tmpDir + "/crash_helper.go"
	binPath := tmpDir + "/crash_helper"
	if err := os.WriteFile(srcPath, []byte(helperSrc), 0644); err != nil {
		t.Fatalf("failed to write helper source: %v", err)
	}

	buildCmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build helper: %v\n%s", err, out)
	}

	// Create a PersistentProcess but we'll manually wire it to our helper binary.
	done := make(chan struct{})
	close(done)
	processDone := make(chan struct{})
	close(processDone)
	p := &PersistentProcess{
		opts:        DefaultOptions(),
		status:      ProcessStatusIdle,
		subagents:   newSubagentTracker(),
		toolUseIDs:  make(map[string]string),
		done:        done,
		processDone: processDone,
		autoRestart: false, // disable auto-restart for test isolation
		stderrTail:  newRingBuffer(20),
	}

	// Launch the helper binary as the "claude" subprocess.
	cmd := exec.Command(binPath)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wire the process fields as Start() would.
	newProcessDone := make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.stdin = stdinPipe
	p.processDone = newProcessDone
	p.status = ProcessStatusBusy

	// Set up a response channel as RunWithOptions would.
	ch := make(chan StreamMessage, 100)
	p.responseCh = ch
	promptDone := make(chan struct{})
	p.done = promptDone
	p.mu.Unlock()

	// Start stderr reader.
	go func() {
		scanner := newLineScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			p.mu.Lock()
			p.stderrTail.add(line)
			p.mu.Unlock()
		}
	}()

	readerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the readLoop (it will read stdout and handle the crash).
	go p.readLoop(readerCtx, stdout, cmd, newProcessDone)

	// Write a message to stdin to trigger the helper to run.
	_, _ = stdinPipe.Write([]byte(`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"))

	// Wait for the process to exit.
	select {
	case <-newProcessDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for subprocess to exit")
	}

	// Drain all messages from the response channel.
	var msgs []StreamMessage
	for msg := range ch {
		msgs = append(msgs, msg)
	}

	// Verify we got messages including the crash error.
	if len(msgs) == 0 {
		t.Fatal("expected at least one message from crash, got none")
	}

	// Find the crash error message.
	var crashMsg *StreamMessage
	for i := range msgs {
		if msgs[i].IsError && msgs[i].Type == MessageTypeResult {
			crashMsg = &msgs[i]
			break
		}
	}

	if crashMsg == nil {
		t.Fatal("expected a crash error message in the stream, found none")
	}

	if !strings.Contains(crashMsg.Result, "exited unexpectedly") {
		t.Errorf("crash message should mention unexpected exit, got: %s", crashMsg.Result)
	}

	if !strings.Contains(crashMsg.Result, "1") {
		t.Errorf("crash message should contain exit code 1, got: %s", crashMsg.Result)
	}

	// Verify the done channel is closed.
	select {
	case <-promptDone:
	default:
		t.Error("expected prompt done channel to be closed after crash")
	}

	// Verify status reflects the error.
	p.mu.RLock()
	status := p.status
	lastErr := p.lastError
	p.mu.RUnlock()

	if status != ProcessStatusError {
		t.Errorf("expected status %q after crash, got %q", ProcessStatusError, status)
	}

	if lastErr == "" {
		t.Error("expected lastError to be set after crash")
	}
}

// newLineScanner is a helper to create a bufio.Scanner from an io.ReadCloser.
func newLineScanner(r interface{ Read([]byte) (int, error) }) *lineScanner {
	return &lineScanner{r: r, buf: make([]byte, 0, 4096)}
}

type lineScanner struct {
	r    interface{ Read([]byte) (int, error) }
	buf  []byte
	line string
	err  error
}

func (s *lineScanner) Scan() bool {
	for {
		// Check for newline in buffer.
		if idx := strings.IndexByte(string(s.buf), '\n'); idx >= 0 {
			s.line = string(s.buf[:idx])
			s.buf = s.buf[idx+1:]
			return true
		}
		tmp := make([]byte, 1024)
		n, err := s.r.Read(tmp)
		if n > 0 {
			s.buf = append(s.buf, tmp[:n]...)
		}
		if err != nil {
			if len(s.buf) > 0 {
				s.line = string(s.buf)
				s.buf = nil
				return true
			}
			s.err = err
			return false
		}
	}
}

func (s *lineScanner) Text() string { return s.line }

func TestPersistentProcess_PreviousErrorPreserved(t *testing.T) {
	process := NewPersistentProcess(DefaultOptions())

	// Simulate a crash that sets lastError.
	process.mu.Lock()
	process.lastError = "exit status 1"
	process.status = ProcessStatusError
	process.mu.Unlock()

	// Verify error is visible in status.
	status := process.Status()
	if status.ErrorMessage != "exit status 1" {
		t.Errorf("expected error message %q, got %q", "exit status 1", status.ErrorMessage)
	}

	// Simulate a new prompt starting (which would happen after watchdog restart).
	// RunWithOptions would set previousError and clear lastError.
	process.mu.Lock()
	process.cmd = &exec.Cmd{} // fake non-nil cmd to pass nil check
	process.stdin = nil       // will cause write to fail, but we're testing the field
	process.previousError = process.lastError
	process.lastError = ""
	process.status = ProcessStatusBusy
	process.mu.Unlock()

	// Verify previousError is queryable.
	status = process.Status()
	if status.PreviousError != "exit status 1" {
		t.Errorf("expected previous_error %q, got %q", "exit status 1", status.PreviousError)
	}
	if status.ErrorMessage != "" {
		t.Errorf("expected empty current error, got %q", status.ErrorMessage)
	}
}

func TestRingBuffer(t *testing.T) {
	t.Run("under capacity", func(t *testing.T) {
		rb := newRingBuffer(5)
		rb.add("a")
		rb.add("b")
		got := rb.contents()
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("expected [a b], got %v", got)
		}
	})

	t.Run("at capacity", func(t *testing.T) {
		rb := newRingBuffer(3)
		rb.add("a")
		rb.add("b")
		rb.add("c")
		got := rb.contents()
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Errorf("expected [a b c], got %v", got)
		}
	})

	t.Run("over capacity wraps", func(t *testing.T) {
		rb := newRingBuffer(3)
		rb.add("a")
		rb.add("b")
		rb.add("c")
		rb.add("d") // overwrites "a"
		got := rb.contents()
		if len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "d" {
			t.Errorf("expected [b c d], got %v", got)
		}
	})
}

func TestExitCodeFromError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if got := exitCodeFromError(nil); got != "0" {
			t.Errorf("expected '0', got %q", got)
		}
	})

	t.Run("non-ExitError", func(t *testing.T) {
		err := os.ErrNotExist
		got := exitCodeFromError(err)
		if got != err.Error() {
			t.Errorf("expected %q, got %q", err.Error(), got)
		}
	})
}

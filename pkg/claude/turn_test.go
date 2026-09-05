package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// turnFixture is a Claude Code stream-json transcript of one prompt: the agent
// pushes to an existing PR, looks up its URL, reads an unrelated PR, states
// its conclusion and then ends the turn with a tool call. The CLI therefore
// emits a result message with an empty result field.
const turnFixture = `{"type":"system","subtype":"init","session_id":"sess-turn-1","model":"claude-opus-5"}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"text","text":"Pushing the fix."}],"usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"tool_use","id":"toolu_push","name":"Bash","input":{"command":"git push origin HEAD:renovate/mcp-go-1.x"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_push","content":"To github.com:acme/widgets.git\n   1a2b3c4..5d6e7f8  HEAD -> renovate/mcp-go-1.x","is_error":false}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"tool_use","id":"toolu_view","name":"Bash","input":{"command":"gh pr view 42 --json url -q .url"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_view","content":"https://github.com/acme/widgets/pull/42\n","is_error":false}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"tool_use","id":"toolu_read","name":"Bash","input":{"command":"gh pr view https://github.com/giantswarm/architect-orb/pull/772 --json body -q .body"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"Bumps the executor image, see https://github.com/giantswarm/architect-orb/pull/767","is_error":false}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"text","text":"Pushed the fix to acme/widgets#42; CI is green."}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"tool_use","id":"toolu_wake","name":"ScheduleWakeup","input":{"delaySeconds":600}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_wake","content":"scheduled","is_error":false}]}}
{"type":"result","subtype":"success","is_error":false,"result":"","total_cost_usd":0.42,"usage":{"input_tokens":100,"output_tokens":50},"session_id":"sess-turn-1"}
`

const (
	wantTurnText  = "Pushed the fix to acme/widgets#42; CI is green."
	wantTurnPRURL = "https://github.com/acme/widgets/pull/42"
)

// installFakeClaude puts a `claude` script on PATH that prints the given
// stream-json transcript and exits, so Process runs end to end without the
// real CLI.
func installFakeClaude(t *testing.T, transcript string) {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(fixture, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat \"" + fixture + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func assertTurnDetail(t *testing.T, detail ResultDetailInfo) {
	t.Helper()
	if detail.ResultText != wantTurnText {
		t.Errorf("ResultText = %q, want %q", detail.ResultText, wantTurnText)
	}
	if len(detail.PRURLs) != 1 || detail.PRURLs[0] != wantTurnPRURL {
		t.Errorf("PRURLs = %v, want [%s]", detail.PRURLs, wantTurnPRURL)
	}
	if detail.TotalCost == nil || *detail.TotalCost != 0.42 {
		t.Errorf("TotalCost = %v, want 0.42", detail.TotalCost)
	}
	if detail.SessionID != "sess-turn-1" {
		t.Errorf("SessionID = %q", detail.SessionID)
	}
	if detail.ToolCalls["Bash"] != 3 || detail.ToolCalls["ScheduleWakeup"] != 1 {
		t.Errorf("ToolCalls = %v", detail.ToolCalls)
	}
}

// TestProcess_RunWithOptions_FinalizesTurnResult covers the chat completions
// path (klausctl prompt/run): RunWithOptions without Submit must still leave
// the turn's result in ResultDetail and on disk.
func TestProcess_RunWithOptions_FinalizesTurnResult(t *testing.T) {
	installFakeClaude(t, turnFixture)
	resultDir := t.TempDir()
	p := NewProcess(Options{ResultDir: resultDir})

	ch, err := p.RunWithOptions(context.Background(), "rescue the renovate PR", nil)
	if err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}
	var streamed []StreamMessage
	for msg := range ch {
		streamed = append(streamed, msg)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the run to finish")
	}
	if len(streamed) == 0 {
		t.Fatal("no messages streamed from the fake claude")
	}

	detail := p.ResultDetail()
	assertTurnDetail(t, detail)
	if detail.Status != ProcessStatusIdle {
		t.Errorf("Status = %q, want idle", detail.Status)
	}
	// Turn messages include the synthetic user prompt and the transcript.
	if len(detail.Messages) != len(streamed)+1 {
		t.Errorf("len(Messages) = %d, want %d", len(detail.Messages), len(streamed)+1)
	}

	// The result survives a process restart via the result store.
	waitForPersistedResult(t, resultDir, wantTurnText)
	persisted, err := NewResultStore(resultDir).Load()
	if err != nil || persisted == nil {
		t.Fatalf("persisted result missing: %v", err)
	}
	if len(persisted.PRURLs) != 1 || persisted.PRURLs[0] != wantTurnPRURL {
		t.Errorf("persisted PRURLs = %v", persisted.PRURLs)
	}
}

// TestProcess_Submit_FinalizesTurnResult keeps the MCP prompt tool path
// working: Submit still ends in status completed with the same result.
func TestProcess_Submit_FinalizesTurnResult(t *testing.T) {
	installFakeClaude(t, turnFixture)
	p := NewProcess(Options{ResultDir: t.TempDir()})

	if err := p.Submit(context.Background(), "rescue the renovate PR", nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for p.Status().Status != ProcessStatusCompleted {
		if time.Now().After(deadline) {
			t.Fatalf("status never reached completed, last %q", p.Status().Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	assertTurnDetail(t, p.ResultDetail())
	if got := p.Status().Result; got != wantTurnText {
		t.Errorf("Status().Result = %q, want %q", got, wantTurnText)
	}
}

// TestProcess_RunWithOptions_ClearsPreviousResult verifies a new prompt does
// not report the previous turn's text while it is running.
func TestProcess_RunWithOptions_ClearsPreviousResult(t *testing.T) {
	p := NewProcess(Options{ResultDir: t.TempDir()})
	p.mu.Lock()
	p.result = resultState{text: "old", completed: true}
	p.liveMessages = []StreamMessage{{Type: MessageTypeResult, Result: "old"}}
	p.mu.Unlock()

	// Point "claude" at a script that blocks until stdin closes so the run
	// stays busy while we inspect the state.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\ncat >/dev/null\n"), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ch, err := p.RunWithOptions(context.Background(), "next", nil)
	if err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}
	if got := p.ResultDetail().ResultText; got != "" {
		t.Errorf("ResultText during the new run = %q, want empty", got)
	}
	p.mu.RLock()
	turnStart := p.turnStart
	p.mu.RUnlock()
	if turnStart != 1 {
		t.Errorf("turnStart = %d, want 1 (after the previous turn's messages)", turnStart)
	}
	_ = p.Stop()
	for range ch {
	}
}

// startCatSubprocess launches `cat` as a stand-in for the persistent Claude
// subprocess: everything the test writes to stdin comes back on stdout and is
// parsed by readLoop.
func startCatSubprocess(t *testing.T, p *PersistentProcess) (stdin *os.File, processDone chan struct{}) {
	t.Helper()
	cmd := exec.Command("cat")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = r
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	processDone = make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.stdin = w
	p.processDone = processDone
	p.status = ProcessStatusIdle
	p.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.readLoop(ctx, stdout, cmd, processDone)
	return w, processDone
}

// TestPersistentProcess_RunWithOptions_FinalizesTurnResult covers the chat
// mode: each turn's result is stored when its final result message arrives,
// and a second turn replaces it.
func TestPersistentProcess_RunWithOptions_FinalizesTurnResult(t *testing.T) {
	resultDir := t.TempDir()
	p := NewPersistentProcess(Options{ResultDir: resultDir})
	p.autoRestart = false
	stdin, processDone := startCatSubprocess(t, p)

	runTurn := func(prompt, transcript string) {
		t.Helper()
		ch, err := p.RunWithOptions(context.Background(), prompt, nil)
		if err != nil {
			t.Fatalf("RunWithOptions(%q): %v", prompt, err)
		}
		go func() {
			for range ch {
			}
		}()
		if _, err := stdin.Write([]byte(transcript)); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
		select {
		case <-p.Done():
		case <-time.After(10 * time.Second):
			t.Fatalf("turn %q did not complete", prompt)
		}
	}

	runTurn("rescue the renovate PR", turnFixture)
	detail := p.ResultDetail()
	assertTurnDetail(t, detail)
	if detail.Status != ProcessStatusIdle {
		t.Errorf("Status = %q, want idle", detail.Status)
	}
	// Persistence happens after Done() is signalled; wait for the file.
	waitForPersistedResult(t, resultDir, wantTurnText)

	// Second turn: only its own text and PRs are reported.
	second := `{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"text","text":"Nothing left to do."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Nothing left to do.","total_cost_usd":0.5,"session_id":"sess-turn-1"}
`
	runTurn("anything else?", second)
	detail = p.ResultDetail()
	if detail.ResultText != "Nothing left to do." {
		t.Errorf("second turn ResultText = %q", detail.ResultText)
	}
	if detail.PRURLs != nil {
		t.Errorf("second turn PRURLs = %v, want none", detail.PRURLs)
	}
	// The prompt echoed back by cat plus the two transcript lines.
	if got := len(detail.Messages); got != 4 {
		t.Errorf("second turn len(Messages) = %d, want 4 (synthetic user, echoed user, assistant, result)", got)
	}
	messages := p.Messages()
	if len(messages.Messages) < 5 {
		t.Errorf("Messages() lost the first turn: %d entries", len(messages.Messages))
	}

	_ = stdin.Close()
	select {
	case <-processDone:
	case <-time.After(10 * time.Second):
		t.Fatal("cat did not exit")
	}
}

// waitForPersistedResult polls the result store until it holds a result
// with the given text. The persistent reader persists after signalling
// completion, so tests must not read the store synchronously.
func waitForPersistedResult(t *testing.T, dir, wantText string) {
	t.Helper()
	store := NewResultStore(dir)
	deadline := time.Now().Add(5 * time.Second)
	for {
		pr, err := store.Load()
		if err == nil && pr != nil && pr.ResultText == wantText {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("persisted result = %+v, err %v; want text %q", pr, err, wantText)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTurnResult(t *testing.T) {
	live := []StreamMessage{
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "turn 1"},
		{Type: MessageTypeResult, Result: "turn 1"},
		syntheticUserMessage("second"),
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "turn 2"},
		{Type: MessageTypeResult},
	}
	rs := turnResult(live, 2)
	if rs.text != "turn 2" || !rs.completed || len(rs.messages) != 3 {
		t.Errorf("turnResult = %+v", rs)
	}
	// Out-of-range starts fall back to the whole history.
	if rs := turnResult(live, 99); len(rs.messages) != len(live) {
		t.Errorf("turnResult with bad start kept %d messages", len(rs.messages))
	}
	if rs := turnResult(nil, 0); rs.text != "" || !rs.completed || rs.messages != nil {
		t.Errorf("turnResult(nil) = %+v", rs)
	}
}

func TestSubagentTracker_CompletesOnToolResult(t *testing.T) {
	tracker := newSubagentTracker()
	args, _ := json.Marshal(taskToolArgs{SubagentType: "base:code-reviewer", Description: "Review the fix"})
	tracker.handleToolUse(StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: ToolNameAgent, ToolID: "toolu_agent", ToolArgs: args})
	tracker.handleToolUse(StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: ToolNameAgent, ToolID: "toolu_other", ToolArgs: args})

	// An unrelated tool result leaves both running.
	if tracker.handleMessage(toolResult("toolu_bash", "ok")) {
		t.Error("unrelated tool_result completed a subagent")
	}

	// The Agent tool's result completes exactly the matching dispatch, even
	// when it is not the oldest one.
	report := "Findings: none.\n<usage>\ntotal_tokens: 1200\ntool_uses: 7\nduration_ms: 3400\n</usage>"
	if !tracker.handleMessage(toolResult("toolu_other", report)) {
		t.Fatal("tool_result for an in-flight subagent did not complete it")
	}
	calls := tracker.calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	var done, running int
	for _, c := range calls {
		switch c.Status {
		case SubagentStatusCompleted:
			done++
			if c.ToolID != "toolu_other" || c.Tokens != 1200 || c.ToolCalls != 7 || c.DurationMS != 3400 {
				t.Errorf("completed call = %+v", c)
			}
		case SubagentStatusRunning:
			running++
			if c.ToolID != "toolu_agent" {
				t.Errorf("running call = %+v", c)
			}
		}
	}
	if done != 1 || running != 1 {
		t.Errorf("done=%d running=%d", done, running)
	}

	// A result without a <usage> block still completes the dispatch.
	if !tracker.handleMessage(toolResult("toolu_agent", "Looks good.")) {
		t.Fatal("tool_result without usage block did not complete the subagent")
	}
	for _, c := range tracker.calls() {
		if c.Status != SubagentStatusCompleted {
			t.Errorf("call %s still %s", c.ToolID, c.Status)
		}
	}
	if len(tracker.inflight) != 0 {
		t.Errorf("inflight = %d, want 0", len(tracker.inflight))
	}
}

// TestCollectSubagentCalls_FromTranscript replays a real-shaped transcript
// (Agent dispatch, tool_result with usage) as persistResult does.
func TestCollectSubagentCalls_FromTranscript(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(`{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"tool_use","id":"toolu_agent","name":"Agent","input":{"subagent_type":"base:code-reviewer","description":"Review mcp-go v1 cache fix","prompt":"..."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_agent","content":[{"type":"text","text":"No blocking findings.\n<usage>\ntotal_tokens: 36568\ntool_uses: 29\nduration_ms: 43095\n</usage>"}]}]}}`), "\n")
	var msgs []StreamMessage
	for _, l := range lines {
		m, err := ParseStreamMessage([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, m)
	}
	calls := collectSubagentCalls(msgs)
	if len(calls) != 1 || calls[0].Status != SubagentStatusCompleted || calls[0].Tokens != 36568 || calls[0].Type != "base:code-reviewer" {
		t.Errorf("calls = %+v", calls)
	}
}

package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResultStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	pr := PersistedResult{
		ResultText:   "Task completed successfully",
		MessageCount: 5,
		ToolCalls:    map[string]int{"Bash": 3, "Read": 2},
		TotalCost:    Float64Ptr(0.15),
		SessionID:    "sess-123",
		Status:       ProcessStatusCompleted,
		StopReason:   StopReasonCompleted,
		Timestamp:    time.Now(),
	}

	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil result")
	}

	if loaded.ResultText != pr.ResultText {
		t.Errorf("expected ResultText %q, got %q", pr.ResultText, loaded.ResultText)
	}
	if loaded.MessageCount != pr.MessageCount {
		t.Errorf("expected MessageCount %d, got %d", pr.MessageCount, loaded.MessageCount)
	}
	if loaded.TotalCost == nil || *loaded.TotalCost != *pr.TotalCost {
		t.Errorf("expected TotalCost %v, got %v", pr.TotalCost, loaded.TotalCost)
	}
	if loaded.SessionID != pr.SessionID {
		t.Errorf("expected SessionID %q, got %q", pr.SessionID, loaded.SessionID)
	}
	if loaded.Status != pr.Status {
		t.Errorf("expected Status %q, got %q", pr.Status, loaded.Status)
	}
	if loaded.StopReason != pr.StopReason {
		t.Errorf("expected StopReason %q, got %q", pr.StopReason, loaded.StopReason)
	}
	if loaded.ToolCalls["Bash"] != 3 {
		t.Errorf("expected ToolCalls[Bash]=3, got %d", loaded.ToolCalls["Bash"])
	}
	if loaded.ToolCalls["Read"] != 2 {
		t.Errorf("expected ToolCalls[Read]=2, got %d", loaded.ToolCalls["Read"])
	}
}

func TestResultStore_LoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for non-existent result, got %v", loaded)
	}
}

func TestResultStore_SaveCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "result", "dir")
	store := NewResultStore(dir)

	pr := PersistedResult{
		ResultText: "hello",
		Status:     ProcessStatusCompleted,
		Timestamp:  time.Now(),
	}

	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify directory was created.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestResultStore_SaveOverwritesPrevious(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	first := PersistedResult{
		ResultText: "first result",
		SessionID:  "sess-1",
		Status:     ProcessStatusCompleted,
		Timestamp:  time.Now(),
	}
	if err := store.Save(first); err != nil {
		t.Fatalf("Save first failed: %v", err)
	}

	second := PersistedResult{
		ResultText: "second result",
		SessionID:  "sess-2",
		Status:     ProcessStatusError,
		StopReason: StopReasonError,
		Timestamp:  time.Now(),
	}
	if err := store.Save(second); err != nil {
		t.Fatalf("Save second failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.ResultText != "second result" {
		t.Errorf("expected second result, got %q", loaded.ResultText)
	}
	if loaded.SessionID != "sess-2" {
		t.Errorf("expected sess-2, got %q", loaded.SessionID)
	}
}

func TestResultStore_SaveWithMessages(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	messages := []StreamMessage{
		{Type: MessageTypeSystem, SessionID: "sess-abc"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "Working..."},
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Bash"},
		{Type: MessageTypeResult, Result: "Done!", TotalCost: 0.10},
	}

	pr := PersistedResult{
		ResultText:   "Done!",
		Messages:     messages,
		MessageCount: len(messages),
		SessionID:    "sess-abc",
		TotalCost:    Float64Ptr(0.10),
		Status:       ProcessStatusCompleted,
		StopReason:   StopReasonCompleted,
		Timestamp:    time.Now(),
	}

	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].SessionID != "sess-abc" {
		t.Errorf("expected session_id in first message")
	}
	if loaded.Messages[3].Result != "Done!" {
		t.Errorf("expected result in last message")
	}
}

func TestResultStore_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	// Write corrupt data.
	path := filepath.Join(dir, resultFileName)
	if err := os.WriteFile(path, []byte("not json"), 0o640); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	loaded, err := store.Load()
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}
	if loaded != nil {
		t.Errorf("expected nil result for corrupt file")
	}
}

func TestPersistedResult_ToResultDetailInfo(t *testing.T) {
	pr := PersistedResult{
		ResultText:   "Full result",
		MessageCount: 3,
		ToolCalls:    map[string]int{"Bash": 2},
		TotalCost:    Float64Ptr(0.25),
		SessionID:    "sess-456",
		Status:       ProcessStatusCompleted,
		ErrorMessage: "",
	}

	detail := pr.ToResultDetailInfo()

	if detail.ResultText != "Full result" {
		t.Errorf("expected ResultText %q, got %q", "Full result", detail.ResultText)
	}
	if detail.MessageCount != 3 {
		t.Errorf("expected MessageCount 3, got %d", detail.MessageCount)
	}
	if detail.SessionID != "sess-456" {
		t.Errorf("expected SessionID %q, got %q", "sess-456", detail.SessionID)
	}
	if detail.TotalCost == nil || *detail.TotalCost != 0.25 {
		t.Errorf("expected TotalCost 0.25, got %v", detail.TotalCost)
	}
}

func TestResultStorePath_EnvVar(t *testing.T) {
	t.Setenv("KLAUS_RESULT_DIR", "/custom/result/dir")
	got := ResultStorePath("/workspace")
	if got != "/custom/result/dir" {
		t.Errorf("ResultStorePath with env = %q, want %q", got, "/custom/result/dir")
	}
}

func TestResultStorePath_DefaultHome(t *testing.T) {
	// Ensure the env var is unset so the home-based default is used.
	t.Setenv("KLAUS_RESULT_DIR", "")

	got := ResultStorePath("/workspace")
	// The result should be under $HOME/.klaus/results/, not inside /workspace.
	if got == "/workspace/.klaus" {
		t.Errorf("ResultStorePath should NOT return workspace-relative path, got %q", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("ResultStorePath should return an absolute path, got %q", got)
	}
}

func Test_resultStorePath(t *testing.T) {
	tests := []struct {
		name    string
		getenv  func(string) string
		homeDir func() (string, error)
		want    string
	}{
		{
			name:    "env var takes precedence",
			getenv:  func(k string) string { return "/env/result/dir" },
			homeDir: func() (string, error) { return "/home/user", nil },
			want:    "/env/result/dir",
		},
		{
			name:    "home directory default",
			getenv:  func(string) string { return "" },
			homeDir: func() (string, error) { return "/home/user", nil },
			want:    "/home/user/.klaus/results",
		},
		{
			name:    "home dir error falls back to tmp",
			getenv:  func(string) string { return "" },
			homeDir: func() (string, error) { return "", fmt.Errorf("no home") },
			want:    filepath.Join(os.TempDir(), "klaus-results"),
		},
		{
			name:    "empty home falls back to tmp",
			getenv:  func(string) string { return "" },
			homeDir: func() (string, error) { return "", nil },
			want:    filepath.Join(os.TempDir(), "klaus-results"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resultStorePath("", tt.getenv, tt.homeDir)
			if got != tt.want {
				t.Errorf("resultStorePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_resultStoreDir(t *testing.T) {
	t.Run("explicit ResultDir takes precedence", func(t *testing.T) {
		opts := Options{
			WorkDir:   "/workspace",
			ResultDir: "/explicit/results",
		}
		got := resultStoreDir(opts)
		if got != "/explicit/results" {
			t.Errorf("resultStoreDir() = %q, want %q", got, "/explicit/results")
		}
	})

	t.Run("falls back to ResultStorePath when ResultDir empty", func(t *testing.T) {
		opts := Options{WorkDir: "/workspace"}
		got := resultStoreDir(opts)
		expected := ResultStorePath("/workspace")
		if got != expected {
			t.Errorf("resultStoreDir() = %q, want %q", got, expected)
		}
	})
}

func TestStopReasonFromStatus(t *testing.T) {
	tests := []struct {
		status   ProcessStatus
		expected StopReason
	}{
		{ProcessStatusCompleted, StopReasonCompleted},
		{ProcessStatusIdle, StopReasonCompleted},
		{ProcessStatusStopped, StopReasonStopped},
		{ProcessStatusError, StopReasonError},
		{ProcessStatusBusy, StopReasonCompleted},
	}

	for _, tt := range tests {
		got := stopReasonFromStatus(tt.status)
		if got != tt.expected {
			t.Errorf("stopReasonFromStatus(%q) = %q, want %q", tt.status, got, tt.expected)
		}
	}
}

func TestCollectToolCalls(t *testing.T) {
	messages := []StreamMessage{
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Bash"},
		{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "hello"},
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Bash"},
		{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Read"},
		{Type: MessageTypeResult, Result: "done"},
	}

	calls := collectToolCalls(messages)
	if calls["Bash"] != 2 {
		t.Errorf("expected Bash=2, got %d", calls["Bash"])
	}
	if calls["Read"] != 1 {
		t.Errorf("expected Read=1, got %d", calls["Read"])
	}
}

func TestCollectToolCalls_Empty(t *testing.T) {
	calls := collectToolCalls(nil)
	if calls != nil {
		t.Errorf("expected nil for empty messages, got %v", calls)
	}
}

func TestPersistResult_NilStore(t *testing.T) {
	// Should not panic with nil store.
	rs := resultState{text: "result", completed: true}
	persistResult(nil, rs, ProcessStatusCompleted, "sess", Float64Ptr(0.1), "", nil)
}

func TestPersistResult_IncompleteResult(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	// Incomplete result should not be persisted.
	rs := resultState{text: "partial", completed: false}
	persistResult(store, rs, ProcessStatusBusy, "sess", nil, "", nil)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for incomplete result, got %v", loaded)
	}
}

func TestPersistResult_CompletedResult(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	rs := resultState{
		text:      "final result",
		messages:  []StreamMessage{{Type: MessageTypeResult, Result: "final result"}},
		completed: true,
	}
	persistResult(store, rs, ProcessStatusCompleted, "sess-abc", Float64Ptr(0.50), "", nil)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil result")
	}
	if loaded.ResultText != "final result" {
		t.Errorf("expected result %q, got %q", "final result", loaded.ResultText)
	}
	if loaded.SessionID != "sess-abc" {
		t.Errorf("expected session_id %q, got %q", "sess-abc", loaded.SessionID)
	}
	if loaded.TotalCost == nil || *loaded.TotalCost != 0.50 {
		t.Errorf("expected total_cost 0.50, got %v", loaded.TotalCost)
	}
	if loaded.StopReason != StopReasonCompleted {
		t.Errorf("expected stop_reason %q, got %q", StopReasonCompleted, loaded.StopReason)
	}
}

// TestProcess_StatusFallbackToDisk verifies that Status() falls back to
// the persisted result when in-memory state is empty.
func TestProcess_StatusFallbackToDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	// Pre-populate disk with a result.
	pr := PersistedResult{
		ResultText: "persisted result text",
		SessionID:  "sess-persisted",
		TotalCost:  Float64Ptr(1.23),
		Status:     ProcessStatusCompleted,
		StopReason: StopReasonCompleted,
		Timestamp:  time.Now(),
	}
	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Create a process with empty in-memory state but the same store.
	p := NewProcess(Options{WorkDir: dir})
	// Override the result store to point at our test dir.
	p.resultStore = store

	status := p.Status()
	if status.Result != "persisted result text" {
		t.Errorf("expected fallback result %q, got %q", "persisted result text", status.Result)
	}
	if status.SessionID != "sess-persisted" {
		t.Errorf("expected fallback session_id %q, got %q", "sess-persisted", status.SessionID)
	}
	if status.TotalCost == nil || *status.TotalCost != 1.23 {
		t.Errorf("expected fallback total_cost 1.23, got %v", status.TotalCost)
	}
}

// TestProcess_ResultDetailFallbackToDisk verifies that ResultDetail()
// falls back to the persisted result when in-memory state is empty.
func TestProcess_ResultDetailFallbackToDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	pr := PersistedResult{
		ResultText:   "full persisted result",
		MessageCount: 10,
		SessionID:    "sess-detail",
		TotalCost:    Float64Ptr(2.50),
		Status:       ProcessStatusCompleted,
		Timestamp:    time.Now(),
	}
	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	p := NewProcess(Options{WorkDir: dir})
	p.resultStore = store

	detail := p.ResultDetail()
	if detail.ResultText != "full persisted result" {
		t.Errorf("expected fallback result_text %q, got %q", "full persisted result", detail.ResultText)
	}
	if detail.SessionID != "sess-detail" {
		t.Errorf("expected fallback session_id %q, got %q", "sess-detail", detail.SessionID)
	}
	if detail.TotalCost == nil || *detail.TotalCost != 2.50 {
		t.Errorf("expected fallback total_cost 2.50, got %v", detail.TotalCost)
	}
}

// TestProcess_InMemoryResultTakesPrecedence verifies that in-memory
// results are preferred over disk results.
func TestProcess_InMemoryResultTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	// Save an old result to disk.
	pr := PersistedResult{
		ResultText: "old persisted result",
		SessionID:  "sess-old",
		Status:     ProcessStatusCompleted,
		Timestamp:  time.Now(),
	}
	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	p := NewProcess(Options{WorkDir: dir})
	p.resultStore = store

	// Set in-memory result.
	p.mu.Lock()
	p.result = resultState{text: "fresh in-memory result", completed: true}
	p.status = ProcessStatusCompleted
	p.sessionID = "sess-fresh"
	p.mu.Unlock()

	status := p.Status()
	if status.Result != "fresh in-memory result" {
		t.Errorf("expected in-memory result %q, got %q", "fresh in-memory result", status.Result)
	}
	if status.SessionID != "sess-fresh" {
		t.Errorf("expected in-memory session_id %q, got %q", "sess-fresh", status.SessionID)
	}

	detail := p.ResultDetail()
	if detail.ResultText != "fresh in-memory result" {
		t.Errorf("expected in-memory result_text %q, got %q", "fresh in-memory result", detail.ResultText)
	}
}

func TestPersistResult_WithTokenUsage(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	rs := resultState{
		text:      "result with tokens",
		messages:  []StreamMessage{{Type: MessageTypeResult, Result: "result with tokens"}},
		completed: true,
	}
	tu := &TokenUsage{
		InputTokens:              10000,
		OutputTokens:             2000,
		CacheCreationInputTokens: 5000,
		CacheReadInputTokens:     3000,
	}
	persistResult(store, rs, ProcessStatusCompleted, "sess-tu", Float64Ptr(0.42), "", tu)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil result")
	}
	if loaded.TokenUsage == nil {
		t.Fatal("expected non-nil TokenUsage")
	}
	if loaded.TokenUsage.InputTokens != 10000 {
		t.Errorf("expected InputTokens 10000, got %d", loaded.TokenUsage.InputTokens)
	}
	if loaded.TokenUsage.OutputTokens != 2000 {
		t.Errorf("expected OutputTokens 2000, got %d", loaded.TokenUsage.OutputTokens)
	}
	if loaded.TokenUsage.CacheCreationInputTokens != 5000 {
		t.Errorf("expected CacheCreationInputTokens 5000, got %d", loaded.TokenUsage.CacheCreationInputTokens)
	}
	if loaded.TokenUsage.CacheReadInputTokens != 3000 {
		t.Errorf("expected CacheReadInputTokens 3000, got %d", loaded.TokenUsage.CacheReadInputTokens)
	}
}

func TestPersistResult_NilCost(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	rs := resultState{
		text:      "result without cost",
		completed: true,
	}
	persistResult(store, rs, ProcessStatusCompleted, "sess", nil, "", nil)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.TotalCost != nil {
		t.Errorf("expected nil TotalCost when not observed, got %v", loaded.TotalCost)
	}
}

func TestPersistedResult_ToResultDetailInfo_WithTokenUsage(t *testing.T) {
	pr := PersistedResult{
		ResultText:   "Full result",
		MessageCount: 3,
		TotalCost:    Float64Ptr(0.25),
		TokenUsage: &TokenUsage{
			InputTokens:  500,
			OutputTokens: 100,
		},
		SessionID: "sess-456",
		Status:    ProcessStatusCompleted,
	}

	detail := pr.ToResultDetailInfo()
	if detail.TokenUsage == nil {
		t.Fatal("expected non-nil TokenUsage in ResultDetailInfo")
	}
	if detail.TokenUsage.InputTokens != 500 {
		t.Errorf("expected InputTokens 500, got %d", detail.TokenUsage.InputTokens)
	}
}

func TestPersistedResult_ToResultDetailInfo_NilCost(t *testing.T) {
	pr := PersistedResult{
		ResultText: "Result",
		TotalCost:  nil,
		Status:     ProcessStatusCompleted,
	}

	detail := pr.ToResultDetailInfo()
	if detail.TotalCost != nil {
		t.Errorf("expected nil TotalCost, got %v", detail.TotalCost)
	}
}

func TestResultStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewResultStore(dir)

	pr := PersistedResult{
		ResultText: "test atomicity",
		Status:     ProcessStatusCompleted,
		Timestamp:  time.Now(),
	}

	if err := store.Save(pr); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify no .tmp file is left behind.
	tmpPath := filepath.Join(dir, resultFileName+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be cleaned up, but it exists")
	}

	// Verify the result file exists.
	path := filepath.Join(dir, resultFileName)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected result file to exist: %v", err)
	}
}

package claude

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultResultDir is the subdirectory under the workspace where
	// persisted results are stored.
	DefaultResultDir = ".klaus"

	// resultFileName is the name of the persisted result file.
	resultFileName = "last-result.json"
)

// StopReason indicates why the agent stopped.
type StopReason string

const (
	StopReasonCompleted StopReason = "completed"
	StopReasonBudget    StopReason = "budget"
	StopReasonError     StopReason = "error"
	StopReasonStopped   StopReason = "stopped"
)

// PersistedResult is the on-disk representation of a session result.
// It extends ResultDetailInfo with metadata for post-mortem retrieval.
type PersistedResult struct {
	ResultText    string          `json:"result_text"`
	Messages      []StreamMessage `json:"messages,omitempty"`
	MessageCount  int             `json:"message_count"`
	ToolCalls     map[string]int  `json:"tool_calls,omitempty"`
	SubagentCalls []SubagentCall  `json:"subagent_calls,omitempty"`
	TotalCost     float64         `json:"total_cost_usd,omitempty"`
	SessionID     string          `json:"session_id,omitempty"`
	Status        ProcessStatus   `json:"status"`
	ErrorMessage  string          `json:"error,omitempty"`
	StopReason    StopReason      `json:"stop_reason,omitempty"`
	Timestamp     time.Time       `json:"timestamp"`
}

// ToResultDetailInfo converts a PersistedResult back to a ResultDetailInfo.
func (pr *PersistedResult) ToResultDetailInfo() ResultDetailInfo {
	return ResultDetailInfo{
		ResultText:    pr.ResultText,
		Messages:      pr.Messages,
		MessageCount:  pr.MessageCount,
		ToolCalls:     pr.ToolCalls,
		SubagentCalls: copySubagentCalls(pr.SubagentCalls),
		TotalCost:     pr.TotalCost,
		SessionID:     pr.SessionID,
		Status:        pr.Status,
		ErrorMessage:  pr.ErrorMessage,
	}
}

// ResultStore persists session results to disk so they survive process
// restarts. It writes JSON files to a well-known directory.
type ResultStore struct {
	dir string
}

// NewResultStore creates a ResultStore that writes to the given directory.
// The directory is created on first Save if it doesn't exist.
func NewResultStore(dir string) *ResultStore {
	return &ResultStore{dir: dir}
}

// ResultStorePath returns the default result store directory for a workspace.
func ResultStorePath(workDir string) string {
	if workDir == "" {
		workDir = "."
	}
	return filepath.Join(workDir, DefaultResultDir)
}

// Save persists a result to disk. It creates the directory if needed.
// Save is not safe for concurrent use; callers must ensure single-writer
// semantics (which is naturally provided by the drain goroutine pattern).
func (s *ResultStore) Save(result PersistedResult) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating result store directory: %w", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}

	path := filepath.Join(s.dir, resultFileName)

	// Write atomically via temp file + rename to avoid partial reads.
	// Use CreateTemp for an unpredictable filename to prevent symlink attacks.
	tmp, err := os.CreateTemp(s.dir, ".last-result-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing result file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("setting file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming result file: %w", err)
	}

	return nil
}

// Load reads the persisted result from disk. Returns nil if no persisted
// result exists. Returns an error for I/O or parse failures.
func (s *ResultStore) Load() (*PersistedResult, error) {
	path := filepath.Join(s.dir, resultFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading result file: %w", err)
	}

	var result PersistedResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing result file: %w", err)
	}

	return &result, nil
}

// persistResult is a helper called from the setResult callbacks in both
// Process and PersistentProcess. It saves the result to disk if a store
// is configured and the result is complete.
func persistResult(store *ResultStore, rs resultState, status ProcessStatus, sessionID string, totalCost float64, lastError string) {
	if store == nil || !rs.completed {
		return
	}

	reason := stopReasonFromStatus(status)

	pr := PersistedResult{
		ResultText:    rs.text,
		Messages:      rs.messages,
		MessageCount:  len(rs.messages),
		ToolCalls:     collectToolCalls(rs.messages),
		SubagentCalls: collectSubagentCalls(rs.messages),
		TotalCost:     totalCost,
		SessionID:     sessionID,
		Status:        status,
		ErrorMessage:  lastError,
		StopReason:    reason,
		Timestamp:     time.Now(),
	}

	if err := store.Save(pr); err != nil {
		log.Printf("[klaus] failed to persist result: %v", err)
	}
}

// stopReasonFromStatus maps a process status to a stop reason.
func stopReasonFromStatus(status ProcessStatus) StopReason {
	switch status {
	case ProcessStatusCompleted, ProcessStatusIdle:
		return StopReasonCompleted
	case ProcessStatusStopped:
		return StopReasonStopped
	case ProcessStatusError:
		return StopReasonError
	default:
		return StopReasonCompleted
	}
}

// collectToolCalls extracts tool call counts from a set of messages.
func collectToolCalls(messages []StreamMessage) map[string]int {
	calls := make(map[string]int)
	for _, msg := range messages {
		if msg.Type == MessageTypeAssistant && msg.Subtype == SubtypeToolUse {
			calls[msg.ToolName]++
		}
	}
	if len(calls) == 0 {
		return nil
	}
	return calls
}

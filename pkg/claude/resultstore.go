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
	// defaultResultSubdir is the subdirectory name used under the user's
	// home directory for persisting session results (e.g. $HOME/.klaus/results/).
	defaultResultSubdir = ".klaus/results"

	// resultFileName is the name of the persisted result file.
	resultFileName = "last-result.json"

	// resultDirEnvVar is the environment variable that overrides the default
	// result store directory.
	resultDirEnvVar = "KLAUS_RESULT_DIR"
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
	ModelUsage    map[string]int  `json:"model_usage,omitempty"`
	PRURLs        []string        `json:"pr_urls,omitempty"`
	ErrorCount    int             `json:"error_count,omitempty"`
	TokenUsage    *TokenUsage     `json:"token_usage,omitempty"`
	TotalCost     *float64        `json:"total_cost_usd"`
	SessionID     string          `json:"session_id,omitempty"`
	Status        ProcessStatus   `json:"status"`
	ErrorMessage  string          `json:"error,omitempty"`
	StopReason    StopReason      `json:"stop_reason,omitempty"`
	Timestamp     time.Time       `json:"timestamp"`
}

// ToResultDetailInfo converts a PersistedResult back to a ResultDetailInfo.
func (pr *PersistedResult) ToResultDetailInfo() ResultDetailInfo {
	info := ResultDetailInfo{
		ResultText:    pr.ResultText,
		Messages:      pr.Messages,
		MessageCount:  pr.MessageCount,
		ToolCalls:     pr.ToolCalls,
		SubagentCalls: copySubagentCalls(pr.SubagentCalls),
		ModelUsage:    copyToolCalls(pr.ModelUsage),
		PRURLs:        copyStringSlice(pr.PRURLs),
		ErrorCount:    pr.ErrorCount,
		TotalCost:     pr.TotalCost,
		SessionID:     pr.SessionID,
		Status:        pr.Status,
		ErrorMessage:  pr.ErrorMessage,
	}
	if pr.TokenUsage != nil {
		tu := *pr.TokenUsage
		info.TokenUsage = &tu
	}
	return info
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

// ResultStorePath returns the default result store directory based on the
// environment. It checks (in order):
//  1. The KLAUS_RESULT_DIR environment variable (must be absolute)
//  2. $HOME/.klaus/results/
//  3. A UID-scoped directory under os.TempDir() as a last resort
//
// Note: Options.ResultDir is handled by resultStoreDir, not this function.
func ResultStorePath() string {
	return resultStorePath(os.Getenv, os.UserHomeDir)
}

// resultStorePath is the testable inner implementation of ResultStorePath.
func resultStorePath(getenv func(string) string, homeDir func() (string, error)) string {
	if dir := getenv(resultDirEnvVar); dir != "" {
		dir = filepath.Clean(dir)
		if filepath.IsAbs(dir) {
			return dir
		}
		log.Printf("[klaus] WARNING: %s=%q is not an absolute path, ignoring", resultDirEnvVar, dir)
	}
	if home, err := homeDir(); err == nil && home != "" {
		return filepath.Join(home, defaultResultSubdir)
	}
	// Last resort: fall back to /tmp so we never write inside a workspace.
	// Include UID to prevent other users from pre-creating the directory.
	return filepath.Join(os.TempDir(), fmt.Sprintf("klaus-results-%d", os.Getuid()))
}

// resultStoreDir returns the result store directory for the given options.
// It uses Options.ResultDir if set (and absolute), otherwise falls back to
// ResultStorePath.
func resultStoreDir(opts Options) string {
	if opts.ResultDir != "" {
		cleaned := filepath.Clean(opts.ResultDir)
		if filepath.IsAbs(cleaned) {
			return cleaned
		}
		log.Printf("[klaus] WARNING: ResultDir %q is not an absolute path, falling back to default", opts.ResultDir)
	}
	return ResultStorePath()
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
func persistResult(store *ResultStore, rs resultState, status ProcessStatus, sessionID string, totalCost *float64, lastError string, tokenUsage *TokenUsage) {
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
		ModelUsage:    CollectModelUsage(rs.messages),
		PRURLs:        CollectPRURLs(rs.messages),
		ErrorCount:    CollectErrorCount(rs.messages),
		TotalCost:     totalCost,
		TokenUsage:    tokenUsage,
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

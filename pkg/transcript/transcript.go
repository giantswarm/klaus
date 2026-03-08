package transcript

import (
	"github.com/giantswarm/klaus/pkg/claude"
)

// maxErrorTextLen is the maximum number of runes kept per error message.
const maxErrorTextLen = 200

// PluginCompliance reports whether the required review plugins were dispatched
// and completed successfully.
type PluginCompliance struct {
	CodeReviewer    bool `json:"code_reviewer"`
	SecurityAuditor bool `json:"security_auditor"`
}

// TranscriptSummary aggregates all analysis results from a completed session.
// It covers the same sections as parse-transcript.py: session metadata,
// message distribution, tool/model/token usage, PR URLs, subagent dispatches,
// errors, and plugin compliance.
type TranscriptSummary struct {
	// Session metadata.
	SessionID    string               `json:"session_id,omitempty"`
	Status       claude.ProcessStatus `json:"status"`
	MessageCount int                  `json:"message_count"`
	TotalCost    *float64             `json:"total_cost_usd"`
	ErrorMessage string               `json:"error,omitempty"`

	// Result summary.
	ResultText string `json:"result_text"`

	// Message type distribution (type -> count).
	MessageTypes map[string]int `json:"message_types,omitempty"`

	// Tool usage (tool name -> count).
	ToolCalls map[string]int `json:"tool_calls,omitempty"`

	// Model usage (model name -> count).
	ModelUsage map[string]int `json:"model_usage,omitempty"`

	// Token usage.
	TokenUsage *claude.TokenUsage `json:"token_usage,omitempty"`

	// GitHub PR URLs extracted from tool results.
	PRURLs []string `json:"pr_urls,omitempty"`

	// Subagent dispatches.
	SubagentCalls []claude.SubagentCall `json:"subagent_calls,omitempty"`

	// Error count and truncated error texts.
	ErrorCount int      `json:"error_count,omitempty"`
	ErrorTexts []string `json:"error_texts,omitempty"`

	// Plugin compliance check.
	PluginCompliance PluginCompliance `json:"plugin_compliance"`
}

// Summarize computes a TranscriptSummary from a ResultDetailInfo.
// It works for both live and archived (persisted) results.
func Summarize(detail claude.ResultDetailInfo) TranscriptSummary {
	summary := TranscriptSummary{
		SessionID:    detail.SessionID,
		Status:       detail.Status,
		MessageCount: detail.MessageCount,
		TotalCost:    detail.TotalCost,
		ErrorMessage: detail.ErrorMessage,
		ResultText:   detail.ResultText,
		ToolCalls:    copyStringIntMap(detail.ToolCalls),
		ModelUsage:   copyStringIntMap(detail.ModelUsage),
		PRURLs:       copyStrings(detail.PRURLs),
		ErrorCount:   detail.ErrorCount,
	}

	if detail.TokenUsage != nil {
		tu := *detail.TokenUsage
		summary.TokenUsage = &tu
	}

	// Copy subagent calls.
	if len(detail.SubagentCalls) > 0 {
		summary.SubagentCalls = make([]claude.SubagentCall, len(detail.SubagentCalls))
		copy(summary.SubagentCalls, detail.SubagentCalls)
	}

	// Compute message type distribution from raw messages.
	summary.MessageTypes = collectMessageTypes(detail.Messages)

	// Extract error texts from tool_result blocks.
	summary.ErrorTexts = collectErrorTexts(detail.Messages)

	// Check plugin compliance.
	summary.PluginCompliance = checkPluginCompliance(detail.SubagentCalls)

	return summary
}

// collectMessageTypes counts messages by type.
func collectMessageTypes(messages []claude.StreamMessage) map[string]int {
	if len(messages) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, msg := range messages {
		counts[string(msg.Type)]++
	}
	return counts
}

// maxErrorTexts caps the number of error text entries collected.
const maxErrorTexts = 100

// collectErrorTexts extracts truncated error messages from tool_result blocks.
func collectErrorTexts(messages []claude.StreamMessage) []string {
	var texts []string
	for _, msg := range messages {
		if len(texts) >= maxErrorTexts {
			break
		}
		blocks := claude.ExtractToolResults(msg)
		for _, block := range blocks {
			if block.IsError && block.Content != "" {
				texts = append(texts, claude.Truncate(block.Content, maxErrorTextLen))
				if len(texts) >= maxErrorTexts {
					break
				}
			}
		}
	}
	return texts
}

// checkPluginCompliance verifies that the required review plugins were dispatched
// and completed. Only calls with Status "completed" count toward compliance.
func checkPluginCompliance(calls []claude.SubagentCall) PluginCompliance {
	completed := make(map[string]bool)
	for _, call := range calls {
		if call.Status == "completed" {
			completed[call.Type] = true
		}
	}
	return PluginCompliance{
		CodeReviewer:    completed["base:code-reviewer"],
		SecurityAuditor: completed["code-quality:security-auditor"],
	}
}

// copyStringIntMap returns a shallow copy of the map. Returns nil for nil input.
func copyStringIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	cp := make(map[string]int, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// copyStrings returns a copy of the slice. Returns nil for nil/empty input.
func copyStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}

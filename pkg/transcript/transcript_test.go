package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/giantswarm/klaus/pkg/claude"
)

func TestSummarize(t *testing.T) {
	cost := 0.42
	tu := &claude.TokenUsage{
		InputTokens:  1000,
		OutputTokens: 500,
	}
	detail := claude.ResultDetailInfo{
		ResultText:   "All done",
		MessageCount: 5,
		SessionID:    "sess-123",
		Status:       claude.ProcessStatusCompleted,
		TotalCost:    &cost,
		TokenUsage:   tu,
		ToolCalls:    map[string]int{"Read": 3, "Write": 1},
		ModelUsage:   map[string]int{"claude-sonnet-4-20250514": 4},
		PRURLs:       []string{"https://github.com/owner/repo/pull/42"},
		ErrorCount:   1,
		SubagentCalls: []claude.SubagentCall{
			{Type: "base:code-reviewer", Status: "completed"},
			{Type: "code-quality:security-auditor", Status: "completed"},
		},
		Messages: []claude.StreamMessage{
			{Type: claude.MessageTypeSystem},
			{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeText},
			{Type: claude.MessageTypeAssistant, Subtype: claude.SubtypeToolUse},
			{
				Type:    "user",
				Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"error happened","is_error":true}]}`),
			},
			{Type: claude.MessageTypeResult},
		},
	}

	summary := Summarize(detail)

	if summary.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want %q", summary.SessionID, "sess-123")
	}
	if summary.Status != claude.ProcessStatusCompleted {
		t.Errorf("Status = %q", summary.Status)
	}
	if summary.MessageCount != 5 {
		t.Errorf("MessageCount = %d, want 5", summary.MessageCount)
	}
	if summary.TotalCost == nil || *summary.TotalCost != 0.42 {
		t.Errorf("TotalCost = %v", summary.TotalCost)
	}
	if summary.ResultText != "All done" {
		t.Errorf("ResultText = %q", summary.ResultText)
	}
	if summary.TokenUsage == nil || summary.TokenUsage.InputTokens != 1000 {
		t.Errorf("TokenUsage = %v", summary.TokenUsage)
	}
	if summary.ToolCalls["Read"] != 3 {
		t.Errorf("ToolCalls[Read] = %d", summary.ToolCalls["Read"])
	}
	if summary.ModelUsage["claude-sonnet-4-20250514"] != 4 {
		t.Errorf("ModelUsage = %v", summary.ModelUsage)
	}
	if len(summary.PRURLs) != 1 || summary.PRURLs[0] != "https://github.com/owner/repo/pull/42" {
		t.Errorf("PRURLs = %v", summary.PRURLs)
	}
	if summary.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d", summary.ErrorCount)
	}

	// Message type distribution.
	if summary.MessageTypes["system"] != 1 {
		t.Errorf("MessageTypes[system] = %d", summary.MessageTypes["system"])
	}
	if summary.MessageTypes["assistant"] != 2 {
		t.Errorf("MessageTypes[assistant] = %d", summary.MessageTypes["assistant"])
	}
	if summary.MessageTypes["result"] != 1 {
		t.Errorf("MessageTypes[result] = %d", summary.MessageTypes["result"])
	}

	// Error texts.
	if len(summary.ErrorTexts) != 1 || summary.ErrorTexts[0] != "error happened" {
		t.Errorf("ErrorTexts = %v", summary.ErrorTexts)
	}

	// Plugin compliance.
	if !summary.PluginCompliance.CodeReviewer {
		t.Error("expected CodeReviewer = true")
	}
	if !summary.PluginCompliance.SecurityAuditor {
		t.Error("expected SecurityAuditor = true")
	}
}

func TestSummarize_Empty(t *testing.T) {
	summary := Summarize(claude.ResultDetailInfo{})

	if summary.MessageTypes != nil {
		t.Errorf("expected nil MessageTypes, got %v", summary.MessageTypes)
	}
	if summary.ErrorTexts != nil {
		t.Errorf("expected nil ErrorTexts, got %v", summary.ErrorTexts)
	}
	if summary.PluginCompliance.CodeReviewer {
		t.Error("expected CodeReviewer = false")
	}
	if summary.PluginCompliance.SecurityAuditor {
		t.Error("expected SecurityAuditor = false")
	}
}

func TestErrorTextTruncation(t *testing.T) {
	longError := strings.Repeat("x", 300)
	messages := []claude.StreamMessage{
		{
			Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"` + longError + `","is_error":true}]}`),
		},
	}

	texts := collectErrorTexts(messages)
	if len(texts) != 1 {
		t.Fatalf("expected 1 error text, got %d", len(texts))
	}
	if len([]rune(texts[0])) > maxErrorTextLen+3 { // +3 for "..."
		t.Errorf("error text not truncated: %d runes", len([]rune(texts[0])))
	}
	if !strings.HasSuffix(texts[0], "...") {
		t.Error("expected truncated text to end with ...")
	}
}

func TestPluginCompliance_Partial(t *testing.T) {
	calls := []claude.SubagentCall{
		{Type: "base:code-reviewer", Status: "completed"},
	}
	pc := checkPluginCompliance(calls)
	if !pc.CodeReviewer {
		t.Error("expected CodeReviewer = true")
	}
	if pc.SecurityAuditor {
		t.Error("expected SecurityAuditor = false")
	}
}

func TestPluginCompliance_None(t *testing.T) {
	pc := checkPluginCompliance(nil)
	if pc.CodeReviewer || pc.SecurityAuditor {
		t.Error("expected both false for nil input")
	}
}

func TestSummarize_DefensiveCopies(t *testing.T) {
	detail := claude.ResultDetailInfo{
		ToolCalls:  map[string]int{"Read": 1},
		ModelUsage: map[string]int{"model-a": 2},
		PRURLs:     []string{"https://github.com/a/b/pull/1"},
	}

	summary := Summarize(detail)

	// Mutate the summary copies and verify originals are unaffected.
	summary.ToolCalls["Read"] = 99
	if detail.ToolCalls["Read"] != 1 {
		t.Error("ToolCalls mutation leaked to original")
	}
	summary.ModelUsage["model-a"] = 99
	if detail.ModelUsage["model-a"] != 2 {
		t.Error("ModelUsage mutation leaked to original")
	}
	summary.PRURLs[0] = "mutated"
	if detail.PRURLs[0] != "https://github.com/a/b/pull/1" {
		t.Error("PRURLs mutation leaked to original")
	}
}

func TestSummarize_JSONRoundTrip(t *testing.T) {
	cost := 1.23
	detail := claude.ResultDetailInfo{
		ResultText:   "done",
		MessageCount: 3,
		SessionID:    "s1",
		Status:       claude.ProcessStatusCompleted,
		TotalCost:    &cost,
		ModelUsage:   map[string]int{"m1": 2},
		PRURLs:       []string{"https://github.com/a/b/pull/1"},
		ErrorCount:   1,
	}

	summary := Summarize(detail)

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded TranscriptSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.SessionID != summary.SessionID {
		t.Errorf("SessionID mismatch after round-trip")
	}
	if decoded.ModelUsage["m1"] != 2 {
		t.Errorf("ModelUsage mismatch after round-trip")
	}
	if len(decoded.PRURLs) != 1 {
		t.Errorf("PRURLs mismatch after round-trip")
	}
	if decoded.ErrorCount != 1 {
		t.Errorf("ErrorCount mismatch after round-trip")
	}
	if !decoded.PluginCompliance.CodeReviewer == summary.PluginCompliance.CodeReviewer {
		t.Errorf("PluginCompliance mismatch after round-trip")
	}
}

package claude

import (
	"encoding/json"
	"testing"
)

func TestExtractModel(t *testing.T) {
	tests := []struct {
		name string
		msg  StreamMessage
		want string
	}{
		{
			name: "assistant message with model",
			msg: StreamMessage{
				Type:    MessageTypeAssistant,
				Message: json.RawMessage(`{"model":"claude-sonnet-4-20250514","content":[]}`),
			},
			want: "claude-sonnet-4-20250514",
		},
		{
			name: "empty message",
			msg:  StreamMessage{Type: MessageTypeAssistant},
			want: "",
		},
		{
			name: "invalid JSON",
			msg: StreamMessage{
				Type:    MessageTypeAssistant,
				Message: json.RawMessage(`{invalid`),
			},
			want: "",
		},
		{
			name: "no model field",
			msg: StreamMessage{
				Type:    MessageTypeAssistant,
				Message: json.RawMessage(`{"content":[]}`),
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractModel(tt.msg)
			if got != tt.want {
				t.Errorf("ExtractModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractToolResults(t *testing.T) {
	tests := []struct {
		name      string
		msg       StreamMessage
		wantCount int
		wantError bool
	}{
		{
			name: "single tool_result",
			msg: StreamMessage{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"ok","is_error":false}]}`),
			},
			wantCount: 1,
		},
		{
			name: "tool_result with error",
			msg: StreamMessage{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"failed","is_error":true}]}`),
			},
			wantCount: 1,
			wantError: true,
		},
		{
			name: "mixed content blocks",
			msg: StreamMessage{
				Message: json.RawMessage(`{"content":[{"type":"text","text":"hello"},{"type":"tool_result","content":"ok","is_error":false}]}`),
			},
			wantCount: 1,
		},
		{
			name: "multiple tool_results",
			msg: StreamMessage{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"a","is_error":false},{"type":"tool_result","content":"b","is_error":true}]}`),
			},
			wantCount: 2,
		},
		{
			name:      "empty message",
			msg:       StreamMessage{},
			wantCount: 0,
		},
		{
			name: "invalid JSON",
			msg: StreamMessage{
				Message: json.RawMessage(`{invalid`),
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := ExtractToolResults(tt.msg)
			if len(blocks) != tt.wantCount {
				t.Errorf("ExtractToolResults() returned %d blocks, want %d", len(blocks), tt.wantCount)
			}
			if tt.wantError && tt.wantCount > 0 && !blocks[0].IsError {
				t.Error("expected first block to be an error")
			}
		})
	}
}

func TestExtractPRURLs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single PR URL",
			text: "Created PR: https://github.com/owner/repo/pull/42",
			want: []string{"https://github.com/owner/repo/pull/42"},
		},
		{
			name: "multiple PR URLs",
			text: "PR1: https://github.com/a/b/pull/1 and PR2: https://github.com/c/d/pull/2",
			want: []string{"https://github.com/a/b/pull/1", "https://github.com/c/d/pull/2"},
		},
		{
			name: "no PR URLs",
			text: "No PRs here",
			want: nil,
		},
		{
			name: "URL with dots and hyphens in org/repo",
			text: "https://github.com/giant-swarm/my.repo/pull/123",
			want: []string{"https://github.com/giant-swarm/my.repo/pull/123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRURLs(tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("extractPRURLs() returned %d URLs, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractPRURLs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCountErrors(t *testing.T) {
	blocks := []ToolResultBlock{
		{Type: "tool_result", Content: "ok", IsError: false},
		{Type: "tool_result", Content: "fail", IsError: true},
		{Type: "tool_result", Content: "also fail", IsError: true},
	}
	if got := countErrors(blocks); got != 2 {
		t.Errorf("countErrors() = %d, want 2", got)
	}

	if got := countErrors(nil); got != 0 {
		t.Errorf("countErrors(nil) = %d, want 0", got)
	}
}

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name   string
		dst    []string
		values []string
		want   []string
	}{
		{
			name:   "no duplicates",
			dst:    []string{"a"},
			values: []string{"b", "c"},
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "with duplicates",
			dst:    []string{"a", "b"},
			values: []string{"b", "c", "a"},
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "nil dst",
			dst:    nil,
			values: []string{"a"},
			want:   []string{"a"},
		},
		{
			name:   "empty values",
			dst:    []string{"a"},
			values: nil,
			want:   []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.dst, tt.values...)
			if len(got) != len(tt.want) {
				t.Fatalf("appendUnique() returned %d elements, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("appendUnique()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCopyStringSlice(t *testing.T) {
	// nil input
	if got := copyStringSlice(nil); got != nil {
		t.Errorf("copyStringSlice(nil) = %v, want nil", got)
	}

	// empty input
	if got := copyStringSlice([]string{}); got != nil {
		t.Errorf("copyStringSlice([]) = %v, want nil", got)
	}

	// normal copy
	orig := []string{"a", "b"}
	cp := copyStringSlice(orig)
	if len(cp) != 2 || cp[0] != "a" || cp[1] != "b" {
		t.Errorf("copyStringSlice() = %v, want [a b]", cp)
	}

	// mutation isolation
	cp[0] = "x"
	if orig[0] != "a" {
		t.Error("copyStringSlice() did not produce an independent copy")
	}
}

func TestCollectModelUsage(t *testing.T) {
	messages := []StreamMessage{
		{
			Type:    MessageTypeAssistant,
			Message: json.RawMessage(`{"model":"claude-sonnet-4-20250514"}`),
		},
		{
			Type:    MessageTypeAssistant,
			Message: json.RawMessage(`{"model":"claude-sonnet-4-20250514"}`),
		},
		{
			Type:    MessageTypeAssistant,
			Message: json.RawMessage(`{"model":"claude-haiku-4-5-20251001"}`),
		},
		{
			Type: MessageTypeResult,
		},
	}

	usage := CollectModelUsage(messages)
	if usage["claude-sonnet-4-20250514"] != 2 {
		t.Errorf("sonnet count = %d, want 2", usage["claude-sonnet-4-20250514"])
	}
	if usage["claude-haiku-4-5-20251001"] != 1 {
		t.Errorf("haiku count = %d, want 1", usage["claude-haiku-4-5-20251001"])
	}

	// Empty returns nil.
	if got := CollectModelUsage(nil); got != nil {
		t.Errorf("CollectModelUsage(nil) = %v, want nil", got)
	}
}

func TestCollectPRURLs(t *testing.T) {
	t.Run("legacy blocks without tool_use_id", func(t *testing.T) {
		messages := []StreamMessage{
			{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"Created PR: https://github.com/owner/repo/pull/42","is_error":false}]}`),
			},
			{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"Also: https://github.com/owner/repo/pull/42 and https://github.com/other/repo/pull/7","is_error":false}]}`),
			},
		}

		urls := CollectPRURLs(messages)
		if len(urls) != 2 {
			t.Fatalf("CollectPRURLs() returned %d URLs, want 2", len(urls))
		}
		if urls[0] != "https://github.com/owner/repo/pull/42" {
			t.Errorf("urls[0] = %q", urls[0])
		}
		if urls[1] != "https://github.com/other/repo/pull/7" {
			t.Errorf("urls[1] = %q", urls[1])
		}
	})

	t.Run("extracts PR URLs from Bash tool results", func(t *testing.T) {
		messages := []StreamMessage{
			// Assistant invokes Bash tool.
			{
				Type:     MessageTypeAssistant,
				Subtype:  SubtypeToolUse,
				ToolName: "Bash",
				ToolID:   "tool_bash_1",
			},
			// Tool result from Bash with a PR URL.
			{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"tool_bash_1","content":"https://github.com/owner/repo/pull/99","is_error":false}]}`),
			},
		}

		urls := CollectPRURLs(messages)
		if len(urls) != 1 {
			t.Fatalf("CollectPRURLs() returned %d URLs, want 1", len(urls))
		}
		if urls[0] != "https://github.com/owner/repo/pull/99" {
			t.Errorf("urls[0] = %q", urls[0])
		}
	})

	t.Run("ignores PR URLs from Read tool results", func(t *testing.T) {
		messages := []StreamMessage{
			// Assistant invokes Read tool.
			{
				Type:     MessageTypeAssistant,
				Subtype:  SubtypeToolUse,
				ToolName: "Read",
				ToolID:   "tool_read_1",
			},
			// Tool result from Read containing a PR URL in file content.
			{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"tool_read_1","content":"Test fixture: https://github.com/owner/repo/pull/42","is_error":false}]}`),
			},
		}

		urls := CollectPRURLs(messages)
		if urls != nil {
			t.Errorf("CollectPRURLs() = %v, want nil (Read tool results should be ignored)", urls)
		}
	})

	t.Run("mixed Bash and Read results", func(t *testing.T) {
		messages := []StreamMessage{
			// Bash tool_use
			{
				Type:     MessageTypeAssistant,
				Subtype:  SubtypeToolUse,
				ToolName: "Bash",
				ToolID:   "tool_bash_1",
			},
			// Read tool_use
			{
				Type:     MessageTypeAssistant,
				Subtype:  SubtypeToolUse,
				ToolName: "Read",
				ToolID:   "tool_read_1",
			},
			// Bash result with real PR URL.
			{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"tool_bash_1","content":"Created https://github.com/owner/repo/pull/10","is_error":false}]}`),
			},
			// Read result with PR URL in file content (should be ignored).
			{
				Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"tool_read_1","content":"docs mention https://github.com/owner/repo/pull/999","is_error":false}]}`),
			},
		}

		urls := CollectPRURLs(messages)
		if len(urls) != 1 {
			t.Fatalf("CollectPRURLs() returned %d URLs, want 1", len(urls))
		}
		if urls[0] != "https://github.com/owner/repo/pull/10" {
			t.Errorf("urls[0] = %q, want https://github.com/owner/repo/pull/10", urls[0])
		}
	})

	t.Run("nil returns nil", func(t *testing.T) {
		if got := CollectPRURLs(nil); got != nil {
			t.Errorf("CollectPRURLs(nil) = %v, want nil", got)
		}
	})
}

func TestIsBashTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Bash", true},
		{"bash", true},
		{"Read", false},
		{"Write", false},
		{"Edit", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isBashTool(tt.name); got != tt.want {
			t.Errorf("isBashTool(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCollectErrorCount(t *testing.T) {
	messages := []StreamMessage{
		{
			Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"ok","is_error":false}]}`),
		},
		{
			Message: json.RawMessage(`{"content":[{"type":"tool_result","content":"fail1","is_error":true},{"type":"tool_result","content":"fail2","is_error":true}]}`),
		},
	}

	if got := CollectErrorCount(messages); got != 2 {
		t.Errorf("CollectErrorCount() = %d, want 2", got)
	}

	if got := CollectErrorCount(nil); got != 0 {
		t.Errorf("CollectErrorCount(nil) = %d, want 0", got)
	}
}

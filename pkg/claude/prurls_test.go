package claude

import (
	"encoding/json"
	"reflect"
	"testing"
)

// bashUse builds an assistant tool_use message for the Bash tool.
func bashUse(id, command string) StreamMessage {
	args, _ := json.Marshal(map[string]string{"command": command})
	return StreamMessage{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: ToolNameBash, ToolID: id, ToolArgs: args}
}

// toolResult builds a user message carrying one tool_result block.
func toolResult(id, content string) StreamMessage {
	raw, _ := json.Marshal(map[string]any{
		"role": "user",
		"content": []map[string]any{{
			"type": "tool_result", "tool_use_id": id, "content": content, "is_error": false,
		}},
	})
	return StreamMessage{Type: MessageTypeUser, Message: raw}
}

func TestCollectPRURLs(t *testing.T) {
	const own = "https://github.com/acme/widgets/pull/42"
	const unrelated = "https://github.com/web-infra-dev/rspack/pull/13636"

	tests := []struct {
		name     string
		messages []StreamMessage
		want     []string
	}{
		{
			name: "reads an unrelated PR and pushes to its own",
			messages: []StreamMessage{
				// Research: reading a PR in a repository the run never pushes to.
				bashUse("t1", "gh pr view "+unrelated+" --json title,body"),
				toolResult("t1", `{"title":"chore: bump","body":"see `+unrelated+`"}`),
				// Work: push to the existing PR branch, then look up its URL.
				bashUse("t2", "git push origin HEAD:renovate/mcp-go-1.x"),
				toolResult("t2", "To github.com:acme/widgets.git\n   1a2b3c4..5d6e7f8  HEAD -> renovate/mcp-go-1.x"),
				bashUse("t3", "gh pr view 42 --json url -q .url"),
				toolResult("t3", own+"\n"),
			},
			want: []string{own},
		},
		{
			name: "gh pr create output counts as created without a push line",
			messages: []StreamMessage{
				bashUse("t1", `git push -u origin fix/thing -q && gh pr create --title "fix: thing" --body "closes #7"`),
				toolResult("t1", "Creating pull request for fix/thing into main in acme/widgets\n\n"+own),
			},
			want: []string{own},
		},
		{
			name: "merge by number resolves against the single pushed repository",
			messages: []StreamMessage{
				bashUse("t1", "git push origin HEAD"),
				toolResult("t1", "To https://github.com/Acme/Widgets.git\n   abc..def  fix -> fix"),
				bashUse("t2", "gh pr merge 42 --squash --delete-branch --admin"),
				toolResult("t2", "✓ Squashed and merged pull request #42 (fix: thing)"),
			},
			want: []string{"https://github.com/acme/widgets/pull/42"},
		},
		{
			name: "number with --repo pointing at a repository never pushed to is excluded",
			messages: []StreamMessage{
				bashUse("t1", "git push origin HEAD"),
				toolResult("t1", "To github.com:acme/widgets.git\n   abc..def  fix -> fix"),
				bashUse("t2", "gh pr view 772 --repo giantswarm/architect-orb --json body"),
				toolResult("t2", `{"body":"..."}`),
				bashUse("t3", "gh pr checks 767 -R giantswarm/architect-orb"),
				toolResult("t3", "All checks were successful"),
			},
			want: nil,
		},
		{
			name: "bare number is ambiguous when several repositories were pushed to",
			messages: []StreamMessage{
				bashUse("t1", "git push origin HEAD"),
				toolResult("t1", "To github.com:acme/widgets.git\n   abc..def  fix -> fix"),
				bashUse("t2", "git -C ../fleet push origin HEAD"),
				toolResult("t2", "To github.com:acme/fleet.git\n   abc..def  bump -> bump"),
				bashUse("t3", "gh pr merge 42 --squash"),
				toolResult("t3", "✓ Squashed and merged pull request #42"),
				bashUse("t4", "gh pr merge https://github.com/acme/fleet/pull/9 --squash"),
				toolResult("t4", "✓ Squashed and merged pull request #9"),
			},
			want: []string{"https://github.com/acme/fleet/pull/9"},
		},
		{
			name: "URLs in file reads, gh pr list output and plain command output are ignored",
			messages: []StreamMessage{
				bashUse("t1", "git push origin HEAD"),
				toolResult("t1", "To github.com:acme/widgets.git\n   abc..def  fix -> fix"),
				{Type: MessageTypeAssistant, Subtype: SubtypeToolUse, ToolName: "Read", ToolID: "t2"},
				toolResult("t2", "See https://github.com/acme/widgets/pull/1 for context"),
				bashUse("t3", "gh pr list --state open"),
				toolResult("t3", "2\tchore\thttps://github.com/acme/widgets/pull/2\n3\tfeat\thttps://github.com/acme/widgets/pull/3"),
				bashUse("t4", "cat CHANGELOG.md"),
				toolResult("t4", "- fixed thing (https://github.com/acme/widgets/pull/4)"),
			},
			want: nil,
		},
		{
			name: "gh pr view on the current branch takes the URL from its output",
			messages: []StreamMessage{
				bashUse("t1", "git push"),
				toolResult("t1", "To https://x-access-token:secret@github.com/acme/widgets.git\n   abc..def  fix -> fix"),
				bashUse("t2", "gh pr view"),
				toolResult("t2", "fix: thing #42\nOpen • dev wants to merge 1 commit into main from fix\n\nView this pull request on GitHub: "+own),
			},
			want: []string{own},
		},
		{
			name: "duplicates collapse and order follows first appearance",
			messages: []StreamMessage{
				bashUse("t1", "git push origin HEAD && gh pr create --fill"),
				toolResult("t1", "To github.com:acme/widgets.git\n * [new branch]  fix -> fix\n"+own),
				bashUse("t2", "gh pr checks "+own+" --watch"),
				toolResult("t2", "All checks were successful"),
				bashUse("t3", "gh pr merge "+own+" --squash"),
				toolResult("t3", "✓ Squashed and merged pull request #42"),
			},
			want: []string{own},
		},
		{
			name: "array-valued tool_result content is parsed",
			messages: []StreamMessage{
				bashUse("t1", "git push origin HEAD"),
				{Type: MessageTypeUser, Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"To github.com:acme/widgets.git\n   abc..def  fix -> fix"}]}]}`)},
				bashUse("t2", "gh pr view 42 --json url"),
				{Type: MessageTypeUser, Message: json.RawMessage(`{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"{\"url\":\"` + own + `\"}"}]}]}`)},
			},
			want: []string{own},
		},
		{
			name: "no Bash tool calls",
			messages: []StreamMessage{
				{Type: MessageTypeAssistant, Subtype: SubtypeText, Text: "see " + own},
				{Type: MessageTypeResult, Result: own},
			},
			want: nil,
		},
		{name: "nil", messages: nil, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectPRURLs(tt.messages)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CollectPRURLs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseGHPRInvocations(t *testing.T) {
	tests := []struct {
		segment string
		want    []ghPRInvocation
	}{
		{"gh pr view 42 --json url -q .url", []ghPRInvocation{{sub: "view", number: "42"}}},
		{"gh pr merge --squash --admin 331", []ghPRInvocation{{sub: "merge", number: "331"}}},
		{"gh pr view 772 --repo giantswarm/architect-orb", []ghPRInvocation{{sub: "view", number: "772", repo: "giantswarm/architect-orb"}}},
		{"gh pr checks 767 -R https://github.com/GiantSwarm/Architect-Orb.git", []ghPRInvocation{{sub: "checks", number: "767", repo: "giantswarm/architect-orb"}}},
		{"gh pr edit --repo=acme/widgets 5 --add-label x", []ghPRInvocation{{sub: "edit", number: "5", repo: "acme/widgets"}}},
		{"gh pr merge 'https://github.com/acme/widgets/pull/42' --squash", []ghPRInvocation{{sub: "merge", url: "https://github.com/acme/widgets/pull/42"}}},
		{"gh pr view renovate/foo --json url", []ghPRInvocation{{sub: "view"}}},
		{`gh pr create --title "x" --body "y"`, []ghPRInvocation{{sub: "create"}}},
		{"echo gh pr", nil},
		{"git push", nil},
	}
	for _, tt := range tests {
		t.Run(tt.segment, func(t *testing.T) {
			got := parseGHPRInvocations(tt.segment)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseGHPRInvocations(%q) = %+v, want %+v", tt.segment, got, tt.want)
			}
		})
	}
}

func TestPushTargetPattern(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"To github.com:acme/widgets.git\n   abc..def  fix -> fix", "acme/widgets"},
		{"To https://github.com/acme/widgets.git\n   abc..def  fix -> fix", "acme/widgets"},
		{"To https://github.com/acme/widgets\n * [new branch]  fix -> fix", "acme/widgets"},
		{"To ssh://git@github.com/acme/widgets.git\n   abc..def  fix -> fix", "acme/widgets"},
		{"To git@github.com:acme/widgets.git\n   abc..def  fix -> fix", "acme/widgets"},
		{"To https://x-access-token:ghp_x@github.com/acme/widgets.git\n   abc..def  fix -> fix", "acme/widgets"},
		{"Everything up-to-date", ""},
		{"To gitlab.com:acme/widgets.git\n   abc..def  fix -> fix", ""},
	}
	for _, tt := range tests {
		m := pushTargetPattern.FindStringSubmatch(tt.output)
		got := ""
		if len(m) > 1 {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("pushTargetPattern(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestNormalizeRepo(t *testing.T) {
	tests := map[string]string{
		"acme/widgets":                          "acme/widgets",
		"Acme/Widgets":                          "acme/widgets",
		"github.com/acme/widgets":               "acme/widgets",
		"https://github.com/acme/widgets":       "acme/widgets",
		"https://github.com/acme/widgets.git":   "acme/widgets",
		`"acme/widgets"`:                        "acme/widgets",
		"widgets":                               "",
		"https://github.com/acme/widgets/pulls": "",
	}
	for in, want := range tests {
		if got := normalizeRepo(in); got != want {
			t.Errorf("normalizeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractToolResults_ArrayContent(t *testing.T) {
	msg := StreamMessage{Message: json.RawMessage(`{"content":[
		{"type":"tool_result","tool_use_id":"a","content":"plain"},
		{"type":"tool_result","tool_use_id":"b","content":[{"type":"text","text":"first"},{"type":"text","text":"second"}],"is_error":true},
		{"type":"text","text":"not a tool result"}
	]}`)}
	blocks := ExtractToolResults(msg)
	if len(blocks) != 2 {
		t.Fatalf("ExtractToolResults() returned %d blocks, want 2", len(blocks))
	}
	if blocks[0].Content != "plain" || blocks[0].ToolUseID != "a" {
		t.Errorf("blocks[0] = %+v", blocks[0])
	}
	// Text blocks are concatenated the same way the OpenAI conversion does.
	if blocks[1].Content != "firstsecond" || !blocks[1].IsError || blocks[1].ToolUseID != "b" {
		t.Errorf("blocks[1] = %+v", blocks[1])
	}
}

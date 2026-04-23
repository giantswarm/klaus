package claude

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"time"
)

// taskToolNames is the set of tool names that represent subagent dispatches.
// Claude Code uses "Task" in older versions and "Agent" in newer ones.
var taskToolNames = map[string]bool{
	"Task":  true,
	"Agent": true,
}

// isSubagentTool returns true if the given tool name is a subagent dispatch tool.
func isSubagentTool(toolName string) bool {
	return taskToolNames[toolName]
}

// taskToolArgs represents the JSON arguments of a Task/Agent tool call.
type taskToolArgs struct {
	SubagentType string `json:"subagent_type"`
	Description  string `json:"description"`
}

// maxInflightSubagents caps the number of concurrently tracked in-flight
// subagent calls to prevent unbounded memory growth from a misbehaving
// subprocess.
const maxInflightSubagents = 100

// subagentTracker manages in-flight subagent calls, matching tool_use messages
// to their corresponding results to compute duration and extract metadata.
type subagentTracker struct {
	// inflight maps tool IDs to in-progress subagent calls with their start time.
	inflight map[string]inflightSubagent

	// completed holds finished subagent calls in dispatch order.
	completed []SubagentCall

	// seq is a monotonic counter used to order in-flight subagents
	// deterministically when their start times are identical.
	seq uint64
}

type inflightSubagent struct {
	call  SubagentCall
	start time.Time
	seq   uint64
}

func newSubagentTracker() *subagentTracker {
	return &subagentTracker{
		inflight: make(map[string]inflightSubagent),
	}
}

// handleToolUse processes a tool_use message. If the tool is a subagent dispatch,
// it records the in-flight call and returns true.
func (st *subagentTracker) handleToolUse(msg StreamMessage) bool {
	if !isSubagentTool(msg.ToolName) {
		return false
	}

	// Cap in-flight tracking to prevent unbounded growth.
	if len(st.inflight) >= maxInflightSubagents {
		log.Printf("[claude] subagent tracking limit reached (%d), ignoring new dispatch: %s",
			maxInflightSubagents, msg.ToolID)
		return false
	}

	call := SubagentCall{
		ToolID: msg.ToolID,
		Status: SubagentStatusRunning,
	}

	// Parse the tool arguments to extract subagent_type and description.
	if len(msg.ToolArgs) > 0 {
		var args taskToolArgs
		if err := json.Unmarshal(msg.ToolArgs, &args); err == nil {
			call.Type = args.SubagentType
			call.Description = args.Description
		}
	}

	if call.Type == "" {
		call.Type = msg.ToolName
	}

	st.seq++
	st.inflight[msg.ToolID] = inflightSubagent{
		call:  call,
		start: time.Now(),
		seq:   st.seq,
	}
	return true
}

// usageBlockRe matches the <usage> block that Claude Code includes in subagent
// tool results: total_tokens, tool_uses, and duration_ms.
var usageBlockRe = regexp.MustCompile(
	`<usage>\s*` +
		`total_tokens:\s*(\d+)\s*` +
		`tool_uses:\s*(\d+)\s*` +
		`duration_ms:\s*(\d+)\s*` +
		`</usage>`,
)

// handleMessage processes a non-tool_use stream message looking for subagent
// result data via <usage> blocks in text content. Must NOT be called with
// tool_use messages (use handleToolUse for those). Returns true if a subagent
// was completed.
func (st *subagentTracker) handleMessage(msg StreamMessage) bool {
	if len(st.inflight) == 0 {
		return false
	}

	// Look for <usage> blocks in text content that signal subagent completion.
	// These appear in the tool result content returned by Claude Code.
	content := msg.Text
	if content == "" {
		content = msg.Result
	}
	if content == "" {
		return false
	}

	matches := usageBlockRe.FindStringSubmatch(content)
	if len(matches) != 4 {
		return false
	}

	// Match to the oldest in-flight subagent by sequence number (FIFO).
	var oldestID string
	var oldestInf inflightSubagent
	for id, inf := range st.inflight {
		if oldestID == "" || inf.seq < oldestInf.seq {
			oldestID = id
			oldestInf = inf
		}
	}
	if oldestID == "" {
		return false
	}

	st.completeSubagent(oldestID, oldestInf, content)
	return true
}

func (st *subagentTracker) completeSubagent(toolID string, inf inflightSubagent, content string) {
	delete(st.inflight, toolID)

	call := inf.call
	call.Status = SubagentStatusCompleted
	call.DurationMS = float64(time.Since(inf.start).Milliseconds())

	// Try to parse the <usage> block from the result content.
	if matches := usageBlockRe.FindStringSubmatch(content); len(matches) == 4 {
		if tokens, err := strconv.Atoi(matches[1]); err == nil {
			call.Tokens = tokens
		}
		if toolUses, err := strconv.Atoi(matches[2]); err == nil {
			call.ToolCalls = toolUses
		}
		if durationMS, err := strconv.Atoi(matches[3]); err == nil {
			call.DurationMS = float64(durationMS)
		}
	}

	st.completed = append(st.completed, call)
	log.Printf("[claude] subagent completed: type=%q description=%q tool_calls=%d tokens=%d duration_ms=%.0f",
		call.Type, call.Description, call.ToolCalls, call.Tokens, call.DurationMS)
}

// calls returns a copy of all completed and in-flight subagent calls.
// In-flight calls are appended after completed ones.
func (st *subagentTracker) calls() []SubagentCall {
	total := len(st.completed) + len(st.inflight)
	if total == 0 {
		return nil
	}

	result := make([]SubagentCall, 0, total)
	result = append(result, st.completed...)
	for _, inf := range st.inflight {
		result = append(result, inf.call)
	}
	return result
}

// reset clears all tracking state.
func (st *subagentTracker) reset() {
	st.inflight = make(map[string]inflightSubagent)
	st.completed = nil
	st.seq = 0
}

// copySubagentCalls returns a shallow copy of the slice. Returns nil for nil/empty input.
func copySubagentCalls(calls []SubagentCall) []SubagentCall {
	if len(calls) == 0 {
		return nil
	}
	cp := make([]SubagentCall, len(calls))
	copy(cp, calls)
	return cp
}

// collectSubagentCalls extracts subagent calls from a set of messages by
// replaying them through a tracker. Used for persisted result reconstruction.
func collectSubagentCalls(messages []StreamMessage) []SubagentCall {
	tracker := newSubagentTracker()
	for _, msg := range messages {
		if msg.Type == MessageTypeAssistant && msg.Subtype == SubtypeToolUse {
			tracker.handleToolUse(msg)
		} else {
			tracker.handleMessage(msg)
		}
	}
	return tracker.calls()
}

# Klaus Manual Test Plan

This document describes the manual integration tests for verifying klaus against a real claude-code CLI. These tests exercise the MCP tools, environment variable configuration, subprocess lifecycle, and the async prompt workflow.

All tests run inside the Docker container, which bundles the `klaus` binary, Node.js, and the Claude Code CLI.

## Prerequisites

- Docker installed
- A valid `ANTHROPIC_API_KEY`
- `curl` and `python3` available on the host for MCP requests

## Building the Image

```bash
docker build -t klaus:test .
```

## Running the Container

Start klaus with minimal config:

```bash
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  klaus:test serve
```

To restart with different environment variables:

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e <EXTRA_VAR>=<value> \
  klaus:test serve
```

For validation tests that should fail at startup, use `docker run --rm` (no `-d`) so the error is printed to stdout:

```bash
docker run --rm \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_EFFORT=invalid \
  klaus:test serve
```

## Helper: MCP Request

All MCP tests follow this pattern:

```bash
# 1. Initialize a session
MCP_SESSION=$(curl -s -i -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
    "protocolVersion":"2025-03-26","capabilities":{},
    "clientInfo":{"name":"test","version":"1.0"}
  }}' 2>&1 | grep -i "Mcp-Session-Id:" | awk '{print $2}' | tr -d '\r')

# 2. Call a tool
curl -s -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{"message":"Hello"}
  }}'
```

## 1. Server Startup and Health Endpoints

```bash
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| `curl http://localhost:9090/healthz` | `ok` |
| `curl http://localhost:9090/readyz` | `ok` |
| `curl http://localhost:9090/status` | JSON with `name`, `version`, `mode: "single-shot"`, `agent.status: "idle"` |
| `curl http://localhost:9090/` | `klaus <version>` |

## 2. MCP Protocol

| Check | Expected |
|-------|----------|
| POST `/mcp` with `initialize` | Returns `protocolVersion`, `serverInfo.name: "klaus"`, `Mcp-Session-Id` header |
| POST `/mcp` with `tools/list` | Returns 4 tools: `prompt`, `status`, `result`, `stop` |
| `prompt` tool schema has `blocking` param | `type: boolean`, described as default `false` |
| `result` tool exists with debug description | Description mentions "debugging" and "troubleshooting" |
| `status` tool description mentions "primary way" | Description says it is the primary way to check progress |

## 3. Non-Blocking Prompt (Default)

```bash
curl -s -X POST http://localhost:9090/mcp \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{"message":"Write a poem about Kubernetes."}
  }}'
```

| Check | Expected |
|-------|----------|
| Response time | Under 1 second (non-blocking) |
| Response body | `{"status":"started"}` or `{"status":"started","session_id":"..."}` |
| `status` tool while running | `status: "busy"`, `message_count` > 0, `session_id` present |
| `status` tool after completion | `status: "completed"`, `result` field contains the agent output |
| `result` tool after completion | Full output with `result_text`, `messages` array, `total_cost_usd` |

## 4. Blocking Prompt

```bash
curl -s -X POST http://localhost:9090/mcp \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{"message":"What is 2+2? Reply with just the number.","blocking":true}
  }}'
```

| Check | Expected |
|-------|----------|
| Response time | Several seconds (waits for completion) |
| Response body | `{"result":"4","message_count":3,"total_cost_usd":...,"session_id":"..."}` |

## 5. Stop During Async Execution

1. Fire a non-blocking prompt with a long task
2. Confirm `status` shows `"busy"`
3. Call `stop` tool
4. Confirm `status` shows `"stopped"`

## 6. Concurrent Prompt Rejection

1. Fire a non-blocking prompt
2. Immediately fire a second prompt
3. Second prompt should return `isError: true` with "claude process is already busy"

## 7. Session Management (Single-Shot Mode)

| Check | How | Expected |
|-------|-----|----------|
| Resume by session_id | Prompt 1: store a word. Prompt 2: `resume` with session_id, ask for the word | Recalls the word correctly |
| Continue session | Prompt with `continue: true` | Continues the most recent conversation |

## 8. Environment Variables

For each variable, restart the container with the variable set and verify the behavior.

### CLAUDE_MODEL

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_MODEL=haiku \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| Prompt "What model are you?" | Responds with haiku model identifier |
| Invalid model (e.g. `nonexistent-model`) | Returns error about model not available |

### CLAUDE_SYSTEM_PROMPT

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_SYSTEM_PROMPT="You are a pirate. Always say arr." \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| Prompt "Say hello" | Response includes pirate language |

### CLAUDE_MAX_TURNS

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_MAX_TURNS=1 \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| Prompt a multi-step task | Truncated result, limited to 1 agentic turn |

### CLAUDE_MAX_BUDGET_USD

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_MAX_BUDGET_USD=0.01 \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| Any prompt | Result truncated or empty when budget exceeded |

### CLAUDE_EFFORT

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_EFFORT=low \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| Simple prompt | Accepted and produces a response |
| `CLAUDE_EFFORT=invalid` | Fails at startup with validation error listing valid levels (see section 10) |

### CLAUDE_WORKSPACE

Create a marker file inside the running container, then verify the agent can read it:

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_WORKSPACE=/workspace \
  klaus:test serve

# Create the marker file inside the container
docker exec klaus-test bash -c 'echo "marker-12345" > /workspace/marker.txt'
```

| Check | Expected |
|-------|----------|
| Prompt "Read marker.txt and return its contents" | Returns "marker-12345" |

### CLAUDE_PERMISSION_MODE

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_PERMISSION_MODE=plan \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| Simple prompt | Produces a response |
| `CLAUDE_PERMISSION_MODE=invalid` | Fails at startup with validation error listing valid modes (see section 10) |

### CLAUDE_PERSISTENT_MODE

See section 9 below.

## 9. Persistent Mode

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_PERSISTENT_MODE=true \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| `/status` endpoint | `mode: "persistent"` |
| Startup logs (`docker logs klaus-test`) | `Starting in persistent mode (bidirectional stream-json)` |
| First prompt | Auto-starts subprocess, returns response |
| Second prompt recalls first | Conversation memory maintained (same session_id) |
| Cost is cumulative | `total_cost_usd` increases across prompts |
| Stop tool | Subprocess exits gracefully, no watchdog restart |
| Prompt after stop | Auto-restarts subprocess with new session_id, cost resets |

### Persistent + Async

| Check | Expected |
|-------|----------|
| Non-blocking prompt in persistent mode | Returns `{"status":"started"}` immediately |
| Status polling | Shows busy, then completed with result |
| Multi-turn memory across async prompts | Second async prompt recalls data from first |

### Persistent + Env Vars

| Check | Expected |
|-------|----------|
| `CLAUDE_SYSTEM_PROMPT` with persistent mode | System prompt applied to persistent subprocess |
| `CLAUDE_EFFORT` with persistent mode | Effort level applied at subprocess start |

## 10. Input Validation

These should all fail at startup with clear error messages. Use `docker run --rm` (no `-d`) to see the output:

```bash
# Example:
docker run --rm \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_EFFORT=invalid \
  klaus:test serve
```

| Input | Expected Error |
|-------|---------------|
| `CLAUDE_EFFORT=invalid` | `invalid effort level "invalid"; valid levels: low, medium, high` |
| `CLAUDE_PERMISSION_MODE=invalid` | `invalid permission mode "invalid"; valid modes: default, acceptEdits, bypassPermissions, dontAsk, plan, delegate` |
| `CLAUDE_MAX_TURNS=-1` | `invalid CLAUDE_MAX_TURNS "-1": must be >= 0` |
| `CLAUDE_MAX_BUDGET_USD=-5` | `invalid CLAUDE_MAX_BUDGET_USD "-5": must be >= 0` |

## 11. Result Debug Tool

After any completed prompt, call the `result` tool:

| Check | Expected |
|-------|----------|
| `result_text` field | Full untruncated agent output |
| `messages` array | Contains system, assistant, and result message types |
| Assistant messages | Include `model`, `usage` (tokens), `content` |
| Result message | Includes `duration_ms`, `total_cost_usd` |
| `session_id` | Matches the session from the prompt |
| Before any prompt has run | Returns appropriate empty/no-result state |

## 12. MCP Server Passthrough

This test verifies that the Claude agent inside klaus can connect to and use tools from an external MCP server via the `CLAUDE_MCP_CONFIG` environment variable.

The config file follows the [Claude Code `.mcp.json` format](https://code.claude.com/docs/en/mcp#project-scope):

```json
{
  "mcpServers": {
    "my-mcp-server": {
      "type": "http",
      "url": "http://localhost:8090/mcp"
    }
  }
}
```

### Prerequisites

- An MCP server running and reachable (e.g. at `http://localhost:8090/mcp`)
- Verify it responds to MCP `initialize`:

```bash
curl -s -X POST http://localhost:8090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
    "protocolVersion":"2025-03-26","capabilities":{},
    "clientInfo":{"name":"test","version":"1.0"}
  }}'
```

### Setup

Create the MCP config file on the host:

```bash
cat > /tmp/mcp-config.json << 'EOF'
{
  "mcpServers": {
    "my-mcp-server": {
      "type": "http",
      "url": "http://localhost:8090/mcp"
    }
  }
}
EOF
```

Start the container with the config file mounted and `CLAUDE_MCP_CONFIG` pointing to it. Use `--network host` so the container can reach the MCP server on localhost:

```bash
docker rm -f klaus-test

docker run -d --name klaus-test --network host \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_MCP_CONFIG=/tmp/mcp-config.json \
  -v /tmp/mcp-config.json:/tmp/mcp-config.json:ro \
  klaus:test serve
```

### Tool Discovery via MCP

Send a prompt that asks the agent to discover and use the external MCP tools:

```bash
# 1. Initialize a klaus session
MCP_SESSION=$(curl -s -i -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
    "protocolVersion":"2025-03-26","capabilities":{},
    "clientInfo":{"name":"test","version":"1.0"}
  }}' 2>&1 | grep -i "Mcp-Session-Id:" | awk '{print $2}' | tr -d '\r')

# 2. Send a non-blocking prompt that exercises the MCP tools
curl -s -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{
      "message":"Use the list_tools MCP tool from my-mcp-server to list available tools. Then use the list_resources MCP tool to list available resources. Report what you found."
    }
  }}'

# 3. Poll for completion
curl -s -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{
    "name":"status","arguments":{}
  }}'
```

| Check | Expected |
|-------|----------|
| Prompt returns | `{"status":"started"}` |
| Status after completion | `status: "completed"` with `result` containing tool/resource lists |
| `result` tool messages | Contains `tool_use` entries with names prefixed `mcp__my-mcp-server__` |
| Agent used MCP tools | Messages array shows calls to `mcp__my-mcp-server__list_tools` and `mcp__my-mcp-server__list_resources` |
| Tool results returned | Tool result content contains the list of tools/resources from the external server |

### Blocking Mode with MCP Tools

```bash
curl -s -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{
      "message":"Use the list_tools MCP tool to list tools from my-mcp-server. Reply with just the count of tools found.",
      "blocking":true
    }
  }}'
```

| Check | Expected |
|-------|----------|
| Response | Blocking result with `result` field containing the tool count |
| `total_cost_usd` | Present and > 0 |

## 13. Inline Skills

This test verifies that inline skills (SKILL.md files) are loaded via `--add-dir` and influence agent behavior.

### Setup

Create skill files on the host:

```bash
mkdir -p /tmp/klaus-test/skills/api-conventions /tmp/klaus-test/skills/security

cat > /tmp/klaus-test/skills/api-conventions/SKILL.md << 'EOF'
---
description: "API design conventions for testing"
---
When answering questions about API design, always mention that you follow REST conventions.
Include the phrase "REST-VERIFIED" in your response when this skill is relevant.
EOF

cat > /tmp/klaus-test/skills/security/SKILL.md << 'EOF'
---
description: "Security best practices for testing"
---
When answering questions about security, always mention input validation as the first principle.
Include the phrase "SECURITY-VERIFIED" in your response when this skill is relevant.
EOF
```

Start the container with skills mounted:

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions \
  -e CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=true \
  -v /tmp/klaus-test/skills/api-conventions/SKILL.md:/etc/klaus/extensions/.claude/skills/api-conventions/SKILL.md:ro \
  -v /tmp/klaus-test/skills/security/SKILL.md:/etc/klaus/extensions/.claude/skills/security/SKILL.md:ro \
  klaus:test serve
```

### Skill Loading

| Check | Expected |
|-------|----------|
| `docker exec klaus-test ls /etc/klaus/extensions/.claude/skills/` | Lists `api-conventions` and `security` directories |
| `docker exec klaus-test cat /etc/klaus/extensions/.claude/skills/api-conventions/SKILL.md` | SKILL.md with frontmatter and content |
| Prompt "What are the best practices for API design?" | Response references REST conventions and includes "REST-VERIFIED" |
| Prompt about security best practices | Response mentions input validation first |

### Multiple Skills

| Check | Expected |
|-------|----------|
| Both skills mounted simultaneously | Both directories present under `.claude/skills/` |
| `CLAUDE_ADD_DIRS` env var | Set to `/etc/klaus/extensions` |
| `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD` env var | Set to `true` when `loadAdditionalDirsMemory` is enabled |

## 14. Hooks and Hook Scripts

This test verifies that lifecycle hooks fire correctly and hook scripts receive proper JSON input.

### Setup

Create a settings.json and hook script:

```bash
mkdir -p /tmp/klaus-test/hooks

cat > /tmp/klaus-test/settings.json << 'EOF'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "/etc/klaus/hooks/log-tool-use.sh"
          }
        ]
      }
    ]
  }
}
EOF

cat > /tmp/klaus-test/hooks/log-tool-use.sh << 'SCRIPT'
#!/bin/bash
INPUT=$(cat)
echo "$(date -Iseconds) RAW_INPUT: $INPUT" >> /tmp/hook-log.txt
exit 0
SCRIPT
chmod +x /tmp/klaus-test/hooks/log-tool-use.sh
```

Start the container:

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_SETTINGS_FILE=/etc/klaus/settings.json \
  -v /tmp/klaus-test/settings.json:/etc/klaus/settings.json:ro \
  -v /tmp/klaus-test/hooks/log-tool-use.sh:/etc/klaus/hooks/log-tool-use.sh:ro \
  klaus:test serve
```

### Hook Execution

Send a prompt that triggers tool use, then check the log:

```bash
# Send a prompt that triggers Bash tool use
# (initialize MCP session first, then prompt)
curl -s -X POST http://localhost:9090/mcp \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{
      "message":"Run echo hello in bash.",
      "blocking":true
    }
  }}'

# Check the hook log
docker exec klaus-test cat /tmp/hook-log.txt
```

| Check | Expected |
|-------|----------|
| Hook log file created | `/tmp/hook-log.txt` exists inside the container |
| Hook log entry timestamp | Matches the time of the tool use event |
| Hook JSON input contains `tool_name` | `"tool_name":"Bash"` or `"tool_name":"Write"` etc. |
| Hook JSON input contains `tool_input` | Includes the tool's arguments (e.g. `command`, `file_path`) |
| Hook JSON input contains `session_id` | Valid UUID session identifier |
| Hook JSON input contains `hook_event_name` | `"hook_event_name":"PreToolUse"` |
| Hook JSON input contains `permission_mode` | `"permission_mode":"bypassPermissions"` |
| Hook JSON input contains `tool_use_id` | Tool use identifier string |
| Hook exit code 0 | Tool execution proceeds normally |

### Hook Script Permissions

| Check | Expected |
|-------|----------|
| `docker exec klaus-test ls -la /etc/klaus/hooks/log-tool-use.sh` | File has execute permission (0755 via `defaultMode`) |
| Hook script uses `#!/bin/bash` | Proper shebang for execution |

### Hooks / Settings Mutual Exclusivity

| Check | Expected |
|-------|----------|
| Helm template with both `hooks` and `settingsFile` | Fails with: "claude.hooks and claude.settingsFile are mutually exclusive" |
| `CLAUDE_SETTINGS_FILE` env var | Set to `/etc/klaus/settings.json` when hooks are defined |

## 15. Subagent Delegation

This test verifies that custom subagents are available for delegation by the main Claude Code agent via the built-in Task tool. See [Claude Code subagent docs](https://code.claude.com/docs/en/sub-agents) for background.

**Key concepts:**

- **Subagent delegation**: The main agent spawns a subagent via the Task tool to handle a subtask. The subagent runs in its own context with its own prompt, tools, and model.
- **Agent selection** (`--agent`): Changes _who_ is the top-level agent. This is tested separately in section 15b.
- Subagents defined via `CLAUDE_AGENTS` (JSON, `--agents` flag) have the highest priority and are always available for delegation.
- Subagents defined as markdown files via `--add-dir` (`.claude/agents/<name>.md`) are also discoverable.

### Setup: Subagents via JSON (CLAUDE_AGENTS)

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_MAX_TURNS=10 \
  -e CLAUDE_MAX_BUDGET_USD=0.50 \
  -e 'CLAUDE_AGENTS={"poet":{"description":"A creative writing agent that writes short poems","prompt":"You are a poet. When given a topic, write a short haiku about it."},"calculator":{"description":"A math agent that solves arithmetic problems","prompt":"You are a calculator. Solve the given math problem and return the numeric result."}}' \
  klaus:test serve
```

### Subagent Delegation via Task Tool

Send a prompt that explicitly asks for delegation:

```bash
curl -s -X POST http://localhost:9090/mcp \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{
      "message":"Use the poet subagent to write a haiku about kubernetes. Use the calculator subagent to compute 17 * 23. Delegate each to the appropriate subagent."
    }
  }}'
```

After completion, inspect the full message stream via the `result` tool:

| Check | Expected |
|-------|----------|
| Main agent text | "I'll delegate both tasks" or similar delegation language |
| Task tool calls present | `tool_use` messages with `name: "Task"` |
| Poet delegation | `Task` call with `subagent_type: "poet"` in input |
| Calculator delegation | `Task` call with `subagent_type: "calculator"` in input |
| Tool results from subagents | `tool_result` messages containing each subagent's output |
| Poet result | Contains a haiku (three-line poem) |
| Calculator result | Contains correct answer (391) |
| Main agent synthesis | Final text combines results from both subagents |
| Message count | Higher than non-delegating case (~10 vs ~6 messages) |
| Cost | Higher than non-delegating case (multiple context windows) |

### Subagent Files via --add-dir

Subagent files mounted at `.claude/agents/<name>.md` via `--add-dir` are also discoverable:

```bash
mkdir -p /tmp/klaus-test/agents

cat > /tmp/klaus-test/agents/researcher.md << 'EOF'
---
name: researcher
description: A research agent that gathers and synthesizes information
tools: Read, Grep, Glob
---
You are a research agent. Gather information and provide thorough summaries.
EOF

docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions \
  -e CLAUDE_MAX_TURNS=10 \
  -v /tmp/klaus-test/agents/researcher.md:/etc/klaus/extensions/.claude/agents/researcher.md:ro \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| File mounted correctly | `docker exec klaus-test cat /etc/klaus/extensions/.claude/agents/researcher.md` shows frontmatter + content |
| `CLAUDE_ADD_DIRS` set | `/etc/klaus/extensions` |
| Subagent discoverable | Main agent can delegate to `researcher` when asked |

### Parallel Subagent Execution

Claude Code can run multiple subagents in parallel when the tasks are independent:

| Check | Expected |
|-------|----------|
| Multiple Task calls | Both `poet` and `calculator` Task calls appear before either result |
| Both results returned | Tool results from both subagents present in messages |
| Main agent waits for both | Synthesis text appears after both results |

### Subagent Tool Restrictions

Each subagent runs with only the tools specified in its config:

```bash
# Define a read-only subagent
-e 'CLAUDE_AGENTS={"reader":{"description":"Read-only agent","prompt":"You can only read files.","tools":["Read","Grep","Glob"]}}'
```

| Check | Expected |
|-------|----------|
| Subagent uses only allowed tools | `reader` subagent cannot use Bash, Edit, or Write |
| Tool restriction enforced | If asked to write, the subagent reports it cannot |

## 15b. Agent Selection (--agent)

Agent selection is **distinct from subagent delegation**. The `--agent` flag (or `CLAUDE_ACTIVE_AGENT` env var, or MCP `agent` parameter) changes _which agent persona runs as the top-level agent_ for the session. It does NOT control subagent delegation.

### Per-Prompt Agent Selection via MCP

```bash
curl -s -X POST http://localhost:9090/mcp \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{
      "message":"What tools do you have access to?",
      "agent":"researcher",
      "blocking":true
    }
  }}'
```

| Check | Expected |
|-------|----------|
| `prompt` tool accepts `agent` parameter | No error, prompt executes |
| `--agent researcher` passed to CLI | The researcher persona handles the prompt |
| Response completes | Result reflects the agent's tool restrictions |

### Default Active Agent via Env Var

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions \
  -e CLAUDE_ACTIVE_AGENT=researcher \
  -v /tmp/klaus-test/agents/researcher.md:/etc/klaus/extensions/.claude/agents/researcher.md:ro \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| All prompts use researcher agent | `--agent researcher` applied to every prompt |
| No `agent` parameter needed | Default agent used automatically |

### Known Issue: --agents + --agent Combination

When `CLAUDE_ACTIVE_AGENT` selects an agent that is also defined in `CLAUDE_AGENTS` (JSON), prompts may hang indefinitely. This has been reproduced consistently. The workaround is:

- Use `CLAUDE_AGENTS` (JSON) for subagent definitions **without** setting `CLAUDE_ACTIVE_AGENT`
- Use `agentFiles` (markdown via `--add-dir`) when you need agent selection via `--agent`
- Do not combine `CLAUDE_AGENTS` JSON definitions with `CLAUDE_ACTIVE_AGENT` pointing to one of those agents

## 16. Combined Extensions

This test verifies that skills, hooks, and subagents work together.

### Setup

Start with all extensions:

```bash
docker rm -f klaus-test
docker run -d --name klaus-test -p 9090:9090 \
  -e ANTHROPIC_API_KEY=<key> \
  -e PORT=9090 \
  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions \
  -e CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=true \
  -e CLAUDE_SETTINGS_FILE=/etc/klaus/settings.json \
  -e CLAUDE_MAX_TURNS=10 \
  -e 'CLAUDE_AGENTS={"poet":{"description":"Writes poems","prompt":"Write haikus when asked."}}' \
  -v /tmp/klaus-test/skills/api-conventions/SKILL.md:/etc/klaus/extensions/.claude/skills/api-conventions/SKILL.md:ro \
  -v /tmp/klaus-test/skills/security/SKILL.md:/etc/klaus/extensions/.claude/skills/security/SKILL.md:ro \
  -v /tmp/klaus-test/settings.json:/etc/klaus/settings.json:ro \
  -v /tmp/klaus-test/hooks/log-tool-use.sh:/etc/klaus/hooks/log-tool-use.sh:ro \
  klaus:test serve
```

| Check | Expected |
|-------|----------|
| All volume mounts present | Skills, settings.json, hook scripts all mounted |
| `CLAUDE_ADD_DIRS` set | `/etc/klaus/extensions` |
| `CLAUDE_SETTINGS_FILE` set | `/etc/klaus/settings.json` |
| `CLAUDE_AGENTS` set | JSON with poet subagent |
| Skill-triggering prompt | Skills influence response |
| Tool-triggering prompt | Hook log captures JSON with `tool_name` and `tool_input` |
| Subagent delegation prompt | Main agent delegates to poet via Task tool |
| Combined prompt (skill + tool use) | Skills guide response AND hooks fire for tool use |

## Cleanup

```bash
docker rm -f klaus-test
rm -rf /tmp/klaus-test
```

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

## Cleanup

```bash
docker rm -f klaus-test
```

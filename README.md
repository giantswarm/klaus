# klaus

An [MCP](https://modelcontextprotocol.io/) server that wraps [Claude Code](https://docs.anthropic.com/en/docs/claude-code) to run AI coding agents inside Kubernetes.

## Overview

Klaus runs the Claude Code CLI as a managed subprocess and exposes it over HTTP as a Streamable HTTP MCP endpoint. This allows orchestrating AI coding agents in Kubernetes with proper lifecycle management, health checks, and optional OAuth authentication.

### MCP Tools

| Tool | Description |
|------|-------------|
| `prompt` | Send a task to the Claude agent. Non-blocking by default (set `blocking=true` to wait for completion). |
| `status` | Query agent state, progress, and result. Returns `completed` with result when a non-blocking task finishes. |
| `stop` | Terminate the running agent |
| `result` | Get full untruncated result and message history from the last run (debugging tool) |

### Non-blocking workflow

By default, `prompt` returns immediately and the task runs in the background:

```
1. prompt(message: "Refactor the auth module")
   -> {status: "started", session_id: "..."}

2. status()                          # poll periodically
   -> {status: "busy", message_count: 12, tool_call_count: 5, last_tool_name: "Edit", ...}

3. status()                          # when complete
   -> {status: "completed", result: "I've refactored...", total_cost_usd: 0.45, session_id: "..."}

4. result()                          # optional: full debug output
   -> {result_text: "...", messages: [...], message_count: 42, ...}
```

For short queries, use `prompt(message: "...", blocking: true)` to get the result inline.

**Status lifecycle:** `idle` (never ran) -> `busy` (task running) -> `completed` (result available). The result and `completed` status persist until the next `prompt` call clears them. There is no explicit "consumed" acknowledgement, so callers that poll should track whether they have already processed a given result.

### HTTP Endpoints

| Path | Purpose |
|------|---------|
| `/mcp` | MCP Streamable HTTP (optionally OAuth-protected) |
| `/healthz` | Liveness probe |
| `/readyz` | Readiness probe (Claude process state) |
| `/status` | JSON status (version, agent state, cost) |

## Quick Start

```bash
export ANTHROPIC_API_KEY=sk-ant-...
klaus serve
# MCP endpoint available at http://localhost:8080/mcp
```

## Architecture

```
MCP Client --> /mcp --> MCP Server --> Prompter --> Claude Code CLI (subprocess)
```

The container image is based on `node:22-slim` with the Claude Code CLI installed globally via npm. The Go binary manages the Claude subprocess in one of two modes:

- **Single-shot** (default): spawns a new subprocess per prompt. Supports per-invocation overrides (`session_id`, `resume`, `effort`, `agent`). Session persistence is optional via `--resume`.
- **Persistent** (`CLAUDE_PERSISTENT_MODE=true`): maintains a long-running subprocess with bidirectional stream-json. Provides multi-turn conversation memory, cumulative cost tracking, and lower latency (no startup overhead per prompt). A watchdog auto-restarts the subprocess on crash.

## Configuration

All Claude options are configured via environment variables. The `ANTHROPIC_API_KEY` is the only requirement.

| Variable | Description | Default |
|----------|-------------|---------|
| `ANTHROPIC_API_KEY` | Anthropic API key (required) | -- |
| `PORT` | HTTP server port | `8080` |
| `CLAUDE_MODEL` | Model name or alias (`sonnet`, `opus`, `haiku`) | CLI default |
| `CLAUDE_SYSTEM_PROMPT` | Override the default system prompt | -- |
| `CLAUDE_APPEND_SYSTEM_PROMPT` | Append to the default system prompt | -- |
| `CLAUDE_MAX_TURNS` | Max agentic turns per prompt (0 = unlimited) | `0` |
| `CLAUDE_MAX_BUDGET_USD` | Spending cap per invocation in USD | -- |
| `CLAUDE_EFFORT` | Effort level: `low`, `medium`, `high` | CLI default |
| `CLAUDE_PERMISSION_MODE` | `bypassPermissions`, `acceptEdits`, `plan`, `delegate`, `default` | `bypassPermissions` |
| `CLAUDE_WORKSPACE` | Working directory for the agent | -- |
| `CLAUDE_PERSISTENT_MODE` | Use persistent subprocess mode | `false` |
| `CLAUDE_MCP_CONFIG` | Path to MCP servers config file | -- |
| `CLAUDE_AGENTS` | JSON object defining named agent personas | -- |
| `CLAUDE_ACTIVE_AGENT` | Default named agent to use | -- |

See `klaus serve --help` and the [Helm values](helm/klaus/values.yaml) for the full list including OAuth, tools, plugins, and session options.

## Deployment

Deployed via Helm chart in [`helm/klaus/`](helm/klaus/).

Key configuration in [`values.yaml`](helm/klaus/values.yaml):

- **Claude**: `model`, `maxTurns`, `systemPrompt`, `persistentMode`, `maxBudgetUSD`
- **API key**: via Kubernetes Secret (`anthropicApiKey.secretName`)
- **Workspace**: optional PVC for the agent workspace
- **OAuth**: optional OAuth 2.1 protection for the `/mcp` endpoint

## Development

See [docs/development.md](docs/development.md) for local setup, building, and testing.

## License

[Apache 2.0](LICENSE)

# Environment Variables

All klaus configuration is done via environment variables. Only `ANTHROPIC_API_KEY` is required.

## Required

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |

## Claude Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CLAUDE_MODEL` | Model name or alias (`sonnet`, `opus`, `haiku`) | CLI default |
| `CLAUDE_SYSTEM_PROMPT` | Override the default system prompt | -- |
| `CLAUDE_APPEND_SYSTEM_PROMPT` | Append to the default system prompt | -- |
| `CLAUDE_MAX_TURNS` | Max agentic turns per prompt (0 = unlimited) | `0` |
| `CLAUDE_MAX_BUDGET_USD` | Spending cap per invocation in USD | -- |
| `CLAUDE_EFFORT` | Effort level: `low`, `medium`, `high` | CLI default |
| `CLAUDE_FALLBACK_MODEL` | Fallback model when primary is overloaded | -- |
| `CLAUDE_PERMISSION_MODE` | Permission mode (see below) | `bypassPermissions` |
| `CLAUDE_WORKSPACE` | Working directory for the agent | -- |
| `CLAUDE_JSON_SCHEMA` | JSON Schema for structured output | -- |

### Permission modes

| Mode | Behavior |
|------|----------|
| `bypassPermissions` | Skip all permission checks (for headless operation) |
| `acceptEdits` | Auto-accept file edits |
| `dontAsk` | Skip tasks requiring permission |
| `plan` | Planning mode, no actions taken |
| `delegate` | Delegate permission decisions |
| `default` | Normal interactive permissions |

## Operating Mode

| Variable | Description | Default |
|----------|-------------|---------|
| `CLAUDE_MODE` | Operating mode: `agent` or `chat` | `agent` |
| `CLAUDE_INCLUDE_PARTIAL_MESSAGES` | Emit partial message chunks during streaming | `false` |

## Tool Control

| Variable | Description | Default |
|----------|-------------|---------|
| `CLAUDE_TOOLS` | Base set of built-in tools (comma-separated) | all defaults |
| `CLAUDE_ALLOWED_TOOLS` | Tool access patterns (e.g. `Bash(git:*)`) | -- |
| `CLAUDE_DISALLOWED_TOOLS` | Tools to block (e.g. `Bash(rm:*)`) | -- |

## Extensions

| Variable | Description | Default |
|----------|-------------|---------|
| `CLAUDE_ADD_DIRS` | Additional directories for skills/agents (comma-separated) | -- |
| `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD` | Load CLAUDE.md from additional dirs | -- |
| `CLAUDE_AGENTS` | JSON object defining named agents | -- |
| `CLAUDE_ACTIVE_AGENT` | Default agent to use | -- |
| `CLAUDE_MCP_CONFIG` | Path to MCP servers config file | -- |
| `CLAUDE_STRICT_MCP_CONFIG` | Only use servers from MCP config | `true` |
| `CLAUDE_SETTINGS_FILE` | Path to settings JSON (for hooks) | -- |
| `CLAUDE_SETTING_SOURCES` | Setting sources to load (comma-separated) | -- |
| `CLAUDE_PLUGIN_DIRS` | Plugin directories (comma-separated) | -- |

## Subprocess retry

| Variable | Description | Default |
|----------|-------------|---------|
| `KLAUS_RETRY_MAX_ATTEMPTS` | Maximum subprocess restart attempts by the watchdog | `3` |
| `KLAUS_RETRY_BASE_BACKOFF` | Initial backoff before first restart (e.g. `2s`); doubles each attempt | `2s` |

## Session store

| Variable | Description | Default |
|----------|-------------|---------|
| `KLAUS_PGSQL_DSN` | PostgreSQL DSN; when set, Postgres is used as the session backend | -- |
| `KLAUS_SESSION_DIR` | Override directory for the local backend | -- |
| `CLAUDE_CONTEXT_ID` | Pre-seed context ID at startup | -- |
| `CLAUDE_SESSION_ID` | Pre-seed session ID at startup; also passed as `--resume` in chat mode | -- |

## kagent

| Variable | Description | Default |
|----------|-------------|---------|
| `KAGENT_API_ENDPOINT` | kagent controller base URL; enables A2A turn push to the kagent UI | -- |

## Memory augmentation

Requires `KAGENT_MEMORY_ENDPOINT` and `KLAUS_EMBEDDING_MODEL` to be set. When either is absent, memory is silently disabled.

| Variable | Description | Default |
|----------|-------------|---------|
| `KAGENT_MEMORY_ENDPOINT` | kagent controller base URL for memory storage/retrieval | -- |
| `KAGENT_MEMORY_AGENT_NAME` | Agent identifier for memory attribution | `klaus` |
| `KAGENT_MEMORY_USER_ID` | User identifier for memory attribution | `default` |
| `KLAUS_EMBEDDING_ENDPOINT` | OpenAI-compatible embedding base URL | `https://api.openai.com/v1` |
| `KLAUS_EMBEDDING_MODEL` | Embedding model name (e.g. `text-embedding-3-small`); required to enable memory | -- |
| `KLAUS_EMBEDDING_API_KEY` | API key for the embedding endpoint; omit for unauthenticated endpoints | -- |

## OTel tracing (server-side)

Klaus exports its own traces (A2A task lifecycle, subprocess restarts) when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. The Claude Code subprocess has its own OTel env vars (see [Telemetry](../telemetry.md)).

| Variable | Description | Default |
|----------|-------------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP exporter endpoint; leave unset to disable | -- |

## Server

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `KLAUS_OWNER_SUBJECT` | Owner identity for JWT access control | -- |

## Validation

The following variables are validated at startup:

- `CLAUDE_MODE` must be `agent` or `chat`
- `CLAUDE_EFFORT` must be `low`, `medium`, or `high`
- `CLAUDE_PERMISSION_MODE` must be a valid mode
- `CLAUDE_MAX_TURNS` must be >= 0
- `CLAUDE_MAX_BUDGET_USD` must be >= 0
- `KLAUS_RETRY_BASE_BACKOFF` must be a valid Go duration (e.g. `2s`, `500ms`)

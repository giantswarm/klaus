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

## Process Mode

| Variable | Description | Default |
|----------|-------------|---------|
| `CLAUDE_PERSISTENT_MODE` | Use persistent subprocess mode | `false` |
| `CLAUDE_INCLUDE_PARTIAL_MESSAGES` | Emit partial message chunks during streaming | `false` |

## Session Management

| Variable | Description | Default |
|----------|-------------|---------|
| `CLAUDE_NO_SESSION_PERSISTENCE` | Don't save sessions to disk | `true` |

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

## Server

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `KLAUS_OWNER_SUBJECT` | Owner identity for JWT access control | -- |

## Validation

The following variables are validated at startup:

- `CLAUDE_EFFORT` must be `low`, `medium`, or `high`
- `CLAUDE_PERMISSION_MODE` must be a valid mode
- `CLAUDE_MAX_TURNS` must be >= 0
- `CLAUDE_MAX_BUDGET_USD` must be >= 0

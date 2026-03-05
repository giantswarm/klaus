# MCP Tools

Klaus exposes four MCP tools via the `/mcp` Streamable HTTP endpoint.

## `prompt`

Send a task to the Claude agent.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `message` | string | yes | The prompt to send |
| `blocking` | boolean | no | Wait for completion (default: `false`) |
| `session_id` | string | no | Resume a specific session |
| `resume` | boolean | no | Resume the session specified by `session_id` |
| `continue` | boolean | no | Continue the most recent session |
| `fork_session` | boolean | no | Fork the session when resuming |
| `agent` | string | no | Select a named agent for this prompt |
| `effort` | string | no | Override effort level (`low`, `medium`, `high`) |
| `max_budget_usd` | number | no | Override per-invocation spending cap |
| `json_schema` | string | no | Override JSON Schema for structured output |

### Non-blocking response (default)

```json
{"status": "started", "session_id": "..."}
```

### Blocking response (`blocking: true`)

```json
{
  "result": "...",
  "message_count": 12,
  "total_cost_usd": 0.45,
  "session_id": "..."
}
```

### Concurrent rejection

Klaus handles one prompt at a time. A second prompt while busy returns an error: `"claude process is already busy"`.

## `status`

Query agent state, progress, and result. This is the primary way to monitor non-blocking tasks.

| Parameter | None |

### Response fields

| Field | Description |
|-------|-------------|
| `status` | `idle`, `busy`, `completed`, `stopped`, `error` |
| `result` | Agent output text (when `completed`) |
| `message_count` | Messages processed so far |
| `tool_call_count` | Tool calls made so far |
| `last_tool_name` | Most recent tool used |
| `last_message` | Most recent assistant message |
| `total_cost_usd` | Cumulative cost |
| `session_id` | Current session identifier |

### Status lifecycle

```
idle --> busy --> completed
                      |
                      v (next prompt clears result)
                    idle
```

The `completed` status persists until the next `prompt` call.

## `stop`

Terminate the running agent.

| Parameter | None |

Returns confirmation that the process was stopped.

## `result`

Get full untruncated result and message history from the last run. Intended for debugging and troubleshooting.

| Parameter | None |

### Response fields

| Field | Description |
|-------|-------------|
| `result_text` | Full untruncated agent output |
| `messages` | Complete message history array |
| `message_count` | Total messages |
| `total_cost_usd` | Total cost |
| `session_id` | Session identifier |

## MCP progress notifications

During non-blocking execution, klaus streams `notifications/progress` messages to MCP clients reporting tool usage, assistant output, and task completion.

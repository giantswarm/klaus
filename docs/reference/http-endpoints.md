# HTTP Endpoints

Klaus serves the following HTTP endpoints.

## `/mcp`

**MCP Streamable HTTP endpoint.** This is the main interface for MCP clients.

- Method: `POST`
- Content-Type: `application/json`
- Optionally protected by OAuth 2.1

Supports the standard MCP JSON-RPC protocol: `initialize`, `tools/list`, `tools/call`.

Returns an `Mcp-Session-Id` header on initialization for session tracking.

## `/healthz`

**Liveness probe.**

- Method: `GET`
- Response: `ok` (200)

Always returns 200 if the server is running.

## `/readyz`

**Readiness probe.** Reflects Claude process health.

- Method: `GET`
- Response: `ok` (200) or error (503)

Returns 503 when the process is `starting`, `stopped`, or `error`. Returns 200 for `idle`, `busy`, and `completed` states.

## `/status`

**JSON status endpoint.** Returns detailed server and agent status.

- Method: `GET`
- Response: `application/json`

```json
{
  "name": "klaus",
  "version": "0.1.0",
  "mode": "single-shot",
  "agent": {
    "status": "idle",
    "message_count": 0,
    "tool_call_count": 0,
    "total_cost_usd": 0
  }
}
```

## `/metrics`

**Prometheus metrics.** Always available regardless of OpenTelemetry configuration.

- Method: `GET`
- Response: `text/plain` (Prometheus exposition format)

See [Set Up Monitoring](../how-to/set-up-monitoring.md) for available metrics.

## `/v1/chat/completions`

**OpenAI-compatible chat completions endpoint.** Accepts a prompt and returns the agent's response, either streamed as SSE or as a single JSON response.

- Method: `POST`
- Content-Type: `application/json`
- Protected by owner authentication (same as `/mcp`)

Only the last user message in the `messages` array is used as the prompt -- the instance maintains its own conversation state.

Returns `429 Too Many Requests` if the agent is already processing a prompt.

### Request body

```json
{
  "messages": [
    {"role": "user", "content": "Explain how kubernetes pods work"}
  ],
  "stream": true,
  "model": "klaus"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `messages` | array | yes | Array of `{role, content}` messages. Only the last `user` message is sent as the prompt. |
| `stream` | bool | no | When `true`, response is streamed as SSE in OpenAI delta format. Default `false`. |
| `model` | string | no | Echoed in the response. Defaults to `"klaus"`. |

### Streaming response (`stream: true`)

Returns `text/event-stream` with SSE events in OpenAI delta format:

```
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1711234567,"model":"klaus","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1711234567,"model":"klaus","choices":[{"index":0,"delta":{"role":"assistant","content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1711234567,"model":"klaus","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

Tool use events appear as text deltas with `[Using tool: <name>]` content.

### Non-streaming response (`stream: false`)

Returns `application/json`:

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1711234567,
  "model": "klaus",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "Hello world"},
      "finish_reason": "stop"
    }
  ]
}
```

## `/v1/chat/messages`

**Conversation history endpoint.** Returns the agent's conversation messages.

- Method: `GET`
- Response: `application/json`
- Protected by owner authentication (same as `/mcp`)

### Response

```json
{
  "status": "idle",
  "messages": [
    {"role": "user", "content": "hello"},
    {"role": "assistant", "content": "Hi there!"}
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Current agent status (`idle`, `busy`, `completed`, etc.) |
| `messages` | array | Array of `{role, content}` messages. Empty messages are filtered out. |

## `/`

**Root endpoint.** Returns the server name and version.

- Method: `GET`
- Response: `klaus <version>`

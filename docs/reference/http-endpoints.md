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

## `/`

**Root endpoint.** Returns the server name and version.

- Method: `GET`
- Response: `klaus <version>`

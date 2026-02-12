# klaus

An [MCP](https://modelcontextprotocol.io/) server that wraps [Claude Code](https://docs.anthropic.com/en/docs/claude-code) to run AI coding agents inside Kubernetes.

## Overview

Klaus runs the Claude Code CLI as a managed subprocess and exposes it over HTTP as a Streamable HTTP MCP endpoint. This allows orchestrating AI coding agents in Kubernetes with proper lifecycle management, health checks, and optional OAuth authentication.

### MCP Tools

| Tool | Description |
|------|-------------|
| `prompt` | Send a task to the Claude agent |
| `status` | Query agent state, cost, and message count |
| `stop` | Terminate the running agent |

### HTTP Endpoints

| Path | Purpose |
|------|---------|
| `/mcp` | MCP Streamable HTTP (optionally OAuth-protected) |
| `/healthz` | Liveness probe |
| `/readyz` | Readiness probe (Claude process state) |
| `/status` | JSON status (version, agent state, cost) |

## Architecture

```
HTTP Client --> /mcp --> MCP Server --> Prompter --> Claude Code CLI (subprocess)
```

The container image is based on `node:22-slim` with the Claude Code CLI installed globally via npm. The Go binary manages the Claude subprocess in one of two modes:

- **Single-shot** (default): new subprocess per prompt
- **Persistent**: long-running subprocess with multi-turn conversations

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

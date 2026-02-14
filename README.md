# klaus

An [MCP](https://modelcontextprotocol.io/) server that wraps [Claude Code](https://docs.anthropic.com/en/docs/claude-code) to run AI coding agents inside Kubernetes.

## Overview

Klaus runs the Claude Code CLI as a managed subprocess and exposes it over HTTP as a Streamable HTTP MCP endpoint. This allows orchestrating AI coding agents in Kubernetes with proper lifecycle management, health checks, and optional OAuth authentication.

```
MCP Client --> /mcp --> MCP Server --> Prompter --> Claude Code CLI (subprocess)
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `prompt` | Send a task to the Claude agent. Non-blocking by default (set `blocking=true` to wait for completion). |
| `status` | Query agent state, progress, and result. Returns `completed` with result when a non-blocking task finishes. |
| `stop` | Terminate the running agent |
| `result` | Get full untruncated result and message history from the last run (debugging tool) |

### Extension system

Klaus supports the full Claude Code extension surface, configured via Helm values, klausctl config, or operator CRDs:

- **[Skills](docs/how-to/configure-skills.md)** -- Domain knowledge loaded as `SKILL.md` files
- **[Subagents](docs/how-to/configure-subagents.md)** -- Specialized agents for task delegation
- **[Hooks](docs/how-to/configure-hooks.md)** -- Lifecycle hooks for validation and automation
- **[MCP Servers](docs/how-to/configure-mcp-servers.md)** -- External tool integrations
- **[Plugins](docs/how-to/use-plugins.md)** -- OCI-distributed extension bundles

## Quick start

```bash
export ANTHROPIC_API_KEY=sk-ant-...
klaus serve
# MCP endpoint available at http://localhost:8080/mcp
```

Or with Docker:

```bash
docker run -d -p 8080:8080 -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY klaus:latest serve
```

Or deploy to Kubernetes:

```bash
helm install klaus helm/klaus/ -n klaus \
  --set anthropicApiKey.secretName=anthropic-api-key
```

## Documentation

Full documentation is available in the [`docs/`](docs/index.md) directory, organized by the [Diataxis](https://diataxis.fr/) framework:

| Section | Purpose |
|---------|---------|
| [Tutorials](docs/tutorials/) | Step-by-step guides to get started |
| [How-to Guides](docs/how-to/) | Task-oriented guides for specific goals |
| [Reference](docs/reference/) | Technical specifications (env vars, MCP tools, Helm values) |
| [Explanation](docs/explanation/) | Architecture, design, and background context |

## Development

See [docs/development.md](docs/development.md) for local setup, building, and testing.

## Related repositories

| Repository | Description |
|-----------|-------------|
| [klaus-operator](https://github.com/giantswarm/klaus-operator) | Kubernetes operator for dynamic instance management |
| [klausctl](https://github.com/giantswarm/klausctl) | CLI for managing local klaus containers |

## License

[Apache 2.0](LICENSE)

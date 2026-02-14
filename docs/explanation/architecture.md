# Architecture

## Overview

Klaus is a Go binary that manages the Claude Code CLI as a subprocess and exposes it over HTTP as an MCP endpoint.

```
MCP Client --> /mcp --> MCP Server --> Prompter --> Claude Code CLI (subprocess)
```

The container image is based on `node:22-slim` with the Claude Code CLI installed globally via npm. The Go binary handles lifecycle management, health checks, metrics, and authentication.

## Core components

### `pkg/claude` -- Claude CLI wrapper

The `Prompter` interface abstracts over two process modes:

- **`Process`** (single-shot): spawns a new `claude --print` subprocess per prompt. Supports per-invocation overrides (session, effort, agent).
- **`PersistentProcess`**: maintains a long-running subprocess with bidirectional `--input-format stream-json`. Provides conversation continuity, lower latency, and cumulative cost tracking. A watchdog auto-restarts on crash.

Both implementations share a common `Options.baseArgs()` method that builds the CLI flags from configuration.

### `pkg/mcp` -- MCP protocol

Uses the `mcp-go` library to create a Streamable HTTP server with four tools: `prompt`, `status`, `stop`, `result`. The `prompt` tool is non-blocking by default -- it starts the task and returns immediately. Callers poll `status` for progress and results.

### `pkg/server` -- HTTP server

Wraps the MCP server and adds operational endpoints (`/healthz`, `/readyz`, `/status`, `/metrics`). Optionally adds OAuth 2.1 protection via the `mcp-oauth` library.

### `pkg/metrics` -- Prometheus metrics

Server-side metrics exposed at `/metrics`. Tracks prompts, duration, cost, messages, tool calls, and process restarts.

## Request flow

1. MCP client sends `tools/call` with `name: "prompt"` to `/mcp`
2. The MCP server extracts parameters and calls `Prompter.Submit()` (non-blocking) or `Prompter.RunSyncWithOptions()` (blocking)
3. The prompter spawns (or writes to) the Claude Code subprocess
4. Claude Code processes the prompt, using tools, reading files, running commands
5. Output streams back as `stream-json` messages
6. The prompter collects messages, tracks cost, and stores the result
7. The MCP client polls `status` to retrieve the result

## The klaus ecosystem

Klaus has three deployment modes, each producing the same environment variables and file mounts:

| Component | Role |
|-----------|------|
| **klaus** | The Go binary / MCP server (this repo) |
| **Helm chart** | Standalone Kubernetes deployment (`helm/klaus/`) |
| **[klaus-operator](https://github.com/giantswarm/klaus-operator)** | Kubernetes operator for dynamic instance management via CRDs |
| **[klausctl](https://github.com/giantswarm/klausctl)** | CLI for managing local klaus containers (Docker/Podman) |

All three modes produce the same inputs for the klaus binary: environment variables, CLI flags, and mounted files. Klaus itself is mode-agnostic.

## See also

- [Execution Modes](execution-modes.md) for details on single-shot vs persistent mode
- [Extension System](extension-system.md) for how skills, agents, hooks, and plugins are loaded

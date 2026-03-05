# Execution Modes

Klaus supports two process modes for running the Claude Code subprocess, and three deployment modes for running klaus itself.

## Process modes

### Single-shot (default)

Spawns a new `claude --print --output-format stream-json` subprocess for each prompt. The process runs to completion and exits.

```
prompt 1 --> [spawn process] --> [run] --> [exit]
prompt 2 --> [spawn process] --> [run] --> [exit]
```

**Characteristics:**
- Clean isolation between prompts -- no state leaks
- Supports per-invocation overrides (session, effort, agent, budget)
- Higher latency (subprocess startup cost per prompt)
- Session persistence optional via `--resume` with `session_id`

### Persistent (`CLAUDE_PERSISTENT_MODE=true`)

Maintains a single long-running subprocess using `--input-format stream-json`. User messages are written to stdin, responses read from stdout.

```
[spawn process] --> prompt 1 --> response 1
                    prompt 2 --> response 2
                    prompt 3 --> response 3
```

**Characteristics:**
- Conversation continuity -- the agent retains context across prompts
- Lower latency -- no subprocess startup cost
- Cost efficiency -- Claude references prior context instead of re-processing
- Cumulative cost tracking across prompts
- Watchdog auto-restarts on crash with 2-second backoff
- Per-invocation overrides (session, effort, agent) are not supported and generate warnings

### Choosing a mode

| Use case | Recommended mode |
|----------|-----------------|
| Isolated, one-off tasks | Single-shot |
| Multi-turn conversations | Persistent |
| Per-prompt overrides needed | Single-shot |
| Low latency required | Persistent |
| Cost-sensitive workloads | Persistent |

## Async execution

Both process modes support non-blocking (async) execution. The `prompt` MCP tool is non-blocking by default:

1. `prompt(message: "...")` returns `{status: "started"}` immediately
2. The task runs in the background
3. Callers poll `status()` for progress
4. When `status` shows `completed`, the `result` field contains the output

Set `blocking: true` on the `prompt` tool for synchronous execution (waits for completion).

Async is orthogonal to the process mode -- all four combinations are valid:

| | Single-shot | Persistent |
|---|-------------|-----------|
| **Non-blocking** | Long tasks with per-invocation overrides | Long tasks with conversation memory |
| **Blocking** | Short queries (current default) | Short queries with conversation memory |

## Deployment modes

Klaus has three ways to run, each targeting different environments:

### Standalone Helm chart

Direct Kubernetes deployment via `helm/klaus/`. Configuration is in `values.yaml`. Extensions (skills, agents, hooks, plugins) are rendered into ConfigMaps and mounted as volumes.

Best for: teams that manage their own Kubernetes deployments.

### Operator-managed (klaus-operator)

The [klaus-operator](https://github.com/giantswarm/klaus-operator) manages klaus instances via `KlausInstance` CRDs. It handles namespace isolation, credential distribution, and dynamic provisioning. `KlausPersonality` CRDs provide reusable instance templates.

Best for: multi-tenant platforms where instances are provisioned on demand.

### Local (klausctl)

[klausctl](https://github.com/giantswarm/klausctl) manages local klaus containers via Docker or Podman. It renders the same configuration into container mounts and environment variables, and uses ORAS to pull OCI plugins.

Best for: local development and testing.

### Mode comparison

| Aspect | Helm chart | Operator | klausctl |
|--------|-----------|----------|---------|
| **Target** | Kubernetes | Kubernetes | Local machine |
| **Config** | `values.yaml` | CRDs | `~/.config/klausctl/config.yaml` |
| **Plugin delivery** | OCI image volumes | OCI image volumes | ORAS client |
| **Auth** | OAuth 2.1 / muster | Token forwarding | None (local) |
| **Multi-instance** | One per release | Dynamic via CRDs | One per workspace |
| **Orchestration** | Manual | Automatic | Manual |

All three modes produce the same environment variables and file mounts for the klaus binary. Klaus itself is deployment-agnostic.

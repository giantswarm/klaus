# Configure Subagents

Subagents are specialized AI assistants that the main agent can delegate tasks to. Each runs in its own context with its own prompt, tools, model, and permissions. See [Claude Code Subagents](https://code.claude.com/docs/en/sub-agents) for background.

Klaus supports two formats, with JSON taking highest priority.

## JSON format (via `claude.agents`)

Passed to Claude Code via the `--agents` CLI flag. Highest priority -- overrides file-based agents with the same name.

```yaml
claude:
  agents:
    reviewer:
      description: "Reviews code changes. Use proactively after edits."
      prompt: "You are a senior code reviewer. Focus on quality, security, and best practices."
      tools: ["Read", "Grep", "Glob", "Bash"]
      model: "sonnet"
    deployer:
      description: "Handles deployments to staging and production."
      prompt: "You are a deployment specialist..."
      permissionMode: "plan"
```

## Markdown format (via `claude.agentFiles`)

Loaded via `--add-dir` as `.claude/agents/<name>.md` files:

```yaml
claude:
  agentFiles:
    researcher:
      content: |
        ---
        name: researcher
        description: Research agent that gathers information
        tools: Read, Grep, Glob
        model: haiku
        ---
        You are a research agent. Gather information and provide thorough summaries.
```

## Agent fields

| Field | Description |
|-------|-------------|
| `description` | When to delegate to this agent (required) |
| `prompt` | System prompt for the agent (required for JSON format) |
| `tools` | Tools the agent can use (inherits all if omitted) |
| `disallowedTools` | Tools to deny |
| `model` | Model to use (`sonnet`, `opus`, `haiku`, `inherit`) |
| `permissionMode` | Permission handling mode |
| `maxTurns` | Maximum agentic turns |
| `skills` | Skills to preload into the agent's context |
| `mcpServers` | MCP servers available to this agent |
| `hooks` | Lifecycle hooks scoped to this agent |
| `memory` | Persistent memory scope (`user`, `project`, `local`) |

## Select the top-level agent

Use `activeAgent` to change which agent handles all incoming prompts:

```yaml
claude:
  activeAgent: "researcher"  # must match a name in agents or agentFiles
```

Or override per-prompt via the MCP `prompt` tool's `agent` parameter.

> **Known issue:** Combining `agents` (JSON) with `activeAgent` pointing to one of those agents may cause hangs. Use `agentFiles` (markdown) when you need agent selection via `activeAgent`.

## See also

- [Extension System explanation](../explanation/extension-system.md) for how agents fit into the architecture
- [Claude Code Subagents docs](https://code.claude.com/docs/en/sub-agents) for full subagent configuration

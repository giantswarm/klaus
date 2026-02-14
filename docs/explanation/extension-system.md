# Extension System

Klaus supports the full Claude Code extension surface: skills, subagents, hooks, MCP servers, plugins, and memory. All extensions are configured externally and passed to the Claude Code subprocess via environment variables, CLI flags, and mounted files.

## How extensions reach Claude Code

```
Helm values / klausctl config / Operator CRD
         |
         v
    ConfigMaps + Volume Mounts + Env Vars
         |
         v
    klaus binary (passes flags/env to subprocess)
         |
         v
    Claude Code CLI (loads extensions from filesystem)
```

Klaus itself does not interpret extension content. It passes configuration through to Claude Code, which handles loading, matching, and execution.

## Extension types

### Skills

[Skills](https://code.claude.com/docs/en/skills) teach the agent domain knowledge and conventions. Each skill is a `SKILL.md` file with YAML frontmatter and markdown instructions.

- **Mechanism:** Rendered as files at `.claude/skills/<name>/SKILL.md`, loaded via `--add-dir` / `CLAUDE_ADD_DIRS`
- **Invocation:** Claude loads skill descriptions at session start and activates them when relevant. Users can also invoke directly with `/skill-name`.
- **Helm:** `claude.skills` map in `values.yaml`

### Subagents

[Subagents](https://code.claude.com/docs/en/sub-agents) are specialized AI assistants that the main agent delegates tasks to. Each runs in its own context with its own prompt, tools, and model.

- **JSON format:** Passed via `--agents` / `CLAUDE_AGENTS`. Highest priority. Helm: `claude.agents`
- **File format:** Markdown files at `.claude/agents/<name>.md`, loaded via `--add-dir`. Helm: `claude.agentFiles`
- **Selection:** `--agent` / `CLAUDE_ACTIVE_AGENT` sets the top-level agent. Per-prompt override via MCP `agent` parameter.

### Hooks

[Hooks](https://code.claude.com/docs/en/hooks-guide) are shell commands that execute at lifecycle points (before/after tool use, on stop, etc.). They provide deterministic control -- formatting, validation, logging -- without relying on the LLM.

- **Mechanism:** Rendered to `settings.json`, loaded via `--settings` / `CLAUDE_SETTINGS_FILE`. Hook scripts mounted with execute permissions.
- **Helm:** `claude.hooks` for configuration, `claude.hookScripts` for script contents

### MCP servers

[MCP servers](https://code.claude.com/docs/en/mcp) connect the agent to external tools and data sources (GitHub, Sentry, databases, etc.).

- **Mechanism:** Rendered as `.mcp.json`, loaded via `--mcp-config` / `CLAUDE_MCP_CONFIG`
- **Helm:** `claude.mcpConfig` (inline JSON)
- **Security:** `claude.strictMcpConfig: true` prevents loading untrusted MCP servers from the workspace

### Plugins

[Plugins](https://code.claude.com/docs/en/plugins) are the composition unit that bundles skills, agents, hooks, and MCP servers into a single distributable package.

- **Mechanism:** Directories loaded via `--plugin-dir` / `CLAUDE_PLUGIN_DIRS`
- **Delivery:** OCI image volumes on Kubernetes (KEP-4639), ORAS client for local mode
- **Helm:** `claude.plugins` for OCI references, `claude.pluginDirs` for additional directories

### Memory

[Memory](https://code.claude.com/docs/en/memory) files (`CLAUDE.md`) provide persistent instructions and context that Claude loads at session start.

- **Mechanism:** Loaded from directories specified via `--add-dir` when `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=true`
- **Helm:** `claude.loadAdditionalDirsMemory: true` (default)

## Extension priority

When the same name appears at multiple levels:

1. **JSON agents** (`claude.agents`) override file-based agents with the same name
2. **Skills, hooks, MCP config** are additive -- they don't override each other
3. **Plugins** are namespaced and don't conflict with inline extensions
4. **`strictMcpConfig`** prevents workspace-level MCP configs from being loaded

## Consistency across deployment modes

All three deployment modes (Helm chart, operator, klausctl) produce the same underlying inputs:

| Extension | Env var / Flag | Files |
|-----------|---------------|-------|
| Skills | `CLAUDE_ADD_DIRS` | `.claude/skills/<name>/SKILL.md` |
| Agents (JSON) | `CLAUDE_AGENTS` | -- |
| Agent files | `CLAUDE_ADD_DIRS` | `.claude/agents/<name>.md` |
| Hooks | `CLAUDE_SETTINGS_FILE` | `settings.json` + scripts |
| MCP servers | `CLAUDE_MCP_CONFIG` | `.mcp.json` |
| Plugins | `CLAUDE_PLUGIN_DIRS` | Plugin directory trees |

This means configuration knowledge transfers between modes -- the same skill definition works in Helm values, klausctl config, and operator CRDs.

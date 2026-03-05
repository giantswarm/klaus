# Configure Hooks

Hooks are shell commands that execute at specific points in Claude Code's lifecycle. Use them to validate commands, format code after edits, log tool usage, or enforce project rules. See [Claude Code Hooks](https://code.claude.com/docs/en/hooks-guide) for background.

## Define hooks in Helm values

Hooks are rendered to a `settings.json` file and loaded via `CLAUDE_SETTINGS_FILE`:

```yaml
claude:
  hooks:
    PreToolUse:
      - matcher: "Bash"
        hooks:
          - type: command
            command: /etc/klaus/hooks/validate-command.sh
            timeout: 5000
    PostToolUse:
      - matcher: "Edit|Write"
        hooks:
          - type: command
            command: /etc/klaus/hooks/format-code.sh
    Stop:
      - hooks:
          - type: prompt
            prompt: "Check if all tasks are complete."
```

> `claude.hooks` and `claude.settingsFile` are mutually exclusive.

## Hook scripts

Define hook script contents inline. They are mounted at `/etc/klaus/hooks/` with execute permissions:

```yaml
claude:
  hookScripts:
    validate-command.sh: |
      #!/bin/bash
      INPUT=$(cat)
      COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
      if echo "$COMMAND" | grep -qE '\b(rm -rf|DROP TABLE)\b'; then
        echo "Blocked: dangerous command" >&2
        exit 2  # exit 2 = block the action
      fi
      exit 0

    format-code.sh: |
      #!/bin/bash
      jq -r '.tool_input.file_path' | xargs npx prettier --write 2>/dev/null
      exit 0
```

## Hook events

| Event | Fires when | Matcher matches |
|-------|-----------|-----------------|
| `PreToolUse` | Before a tool executes (can block) | Tool name |
| `PostToolUse` | After a tool succeeds | Tool name |
| `Stop` | Agent finishes responding | -- |
| `Notification` | Agent needs attention | Notification type |
| `SubagentStart` | A subagent is spawned | Agent type |
| `SubagentStop` | A subagent finishes | Agent type |

## Exit codes

| Code | Behavior |
|------|----------|
| `0` | Action proceeds; stdout added to context |
| `2` | Action blocked; stderr fed back to the agent |
| Other | Action proceeds; stderr logged |

## Hook types

- `command` -- Run a shell command
- `prompt` -- Single-turn LLM evaluation (yes/no decision)
- `agent` -- Multi-turn subagent verification with tool access

## See also

- [Claude Code Hooks reference](https://code.claude.com/docs/en/hooks) for full event schemas and JSON I/O
- [Extension System explanation](../explanation/extension-system.md)

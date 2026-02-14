# Configure Skills

Skills teach the agent domain-specific knowledge and conventions. They are [Claude Code skills](https://code.claude.com/docs/en/skills) rendered as `SKILL.md` files and loaded via `--add-dir`.

## Inline skills in Helm values

Define skills directly in `values.yaml`:

```yaml
claude:
  skills:
    api-conventions:
      description: "REST API design patterns for our codebase"
      content: |
        When writing API endpoints:
        - Use RESTful naming conventions
        - Return consistent error formats
        - Include request validation

    deploy:
      description: "Deploy the application to production"
      disableModelInvocation: true
      allowedTools: "Bash(kubectl:*)"
      content: |
        Deploy using helm upgrade...
```

Each skill is rendered as `/etc/klaus/extensions/.claude/skills/<name>/SKILL.md` and loaded via `CLAUDE_ADD_DIRS`.

## Supported frontmatter fields

| Field | Description |
|-------|-------------|
| `description` | When the skill should be used (required) |
| `disableModelInvocation` | Only allow manual invocation (`true`/`false`) |
| `userInvocable` | Whether users can invoke directly (`true`/`false`) |
| `allowedTools` | Tools the agent can use when this skill is active |
| `model` | Override model for this skill |
| `context` | Set to `fork` to run in a subagent context |
| `agent` | Which subagent type to use with `context: fork` |
| `argumentHint` | Hint for expected arguments |

## Skills via Docker volumes

For local development, mount skill directories directly:

```bash
mkdir -p /tmp/skills/api-conventions

cat > /tmp/skills/api-conventions/SKILL.md << 'EOF'
---
description: "API design patterns"
---
When writing API endpoints, use RESTful conventions.
EOF

docker run -d --name klaus -p 8080:8080 \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions \
  -e CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=true \
  -v /tmp/skills/api-conventions/SKILL.md:/etc/klaus/extensions/.claude/skills/api-conventions/SKILL.md:ro \
  klaus:latest serve
```

## Additional directories

Add extra directories containing `.claude/` subdirectories:

```yaml
claude:
  addDirs:
    - /mnt/shared-skills
  loadAdditionalDirsMemory: true  # also load CLAUDE.md files from these dirs
```

## See also

- [Extension System explanation](../explanation/extension-system.md) for how skills fit into the architecture
- [Claude Code Skills docs](https://code.claude.com/docs/en/skills) for skill authoring details

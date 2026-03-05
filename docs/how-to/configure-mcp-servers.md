# Configure MCP Servers

Connect the agent to external tools and data sources via [MCP servers](https://code.claude.com/docs/en/mcp). This lets the agent use GitHub, Sentry, databases, and other integrations.

## Inline MCP config

Provide a raw JSON MCP configuration:

```yaml
claude:
  mcpConfig: |
    {
      "mcpServers": {
        "github": {
          "type": "http",
          "url": "https://api.githubcopilot.com/mcp/",
          "headers": {
            "Authorization": "Bearer ${GITHUB_TOKEN}"
          }
        }
      }
    }
  strictMcpConfig: true  # only use servers from this config
```

The config is mounted as a file and passed via `--mcp-config`.

## Environment variable expansion

MCP configs support `${VAR}` syntax for secrets. On Kubernetes, inject the secrets as env vars:

```yaml
# In a separate values file or via --set
extraEnv:
  - name: GITHUB_TOKEN
    valueFrom:
      secretKeyRef:
        name: mcp-secrets
        key: github-token
```

## Strict mode

When `strictMcpConfig: true` (default), Claude Code ignores any user, project, or local MCP configurations and only uses the servers defined in `mcpConfig`. This is recommended for production security.

## Docker example

```bash
cat > /tmp/mcp-config.json << 'EOF'
{
  "mcpServers": {
    "my-server": {
      "type": "http",
      "url": "http://host.docker.internal:8090/mcp"
    }
  }
}
EOF

docker run -d --name klaus -p 8080:8080 \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  -e CLAUDE_MCP_CONFIG=/tmp/mcp-config.json \
  -v /tmp/mcp-config.json:/tmp/mcp-config.json:ro \
  klaus:latest serve
```

## See also

- [Claude Code MCP docs](https://code.claude.com/docs/en/mcp) for supported transports and authentication
- [Extension System explanation](../explanation/extension-system.md)

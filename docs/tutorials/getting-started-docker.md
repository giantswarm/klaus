# Getting Started with Docker

Run a klaus instance locally using Docker.

## Prerequisites

- Docker (or Podman)
- A valid `ANTHROPIC_API_KEY`

## Start klaus

```bash
docker build -t klaus:latest .

docker run -d --name klaus -p 8080:8080 \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  klaus:latest serve
```

Verify it's running:

```bash
curl http://localhost:8080/healthz   # -> ok
curl http://localhost:8080/status    # -> JSON with agent status
```

## Send your first prompt

Initialize an MCP session and send a prompt:

```bash
# 1. Initialize
MCP_SESSION=$(curl -s -i -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
    "protocolVersion":"2025-03-26","capabilities":{},
    "clientInfo":{"name":"test","version":"1.0"}
  }}' 2>&1 | grep -i "Mcp-Session-Id:" | awk '{print $2}' | tr -d '\r')

# 2. Send a blocking prompt
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{"message":"What is 2+2? Reply with just the number.","blocking":true}
  }}'
```

## Non-blocking workflow

For longer tasks, use the default non-blocking mode:

```bash
# Start a task (returns immediately)
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
    "name":"prompt",
    "arguments":{"message":"Write a haiku about Kubernetes."}
  }}'

# Poll for completion
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $MCP_SESSION" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{
    "name":"status","arguments":{}
  }}'
```

## Mount a workspace

Give the agent access to your code:

```bash
docker run -d --name klaus -p 8080:8080 \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  -e CLAUDE_WORKSPACE=/workspace \
  -v $(pwd):/workspace \
  klaus:latest serve
```

## Next steps

- [Deploy to Kubernetes](deploying-to-kubernetes.md) for production use
- [Configure skills](../how-to/configure-skills.md) to teach the agent domain knowledge
- [Configure subagents](../how-to/configure-subagents.md) for specialized task delegation

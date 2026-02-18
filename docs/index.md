# Klaus Documentation

Klaus is an [MCP](https://modelcontextprotocol.io/) server that wraps [Claude Code](https://docs.anthropic.com/en/docs/claude-code) to run AI coding agents inside Kubernetes. It manages the Claude Code CLI as a subprocess and exposes it over HTTP as a Streamable HTTP MCP endpoint.

## Documentation Structure

### [Tutorials](tutorials/)

Step-by-step guides to get you up and running.

- [Getting Started with Docker](tutorials/getting-started-docker.md) -- Run klaus locally in a container
- [Deploying to Kubernetes](tutorials/deploying-to-kubernetes.md) -- Deploy klaus with Helm

### [How-to Guides](how-to/)

Task-oriented guides for specific goals.

- [Configure Skills](how-to/configure-skills.md) -- Add domain knowledge via Claude Code skills
- [Configure Subagents](how-to/configure-subagents.md) -- Define specialized agents for task delegation
- [Configure Hooks](how-to/configure-hooks.md) -- Add lifecycle hooks for tool validation and automation
- [Configure MCP Servers](how-to/configure-mcp-servers.md) -- Connect external tools via MCP
- [Use Plugins](how-to/use-plugins.md) -- Package and distribute extensions as OCI artifacts
- [Set Up Monitoring](how-to/set-up-monitoring.md) -- Prometheus metrics and OpenTelemetry
- [Secure with OAuth](how-to/secure-with-oauth.md) -- Protect the MCP endpoint with OAuth 2.1

### [Reference](reference/)

Technical specifications and configuration details.

- [Environment Variables](reference/environment-variables.md) -- All configuration variables
- [MCP Tools](reference/mcp-tools.md) -- Tool schemas and workflows
- [HTTP Endpoints](reference/http-endpoints.md) -- Health, status, and metrics
- [Helm Values](reference/helm-values.md) -- Complete Helm chart configuration

### [Explanation](explanation/)

Background and design context.

- [Architecture](explanation/architecture.md) -- How klaus works under the hood
- [Extension System](explanation/extension-system.md) -- Skills, agents, hooks, plugins, and MCP servers
- [OCI Artifacts](explanation/oci-artifacts.md) -- Plugin, personality, and toolchain packaging and delivery
- [Execution Modes](explanation/execution-modes.md) -- Single-shot, persistent, and deployment modes

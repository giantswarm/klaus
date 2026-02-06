# klaus

[![CircleCI](https://dl.circleci.com/status-badge/img/gh/giantswarm/klaus/tree/main.svg?style=svg)](https://dl.circleci.com/status-badge/redirect/gh/giantswarm/klaus/tree/main)

A Go wrapper around claude-code to orchestrate AI agents (with Plugins, MCP tools, Subagents, and Skills) within Kubernetes.

## What is this app?

klaus orchestrates AI coding agents inside Kubernetes clusters. It provides:

- Agent lifecycle management
- Plugin and MCP tool integration
- Subagent coordination
- Skill-based task routing

## Getting started

See [docs/development.md](docs/development.md) for local development instructions.

## Configuration

The Helm chart is located in [`helm/klaus/`](helm/klaus/). See [`values.yaml`](helm/klaus/values.yaml) for available configuration options.

## License

[Apache 2.0](LICENSE)

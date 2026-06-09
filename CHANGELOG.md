# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **A2A server** (`a2a.enabled`): Klaus exposes a JSON-RPC A2A endpoint at `/a2a`, accepting tasks from other A2A agents. Per-context serialisation prevents task-ID collisions under concurrent inbound requests.
- **A2A outbound client** (`a2a_call` tool): Calls remote A2A agents by name from within a Claude Code session. Static targets via `KLAUS_A2A_TARGETS` (JSON) or `a2a.targets` in Helm values. Dynamic resolution opt-in via `KLAUS_A2A_ALLOW_DYNAMIC`; all outbound URLs validated against an allowlist (`KLAUS_A2A_ALLOWED_HOSTS` / `a2a.allowedHosts`).
- **Retry with resume**: Persistent subprocess retries on exit up to `KLAUS_RETRY_MAX_ATTEMPTS` times (default 3), resuming from the last successful Claude session ID.
- **Semantic memory** (`KAGENT_MEMORY_ENDPOINT`): Per-user semantic memory backed by the kagent pgvector API. Relevant past turns prepended to each prompt; new turns embedded and stored after each response. Requires `KAGENT_MEMORY_ENDPOINT`, `KLAUS_EMBEDDING_ENDPOINT`, and `KLAUS_EMBEDDING_MODEL`.
- **OpenTelemetry spans**: Spans for subprocess start/stop, turn completion, retry events, and MCP tool calls (`pkg/telemetry`). Attribute key prefix `claude.*` for subprocess/session attributes. Global OTel provider initialised by mcp-toolkit `tracing.Init`.
- **Durable `claude-home` PVC** (`claudeHome.enabled`): A PersistentVolumeClaim for `/home/user/.claude` preserves Claude Code session files and authentication state across pod restarts.

### Changed

- `automountServiceAccountToken` defaults to `false` in the Helm chart Deployment.
- Bump `giantswarm/architect` orb to `8.2.2` and re-enable cosign keyless chart signing.
- Enable `split-china-push: true` on tag-build push jobs; replace cross-Pacific buildx push to Aliyun with `regctl image copy` on the in-China runner.
- Bump `giantswarm/architect` orb to `8.1.0` and migrate all image pushes from `push-to-registries-multiarch` to `push-to-registries` with `multiarch: true`.
- Bump Go to `1.26.3` and `golang.org/x/net` to `v0.53.0`.

### Removed

- **`/v1/chat/messages` HTTP endpoint**: Conversation history is now available via the MCP messages tool.
- **PostgreSQL session store** (`KLAUS_PGSQL_DSN`, `session.postgresSecretName`): Session resumption uses Claude's own session files on the `claudeHome` PVC via `--resume`; no external store needed.
- **kagent session/task push** (`KAGENT_API_ENDPOINT`): Turn push to the kagent UI is handled by the gateway's `ForwardingExecutor`. `pkg/kagentapi/auth.go` is retained for JWT parsing and per-user memory scoping.
- **Custom OTel setup** (`pkg/telemetry/Setup()`): Replaced by mcp-toolkit `tracing.Init` which sets the global provider at startup.


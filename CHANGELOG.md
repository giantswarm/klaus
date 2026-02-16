# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Toolchain image support** (`toolchainImage`): Override the default Klaus container image with a composite toolchain image that includes both the language toolchain and the Klaus agent. Built by `klausctl toolchain build` or pre-built by CI/CD. When empty (default), the chart uses the standard Klaus image -- fully backward compatible.
- **Workspace git clone support** (`workspace.gitRepo`, `workspace.gitRef`, `workspace.gitSecretName`): Populate the workspace PVC with a git repository via an init container. On first run, the repo is cloned; on subsequent runs, the existing checkout is updated. Supports branch/tag/commit selection and credential injection via Kubernetes Secrets.
- **Structured MCP server configuration** (`claude.mcpServers`): Define MCP servers as typed Helm values entries rendered to `.mcp.json` format, replacing the need to hand-craft raw JSON in `claude.mcpConfig`. Each server entry is a free-form object passed through as-is for forward compatibility.
- **MCP server secret injection** (`claude.mcpServerSecrets`): Inject Kubernetes Secret values as container env vars for `${VAR}` expansion in MCP server configuration, enabling secure credential management for MCP integrations.
- **MCP tuning controls** (`claude.mcpTimeout`, `claude.maxMcpOutputTokens`): Set `MCP_TIMEOUT` and `MAX_MCP_OUTPUT_TOKENS` env vars consumed by the Claude Code subprocess for controlling MCP server connection timeout and output token limits.
- Helm `fail` directive when both `claude.mcpConfig` (raw JSON) and `claude.mcpServers` (structured) are set, preventing ambiguous configuration.

### Changed

- **`prompt` MCP tool is now non-blocking by default**: The `prompt` tool returns immediately with `{status: "started", session_id: "..."}` and runs the task in the background. Use the `status` tool to poll for progress and retrieve the result. Set `blocking=true` for the previous behavior of waiting for completion. This is a breaking change for callers that assume blocking behavior.
- **`status` MCP tool now includes result on completion**: When a non-blocking run completes, the status transitions to `completed` with a `result` field containing the agent's final output text. The `completed` status distinguishes "task finished with results" from `idle` ("never ran / no results").
- **Non-blocking drain goroutines now use a server-scoped context**: Previously, drain goroutines used `context.Background()` and could be orphaned during server shutdown. They now use a server-scoped context that is cancelled during graceful shutdown, ensuring clean goroutine cleanup.

### Added

- **Prometheus `/metrics` endpoint**: Server-side Prometheus metrics exposed via `promhttp.Handler` at `/metrics`, always available regardless of OTel configuration. Metrics include `klaus_prompts_total`, `klaus_prompt_duration_seconds`, `klaus_process_status`, `klaus_session_cost_usd_total`, `klaus_messages_total`, `klaus_tool_calls_total`, and `klaus_process_restarts_total`.
- **OpenTelemetry pass-through configuration**: New `telemetry` section in Helm values for passing `OTEL_*` environment variables to the Claude Code subprocess, enabling its native telemetry for tokens, cost, sessions, and tool events. Gated behind `telemetry.enabled`.
- **ServiceMonitor template**: Optional Prometheus Operator `ServiceMonitor` resource (`telemetry.serviceMonitor.enabled`) with configurable interval, timeout, and namespace selector for cross-namespace monitoring.
- **Prometheus scrape annotations**: Independently toggleable `prometheus.io/*` annotations on the Service via `telemetry.scrapeAnnotations`, decoupled from OTel pass-through.
- **`completed` process status**: New status that indicates a non-blocking Submit run has finished and results are available. Callers can now distinguish "task finished" (`completed`) from "never ran" (`idle`), making polling loops unambiguous.
- **`result` MCP debug tool**: New tool that returns the full untruncated result text and detailed metadata (message history, costs, session info) from the last completed run. Intended for debugging and troubleshooting when the agent produces unexpected output.
- `Submit()` method on `Prompter` interface for non-blocking prompt execution with background result collection.
- `ResultDetail()` method on `Prompter` interface for retrieving full debug output from the last completed run.
- Initial project structure with Go app and Helm chart.
- **Bidirectional stream-json mode** (`CLAUDE_PERSISTENT_MODE=true`): Maintains a single long-running Claude subprocess using `--input-format stream-json` for multi-turn conversations with conversation continuity, lower latency, and cost efficiency.
- `Prompter` interface abstracting over single-shot (`Process`) and persistent (`PersistentProcess`) modes, allowing seamless switching via configuration.
- **MCP progress notifications**: The `prompt` tool now streams real-time `notifications/progress` messages to MCP clients during task execution, reporting tool usage, assistant output, and task completion.
- MCP `prompt` tool: added `resume` and `continue` parameters for full session management from MCP clients.
- Budget control via `--max-budget-usd` / `CLAUDE_MAX_BUDGET_USD` to cap per-invocation spending.
- Effort level via `--effort` / `CLAUDE_EFFORT` to control quality vs. speed tradeoff.
- Fallback model via `--fallback-model` / `CLAUDE_FALLBACK_MODEL` for resilience when primary model is overloaded.
- Strict MCP config via `--strict-mcp-config` / `CLAUDE_STRICT_MCP_CONFIG` to prevent loading untrusted MCP servers.
- Session management support: `--session-id`, `--resume`, `--continue`, `--fork-session`, `--no-session-persistence`.
- Custom named agents via `--agents` / `--agent` / `CLAUDE_AGENTS` / `CLAUDE_ACTIVE_AGENT`.
- JSON Schema structured output via `--json-schema` / `CLAUDE_JSON_SCHEMA`.
- Partial message streaming via `--include-partial-messages` / `CLAUDE_INCLUDE_PARTIAL_MESSAGES`.
- Settings file support via `--settings` / `CLAUDE_SETTINGS_FILE` and `--setting-sources` / `CLAUDE_SETTING_SOURCES`.
- Built-in tool set control via `--tools` / `CLAUDE_TOOLS`.
- Tool access control via `CLAUDE_ALLOWED_TOOLS` and `CLAUDE_DISALLOWED_TOOLS`.
- Plugin directory support via `--plugin-dir` / `CLAUDE_PLUGIN_DIRS`.
- Per-invocation RunOptions allowing MCP clients to override session_id, resume, continue, agent, json_schema, max_budget_usd, effort, and fork_session per prompt.
- Enhanced status endpoint with message count, tool call count, last message, last tool name, and operating mode for progress monitoring.
- Helm chart values for all new Claude Code configuration options including `persistentMode`.
- Comprehensive unit tests for the `pkg/mcp` package covering all three tools, parameter extraction helpers, progress messages, and error handling.

### Fixed

- **Persistent mode stream-json input format updated for claude-code v2.1+**: The `stdinMessage` format now uses `{"type":"user","message":{"role":"user","content":"..."}}` instead of the deprecated `{"type":"user","text":"..."}` format, fixing immediate subprocess crashes on every prompt in persistent mode.
- **Persistent mode subprocess lifetime is now decoupled from request contexts**: `PersistentProcess` reader/watchdog now use an internal lifecycle context so cancelling a single MCP request does not terminate persistent stream handling.
- **`/readyz` now reflects Claude process health**: readiness returns `503` when the process is `starting`, `stopped`, or `error`, and `200` otherwise.
- **Negative `CLAUDE_MAX_BUDGET_USD` and `CLAUDE_MAX_TURNS` now fail fast** with clear startup errors instead of being silently ignored.
- **PersistentProcess now warns about ignored per-invocation overrides** instead of silently discarding them. `RunWithOptions` logs which fields cannot be applied in persistent mode.
- **OAuth `/status` endpoint now reports correct mode** (was hardcoded to `single-shot` even when running in persistent mode).
- **Fixed potential goroutine leak** in `Process.Run` when context is cancelled: the stdout-reading goroutine now uses a cancellable select on channel sends, preventing permanent blocking when the consumer stops reading.
- **`OAuthServer.Shutdown` now attempts both shutdowns**: previously returned early if OAuth server shutdown failed, leaving the HTTP server running.
- **Invalid `CLAUDE_MAX_BUDGET_USD` and `CLAUDE_MAX_TURNS` now cause startup errors** instead of being silently ignored. Previously, `CLAUDE_MAX_BUDGET_USD=abc` would start the server with unlimited spend.
- **MCP progress counter no longer has gaps**: `progressCount` is only incremented for messages that actually produce a progress notification, giving clients a sequential 1, 2, 3... series.
- **Eliminated major DRY violation**: `PersistentProcess.persistentArgs()` duplicated all 100+ lines of `Options.args()`. Refactored into shared `baseArgs()` with mode-specific `args()` and `PersistentArgs()` methods on `Options`.
- **Removed dead `DebugMode` config** from `OAuthConfig` (was hardcoded to `false` with no flag or env var to set it).
- **Extracted shared `CollectResultText` and `Truncate` functions** eliminating triplicated result-text fallback logic across `tools.go`, `process.go`, and `persistent.go`.
- PersistentProcess now passes `--replay-user-messages` for reliable request/response synchronization in stream-json mode.
- PersistentProcess now includes a background watchdog that automatically restarts the subprocess on unexpected exit, with a 2-second backoff to prevent tight restart loops.
- MCP `prompt` tool now validates per-invocation `effort` overrides against known valid levels, returning a clear error for invalid values instead of passing them through to the CLI.
- Helm `values.schema.json` updated with all new configuration options (strictMcpConfig, maxBudgetUSD, effort, fallbackModel, etc.) for proper CI validation.
- Permission mode default changed from invalid `accept-all` to `bypassPermissions` with `--dangerously-skip-permissions` flag.
- Permission mode is now validated at startup against known valid values.
- Effort level validated at startup against known values (`low`, `medium`, `high`).
- `Truncate` now operates on runes instead of bytes, preventing invalid UTF-8 on multi-byte strings.
- MCP tool handler now returns errors for invalid parameter types instead of silently ignoring them.
- `allowedTools` / `disallowedTools` Helm values are now wired to env vars and deployment template.
- Partial message streaming now configurable via `CLAUDE_INCLUDE_PARTIAL_MESSAGES` env var.
- Helm: `includePartialMessages` now exposed in `values.yaml` and wired to `CLAUDE_INCLUDE_PARTIAL_MESSAGES` in deployment template.
- Helm: `noSessionPersistence` and `strictMcpConfig` env vars are now always emitted (not only when truthy), so setting them to `false` via Helm overrides actually takes effect.
- `ForkSession` added to `RunOptions` so MCP clients can fork sessions per-invocation via `fork_session` parameter on the `prompt` tool.

[Unreleased]: https://github.com/giantswarm/klaus/tree/main

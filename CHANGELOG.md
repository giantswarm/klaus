# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Permission mode default changed from invalid `accept-all` to `bypassPermissions` with `--dangerously-skip-permissions` flag.
- Permission mode is now validated at startup against known valid values.

### Added

- Initial project structure with Go app and Helm chart.
- Budget control via `--max-budget-usd` / `CLAUDE_MAX_BUDGET_USD` to cap per-invocation spending.
- Effort level via `--effort` / `CLAUDE_EFFORT` to control quality vs. speed tradeoff.
- Fallback model via `--fallback-model` / `CLAUDE_FALLBACK_MODEL` for resilience when primary model is overloaded.
- Strict MCP config via `--strict-mcp-config` / `CLAUDE_STRICT_MCP_CONFIG` to prevent loading untrusted MCP servers.
- Session management support: `--session-id`, `--resume`, `--continue`, `--fork-session`, `--no-session-persistence`.
- Custom named agents via `--agents` / `--agent` / `CLAUDE_AGENTS` / `CLAUDE_ACTIVE_AGENT`.
- JSON Schema structured output via `--json-schema` / `CLAUDE_JSON_SCHEMA`.
- Partial message streaming via `--include-partial-messages`.
- Settings file support via `--settings` / `CLAUDE_SETTINGS_FILE` and `--setting-sources` / `CLAUDE_SETTING_SOURCES`.
- Built-in tool set control via `--tools` / `CLAUDE_TOOLS`.
- Plugin directory support via `--plugin-dir` / `CLAUDE_PLUGIN_DIRS`.
- Per-invocation RunOptions allowing MCP clients to override session_id, agent, json_schema, max_budget_usd, and effort per prompt.
- Enhanced status endpoint with message count, tool call count, last message, and last tool name for progress monitoring.
- Helm chart values for all new Claude Code configuration options.

[Unreleased]: https://github.com/giantswarm/klaus/tree/main

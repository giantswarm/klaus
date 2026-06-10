# kagent API compatibility

Klaus calls the following kagent endpoints. These are **internal, undocumented APIs** that
may change between kagent minor releases without a deprecation notice. Pin the kagent version
in your deployment and test after upgrades.

Verified against: kagent v0.5.x (graveler cluster, 2026-06).

## Session push API (`KAGENT_API_ENDPOINT`)

Used by `pkg/kagentapi/client.go` when `KAGENT_API_ENDPOINT` is set. The pod pushes turn
history and token usage directly to kagent after each A2A turn.

| Endpoint | Method | Used for |
|---|---|---|
| `/api/sessions/{contextID}/events` | POST | Push a turn event (user or agent message) |
| `/api/tasks` | POST | Store completed task metadata with usage |

All endpoints require:

- `Authorization: Bearer <token>` — the in-request user JWT when available, otherwise the
  projected SA token at `/var/run/secrets/kagent/token` (audience: `kagent`).
- `X-User-Id` — the `sub` claim of the bearer token, or the JWT `sub` parsed from the caller's
  token. Required by kagent to key the session to a user.
- `X-Agent-Name` — set from `KAGENT_AGENT_REF`; identifies which Agent CR is generating events.

## Memory API (`KAGENT_API_ENDPOINT`)

Used by `pkg/memory/kagent.go` when `MEMORY_ENABLED=true` and an embedding endpoint is
configured. Both endpoints require `X-User-Id`.

| Endpoint | Method | Used for |
|---|---|---|
| `/api/memories/sessions` | POST | Store a content+vector pair |
| `/api/memories/search` | POST | Vector similarity search (top-N chunks) |

Request bodies use caller-supplied float32 vectors; kagent does not embed. The embedding
client (`pkg/memory/embedding.go`) generates vectors via an OpenAI-compatible endpoint
(`KLAUS_EMBEDDING_ENDPOINT`, `Klaus_EMBEDDING_MODEL`).

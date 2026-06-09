# kagent API compatibility

Klaus calls the following kagent endpoints. These are **internal, undocumented APIs** that
may change between kagent minor releases without a deprecation notice. Pin the kagent version
in your deployment and test after upgrades.

Verified against: kagent v0.5.x (graveler cluster, 2026-06).

## Memory API (`KAGENT_MEMORY_ENDPOINT`)

Used by `pkg/memory/kagent.go` when both `KAGENT_MEMORY_ENDPOINT` and `KLAUS_EMBEDDING_MODEL`
are set. Both endpoints require the `X-User-ID` header.

| Endpoint | Method | Used for |
|---|---|---|
| `/api/memories/sessions` | POST | Store a content+vector pair |
| `/api/memories/search` | POST | Vector similarity search (top-N chunks) |

Request bodies use caller-supplied 768-dim float32 vectors; kagent does not embed.
The embedding client (`pkg/memory/embedding.go`) generates vectors via any
OpenAI-compatible embedding endpoint (`KLAUS_EMBEDDING_ENDPOINT`).


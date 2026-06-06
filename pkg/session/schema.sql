-- Session store schema for Klaus. Applied idempotently on startup.
-- Target: the 'agentic-platform' database (CNPG cluster agentic-platform-pg).

CREATE SCHEMA IF NOT EXISTS sessions;

CREATE TABLE IF NOT EXISTS sessions.turns (
    context_id TEXT        NOT NULL,
    session_id TEXT        NOT NULL,
    seq        INT         NOT NULL,
    role       TEXT        NOT NULL,
    content    JSONB       NOT NULL,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (context_id, seq)
);

CREATE INDEX IF NOT EXISTS turns_session_idx
    ON sessions.turns (session_id);

-- Lightweight session binding table. Allows looking up the latest sessionID
-- for a contextID without scanning the turns table.
CREATE TABLE IF NOT EXISTS sessions.bindings (
    context_id TEXT        NOT NULL PRIMARY KEY,
    session_id TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

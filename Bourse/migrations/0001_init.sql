-- Bourse schema. Run with: psql "$DATABASE_URL" -f migrations/0001_init.sql
-- The design is event-sourced: balances/positions are NEVER stored as mutable
-- columns; they are derived by summing the append-only `entries` table.

CREATE TABLE IF NOT EXISTS portfolios (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- monotonically increasing per-portfolio sequence for entry ordering
    seq_counter BIGINT NOT NULL DEFAULT 0
);

-- Immutable event stream. INSERT-only. One trade = two balanced legs.
CREATE TABLE IF NOT EXISTS entries (
    id           BIGSERIAL PRIMARY KEY,
    trade_id     UUID        NOT NULL,
    portfolio_id UUID        NOT NULL REFERENCES portfolios(id),
    instrument   TEXT        NOT NULL,             -- 'CASH' or a ticker
    direction    SMALLINT    NOT NULL,             -- +1 in, -1 out
    quantity     BIGINT      NOT NULL CHECK (quantity > 0),
    price        BIGINT,                           -- cents/share; NULL for cash
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    seq          BIGINT      NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_entries_portfolio ON entries (portfolio_id, seq);
CREATE INDEX IF NOT EXISTS idx_entries_created ON entries (portfolio_id, created_at);

CREATE TABLE IF NOT EXISTS orders (
    id              UUID PRIMARY KEY,
    portfolio_id    UUID NOT NULL REFERENCES portfolios(id),
    idempotency_key TEXT UNIQUE,
    side            TEXT NOT NULL,                 -- buy | sell
    instrument      TEXT NOT NULL,
    quantity        BIGINT NOT NULL CHECK (quantity > 0),
    type            TEXT NOT NULL,                 -- market | limit
    limit_price     BIGINT,
    status          TEXT NOT NULL DEFAULT 'pending',
    fill_price      BIGINT,
    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_orders_portfolio ON orders (portfolio_id, created_at DESC);

-- Durable job queue. Workers claim rows with FOR UPDATE SKIP LOCKED.
CREATE TABLE IF NOT EXISTS jobs (
    id            UUID PRIMARY KEY,
    type          TEXT NOT NULL,
    payload       JSONB NOT NULL,
    priority      INT NOT NULL DEFAULT 0,
    run_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    status        TEXT NOT NULL DEFAULT 'queued',  -- queued|inflight|done|dead
    attempts      INT NOT NULL DEFAULT 0,
    max_attempts  INT NOT NULL DEFAULT 5,
    leased_until  TIMESTAMPTZ,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs (status, priority DESC, run_at);

-- Terminal failures live here and can be inspected / replayed.
CREATE TABLE IF NOT EXISTS dead_letters (
    id           UUID PRIMARY KEY,
    type         TEXT NOT NULL,
    payload      JSONB NOT NULL,
    attempts     INT NOT NULL,
    last_error   TEXT,
    died_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Idempotency ledger: a key recorded here means the side effect already ran.
CREATE TABLE IF NOT EXISTS processed_keys (
    key          TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alerts (
    id          UUID PRIMARY KEY,
    symbol      TEXT NOT NULL,
    direction   TEXT NOT NULL,        -- above | below
    threshold   BIGINT NOT NULL,      -- cents
    webhook_url TEXT NOT NULL,
    triggered   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 0002_ingest_error.sql — Phase 4 (Task 4.1) operational tables for the ingest
-- orchestrator: per-item error isolation, per-source circuit-breaker anomalies,
-- and per-source last-successful counts used to compute the discovery floor.
--
-- All three are append-mostly bookkeeping tables; none participate in the
-- canonical event content_hash. They live in the same SQLite file (WAL).

-- ---------------------------------------------------------------------------
-- ingest_error: one row per item that failed parse/normalize (or panicked).
-- The offending item is SKIPPED (never half-upserted); a pre-existing good row
-- for that event_id is left untouched. This is the audit trail for AC-10 /
-- per-item recover() isolation.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ingest_error (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id    TEXT,                              -- durable id if known (may be NULL)
    source      TEXT NOT NULL,                     -- adapter id, e.g. "coex"
    stage       TEXT NOT NULL,                     -- fetch|parse|normalize|panic|store
    message     TEXT NOT NULL,                     -- error / panic text
    batch_id    TEXT NOT NULL,                     -- the ingest batch
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_ingest_error_batch  ON ingest_error(batch_id);
CREATE INDEX IF NOT EXISTS idx_ingest_error_source ON ingest_error(source);

-- ---------------------------------------------------------------------------
-- fetch_anomaly: one row per source whose batch was ABORTED by the circuit
-- breaker (discovery floor / changed-fraction threshold). When present, NO
-- diffs/upserts were written for that source in that batch, so prior rows are
-- preserved (silent-corruption guard, AC-10).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS fetch_anomaly (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source        TEXT NOT NULL,
    reason        TEXT NOT NULL,                   -- human-readable trip reason
    discovered    INTEGER NOT NULL DEFAULT 0,      -- refs discovered this run
    parsed        INTEGER NOT NULL DEFAULT 0,      -- successfully normalized
    prev_stored   INTEGER NOT NULL DEFAULT 0,      -- last successful stored count (floor basis)
    batch_id      TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_fetch_anomaly_batch  ON fetch_anomaly(batch_id);
CREATE INDEX IF NOT EXISTS idx_fetch_anomaly_source ON fetch_anomaly(source);

-- ---------------------------------------------------------------------------
-- batch_stats: last SUCCESSFUL per-source counts, so the next run can compute
-- the discovery floor (e.g. < 50% of last successful discovered count trips the
-- breaker). One row per source (upserted on every successful, non-aborted run).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS batch_stats (
    source        TEXT PRIMARY KEY,
    discovered    INTEGER NOT NULL DEFAULT 0,      -- refs discovered last success
    parsed        INTEGER NOT NULL DEFAULT 0,      -- normalized last success
    stored        INTEGER NOT NULL DEFAULT 0,      -- applied to store last success
    batch_id      TEXT,                            -- batch that produced these counts
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

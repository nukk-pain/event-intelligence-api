-- 0001_init.sql — initial schema for COEX/KINTEX event intelligence store.
--
-- Mapping notes (v0.1 schema, prototype/manual-dataset-schema.md):
--   * Scalar/enum/date fields map to native columns.
--   * Arrays and nested objects (venue, scale, actions, categories, audience,
--     sources, missing_fields) are stored as JSON TEXT columns.
--   * Plus operational columns required by the PLAN:
--       schema_version TEXT, content_hash TEXT,
--       excluded INTEGER NOT NULL DEFAULT 0.
--
-- All connections open with: journal_mode=WAL, busy_timeout=5000,
-- synchronous=NORMAL, foreign_keys=ON (applied via DSN in db.go).

-- ---------------------------------------------------------------------------
-- sources: per-venue source registry / crawl bookkeeping.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sources (
    source_id        TEXT PRIMARY KEY,          -- e.g. "coex", "kintex"
    name             TEXT NOT NULL,
    base_url         TEXT NOT NULL,
    type             TEXT,                       -- venue|organizer|association|press|social|aggregator
    enabled          INTEGER NOT NULL DEFAULT 1,
    last_run_at      TEXT,                       -- ISO8601 of last successful crawl
    last_status      TEXT,                       -- free text / last batch status
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- ---------------------------------------------------------------------------
-- events: canonical normalized v0.1 records.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS events (
    -- Identity
    event_id              TEXT PRIMARY KEY,       -- durable slug, e.g. coex-2026-ai-expo
    schema_version        TEXT NOT NULL,          -- "0.1"
    series_id             TEXT,                   -- nullable
    name                  TEXT NOT NULL,
    name_ko               TEXT,
    name_en               TEXT,
    edition               TEXT,

    -- When / Where
    start_date            TEXT,                   -- ISO date YYYY-MM-DD or NULL
    end_date              TEXT,
    timezone              TEXT,                   -- IANA tz, e.g. Asia/Seoul
    date_confidence       TEXT,                   -- high|medium|low
    status                TEXT,                   -- scheduled|tentative|postponed|cancelled|ended
    format                TEXT,                   -- onsite|hybrid|online
    venue                 TEXT,                   -- JSON object {venue_id,name,hall,city} or NULL
    venue_id              TEXT,                   -- denormalized for indexing/filtering
    country               TEXT NOT NULL,          -- ISO 3166-1 alpha-2

    -- Classification
    categories            TEXT NOT NULL,          -- JSON array of taxonomy slugs (>=1)
    audience              TEXT,                   -- JSON array (may be empty)
    scale                 TEXT,                   -- JSON object {visitors,exhibitors} or NULL

    -- Founder / operator action fields
    actions               TEXT,                   -- JSON object of booleans
    register_url          TEXT,
    exhibit_url           TEXT,
    registration_deadline TEXT,                   -- ISO date or NULL
    exhibitor_deadline    TEXT,
    cost_hint             TEXT,                   -- free|paid|mixed|unknown
    summary               TEXT,

    -- Provenance & freshness
    sources               TEXT NOT NULL,          -- JSON array of {url,type,publisher,retrieved_at} (>=1)
    homepage_url          TEXT,
    last_checked_at       TEXT NOT NULL,          -- ISO date/datetime of last verification
    update_state          TEXT NOT NULL,          -- new|unchanged|updated|conflicting
    confidence            TEXT NOT NULL,          -- high|medium|low
    missing_fields        TEXT NOT NULL,          -- JSON array of dotted paths
    ambiguity_notes       TEXT,

    -- Curation meta
    curated_by            TEXT,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- Operational (PLAN-mandated)
    content_hash          TEXT,                   -- canonical hash of semantic fields
    excluded              INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_events_updated_at ON events(updated_at);
CREATE INDEX IF NOT EXISTS idx_events_venue_id   ON events(venue_id);
CREATE INDEX IF NOT EXISTS idx_events_excluded   ON events(excluded);

-- ---------------------------------------------------------------------------
-- change_log: field-level change feed.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS change_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id    TEXT NOT NULL,
    field_path  TEXT NOT NULL,                    -- dotted field path, e.g. "status"
    old_value   TEXT,
    new_value   TEXT,
    changed_at  TEXT NOT NULL,                    -- ISO8601
    batch_id    TEXT,
    FOREIGN KEY (event_id) REFERENCES events(event_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_change_log_changed_at ON change_log(changed_at);
CREATE INDEX IF NOT EXISTS idx_change_log_event_id   ON change_log(event_id);

-- ---------------------------------------------------------------------------
-- raw_snapshot: last raw fetched payload per event (provenance / re-parse).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS raw_snapshot (
    event_id      TEXT PRIMARY KEY,
    source_id     TEXT,
    url           TEXT,
    content_type  TEXT,
    body          BLOB,                            -- raw bytes as fetched
    content_hash  TEXT,
    retrieved_at  TEXT NOT NULL,
    FOREIGN KEY (event_id) REFERENCES events(event_id) ON DELETE CASCADE
);

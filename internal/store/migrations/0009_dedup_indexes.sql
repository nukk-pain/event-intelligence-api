-- 0009_dedup_indexes.sql — cross-source de-dup, part 3 of 3: indexes.
--
-- content_key powers cluster lookups in reconcileContentKey; superseded is
-- filtered on every read-path query. Both statements are CREATE INDEX IF NOT
-- EXISTS, so this file is fully idempotent and always runs to completion on every
-- startup (no ALTER, no "duplicate column name" early-exit).

CREATE INDEX IF NOT EXISTS idx_events_content_key ON events(content_key);
CREATE INDEX IF NOT EXISTS idx_events_superseded ON events(superseded);

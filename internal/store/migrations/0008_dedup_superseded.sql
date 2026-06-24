-- 0008_dedup_superseded.sql — cross-source de-dup, part 2 of 3: superseded.
--
-- superseded is 0 for the canonical row of a content_key cluster and 1 for the
-- rows hidden behind it. Soft flag only — the row, its provenance, change_log and
-- raw_snapshot are preserved; it is merely excluded from the public read path.
-- Existing rows default to 0 (canonical/visible). Operational column, NOT part of
-- content_hash.
--
-- Isolated in its own file (see 0007 header) so a crash after 0007's column-add
-- but before this one is self-healing: on the next startup 0007 is skipped as
-- already-applied and THIS file still runs, creating the column.

ALTER TABLE events ADD COLUMN superseded INTEGER NOT NULL DEFAULT 0;

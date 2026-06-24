-- 0007_dedup_content_key.sql — cross-source de-dup, part 1 of 3: content_key.
--
-- content_key is a normalized (name|start_date|venue_id) fingerprint shared by
-- every adapter's version of the same real-world event (e.g. an event KINTEX
-- lists on list.do AND that the SHOWALA portal carries). NULL when the event has
-- no parseable start_date or no venue_id — such an event is then never de-duped
-- and always stands alone. It is an operational/derived column and is NOT part of
-- content_hash, so adding it does not mark existing rows "updated".
--
-- ONE statement per migration file (see 0008/0009): the migrate runner skips a
-- whole file on the first "duplicate column name" re-run, so isolating each
-- ALTER guarantees that a crash between column-adds cannot permanently skip a
-- later column. Re-run idempotency is handled by isAlreadyAppliedMigration.

ALTER TABLE events ADD COLUMN content_key TEXT;

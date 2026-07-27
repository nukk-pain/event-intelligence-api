-- Revoke Solar-enriched deadlines stored before the page-evidence gate.
--
-- An accuracy audit of 26 model-supplied deadlines found page evidence for
-- only 3 and one that named the opposite deadline type. Deadlines mislead
-- users when wrong, so values stored before the enricher required literal
-- page evidence are nulled and returned to missing_fields; the next ingest
-- re-fills them only when evidence supports them. Values written by the
-- evidence-gated enricher carry the distinct publisher
-- eventsintel/solar-enrich/v2 and are exactly excluded by the quoted match
-- below, so this cannot revoke a gated value on later startups. Idempotent:
-- rows already nulled no longer match the WHERE clause. A deterministic
-- deadline on a row that also has the old enrich source is revoked too; that
-- is a re-verification (the next ingest refills it), not a loss.
UPDATE events
SET registration_deadline = NULL,
    missing_fields = CASE
      WHEN instr(missing_fields, '"registration_deadline"') = 0
        THEN json_insert(missing_fields, '$[#]', 'registration_deadline')
      ELSE missing_fields
    END
WHERE registration_deadline IS NOT NULL
  AND sources LIKE '%"publisher":"eventsintel/solar-enrich"%';

UPDATE events
SET exhibitor_deadline = NULL,
    missing_fields = CASE
      WHEN instr(missing_fields, '"exhibitor_deadline"') = 0
        THEN json_insert(missing_fields, '$[#]', 'exhibitor_deadline')
      ELSE missing_fields
    END
WHERE exhibitor_deadline IS NOT NULL
  AND sources LIKE '%"publisher":"eventsintel/solar-enrich"%';

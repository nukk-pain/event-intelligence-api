-- Revoke deadlines that cannot belong to the event they are stored against.
--
-- An organizer homepage describes only its nearest edition, so a later edition
-- listing that same page inherited a deadline that was never its own. Observed
-- 2026-07-30: 제54회 맘앤베이비엑스포 (8/6) and 제55회 (11/26) both carried
-- reg 06-22 / exh 07-07 from momnbabyexpo.co.kr, and 한가위 (8/14) and 설맞이
-- (12/22) both carried 07-10 / 07-17 from fgfair.com. A December show reported
-- as missed by 165 days costs a reader a real opportunity.
--
-- Two impossibilities, mirroring model.DeadlinePlausible:
--   * a deadline already behind us on an event still more than 60 days out
--   * any deadline preceding its event by more than a year (2025-08-14 was
--     stored against a 2026-09-09 event)
--
-- The write-time gate now refuses both, and the carry-forward ratchet declines
-- to resurrect them, so the next ingest cannot re-store these. This clears what
-- is already written: ApplyBatch never writes to an event absent from a batch,
-- so a row that stops being rediscovered would otherwise keep its bad value
-- forever.
--
-- Rule-based rather than a list of event_ids: the same inheritance affects rows
-- beyond the ones sampled through the API. Idempotent — a nulled row no longer
-- matches. Rows with no start_date are left alone, matching the Go rule, which
-- does not judge dates it cannot read.
UPDATE events
SET registration_deadline = NULL,
    missing_fields = CASE
      WHEN instr(missing_fields, '"registration_deadline"') = 0
        THEN json_insert(missing_fields, '$[#]', 'registration_deadline')
      ELSE missing_fields
    END
WHERE registration_deadline IS NOT NULL
  AND start_date IS NOT NULL
  AND ( (registration_deadline < date('now')
         AND julianday(start_date) - julianday('now') > 60)
     OR julianday(registration_deadline) < julianday(start_date) - 365 );

UPDATE events
SET exhibitor_deadline = NULL,
    missing_fields = CASE
      WHEN instr(missing_fields, '"exhibitor_deadline"') = 0
        THEN json_insert(missing_fields, '$[#]', 'exhibitor_deadline')
      ELSE missing_fields
    END
WHERE exhibitor_deadline IS NOT NULL
  AND start_date IS NOT NULL
  AND ( (exhibitor_deadline < date('now')
         AND julianday(start_date) - julianday('now') > 60)
     OR julianday(exhibitor_deadline) < julianday(start_date) - 365 );

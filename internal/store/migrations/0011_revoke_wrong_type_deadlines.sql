-- Revoke the two deadlines the 2026-07-29 accuracy audit classified as
-- wrong_type: page evidence exists for the date, but its context names a
-- different fact than the stored field claims.
--
--   배터리코리아 exhibitor_deadline 2026-10-06 — the page names it as the
--   participation-fee balance due date, not the booth application deadline.
--   홈테이블데코페어 registration_deadline 2026-07-31 — the page names it as
--   the booth early-application deadline, not visitor registration.
--
-- The write-time gate now checks type context (typedDateEvidence), so the
-- next ingest cannot re-store these under the wrong field. Idempotent: the
-- value match stops applying once nulled or changed.
UPDATE events
SET exhibitor_deadline = NULL,
    missing_fields = CASE
      WHEN instr(missing_fields, '"exhibitor_deadline"') = 0
        THEN json_insert(missing_fields, '$[#]', 'exhibitor_deadline')
      ELSE missing_fields
    END
WHERE event_id = 'coex-2026-%eb%b0%b0%ed%84%b0%eb%a6%ac%ec%bd%94%eb%a6%ac%ec%95%84'
  AND exhibitor_deadline = '2026-10-06';

UPDATE events
SET registration_deadline = NULL,
    missing_fields = CASE
      WHEN instr(missing_fields, '"registration_deadline"') = 0
        THEN json_insert(missing_fields, '$[#]', 'registration_deadline')
      ELSE missing_fields
    END
WHERE event_id = 'coex-2026-%ed%99%88%ed%85%8c%ec%9d%b4%eb%b8%94%eb%8d%b0%ec%bd%94%ed%8e%98%ec%96%b4'
  AND registration_deadline = '2026-07-31';

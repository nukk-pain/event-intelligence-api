UPDATE events
SET summary = NULL
WHERE summary IS NOT NULL
    AND COALESCE(TRIM(name), '') != ''
    AND (
        summary = name
        OR summary LIKE name || ' — %'
        OR summary LIKE name || ' @ %'
    );

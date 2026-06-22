UPDATE events
SET summary = substr(
    name ||
    CASE
        WHEN start_date IS NOT NULL AND end_date IS NOT NULL THEN ' — ' || start_date || '~' || end_date
        WHEN start_date IS NOT NULL THEN ' — ' || start_date
        ELSE ''
    END ||
    CASE
        WHEN venue IS NOT NULL
            AND json_valid(venue)
            AND COALESCE(TRIM(json_extract(venue, '$.name')), '') != ''
        THEN ' @ ' || json_extract(venue, '$.name')
        ELSE ''
    END,
    1,
    240
)
WHERE summary IS NULL
    AND COALESCE(TRIM(name), '') != '';

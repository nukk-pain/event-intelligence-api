UPDATE events
SET missing_fields = json_array('summary')
WHERE summary IS NULL
    AND (
        missing_fields IS NULL
        OR TRIM(missing_fields) = ''
        OR NOT json_valid(missing_fields)
    );

UPDATE events
SET missing_fields = json_insert(missing_fields, '$[#]', 'summary')
WHERE summary IS NULL
    AND json_valid(missing_fields)
    AND NOT EXISTS (
        SELECT 1
        FROM json_each(events.missing_fields)
        WHERE value = 'summary'
    );

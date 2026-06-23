package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// query.go is the read layer over a read-only handle (store.OpenRead). It
// reconstructs model.Event values from the canonical row — including the JSON
// TEXT columns (venue, scale, actions, categories, audience, sources,
// missing_fields) — and provides the keyset-paginated listing that the read API
// (Phase 3) serves. The same helpers back GetEvent / ListSources / ListChanges
// reused by Task 3.2.

// EventFilter narrows and paginates a ListEvents call. The zero value lists the
// first page of all events ordered by (updated_at, event_id).
type EventFilter struct {
	// UpdatedSince keeps only events whose updated_at is >= this RFC3339 value.
	UpdatedSince string
	// ChangedSince keeps only events that have at least one change_log row whose
	// changed_at is >= this RFC3339 value.
	ChangedSince string
	// Category keeps events whose categories JSON array contains this taxonomy
	// slug. Empty means no category filter.
	Category string
	// Venue keeps events whose venue_id equals this value. Empty means no filter.
	Venue string
	// MinStartDate keeps only events whose start_date is >= this date (YYYY-MM-DD).
	// Events with NULL start_date are excluded when this is set. Empty means no filter.
	MinStartDate string
	// MaxStartDate keeps only events whose start_date is <= this date (YYYY-MM-DD).
	// Events with NULL start_date are excluded when this is set. Empty means no filter.
	MaxStartDate       string
	Opportunity        bool
	Actionable         bool
	OpportunityQuality string
	// Limit caps the page size. Callers should clamp/default before calling; a
	// non-positive Limit falls back to defaultQueryLimit.
	Limit int
	// Cursor is the keyset position (last row already returned). Empty starts at
	// the beginning.
	Cursor EventCursor
}

// EventCursor is the keyset position used by ListEvents. It mirrors the API
// cursor (updated_at, event_id) but lives in the store layer so the query layer
// has no dependency on the api package. The api package translates its opaque
// cursor to/from this struct.
type EventCursor struct {
	UpdatedAt string
	EventID   string
}

// defaultQueryLimit is the fallback page size when a filter supplies none. The
// API layer owns the user-facing default/clamp; this only guards direct callers.
const defaultQueryLimit = 20

// eventColumns is the SELECT projection shared by every event read. Order is
// load-bearing: scanEvent depends on it.
const eventColumns = `
    event_id, schema_version, series_id, name, name_ko, name_en, edition,
    start_date, end_date, timezone, date_confidence, status, format,
    venue, venue_id, country,
    categories, audience, scale,
    actions, register_url, exhibit_url, registration_deadline, exhibitor_deadline, cost_hint,
    summary, sources, homepage_url, last_checked_at, update_state, confidence, missing_fields, ambiguity_notes,
    curated_by, created_at, updated_at, content_hash, excluded`

// ListEvents returns one keyset page of events matching filter, ordered by
// (updated_at, event_id) ascending, plus the cursor for the NEXT page. The
// returned *EventCursor is nil when there are no further rows.
//
// Pagination is stable across concurrent inserts: the order key is a unique
// total order, so a page returns rows strictly greater than the cursor and the
// full iteration visits each row exactly once.
func ListEvents(ctx context.Context, db *sql.DB, filter EventFilter) ([]model.Event, *EventCursor, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	var (
		where []string
		args  []any
	)

	if filter.UpdatedSince != "" {
		where = append(where, "e.updated_at >= ?")
		args = append(args, filter.UpdatedSince)
	}
	if filter.Venue != "" {
		where = append(where, "e.venue_id = ?")
		args = append(args, filter.Venue)
	}
	if filter.Category != "" {
		// categories is a JSON array TEXT column; EXISTS over json_each matches
		// membership exactly (no substring false positives).
		where = append(where, "EXISTS (SELECT 1 FROM json_each(e.categories) WHERE json_each.value = ?)")
		args = append(args, filter.Category)
	}
	if filter.ChangedSince != "" {
		where = append(where, "EXISTS (SELECT 1 FROM change_log c WHERE c.event_id = e.event_id AND c.changed_at >= ?)")
		args = append(args, filter.ChangedSince)
	}
	if filter.MinStartDate != "" {
		where = append(where, "e.start_date IS NOT NULL AND e.start_date >= ?")
		args = append(args, filter.MinStartDate)
	}
	if filter.MaxStartDate != "" {
		where = append(where, "e.start_date IS NOT NULL AND e.start_date <= ?")
		args = append(args, filter.MaxStartDate)
	}
	if filter.Actionable {
		where = append(where, actionableExpr())
	}
	if filter.Opportunity {
		where = append(where, opportunityEligibleExpr(), opportunitySignalCountExpr()+" >= 1")
	}
	switch filter.OpportunityQuality {
	case "high":
		where = append(where, opportunityEligibleExpr(), opportunitySignalCountExpr()+" >= 2")
	case "medium":
		where = append(where, opportunityEligibleExpr(), opportunitySignalCountExpr()+" = 1")
	case "low":
		where = append(where, "NOT ("+opportunityEligibleExpr()+" AND "+opportunitySignalCountExpr()+" >= 1)")
	}
	// Keyset boundary: (updated_at, event_id) strictly greater than the cursor.
	if filter.Cursor.UpdatedAt != "" || filter.Cursor.EventID != "" {
		where = append(where, "(e.updated_at > ? OR (e.updated_at = ? AND e.event_id > ?))")
		args = append(args, filter.Cursor.UpdatedAt, filter.Cursor.UpdatedAt, filter.Cursor.EventID)
	}

	q := "SELECT" + eventColumns + " FROM events e"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY e.updated_at ASC, e.event_id ASC LIMIT ?"
	// Fetch one extra row to detect whether a further page exists.
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	events := make([]model.Event, 0, limit)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate events: %w", err)
	}

	var next *EventCursor
	if len(events) > limit {
		// The extra probe row proves a further page exists; drop it and emit a
		// cursor pointing at the LAST kept row.
		events = events[:limit]
		last := events[len(events)-1]
		next = &EventCursor{UpdatedAt: last.UpdatedAt, EventID: last.EventID}
	}
	return events, next, nil
}

func opportunityEligibleExpr() string {
	return "e.start_date IS NOT NULL AND e.start_date != '' AND e.end_date IS NOT NULL AND e.end_date != '' AND e.homepage_url IS NOT NULL AND e.homepage_url != '' AND json_array_length(e.categories) > 0 AND json_array_length(e.sources) > 0"
}

func actionableExpr() string {
	return "(e.register_url IS NOT NULL OR e.exhibit_url IS NOT NULL OR e.registration_deadline IS NOT NULL OR e.exhibitor_deadline IS NOT NULL OR e.cost_hint IN ('free', 'paid', 'mixed') OR json_extract(e.actions, '$.can_register') = 1 OR json_extract(e.actions, '$.can_exhibit') = 1 OR json_extract(e.actions, '$.can_sponsor') = 1 OR json_extract(e.actions, '$.has_matchmaking') = 1 OR json_extract(e.actions, '$.has_startup_program') = 1)"
}

func opportunitySignalCountExpr() string {
	terms := []string{
		"e.register_url IS NOT NULL AND e.register_url != ''",
		"e.exhibit_url IS NOT NULL AND e.exhibit_url != ''",
		"e.registration_deadline IS NOT NULL AND e.registration_deadline != ''",
		"e.exhibitor_deadline IS NOT NULL AND e.exhibitor_deadline != ''",
		"e.cost_hint IN ('free', 'paid', 'mixed')",
		"json_extract(e.actions, '$.can_register') = 1",
		"json_extract(e.actions, '$.can_exhibit') = 1",
		"json_extract(e.actions, '$.can_sponsor') = 1",
		"json_extract(e.actions, '$.has_matchmaking') = 1",
		"json_extract(e.actions, '$.has_startup_program') = 1",
		"e.summary IS NOT NULL AND e.summary != ''",
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, "CASE WHEN "+term+" THEN 1 ELSE 0 END")
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

// GetEvent loads a single event by id. It returns (nil, nil) when no row exists
// (so callers distinguish not-found from an error). It does NOT filter on
// excluded — a direct detail lookup by id is allowed even for excluded events.
func GetEvent(ctx context.Context, db *sql.DB, eventID string) (*model.Event, error) {
	q := "SELECT" + eventColumns + " FROM events e WHERE e.event_id = ?"
	row := db.QueryRowContext(ctx, q, eventID)
	e, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListSources returns the provenance entries for one event (decoded from the
// event's sources JSON column). It returns (nil, nil) when the event does not
// exist.
func ListSources(ctx context.Context, db *sql.DB, eventID string) ([]model.Source, error) {
	var sourcesJSON sql.NullString
	err := db.QueryRowContext(ctx, "SELECT sources FROM events WHERE event_id = ?", eventID).Scan(&sourcesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	var sources []model.Source
	if sourcesJSON.Valid && sourcesJSON.String != "" {
		if err := json.Unmarshal([]byte(sourcesJSON.String), &sources); err != nil {
			return nil, fmt.Errorf("decode sources for %s: %w", eventID, err)
		}
	}
	return sources, nil
}

// ChangeFilter narrows the change feed (Task 3.2).
type ChangeFilter struct {
	// Since keeps only change_log rows whose changed_at is >= this RFC3339 value.
	Since string
	// EventID, when set, scopes to one event's changes.
	EventID string
	// Limit caps the page size.
	Limit int
	// Cursor is the keyset position (last id already returned).
	Cursor int64
}

// ListChanges returns change_log rows ordered by id ascending, plus the id-based
// cursor for the next page (0 when exhausted).
//
// The page order is id ALONE, deliberately matching the keyset cursor predicate
// (id > ?) on the SAME key. change_log.id is AUTOINCREMENT (insertion order) and
// already a unique total order. Ordering instead by (changed_at, id) would split
// the sort key (changed_at) from the cursor key (id): because changed_at is the
// ingest-supplied change timestamp — not insertion order — a row with a smaller
// id can carry a LATER changed_at than a row with a larger id (re-ingest of a
// stale event, backfill, preserved original timestamps). Once the cursor advanced
// past the larger id, the smaller-id row would be permanently excluded by
// id > cursor and silently dropped from the feed. Ordering by id keeps order and
// cursor on one key so every row is walked exactly once. The since= range filter
// is still served by idx_change_log_changed_at.
func ListChanges(ctx context.Context, db *sql.DB, filter ChangeFilter) ([]model.ChangeLog, int64, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	var (
		where []string
		args  []any
	)
	if filter.Since != "" {
		where = append(where, "changed_at >= ?")
		args = append(args, filter.Since)
	}
	if filter.EventID != "" {
		where = append(where, "event_id = ?")
		args = append(args, filter.EventID)
	}
	if filter.Cursor > 0 {
		where = append(where, "id > ?")
		args = append(args, filter.Cursor)
	}

	q := "SELECT id, event_id, field_path, old_value, new_value, changed_at, batch_id FROM change_log"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id ASC LIMIT ?"
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list changes: %w", err)
	}
	defer rows.Close()

	out := make([]model.ChangeLog, 0, limit)
	for rows.Next() {
		var c model.ChangeLog
		if err := rows.Scan(&c.ID, &c.EventID, &c.FieldPath, &c.OldValue, &c.NewValue, &c.ChangedAt, &c.BatchID); err != nil {
			return nil, 0, fmt.Errorf("scan change_log: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate changes: %w", err)
	}

	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows so scanEvent serves
// GetEvent and ListEvents from one decode path.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanEvent reconstructs a model.Event from the eventColumns projection,
// decoding the JSON TEXT columns back into their nested structs/slices.
func scanEvent(row rowScanner) (model.Event, error) {
	var (
		e        model.Event
		excluded int

		venueJSON, scaleJSON         sql.NullString
		categoriesJSON, audienceJSON sql.NullString
		actionsJSON                  sql.NullString
		sourcesJSON, missingJSON     sql.NullString
		venueID                      sql.NullString
		curatedBy                    sql.NullString
	)

	err := row.Scan(
		&e.EventID, &e.SchemaVersion, &e.SeriesID, &e.Name, &e.NameKo, &e.NameEn, &e.Edition,
		&e.StartDate, &e.EndDate, &e.Timezone, &e.DateConfidence, &e.Status, &e.Format,
		&venueJSON, &venueID, &e.Country,
		&categoriesJSON, &audienceJSON, &scaleJSON,
		&actionsJSON, &e.RegisterURL, &e.ExhibitURL, &e.RegistrationDeadline, &e.ExhibitorDeadline, &e.CostHint,
		&e.Summary, &sourcesJSON, &e.HomepageURL, &e.LastCheckedAt, &e.UpdateState, &e.Confidence, &missingJSON, &e.AmbiguityNotes,
		&curatedBy, &e.CreatedAt, &e.UpdatedAt, &e.ContentHash, &excluded,
	)
	if err != nil {
		return model.Event{}, err
	}

	e.Excluded = excluded != 0
	e.CuratedBy = curatedBy.String

	if venueJSON.Valid && venueJSON.String != "" {
		var v model.Venue
		if err := json.Unmarshal([]byte(venueJSON.String), &v); err != nil {
			return model.Event{}, fmt.Errorf("decode venue for %s: %w", e.EventID, err)
		}
		e.Venue = &v
	}
	if scaleJSON.Valid && scaleJSON.String != "" {
		var sc model.Scale
		if err := json.Unmarshal([]byte(scaleJSON.String), &sc); err != nil {
			return model.Event{}, fmt.Errorf("decode scale for %s: %w", e.EventID, err)
		}
		e.Scale = &sc
	}
	if actionsJSON.Valid && actionsJSON.String != "" {
		if err := json.Unmarshal([]byte(actionsJSON.String), &e.Actions); err != nil {
			return model.Event{}, fmt.Errorf("decode actions for %s: %w", e.EventID, err)
		}
	}
	if err := decodeStringSlice(categoriesJSON, &e.Categories); err != nil {
		return model.Event{}, fmt.Errorf("decode categories for %s: %w", e.EventID, err)
	}
	if err := decodeStringSlice(audienceJSON, &e.Audience); err != nil {
		return model.Event{}, fmt.Errorf("decode audience for %s: %w", e.EventID, err)
	}
	if err := decodeStringSlice(missingJSON, &e.MissingFields); err != nil {
		return model.Event{}, fmt.Errorf("decode missing_fields for %s: %w", e.EventID, err)
	}
	if sourcesJSON.Valid && sourcesJSON.String != "" {
		if err := json.Unmarshal([]byte(sourcesJSON.String), &e.Sources); err != nil {
			return model.Event{}, fmt.Errorf("decode sources for %s: %w", e.EventID, err)
		}
	}

	return e, nil
}

// decodeStringSlice (defined in diff.go) unmarshals a nullable JSON-array TEXT
// column into dst, yielding a nil slice for a NULL/empty column.

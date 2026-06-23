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
	// ListKind separates venue-calendar rows from benchmark event-family rows.
	// Empty and "all" preserve the original unscoped API listing.
	ListKind string
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
	MaxStartDate string
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

	where, args := filter.commonWhere()
	if filter.Category != "" {
		// categories is a JSON array TEXT column; EXISTS over json_each matches
		// membership exactly (no substring false positives).
		where = append(where, "EXISTS (SELECT 1 FROM json_each(e.categories) WHERE json_each.value = ?)")
		args = append(args, filter.Category)
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

// commonWhere builds the filter predicates shared by ListEvents and
// CategoryCounts: the updated_since floor, the list-kind partition (venue vs
// benchmark), an explicit venue, the changed_since existence check, and the
// start_date range. It deliberately OMITS the category-membership clause and the
// keyset cursor so each caller appends those itself. Sharing one builder keeps
// the facet's list/venue/date scope locked to the list it annotates; the facet
// then narrows further to categorized events (see CategoryCounts).
func (f EventFilter) commonWhere() (where []string, args []any) {
	if f.UpdatedSince != "" {
		where = append(where, "e.updated_at >= ?")
		args = append(args, f.UpdatedSince)
	}
	switch f.ListKind {
	case "venue":
		where = append(where, "e.venue_id IN ('coex', 'kintex')")
	case "benchmark":
		where = append(where, "e.event_id LIKE 'benchmark-%'")
	}
	if f.Venue != "" {
		where = append(where, "e.venue_id = ?")
		args = append(args, f.Venue)
	}
	if f.ChangedSince != "" {
		where = append(where, "EXISTS (SELECT 1 FROM change_log c WHERE c.event_id = e.event_id AND c.changed_at >= ?)")
		args = append(args, f.ChangedSince)
	}
	if f.MinStartDate != "" {
		where = append(where, "e.start_date IS NOT NULL AND e.start_date >= ?")
		args = append(args, f.MinStartDate)
	}
	if f.MaxStartDate != "" {
		where = append(where, "e.start_date IS NOT NULL AND e.start_date <= ?")
		args = append(args, f.MaxStartDate)
	}
	return where, args
}

// CategoryCounts returns, per taxonomy category, the number of NON-EXCLUDED
// events that match the same list/venue/date predicates as ListEvents — but
// IGNORING filter.Category and the keyset cursor. It powers the API category
// facet (the "AI 37" chip counts) so a client can see how many events sit in
// each category under the currently active (non-category) filters, including the
// one it may already be filtered to. Counts are keyed by slug; callers order
// them by the canonical taxonomy order.
//
// The facet is intentionally CATEGORIZED-only (excluded=0): chips count industry
// events, which is what the default UI view shows (the client hides excluded rows
// in its "categorized" scope). This is why the count can be smaller than a raw
// ?category=X list page, which does not filter excluded — though in practice the
// classifier strips categories from excluded events, so such an event is not a
// member of any category to begin with. The excluded=0 predicate is the
// authoritative guard regardless of that classifier invariant.
func CategoryCounts(ctx context.Context, db *sql.DB, filter EventFilter) (map[string]int, error) {
	where, args := filter.commonWhere()
	where = append(where, "e.excluded = 0")

	q := "SELECT je.value AS category, COUNT(*) AS n FROM events e, json_each(e.categories) je" +
		" WHERE " + strings.Join(where, " AND ") +
		" GROUP BY je.value"

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("category counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var (
			cat string
			n   int
		)
		if err := rows.Scan(&cat, &n); err != nil {
			return nil, fmt.Errorf("scan category count: %w", err)
		}
		counts[cat] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate category counts: %w", err)
	}
	return counts, nil
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

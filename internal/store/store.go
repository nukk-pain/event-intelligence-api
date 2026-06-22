package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// Store wraps a write handle to the canonical event database.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by the given write handle.
func New(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the underlying handle for migrations and queries.
func (s *Store) DB() *sql.DB { return s.db }

// ---------------------------------------------------------------------------
// content_hash domain (explicitly defined canonical serialization)
// ---------------------------------------------------------------------------
//
// ContentHash computes a deterministic fingerprint over ONLY the source-derived
// SEMANTIC fields of an event — i.e. the facts a crawl could observe on the
// venue page. Two records that describe the same real-world event state hash to
// the same value regardless of when they were fetched or how they were curated.
//
// INCLUDED (source-derived semantic) fields:
//
//	series_id, name, name_ko, name_en, edition,
//	start_date, end_date, timezone, date_confidence, status, format,
//	venue{venue_id,name,hall,city}, country,
//	categories (SORTED), audience (SORTED), scale{visitors,exhibitors},
//	actions{can_register,can_exhibit,can_sponsor,has_matchmaking,has_startup_program},
//	register_url, exhibit_url, registration_deadline, exhibitor_deadline,
//	cost_hint, homepage_url,
//	sources[] (each {url,type,publisher} WITHOUT retrieved_at; the slice SORTED),
//	missing_fields (SORTED), ambiguity_notes.
//
// EXCLUDED (freshness / operational / derived / curation / identity) fields:
//
//	schema_version (internal versioning marker, NOT source-derived; set from the
//	codebase constant model.SchemaVersion), event_id (immutable primary key),
//	last_checked_at, sources[].retrieved_at, created_at, updated_at, batch_id,
//	update_state (derived FROM the hash comparison), confidence, curated_by,
//	excluded, content_hash itself.
//
// Normalization applied before hashing:
//   - string fields: Unicode whitespace trimmed and internal runs collapsed to a
//     single space (normalizeWS);
//   - arrays (categories, audience, missing_fields, sources): SORTED so source
//     ordering is irrelevant;
//   - map keys: emitted in sorted order (encoding/json sorts map[string]any keys),
//     guaranteeing a canonical byte sequence;
//   - the canonical form is JSON, then SHA-256, returned hex-encoded.
func ContentHash(e model.Event) string {
	// Hash the SAME flat canonical projection the field-level diff compares
	// (CanonicalFields). This makes the hash and the diff agree by construction:
	// equal hash <=> identical projection <=> zero diff rows. Any divergence
	// between the two would be a split-brain bug, so they share one source.
	fields := CanonicalFields(e)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		// Length-prefixed encoding so no key/value byte sequence can be confused
		// with a different (key,value) split (defends the hash domain against
		// delimiter-injection ambiguity).
		writeLenPrefixed(h, k)
		writeLenPrefixed(h, fields[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeLenPrefixed writes len(s) as 8 bytes big-endian followed by s, so the
// concatenation of (key,value,...) is unambiguous regardless of content.
func writeLenPrefixed(w io.Writer, s string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	_, _ = w.Write(n[:])
	_, _ = io.WriteString(w, s)
}

// normalizeWS trims surrounding whitespace and collapses internal whitespace
// runs (spaces, tabs, newlines) to a single space.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// nullSentinel marks an absent (nil) value in the flat canonical projection so
// it never collides with an empty string. It is not a valid JSON value, so a
// real field can never accidentally equal it.
const nullSentinel = "\x00null"

// CanonicalFields is the SHARED canonical projection consumed by BOTH ContentHash
// and the field-level Diff. It maps a stable dotted field_path to a canonical
// string serialization of that field's source-derived semantic value.
//
// field_path SSOT (frozen here so every consumer — diff, change feed, parsers —
// agrees):
//
//   - scalar leaves use the dotted path: "status", "name", "venue.hall",
//     "scale.visitors", "actions.can_register", etc.
//   - ordered/scalar-set arrays (categories, audience, missing_fields) are ONE
//     entry each, keyed by the array name, value = canonical SORTED JSON array
//     string (e.g. `["ai","bio"]`). A reorder is therefore invisible.
//   - sources[] is ONE entry "sources", value = JSON array of {url,type,publisher}
//     (retrieved_at STRIPPED) SORTED by a collision-free composite key, so
//     freshness-only edits and reordering produce no change.
//
// Absent (*string nil, nil Venue/Scale leaf) values map to nullSentinel so the
// nil/empty distinction is preserved. The projection EXCLUDES all freshness /
// operational / curation / identity fields (schema_version, event_id,
// last_checked_at, retrieved_at, created_at, updated_at, batch_id, update_state,
// confidence, curated_by, excluded, content_hash).
func CanonicalFields(e model.Event) map[string]string {
	f := make(map[string]string, 40)

	put := func(path, val string) { f[path] = val }
	putPtr := func(path string, p *string) {
		if p == nil {
			f[path] = nullSentinel
			return
		}
		f[path] = normalizeWS(*p)
	}

	// identity
	// NOTE: schema_version and event_id are deliberately EXCLUDED from this
	// projection (and therefore from ContentHash and Diff). Neither is a
	// source-derived semantic fact: schema_version is an internal versioning
	// marker set from the codebase constant model.SchemaVersion, and event_id is
	// the immutable primary key. Including schema_version would make the first
	// ingest after any model.SchemaVersion bump (e.g. 0.1 -> 0.2) mark EVERY row
	// update_state='updated', advance every updated_at (reordering the
	// (updated_at,event_id) API cursor) and emit a change_log row per event — a
	// non-source change flooding the field-level feed on a pure migration. A
	// genuine re-hash sweep, if ever needed, is an explicit migration step, not a
	// per-field change_log event.
	putPtr("series_id", e.SeriesID)
	put("name", normalizeWS(e.Name))
	putPtr("name_ko", e.NameKo)
	putPtr("name_en", e.NameEn)
	putPtr("edition", e.Edition)

	// when / where
	putPtr("start_date", e.StartDate)
	putPtr("end_date", e.EndDate)
	putPtr("timezone", e.Timezone)
	put("date_confidence", normalizeWS(e.DateConfidence))
	put("status", normalizeWS(e.Status))
	put("format", normalizeWS(e.Format))
	put("country", normalizeWS(e.Country))

	// venue (nested scalar leaves; null when the whole object is absent)
	if e.Venue == nil {
		put("venue.venue_id", nullSentinel)
		put("venue.name", nullSentinel)
		put("venue.hall", nullSentinel)
		put("venue.city", nullSentinel)
	} else {
		putPtr("venue.venue_id", e.Venue.VenueID)
		put("venue.name", normalizeWS(e.Venue.Name))
		putPtr("venue.hall", e.Venue.Hall)
		put("venue.city", normalizeWS(e.Venue.City))
	}

	// classification arrays (one entry each, sorted JSON)
	put("categories", sortedJSONArray(e.Categories))
	put("audience", sortedJSONArray(e.Audience))
	put("missing_fields", sortedJSONArray(e.MissingFields))

	// scale (nested scalar leaves)
	if e.Scale == nil {
		put("scale.visitors", nullSentinel)
		put("scale.exhibitors", nullSentinel)
	} else {
		put("scale.visitors", intPtrStr(e.Scale.Visitors))
		put("scale.exhibitors", intPtrStr(e.Scale.Exhibitors))
	}

	// actions (booleans)
	put("actions.can_register", boolStr(e.Actions.CanRegister))
	put("actions.can_exhibit", boolStr(e.Actions.CanExhibit))
	put("actions.can_sponsor", boolStr(e.Actions.CanSponsor))
	put("actions.has_matchmaking", boolStr(e.Actions.HasMatchmaking))
	put("actions.has_startup_program", boolStr(e.Actions.HasStartupProgram))

	putPtr("register_url", e.RegisterURL)
	putPtr("exhibit_url", e.ExhibitURL)
	putPtr("registration_deadline", e.RegistrationDeadline)
	putPtr("exhibitor_deadline", e.ExhibitorDeadline)
	put("cost_hint", normalizeWS(e.CostHint))
	putPtr("homepage_url", e.HomepageURL)

	// sources (one entry; retrieved_at stripped, sorted, collision-free)
	put("sources", canonicalSources(e.Sources))

	putPtr("ambiguity_notes", e.AmbiguityNotes)

	return f
}

// sortedJSONArray normalizes, sorts and JSON-encodes a string slice. A nil and
// an empty slice both encode to "[]" (the schema treats both as "no values").
func sortedJSONArray(in []string) string {
	cp := make([]string, len(in))
	for i, s := range in {
		cp[i] = normalizeWS(s)
	}
	sort.Strings(cp)
	if cp == nil {
		cp = []string{}
	}
	b, err := json.Marshal(cp)
	if err != nil {
		panic(fmt.Sprintf("marshal canonical array: %v", err))
	}
	return string(b)
}

// canonicalSources encodes sources[] as a JSON array of {url,type,publisher}
// (retrieved_at dropped), each normalized, the slice SORTED by a collision-free
// composite key (the JSON tuple itself), with exact duplicates de-duplicated.
// This makes the value independent of crawler ordering and freshness.
func canonicalSources(in []model.Source) string {
	type cs struct {
		URL       string `json:"url"`
		Type      string `json:"type"`
		Publisher string `json:"publisher"`
	}
	keyed := make([]struct {
		key string
		v   cs
	}, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		v := cs{URL: normalizeWS(s.URL), Type: normalizeWS(s.Type), Publisher: normalizeWS(s.Publisher)}
		// Collision-free sort key: JSON-encode the tuple (component-wise, not a
		// delimiter join), so a '|' inside any field cannot fabricate a tie.
		kb, _ := json.Marshal([]string{v.URL, v.Type, v.Publisher})
		key := string(kb)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keyed = append(keyed, struct {
			key string
			v   cs
		}{key, v})
	}
	sort.Slice(keyed, func(i, j int) bool { return keyed[i].key < keyed[j].key })
	out := make([]cs, len(keyed))
	for i := range keyed {
		out[i] = keyed[i].v
	}
	b, err := json.Marshal(out)
	if err != nil {
		panic(fmt.Sprintf("marshal canonical sources: %v", err))
	}
	return string(b)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intPtrStr(p *int) string {
	if p == nil {
		return nullSentinel
	}
	return strconv.Itoa(*p)
}

// ---------------------------------------------------------------------------
// JSON column helpers
// ---------------------------------------------------------------------------

// mustJSON marshals a value to a JSON string for a TEXT column. It never fails
// for the value shapes used here.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal json column: %v", err))
	}
	return string(b)
}

// jsonNull marshals to a JSON string, or returns a NULL sql argument when the
// pointer/object is nil.
func jsonOrNull(v any, isNil bool) any {
	if isNil {
		return nil
	}
	return mustJSON(v)
}

// ---------------------------------------------------------------------------
// UpsertEvent — single transaction over {event, change_log, raw_snapshot}
// ---------------------------------------------------------------------------

// UpsertEvent writes one event into the canonical store inside a SINGLE
// transaction wrapping, in order:
//
//  1. the event row upsert (INSERT ... ON CONFLICT(event_id) DO UPDATE),
//     which also recomputes content_hash, sets updated_at, and writes the
//     supplied last_checked_at;
//  2. any provided change_log rows;
//  3. the raw_snapshot upsert (if a snapshot is provided).
//
// Any error rolls the whole transaction back, so a failed event leaves the
// prior good row, its change_log, and its raw_snapshot untouched.
//
// content_hash is computed here (not trusted from the caller) so it is always
// consistent with the persisted semantic fields.
func UpsertEvent(ctx context.Context, db *sql.DB, e model.Event, changes []model.ChangeLog, snap *model.RawSnapshot) (err error) {
	hash := ContentHash(e)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = upsertEventRow(ctx, tx, e, hash); err != nil {
		return fmt.Errorf("upsert event %s: %w", e.EventID, err)
	}

	for i, c := range changes {
		if err = insertChangeLog(ctx, tx, c); err != nil {
			return fmt.Errorf("insert change_log[%d] for %s: %w", i, e.EventID, err)
		}
	}

	if snap != nil {
		if err = upsertRawSnapshot(ctx, tx, *snap); err != nil {
			return fmt.Errorf("upsert raw_snapshot %s: %w", e.EventID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", e.EventID, err)
	}
	return nil
}

func upsertEventRow(ctx context.Context, tx *sql.Tx, e model.Event, hash string) error {
	venueID := any(nil)
	if e.Venue != nil && e.Venue.VenueID != nil {
		venueID = *e.Venue.VenueID
	}

	const q = `
INSERT INTO events (
    event_id, schema_version, series_id, name, name_ko, name_en, edition,
    start_date, end_date, timezone, date_confidence, status, format,
    venue, venue_id, country,
    categories, audience, scale,
    actions, register_url, exhibit_url, registration_deadline, exhibitor_deadline, cost_hint,
    summary, sources, homepage_url, last_checked_at, update_state, confidence, missing_fields, ambiguity_notes,
    curated_by, content_hash, excluded, updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now')
)
ON CONFLICT(event_id) DO UPDATE SET
    schema_version=excluded.schema_version,
    series_id=excluded.series_id,
    name=excluded.name,
    name_ko=excluded.name_ko,
    name_en=excluded.name_en,
    edition=excluded.edition,
    start_date=excluded.start_date,
    end_date=excluded.end_date,
    timezone=excluded.timezone,
    date_confidence=excluded.date_confidence,
    status=excluded.status,
    format=excluded.format,
    venue=excluded.venue,
    venue_id=excluded.venue_id,
    country=excluded.country,
    categories=excluded.categories,
    audience=excluded.audience,
    scale=excluded.scale,
    actions=excluded.actions,
    register_url=excluded.register_url,
    exhibit_url=excluded.exhibit_url,
    registration_deadline=excluded.registration_deadline,
    exhibitor_deadline=excluded.exhibitor_deadline,
    cost_hint=excluded.cost_hint,
    summary=excluded.summary,
    sources=excluded.sources,
    homepage_url=excluded.homepage_url,
    last_checked_at=excluded.last_checked_at,
    update_state=excluded.update_state,
    confidence=excluded.confidence,
    missing_fields=excluded.missing_fields,
    ambiguity_notes=excluded.ambiguity_notes,
    curated_by=excluded.curated_by,
    content_hash=excluded.content_hash,
    excluded=excluded.excluded,
    updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
`

	_, err := tx.ExecContext(ctx, q,
		e.EventID, e.SchemaVersion, ptrArg(e.SeriesID), e.Name, ptrArg(e.NameKo), ptrArg(e.NameEn), ptrArg(e.Edition),
		ptrArg(e.StartDate), ptrArg(e.EndDate), ptrArg(e.Timezone), e.DateConfidence, e.Status, e.Format,
		jsonOrNull(e.Venue, e.Venue == nil), venueID, e.Country,
		mustJSON(e.Categories), jsonArrOrNull(e.Audience), jsonOrNull(e.Scale, e.Scale == nil),
		mustJSON(actionsMap(e.Actions)), ptrArg(e.RegisterURL), ptrArg(e.ExhibitURL), ptrArg(e.RegistrationDeadline), ptrArg(e.ExhibitorDeadline), e.CostHint,
		ptrArg(e.Summary), mustJSON(e.Sources), ptrArg(e.HomepageURL), e.LastCheckedAt, e.UpdateState, e.Confidence, mustJSON(e.MissingFields), ptrArg(e.AmbiguityNotes),
		nullIfEmpty(e.CuratedBy), hash, boolToInt(e.Excluded),
	)
	return err
}

func insertChangeLog(ctx context.Context, tx *sql.Tx, c model.ChangeLog) error {
	const q = `
INSERT INTO change_log (event_id, field_path, old_value, new_value, changed_at, batch_id)
VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ExecContext(ctx, q,
		c.EventID, c.FieldPath, ptrArg(c.OldValue), ptrArg(c.NewValue), c.ChangedAt, ptrArg(c.BatchID),
	)
	return err
}

func upsertRawSnapshot(ctx context.Context, tx *sql.Tx, s model.RawSnapshot) error {
	const q = `
INSERT INTO raw_snapshot (event_id, source_id, url, content_type, body, content_hash, retrieved_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_id) DO UPDATE SET
    source_id=excluded.source_id,
    url=excluded.url,
    content_type=excluded.content_type,
    body=excluded.body,
    content_hash=excluded.content_hash,
    retrieved_at=excluded.retrieved_at`
	_, err := tx.ExecContext(ctx, q,
		s.EventID, ptrArg(s.SourceID), ptrArg(s.URL), ptrArg(s.ContentType), s.Body, ptrArg(s.ContentHash), s.RetrievedAt,
	)
	return err
}

// ---------------------------------------------------------------------------
// small arg helpers
// ---------------------------------------------------------------------------

func ptrArg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func actionsMap(a model.Actions) map[string]bool {
	return map[string]bool{
		"can_register":        a.CanRegister,
		"can_exhibit":         a.CanExhibit,
		"can_sponsor":         a.CanSponsor,
		"has_matchmaking":     a.HasMatchmaking,
		"has_startup_program": a.HasStartupProgram,
	}
}

// jsonArrOrNull stores an array as JSON, using a JSON empty array for a nil
// slice (audience may be empty but the column is nullable in the schema).
func jsonArrOrNull(v []string) any {
	if v == nil {
		return nil
	}
	return mustJSON(v)
}

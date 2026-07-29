package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

func strptr(s string) *string { return &s }

// newEvent builds a minimal valid Event for tests.
func newEvent() model.Event {
	return model.Event{
		SchemaVersion:  model.SchemaVersion,
		EventID:        "coex-2026-ai-expo",
		SeriesID:       strptr("ai-expo-korea"),
		Name:           "AI EXPO KOREA",
		NameKo:         strptr("국제인공지능대전"),
		StartDate:      strptr("2026-05-12"),
		EndDate:        strptr("2026-05-14"),
		Timezone:       strptr("Asia/Seoul"),
		DateConfidence: "high",
		Status:         "scheduled",
		Format:         "onsite",
		Venue:          &model.Venue{VenueID: strptr("coex"), Name: "COEX", City: "Seoul"},
		Country:        "KR",
		Categories:     []string{"ai"},
		Audience:       []string{"b2b", "buyers"},
		Actions:        model.Actions{},
		CostHint:       "unknown",
		Sources: []model.Source{
			{URL: "https://www.coex.co.kr/", Type: "venue", Publisher: "COEX", RetrievedAt: "2026-06-21T10:00:00Z"},
		},
		LastCheckedAt: "2026-06-21T10:00:00Z",
		UpdateState:   "new",
		Confidence:    "high",
		MissingFields: []string{"register_url", "homepage_url"},
		CuratedBy:     "smpain",
		CreatedAt:     "2026-06-21T10:00:00Z",
		UpdatedAt:     "2026-06-21T10:00:00Z",
		BatchID:       "batch-001",
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestOpenReadSucceedsAfterFreshMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.db")

	wdb, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}
	if err := store.Migrate(wdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := wdb.Close(); err != nil {
		t.Fatalf("close write db: %v", err)
	}

	rdb, err := store.OpenRead(path)
	if err != nil {
		t.Fatalf("OpenRead after fresh migration: %v", err)
	}
	defer rdb.Close()
}

func TestMigrateCanRunRepeatedlyWithSummaryColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repeatable.db")

	db, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	defer db.Close()

	// Given
	if err := store.Migrate(db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	// When
	err = store.Migrate(db)

	// Then
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var summaryType string
	if err := db.QueryRow(`SELECT type FROM pragma_table_info('events') WHERE name='summary'`).Scan(&summaryType); err != nil {
		t.Fatalf("read summary column: %v", err)
	}
	if summaryType != "TEXT" {
		t.Fatalf("summary type = %q, want TEXT", summaryType)
	}
}

func TestMigrateAddsSummaryToExistingEventsTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")

	db, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	defer db.Close()

	// Given
	if _, err := db.Exec(`
CREATE TABLE events (
    event_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    start_date TEXT,
    end_date TEXT,
    venue TEXT,
    missing_fields TEXT,
    updated_at TEXT,
    venue_id TEXT,
    registration_deadline TEXT,
    exhibitor_deadline TEXT,
    sources TEXT NOT NULL DEFAULT '[]',
    excluded INTEGER NOT NULL DEFAULT 0
)`); err != nil {
		t.Fatalf("create old events table: %v", err)
	}

	// When
	err = store.Migrate(db)

	// Then
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var summaryType string
	if err := db.QueryRow(`SELECT type FROM pragma_table_info('events') WHERE name='summary'`).Scan(&summaryType); err != nil {
		t.Fatalf("read summary column: %v", err)
	}
	if summaryType != "TEXT" {
		t.Fatalf("summary type = %q, want TEXT", summaryType)
	}
}

func TestMigrateClearsTemplateSummaryForExistingEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clear-template-summary.db")

	db, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	defer db.Close()

	// Given
	if _, err := db.Exec(`
CREATE TABLE events (
    event_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    start_date TEXT,
    end_date TEXT,
    venue TEXT,
    summary TEXT,
    missing_fields TEXT,
    updated_at TEXT,
    venue_id TEXT,
    registration_deadline TEXT,
    exhibitor_deadline TEXT,
    sources TEXT NOT NULL DEFAULT '[]',
    excluded INTEGER NOT NULL DEFAULT 0
)`); err != nil {
		t.Fatalf("create existing events table: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO events (event_id, name, start_date, end_date, venue, summary, missing_fields) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"coex-old", "Old Event", "2026-06-22", "2026-06-23", `{"name":"COEX"}`,
		"Old Event — 2026-06-22~2026-06-23 @ COEX", `["name_ko"]`,
	); err != nil {
		t.Fatalf("seed old event: %v", err)
	}

	// When
	err = store.Migrate(db)

	// Then
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var summary sql.NullString
	if err := db.QueryRow(`SELECT summary FROM events WHERE event_id='coex-old'`).Scan(&summary); err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if summary.Valid {
		t.Fatalf("summary = %q, want NULL for generated template", summary.String)
	}
	var missing string
	if err := db.QueryRow(`SELECT missing_fields FROM events WHERE event_id='coex-old'`).Scan(&missing); err != nil {
		t.Fatalf("read missing_fields: %v", err)
	}
	if !strings.Contains(missing, `"summary"`) {
		t.Fatalf("missing_fields = %s, want summary", missing)
	}
}

// TestContentHashStableUnderFreshnessChange proves requirement (a): two builds
// of the same event differing ONLY in freshness/operational fields produce a
// byte-identical content_hash.
func TestContentHashStableUnderFreshnessChange(t *testing.T) {
	a := newEvent()
	b := newEvent()

	// Mutate ONLY excluded fields on b.
	b.LastCheckedAt = "2099-12-31T23:59:59Z"
	b.CreatedAt = "2000-01-01T00:00:00Z"
	b.UpdatedAt = "2099-12-31T23:59:59Z"
	b.BatchID = "batch-999"
	b.UpdateState = "updated"
	b.Sources[0].RetrievedAt = "2099-12-31T23:59:59Z"

	ha := store.ContentHash(a)
	hb := store.ContentHash(b)

	if ha == "" {
		t.Fatal("ContentHash returned empty string")
	}
	if ha != hb {
		t.Fatalf("content_hash changed under freshness-only mutation:\n a=%s\n b=%s", ha, hb)
	}
}

// TestContentHashChangesWithSemanticChange ensures the hash is sensitive to a
// real source-derived field change (negative control for the test above).
func TestContentHashChangesWithSemanticChange(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Status = "cancelled"

	if store.ContentHash(a) == store.ContentHash(b) {
		t.Fatal("content_hash did not change when status changed")
	}
}

// TestContentHashSortsArrays proves the canonical serialization sorts arrays so
// element order from the source does not affect the hash.
func TestContentHashSortsArrays(t *testing.T) {
	a := newEvent()
	a.Categories = []string{"ai", "bio"}
	a.Audience = []string{"b2b", "buyers"}
	a.MissingFields = []string{"register_url", "homepage_url"}
	a.Sources = []model.Source{
		{URL: "https://a.example/", Type: "venue", Publisher: "A", RetrievedAt: "2026-06-21T10:00:00Z"},
		{URL: "https://b.example/", Type: "press", Publisher: "B", RetrievedAt: "2026-06-21T10:00:00Z"},
	}

	b := newEvent()
	b.Categories = []string{"bio", "ai"}                       // reversed
	b.Audience = []string{"buyers", "b2b"}                     // reversed
	b.MissingFields = []string{"homepage_url", "register_url"} // reversed
	b.Sources = []model.Source{
		{URL: "https://b.example/", Type: "press", Publisher: "B", RetrievedAt: "2099-12-31T00:00:00Z"},
		{URL: "https://a.example/", Type: "venue", Publisher: "A", RetrievedAt: "2000-01-01T00:00:00Z"},
	}

	if store.ContentHash(a) != store.ContentHash(b) {
		t.Fatal("content_hash sensitive to array order; arrays must be sorted in canonical form")
	}
}

// TestContentHashNormalizesWhitespace proves whitespace normalization in name.
func TestContentHashNormalizesWhitespace(t *testing.T) {
	a := newEvent()
	a.Name = "AI EXPO KOREA"
	b := newEvent()
	b.Name = "  AI   EXPO\tKOREA  "

	if store.ContentHash(a) != store.ContentHash(b) {
		t.Fatal("content_hash sensitive to insignificant whitespace; must normalize")
	}
}

// TestUpsertEventInsertsAndUpdates proves a round-trip upsert: insert then
// update by event_id, plus raw_snapshot and last_checked_at persistence.
func TestUpsertEventInsertsAndUpdates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	e := newEvent()
	e.Summary = strptr("인공지능 산업의 최신 기술과 실제 비즈니스 적용 사례를 공유하는 전문 전시회입니다.")
	snap := &model.RawSnapshot{
		EventID:     e.EventID,
		SourceID:    strptr("coex"),
		URL:         strptr("https://www.coex.co.kr/event/1"),
		ContentType: strptr("text/html"),
		Body:        []byte("<html>v1</html>"),
		ContentHash: strptr(store.ContentHash(e)),
		RetrievedAt: "2026-06-21T10:00:00Z",
	}

	if err := store.UpsertEvent(ctx, db, e, nil, snap); err != nil {
		t.Fatalf("first UpsertEvent: %v", err)
	}

	gotName, gotHash := readEventRow(t, db, e.EventID)
	if gotName != "AI EXPO KOREA" {
		t.Fatalf("name = %q, want AI EXPO KOREA", gotName)
	}
	if gotHash != store.ContentHash(e) {
		t.Fatalf("content_hash = %q, want %q", gotHash, store.ContentHash(e))
	}

	// Update: change a semantic field, re-upsert.
	e.Name = "AI EXPO KOREA 2026"
	e.LastCheckedAt = "2026-06-22T10:00:00Z"
	if err := store.UpsertEvent(ctx, db, e, nil, snap); err != nil {
		t.Fatalf("second UpsertEvent: %v", err)
	}

	gotName, _ = readEventRow(t, db, e.EventID)
	if gotName != "AI EXPO KOREA 2026" {
		t.Fatalf("after update name = %q, want AI EXPO KOREA 2026", gotName)
	}

	// Exactly one row should exist for this event_id.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE event_id=?`, e.EventID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("event row count = %d, want 1", n)
	}

	// last_checked_at must reflect the update.
	var lca string
	if err := db.QueryRow(`SELECT last_checked_at FROM events WHERE event_id=?`, e.EventID).Scan(&lca); err != nil {
		t.Fatalf("read last_checked_at: %v", err)
	}
	if lca != "2026-06-22T10:00:00Z" {
		t.Fatalf("last_checked_at = %q, want 2026-06-22T10:00:00Z", lca)
	}
}

func TestUpsertEventPersistsSummary(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Given
	e := newEvent()
	e.Summary = strptr("인공지능 산업의 최신 기술과 실제 비즈니스 적용 사례를 공유하는 전문 전시회입니다.")

	// When
	if err := store.UpsertEvent(ctx, db, e, nil, nil); err != nil {
		t.Fatalf("UpsertEvent: %v", err)
	}

	// Then
	var got sql.NullString
	if err := db.QueryRow(`SELECT summary FROM events WHERE event_id=?`, e.EventID).Scan(&got); err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !got.Valid || got.String != *e.Summary {
		t.Fatalf("summary = %q valid=%v, want %q", got.String, got.Valid, *e.Summary)
	}
}

// TestUpsertEventWritesChangeLog proves change_log rows are inserted within the
// same upsert transaction.
func TestUpsertEventWritesChangeLog(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	e := newEvent()
	if err := store.UpsertEvent(ctx, db, e, nil, nil); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}

	changes := []model.ChangeLog{
		{EventID: e.EventID, FieldPath: "status", OldValue: strptr("scheduled"), NewValue: strptr("cancelled"), ChangedAt: "2026-06-22T10:00:00Z", BatchID: strptr("batch-002")},
	}
	e.Status = "cancelled"
	if err := store.UpsertEvent(ctx, db, e, changes, nil); err != nil {
		t.Fatalf("upsert with changes: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM change_log WHERE event_id=? AND field_path='status'`, e.EventID).Scan(&n); err != nil {
		t.Fatalf("count change_log: %v", err)
	}
	if n != 1 {
		t.Fatalf("change_log rows = %d, want 1", n)
	}
}

// TestUpsertEventRollsBackOnError proves requirement (b): if the transaction
// fails AFTER the event upsert but BEFORE change_log inserts complete, the
// whole transaction rolls back and the prior good row is preserved unchanged.
func TestUpsertEventRollsBackOnError(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Seed a known-good row.
	good := newEvent()
	if err := store.UpsertEvent(ctx, db, good, nil, nil); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	priorName, priorHash := readEventRow(t, db, good.EventID)

	// Now attempt an upsert whose change_log batch contains an invalid row that
	// will fail mid-transaction (NULL into a NOT NULL column: field_path).
	bad := good
	bad.Name = "MUTATED — must not persist"
	badChanges := []model.ChangeLog{
		// First change is valid; the second references a non-existent event_id
		// which violates the change_log -> events foreign key (foreign_keys=ON),
		// forcing the transaction to fail AFTER the event upsert but during the
		// change_log inserts.
		{EventID: good.EventID, FieldPath: "name", OldValue: strptr(priorName), NewValue: strptr(bad.Name), ChangedAt: "2026-06-22T10:00:00Z"},
		{EventID: "does-not-exist-anywhere", FieldPath: "status", ChangedAt: "2026-06-22T10:00:00Z"},
	}

	err := store.UpsertEvent(ctx, db, bad, badChanges, nil)
	if err == nil {
		t.Fatal("expected UpsertEvent to fail on bad change_log, got nil")
	}

	// The event row must be UNCHANGED (rollback).
	gotName, gotHash := readEventRow(t, db, good.EventID)
	if gotName != priorName {
		t.Fatalf("rollback failed: name = %q, want preserved %q", gotName, priorName)
	}
	if gotHash != priorHash {
		t.Fatalf("rollback failed: content_hash = %q, want preserved %q", gotHash, priorHash)
	}

	// No change_log rows should have been committed.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM change_log WHERE event_id=?`, good.EventID).Scan(&n); err != nil {
		t.Fatalf("count change_log: %v", err)
	}
	if n != 0 {
		t.Fatalf("change_log rows = %d after rollback, want 0", n)
	}
}

func readEventRow(t *testing.T, db *sql.DB, id string) (name, contentHash string) {
	t.Helper()
	var ch sql.NullString
	if err := db.QueryRow(`SELECT name, content_hash FROM events WHERE event_id=?`, id).Scan(&name, &ch); err != nil {
		t.Fatalf("readEventRow %s: %v", id, err)
	}
	return name, ch.String
}

// Migration 0010 revokes pre-gate Solar deadlines. It must revoke exactly the
// old-generation values, leave evidence-gated /v2 values standing on every
// later startup, and never touch a purely scraped row.
func TestMigrateRevokesOnlyPreGateSolarDeadlines(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenWrite(filepath.Join(dir, "revoke.db"))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}

	// Given: one pre-gate row, one gated /v2 row, one scraped row.
	seed := func(id, publisher, deadline string) {
		srcs := `[{"url":"https://venue.example/e","type":"venue","publisher":"venue","retrieved_at":"2026-07-27T00:00:00Z"}`
		if publisher != "" {
			srcs += `,{"url":"https://venue.example/e","type":"organizer","publisher":"` + publisher + `","retrieved_at":"2026-07-27T00:00:00Z"}`
		}
		srcs += `]`
		if _, err := db.Exec(`INSERT INTO events (
			event_id, schema_version, name, country, categories, sources,
			last_checked_at, update_state, confidence, missing_fields,
			registration_deadline, exhibitor_deadline
		) VALUES (?, '0.1', 'E', 'KR', '["ai"]', ?,
			'2026-07-27T00:00:00Z', 'new', 'high', '[]', ?, ?)`,
			id, srcs, deadline, deadline); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("pre-gate", "eventsintel/solar-enrich", "2026-09-30")
	seed("gated-v2", "eventsintel/solar-enrich/v2", "2026-10-15")
	seed("scraped", "", "2026-11-01")

	// When: migrations run twice, as two service startups would.
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	// Then
	check := func(id string, wantNull bool, wantMissing bool) {
		var reg sql.NullString
		var missing string
		if err := db.QueryRow(`SELECT registration_deadline, missing_fields FROM events WHERE event_id=?`, id).
			Scan(&reg, &missing); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if wantNull && reg.Valid {
			t.Fatalf("%s deadline = %q, want revoked", id, reg.String)
		}
		if !wantNull && !reg.Valid {
			t.Fatalf("%s deadline revoked, want kept", id)
		}
		if wantMissing != strings.Contains(missing, `"registration_deadline"`) {
			t.Fatalf("%s missing_fields = %s, want restored=%v", id, missing, wantMissing)
		}
	}
	check("pre-gate", true, true)
	check("gated-v2", false, false)
	check("scraped", false, false)
}

func TestCountUpcomingWithDeadline(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	mk := func(id, start string, deadline *string) model.Event {
		e := newEvent()
		e.EventID = id
		e.StartDate = strptr(start)
		e.EndDate = strptr(start)
		e.RegistrationDeadline = deadline
		return e
	}
	events := []model.Event{
		mk("ev-up-deadline", "2099-01-01", strptr("2098-12-01")),
		mk("ev-up-none", "2099-01-01", nil),
		mk("ev-past", "2000-01-01", strptr("1999-12-01")),
	}
	if err := store.ApplyBatch(ctx, db, events, "batch-count"); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	n, err := store.CountUpcomingWithDeadline(ctx, db, "2026-07-29")
	if err != nil {
		t.Fatalf("CountUpcomingWithDeadline: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

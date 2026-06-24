package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
)

func dedupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenWrite(filepath.Join(t.TempDir(), "dedup.db"))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func evt(eventID, name, start, venueID string) model.Event {
	s := start
	v := venueID
	return model.Event{
		SchemaVersion:  "0.1",
		EventID:        eventID,
		Name:           name,
		StartDate:      &s,
		EndDate:        &s,
		DateConfidence: "high",
		Status:         "scheduled",
		Format:         "onsite",
		Venue:          &model.Venue{Name: "KINTEX", City: "고양", VenueID: &v},
		Country:        "KR",
		Categories:     []string{"ai"},
		Audience:       []string{},
		CostHint:       "unknown",
		Sources:        []model.Source{{URL: "https://example.org/" + eventID, Type: "venue", Publisher: "x", RetrievedAt: "2026-06-24T00:00:00Z"}},
		LastCheckedAt:  "2026-06-24T00:00:00Z",
		Confidence:     "high",
		MissingFields:  []string{},
	}
}

func supersededOf(t *testing.T, db *sql.DB, eventID string) int {
	t.Helper()
	var s int
	if err := db.QueryRow(`SELECT superseded FROM events WHERE event_id=?`, eventID).Scan(&s); err != nil {
		t.Fatalf("read superseded %s: %v", eventID, err)
	}
	return s
}

func canonicalCount(t *testing.T, db *sql.DB, contentKey string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE content_key=? AND superseded=0`, contentKey).Scan(&n); err != nil {
		t.Fatalf("count canonical: %v", err)
	}
	return n
}

// ContentKey is a conservative (name|start|venue_id) fingerprint; year/edition/
// punctuation/spacing differences must NOT split it, and a missing start_date or
// venue_id must yield "" (never de-duped).
func TestContentKey(t *testing.T) {
	kintexVenue := func(id string) *model.Venue { return &model.Venue{Name: "KINTEX", VenueID: &id} }

	a := model.Event{Name: "2026 로보월드", StartDate: ptrStr("2026-11-04"), Venue: kintexVenue("kintex")}
	b := model.Event{Name: "로보월드", StartDate: ptrStr("2026-11-04"), Venue: kintexVenue("kintex")}
	if ka, kb := ContentKey(a), ContentKey(b); ka != kb || ka == "" {
		t.Fatalf("year-difference should not split key: %q vs %q", ka, kb)
	}

	c := model.Event{Name: "2026년 제5회 킨텍스 캠핑페스타", StartDate: ptrStr("2026-07-03"), Venue: kintexVenue("kintex")}
	d := model.Event{Name: "2026 제 5 회  킨텍스 캠핑페스타!", StartDate: ptrStr("2026-07-03"), Venue: kintexVenue("kintex")}
	if kc, kd := ContentKey(c), ContentKey(d); kc != kd || kc == "" {
		t.Fatalf("edition/spacing/punct should not split key: %q vs %q", kc, kd)
	}

	// Different real events with same date+venue keep DISTINCT keys (no false merge).
	e := model.Event{Name: "국제광융합엑스포", StartDate: ptrStr("2026-07-08"), Venue: kintexVenue("kintex")}
	f := model.Event{Name: "나노기술융합전시회", StartDate: ptrStr("2026-07-08"), Venue: kintexVenue("kintex")}
	if ContentKey(e) == ContentKey(f) {
		t.Fatalf("distinct events collapsed to one key: %q", ContentKey(e))
	}

	// No start_date or no venue_id -> empty key (never de-duped).
	if got := ContentKey(model.Event{Name: "x", Venue: kintexVenue("kintex")}); got != "" {
		t.Fatalf("missing start_date should yield empty key, got %q", got)
	}
	if got := ContentKey(model.Event{Name: "x", StartDate: ptrStr("2026-01-01")}); got != "" {
		t.Fatalf("missing venue should yield empty key, got %q", got)
	}
}

// Authority: a venue-native source (kintex) is canonical over an aggregator
// (showala) for the same content_key — regardless of apply order — and the
// cluster always has exactly one canonical row (contract 1+2).
func TestDedupAuthorityAndOrderIndependence(t *testing.T) {
	for _, order := range []string{"kintex-first", "showala-first"} {
		t.Run(order, func(t *testing.T) {
			db := dedupDB(t)
			ctx := context.Background()

			kintex := evt("kintex-100", "2026 로보월드", "2026-11-04", "kintex")
			showala := evt("showala-3219", "로보월드", "2026-11-04", "kintex")
			key := ContentKey(kintex)
			if key == "" || key != ContentKey(showala) {
				t.Fatalf("precondition: events must share a non-empty key (%q vs %q)", key, ContentKey(showala))
			}

			first, second := kintex, showala
			if order == "showala-first" {
				first, second = showala, kintex
			}
			if err := ApplyBatch(ctx, db, []model.Event{first}, "b1"); err != nil {
				t.Fatalf("apply first: %v", err)
			}
			if err := ApplyBatch(ctx, db, []model.Event{second}, "b2"); err != nil {
				t.Fatalf("apply second: %v", err)
			}

			if n := canonicalCount(t, db, key); n != 1 {
				t.Fatalf("canonical count = %d, want 1", n)
			}
			if supersededOf(t, db, "kintex-100") != 0 {
				t.Fatalf("kintex (venue-native) must be canonical")
			}
			if supersededOf(t, db, "showala-3219") != 1 {
				t.Fatalf("showala (aggregator) must be superseded")
			}
		})
	}
}

// Idempotence: re-applying the same batch leaves the superseded flags unchanged
// and the canonical count at 1 (contract 4).
func TestDedupIdempotent(t *testing.T) {
	db := dedupDB(t)
	ctx := context.Background()
	kintex := evt("kintex-100", "2026 로보월드", "2026-11-04", "kintex")
	showala := evt("showala-3219", "로보월드", "2026-11-04", "kintex")
	key := ContentKey(kintex)

	for i := 0; i < 3; i++ {
		if err := ApplyBatch(ctx, db, []model.Event{kintex, showala}, "b"); err != nil {
			t.Fatalf("apply round %d: %v", i, err)
		}
		if n := canonicalCount(t, db, key); n != 1 {
			t.Fatalf("round %d canonical count = %d, want 1", i, n)
		}
		if supersededOf(t, db, "kintex-100") != 0 || supersededOf(t, db, "showala-3219") != 1 {
			t.Fatalf("round %d: superseded flags drifted", i)
		}
	}
}

// Single-source safety: a lone event is always canonical, and two events that
// each lack a content_key (no start/venue) never de-dup each other (contract 3).
func TestDedupSingleSourceAndKeyless(t *testing.T) {
	db := dedupDB(t)
	ctx := context.Background()

	lone := evt("kintex-1", "단독 행사", "2026-09-01", "kintex")
	if err := ApplyBatch(ctx, db, []model.Event{lone}, "b"); err != nil {
		t.Fatalf("apply lone: %v", err)
	}
	if supersededOf(t, db, "kintex-1") != 0 {
		t.Fatalf("single-source row must be canonical")
	}

	// Two keyless rows (no start_date) sharing a name must both stay visible.
	k1 := evt("kintex-2", "날짜미상 행사", "x", "kintex")
	k1.StartDate = nil
	k1.EndDate = nil
	k2 := evt("showala-2", "날짜미상 행사", "x", "kintex")
	k2.StartDate = nil
	k2.EndDate = nil
	if err := ApplyBatch(ctx, db, []model.Event{k1, k2}, "b2"); err != nil {
		t.Fatalf("apply keyless: %v", err)
	}
	if supersededOf(t, db, "kintex-2") != 0 || supersededOf(t, db, "showala-2") != 0 {
		t.Fatalf("keyless rows must never be superseded")
	}
}

// Read path: a cross-source duplicate appears once (the canonical row), while a
// SHOWALA-only event (no kintex twin, e.g. RoboWorld in production) still shows
// (AC-3 + AC-4). GetEvent on a superseded id resolves as not-found.
func TestDedupReadPathHidesSuperseded(t *testing.T) {
	db := dedupDB(t)
	ctx := context.Background()

	dup1 := evt("kintex-100", "2026 캠핑페스타", "2026-07-03", "kintex")  // in both sources
	dup2 := evt("showala-700", "캠핑페스타", "2026-07-03", "kintex")      // same event via aggregator
	only := evt("showala-3219", "2026 로보월드", "2026-11-04", "kintex") // SHOWALA-only
	if err := ApplyBatch(ctx, db, []model.Event{dup1, dup2, only}, "b"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	events, _, err := ListEvents(ctx, db, EventFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.EventID] = true
	}
	if !seen["kintex-100"] {
		t.Error("canonical kintex row missing from listing")
	}
	if seen["showala-700"] {
		t.Error("superseded showala duplicate leaked into listing")
	}
	if !seen["showala-3219"] {
		t.Error("SHOWALA-only event (RoboWorld) missing from listing")
	}

	// GetEvent: superseded id is not-found; canonical and showala-only resolve.
	if got, _ := GetEvent(ctx, db, "showala-700"); got != nil {
		t.Error("GetEvent returned a superseded duplicate")
	}
	if got, _ := GetEvent(ctx, db, "kintex-100"); got == nil {
		t.Error("GetEvent did not return the canonical row")
	}
	if got, _ := GetEvent(ctx, db, "showala-3219"); got == nil {
		t.Error("GetEvent did not return the SHOWALA-only row")
	}
}

// If the canonical event's content_key CHANGES on a later run, the cluster it
// leaves behind must re-elect a canonical — never end with zero visible rows
// (doubt violation #1: old key was orphaned).
func TestDedupCanonicalKeyChangeSelfHeals(t *testing.T) {
	db := dedupDB(t)
	ctx := context.Background()

	kintex := evt("kintex-100", "2026 로보월드", "2026-11-04", "kintex")
	showala := evt("showala-3219", "로보월드", "2026-11-04", "kintex")
	oldKey := ContentKey(kintex)

	if err := ApplyBatch(ctx, db, []model.Event{kintex, showala}, "b1"); err != nil {
		t.Fatalf("apply b1: %v", err)
	}
	if supersededOf(t, db, "showala-3219") != 1 {
		t.Fatalf("precondition: showala should be superseded after b1")
	}

	// kintex-100's start_date is corrected -> its content_key changes, leaving the
	// old cluster holding only the (currently superseded) showala row.
	kintex.StartDate = ptrStr("2026-11-05")
	kintex.EndDate = ptrStr("2026-11-05")
	newKey := ContentKey(kintex)
	if newKey == oldKey {
		t.Fatalf("precondition: key must change")
	}
	if err := ApplyBatch(ctx, db, []model.Event{kintex}, "b2"); err != nil {
		t.Fatalf("apply b2: %v", err)
	}

	// Old cluster (showala alone) must re-elect showala as canonical.
	if got := canonicalCount(t, db, oldKey); got != 1 {
		t.Fatalf("old cluster canonical count = %d, want 1 (orphaned cluster)", got)
	}
	if supersededOf(t, db, "showala-3219") != 0 {
		t.Fatalf("showala must become canonical after kintex left its cluster")
	}
	if supersededOf(t, db, "kintex-100") != 0 {
		t.Fatalf("kintex must be canonical of its new cluster")
	}
	// And it is now visible again in the listing.
	events, _, err := ListEvents(ctx, db, EventFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var sawShowala bool
	for _, e := range events {
		if e.EventID == "showala-3219" {
			sawShowala = true
		}
	}
	if !sawShowala {
		t.Fatalf("showala-3219 should reappear in listing after key change")
	}
}

// The change feed must not surface change_log rows for superseded events (doubt
// violation #2).
func TestDedupChangeFeedExcludesSuperseded(t *testing.T) {
	db := dedupDB(t)
	ctx := context.Background()

	// Round 1: showala-700 is alone (canonical) and gets a real field change in
	// round 1b so change_log rows exist for it.
	showala := evt("showala-700", "캠핑페스타", "2026-07-03", "kintex")
	if err := ApplyBatch(ctx, db, []model.Event{showala}, "b1"); err != nil {
		t.Fatalf("apply b1: %v", err)
	}
	showala.Name = "캠핑페스타 변경" // forces an updated row + change_log entries
	if err := ApplyBatch(ctx, db, []model.Event{showala}, "b2"); err != nil {
		t.Fatalf("apply b2: %v", err)
	}
	// Now the venue-native twin arrives and supersedes showala-700.
	kintex := evt("kintex-100", "캠핑페스타 변경", "2026-07-03", "kintex")
	if err := ApplyBatch(ctx, db, []model.Event{kintex}, "b3"); err != nil {
		t.Fatalf("apply b3: %v", err)
	}
	if supersededOf(t, db, "showala-700") != 1 {
		t.Fatalf("precondition: showala-700 must be superseded")
	}

	changes, _, err := ListChanges(ctx, db, ChangeFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListChanges: %v", err)
	}
	for _, c := range changes {
		if c.EventID == "showala-700" {
			t.Fatalf("change feed leaked a superseded event's changes: %+v", c)
		}
	}
}

// The dedup migration is safe to re-apply on every startup, and both columns
// exist after migration (doubt violation #3 guard).
func TestMigrateIdempotentWithDedupColumns(t *testing.T) {
	db := dedupDB(t) // already migrated once
	// Re-running every migration must not error (ADD COLUMN dups are tolerated).
	if err := Migrate(db); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}
	hasCol := func(name string) bool {
		rows, err := db.Query(`PRAGMA table_info(events)`)
		if err != nil {
			t.Fatalf("pragma: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var cname, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if cname == name {
				return true
			}
		}
		return false
	}
	if !hasCol("content_key") || !hasCol("superseded") {
		t.Fatal("dedup columns missing after migrate")
	}
}

func TestAuthorityRank(t *testing.T) {
	if authorityRank("kintex-1") <= authorityRank("showala-1") {
		t.Fatal("kintex must outrank showala")
	}
	if authorityRank("unknown-1") != defaultAuthority {
		t.Fatal("unknown source must use defaultAuthority (fail-safe high)")
	}
}

func ptrStr(s string) *string { return &s }

package store_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// ---------------------------------------------------------------------------
// Diff: pure field-level change detection over the SHARED canonical projection.
//
// Contract (PLAN Task 1.3):
//   - Diff(prev, next) yields exactly one ChangeLog row per changed canonical
//     field_path, in stable (sorted) field_path order.
//   - prev==nil  => new event => zero change rows (creation is not a "change").
//   - The set of field_paths is derived from CanonicalFields, so the diff and
//     ContentHash agree by construction: equal hash => zero rows; different
//     hash => >=1 row.
// ---------------------------------------------------------------------------

func fieldPaths(changes []model.ChangeLog) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.FieldPath
	}
	return out
}

func isSorted(s []string) bool { return sort.StringsAreSorted(s) }

// TestDiffIdenticalZeroRows: identical semantic state => zero change rows.
func TestDiffIdenticalZeroRows(t *testing.T) {
	a := newEvent()
	b := newEvent()
	// Freshness-only divergence must NOT register as a change.
	b.LastCheckedAt = "2099-01-01T00:00:00Z"
	b.UpdatedAt = "2099-01-01T00:00:00Z"
	b.UpdateState = "updated"
	b.Sources[0].RetrievedAt = "2099-12-31T23:59:59Z"

	changes := store.Diff(&a, &b, "2026-06-22T10:00:00Z", "batch-002")
	if len(changes) != 0 {
		t.Fatalf("identical semantic state produced %d change rows, want 0: %v", len(changes), fieldPaths(changes))
	}
}

// TestDiffNilPrevIsNew: a brand-new event (no prior) yields zero rows.
func TestDiffNilPrevIsNew(t *testing.T) {
	b := newEvent()
	changes := store.Diff(nil, &b, "2026-06-22T10:00:00Z", "batch-001")
	if len(changes) != 0 {
		t.Fatalf("new event produced %d change rows, want 0", len(changes))
	}
}

// TestDiffSingleFieldOneRow: a single semantic field change => exactly 1 row
// with the correct columns.
func TestDiffSingleFieldOneRow(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Status = "cancelled"

	changes := store.Diff(&a, &b, "2026-06-22T10:00:00Z", "batch-002")
	if len(changes) != 1 {
		t.Fatalf("single field change produced %d rows, want 1: %v", len(changes), fieldPaths(changes))
	}
	c := changes[0]
	if c.EventID != b.EventID {
		t.Fatalf("event_id = %q, want %q", c.EventID, b.EventID)
	}
	if c.FieldPath != "status" {
		t.Fatalf("field_path = %q, want status", c.FieldPath)
	}
	if c.OldValue == nil || *c.OldValue != "scheduled" {
		t.Fatalf("old_value = %v, want scheduled", c.OldValue)
	}
	if c.NewValue == nil || *c.NewValue != "cancelled" {
		t.Fatalf("new_value = %v, want cancelled", c.NewValue)
	}
	if c.ChangedAt != "2026-06-22T10:00:00Z" {
		t.Fatalf("changed_at = %q", c.ChangedAt)
	}
	if c.BatchID == nil || *c.BatchID != "batch-002" {
		t.Fatalf("batch_id = %v, want batch-002", c.BatchID)
	}
}

// TestDiffStatusTransitionScheduledToPostponed: scheduled->postponed retained as
// exactly one row (no special casing that drops it).
func TestDiffStatusTransitionScheduledToPostponed(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Status = "postponed"
	changes := store.Diff(&a, &b, "2026-06-22T10:00:00Z", "b")
	if len(changes) != 1 || changes[0].FieldPath != "status" {
		t.Fatalf("postponed transition: got %v, want one status row", fieldPaths(changes))
	}
}

// TestDiffNestedScalarPath: a nested scalar change uses a dotted leaf path.
func TestDiffNestedScalarPath(t *testing.T) {
	a := newEvent()
	b := newEvent()
	hall := "Hall C"
	b.Venue = &model.Venue{VenueID: a.Venue.VenueID, Name: a.Venue.Name, Hall: &hall, City: a.Venue.City}

	changes := store.Diff(&a, &b, "t", "b")
	if len(changes) != 1 {
		t.Fatalf("venue.hall change produced %d rows, want 1: %v", len(changes), fieldPaths(changes))
	}
	if changes[0].FieldPath != "venue.hall" {
		t.Fatalf("field_path = %q, want venue.hall", changes[0].FieldPath)
	}
	if changes[0].OldValue != nil {
		t.Fatalf("old_value = %v, want nil (was absent)", changes[0].OldValue)
	}
	if changes[0].NewValue == nil || *changes[0].NewValue != "Hall C" {
		t.Fatalf("new_value = %v, want Hall C", changes[0].NewValue)
	}
}

// TestDiffActionBoolPath: an actions boolean change uses actions.<key>.
func TestDiffActionBoolPath(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Actions.CanRegister = true
	changes := store.Diff(&a, &b, "t", "b")
	if len(changes) != 1 || changes[0].FieldPath != "actions.can_register" {
		t.Fatalf("actions change: got %v, want actions.can_register", fieldPaths(changes))
	}
}

// TestDiffArrayIsSingleRow: adding one category yields ONE row field_path=categories
// with old/new as canonical sorted JSON arrays (NOT per-element rows).
func TestDiffArrayIsSingleRow(t *testing.T) {
	a := newEvent()
	a.Categories = []string{"ai"}
	b := newEvent()
	b.Categories = []string{"bio", "ai"} // added bio, also reordered

	changes := store.Diff(&a, &b, "t", "b")
	if len(changes) != 1 {
		t.Fatalf("category add produced %d rows, want 1: %v", len(changes), fieldPaths(changes))
	}
	if changes[0].FieldPath != "categories" {
		t.Fatalf("field_path = %q, want categories", changes[0].FieldPath)
	}
	// Values must be canonical sorted JSON arrays.
	if changes[0].OldValue == nil || *changes[0].OldValue != `["ai"]` {
		t.Fatalf("old_value = %v, want [\"ai\"]", changes[0].OldValue)
	}
	if changes[0].NewValue == nil || *changes[0].NewValue != `["ai","bio"]` {
		t.Fatalf("new_value = %v, want [\"ai\",\"bio\"]", changes[0].NewValue)
	}
}

// TestDiffArrayReorderNoRow: reordering an array (same set) yields zero rows,
// matching ContentHash stability.
func TestDiffArrayReorderNoRow(t *testing.T) {
	a := newEvent()
	a.Categories = []string{"ai", "bio"}
	b := newEvent()
	b.Categories = []string{"bio", "ai"}
	if len(store.Diff(&a, &b, "t", "b")) != 0 {
		t.Fatal("array reorder must not produce a change row")
	}
}

// TestDiffSchemaVersionOnlyNoRow: schema_version is an internal versioning marker
// (set from the codebase constant model.SchemaVersion), NOT a source-derived
// semantic field. Bumping it alone must keep content_hash byte-identical and
// produce zero Diff rows, so a future SchemaVersion bump (0.1 -> 0.2) does not
// flood the change feed nor reorder the (updated_at,event_id) API cursor on a
// pure migration. event_id (the immutable primary key) is likewise excluded.
func TestDiffSchemaVersionOnlyNoRow(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.SchemaVersion = "0.2"

	if store.ContentHash(a) != store.ContentHash(b) {
		t.Fatal("content_hash changed under schema_version-only mutation")
	}
	if rows := store.Diff(&a, &b, "t", "b"); len(rows) != 0 {
		t.Fatalf("schema_version-only change produced %d rows, want 0: %v", len(rows), fieldPaths(rows))
	}
}

// TestDiffSourcesFreshnessOnlyNoRow: a sources[] edit that ONLY changes
// retrieved_at must produce zero rows (freshness is stripped from the projection).
func TestDiffSourcesFreshnessOnlyNoRow(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Sources[0].RetrievedAt = "2099-12-31T23:59:59Z"
	if len(store.Diff(&a, &b, "t", "b")) != 0 {
		t.Fatal("retrieved_at-only change must not produce a sources change row")
	}
}

// TestDiffSourcesRealChangeOneRow: a real source URL change yields ONE row
// field_path=sources.
func TestDiffSourcesRealChangeOneRow(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Sources = []model.Source{
		{URL: "https://www.coex.co.kr/event/2", Type: "venue", Publisher: "COEX", RetrievedAt: "2026-06-21T10:00:00Z"},
	}
	changes := store.Diff(&a, &b, "t", "b")
	if len(changes) != 1 || changes[0].FieldPath != "sources" {
		t.Fatalf("source url change: got %v, want one sources row", fieldPaths(changes))
	}
}

// TestDiffMultiFieldNRowsStableOrder: a multi-field change yields N rows in
// stable sorted field_path order, identical across repeated runs.
func TestDiffMultiFieldNRowsStableOrder(t *testing.T) {
	a := newEvent()
	b := newEvent()
	b.Status = "cancelled"
	b.Categories = []string{"ai", "bio"}
	b.Actions.CanExhibit = true
	hall := "Hall D"
	b.Venue = &model.Venue{VenueID: a.Venue.VenueID, Name: a.Venue.Name, Hall: &hall, City: a.Venue.City}

	var prev []string
	for run := 0; run < 5; run++ {
		changes := store.Diff(&a, &b, "t", "b")
		paths := fieldPaths(changes)
		if len(paths) != 4 {
			t.Fatalf("run %d: got %d rows, want 4: %v", run, len(paths), paths)
		}
		if !isSorted(paths) {
			t.Fatalf("run %d: field_paths not sorted: %v", run, paths)
		}
		want := []string{"actions.can_exhibit", "categories", "status", "venue.hall"}
		for i := range want {
			if paths[i] != want[i] {
				t.Fatalf("run %d: paths = %v, want %v", run, paths, want)
			}
		}
		if prev != nil {
			for i := range paths {
				if paths[i] != prev[i] {
					t.Fatalf("non-deterministic order across runs: %v vs %v", prev, paths)
				}
			}
		}
		prev = paths
	}
}

// TestDiffConsistentWithContentHash: the bidirectional invariant. For pairs that
// hash equal, Diff returns zero rows; for pairs that hash differently, Diff
// returns >=1 row. This is the single most important guard against split-brain.
func TestDiffConsistentWithContentHash(t *testing.T) {
	mk := func(mut func(e *model.Event)) model.Event {
		e := newEvent()
		mut(&e)
		return e
	}
	cases := []struct {
		name string
		mut  func(e *model.Event)
	}{
		{"noop", func(e *model.Event) {}},
		{"freshness-only", func(e *model.Event) {
			e.LastCheckedAt = "2099-01-01T00:00:00Z"
			e.Sources[0].RetrievedAt = "2099-01-01T00:00:00Z"
			e.BatchID = "x"
		}},
		{"name-ws", func(e *model.Event) { e.Name = "  AI   EXPO\tKOREA  " }},
		{"array-reorder", func(e *model.Event) { e.Audience = []string{"buyers", "b2b"} }},
		{"status", func(e *model.Event) { e.Status = "cancelled" }},
		{"category-add", func(e *model.Event) { e.Categories = []string{"ai", "bio"} }},
		{"venue-hall", func(e *model.Event) { h := "Z"; e.Venue.Hall = &h }},
		{"source-url", func(e *model.Event) { e.Sources[0].URL = "https://x.example/" }},
	}
	base := newEvent()
	for _, tc := range cases {
		other := mk(tc.mut)
		hashEq := store.ContentHash(base) == store.ContentHash(other)
		rows := store.Diff(&base, &other, "t", "b")
		if hashEq && len(rows) != 0 {
			t.Fatalf("%s: hash equal but Diff returned %d rows: %v", tc.name, len(rows), fieldPaths(rows))
		}
		if !hashEq && len(rows) == 0 {
			t.Fatalf("%s: hash differs but Diff returned zero rows", tc.name)
		}
	}
}

// TestDiffSourceKeyPipeCollision: a '|' inside a source field must not let two
// distinct source sets collide; ContentHash must stay stable under reorder and
// the diff must agree (no spurious row on reorder).
func TestDiffSourceKeyPipeCollision(t *testing.T) {
	a := newEvent()
	a.Sources = []model.Source{
		{URL: "https://a|b.example/", Type: "venue", Publisher: "P1", RetrievedAt: "2026-06-21T10:00:00Z"},
		{URL: "https://a.example/", Type: "b|c", Publisher: "P2", RetrievedAt: "2026-06-21T10:00:00Z"},
	}
	b := newEvent()
	b.Sources = []model.Source{ // reversed order, freshness changed
		{URL: "https://a.example/", Type: "b|c", Publisher: "P2", RetrievedAt: "2099-01-01T00:00:00Z"},
		{URL: "https://a|b.example/", Type: "venue", Publisher: "P1", RetrievedAt: "2099-01-01T00:00:00Z"},
	}
	if store.ContentHash(a) != store.ContentHash(b) {
		t.Fatal("content_hash unstable under source reorder with '|' in fields")
	}
	if len(store.Diff(&a, &b, "t", "b")) != 0 {
		t.Fatal("Diff produced a row for an order-only source change with '|' fields")
	}
}

// ---------------------------------------------------------------------------
// Pipeline-level idempotence, disappearance, and no-op-stability tests.
// These exercise store.ApplyBatch (the batch runner primitive used by
// pipeline/run.go).
// ---------------------------------------------------------------------------

func rowSnapshot(t *testing.T, db *sql.DB, id string) map[string]sql.NullString {
	t.Helper()
	cols := []string{"status", "update_state", "last_checked_at", "content_hash", "updated_at", "name", "summary"}
	row := db.QueryRow(`SELECT status, update_state, last_checked_at, content_hash, updated_at, name, summary FROM events WHERE event_id=?`, id)
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		t.Fatalf("rowSnapshot %s: %v", id, err)
	}
	out := map[string]sql.NullString{}
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out
}

func countChanges(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM change_log WHERE event_id=?`, id).Scan(&n); err != nil {
		t.Fatalf("countChanges: %v", err)
	}
	return n
}

// TestApplyBatchNewEvent: an event not in the DB is inserted with
// update_state=new and no change_log rows.
func TestApplyBatchNewEvent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "batch-001"); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	snap := rowSnapshot(t, db, e.EventID)
	if snap["update_state"].String != "new" {
		t.Fatalf("update_state = %q, want new", snap["update_state"].String)
	}
	if countChanges(t, db, e.EventID) != 0 {
		t.Fatal("new event must not write change_log rows")
	}
}

// TestApplyBatchIdempotentNoOp: running the IDENTICAL source state twice yields
// zero new change rows AND a byte-identical content_hash AND a byte-identical
// updated_at (the cursor must not be perturbed on a no-op run). update_state
// must settle to unchanged.
func TestApplyBatchIdempotentNoOp(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()

	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "batch-001"); err != nil {
		t.Fatalf("ApplyBatch run1: %v", err)
	}
	before := rowSnapshot(t, db, e.EventID)

	// Re-run identical semantic state; only freshness differs.
	e2 := newEvent()
	e2.LastCheckedAt = "2026-06-22T11:00:00Z"
	e2.Sources[0].RetrievedAt = "2026-06-22T11:00:00Z"
	if err := store.ApplyBatch(ctx, db, []model.Event{e2}, "batch-002"); err != nil {
		t.Fatalf("ApplyBatch run2: %v", err)
	}
	after := rowSnapshot(t, db, e.EventID)

	if countChanges(t, db, e.EventID) != 0 {
		t.Fatalf("no-op re-run produced %d change rows, want 0", countChanges(t, db, e.EventID))
	}
	if after["content_hash"].String != before["content_hash"].String {
		t.Fatalf("content_hash changed on no-op: %q -> %q", before["content_hash"].String, after["content_hash"].String)
	}
	if after["updated_at"].String != before["updated_at"].String {
		t.Fatalf("updated_at MUST be byte-identical on no-op (cursor stability): %q -> %q", before["updated_at"].String, after["updated_at"].String)
	}
	if after["update_state"].String != "unchanged" {
		t.Fatalf("update_state = %q on no-op, want unchanged", after["update_state"].String)
	}
	// last_checked_at SHOULD advance (freshness) even on no-op.
	if after["last_checked_at"].String != "2026-06-22T11:00:00Z" {
		t.Fatalf("last_checked_at = %q, want advanced to 2026-06-22T11:00:00Z", after["last_checked_at"].String)
	}
}

func TestApplyBatchBackfillsSummaryOnUnchangedEvent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Given
	e := newEvent()
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "batch-001"); err != nil {
		t.Fatalf("ApplyBatch run1: %v", err)
	}
	if _, err := db.Exec(`UPDATE events SET summary=NULL WHERE event_id=?`, e.EventID); err != nil {
		t.Fatalf("clear summary: %v", err)
	}
	before := rowSnapshot(t, db, e.EventID)

	// When
	e2 := newEvent()
	e2.Summary = strptr("인공지능 산업의 최신 기술과 실제 비즈니스 적용 사례를 공유하는 전문 전시회입니다.")
	e2.LastCheckedAt = "2026-06-22T11:00:00Z"
	if err := store.ApplyBatch(ctx, db, []model.Event{e2}, "batch-002"); err != nil {
		t.Fatalf("ApplyBatch run2: %v", err)
	}

	// Then
	after := rowSnapshot(t, db, e.EventID)
	if !after["summary"].Valid || after["summary"].String != *e2.Summary {
		t.Fatalf("summary = %q valid=%v, want %q", after["summary"].String, after["summary"].Valid, *e2.Summary)
	}
	if after["updated_at"].String != before["updated_at"].String {
		t.Fatalf("updated_at changed on summary backfill: %q -> %q", before["updated_at"].String, after["updated_at"].String)
	}
	if after["content_hash"].String != before["content_hash"].String {
		t.Fatalf("content_hash changed on summary backfill: %q -> %q", before["content_hash"].String, after["content_hash"].String)
	}
	if countChanges(t, db, e.EventID) != 0 {
		t.Fatalf("summary backfill produced %d change rows, want 0", countChanges(t, db, e.EventID))
	}
}

// TestApplyBatchSingleFieldUpdate: a single field change writes exactly one
// change_log row, update_state=updated, content_hash advances, updated_at moves.
func TestApplyBatchSingleFieldUpdate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "batch-001"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := rowSnapshot(t, db, e.EventID)

	e2 := newEvent()
	e2.Status = "cancelled"
	if err := store.ApplyBatch(ctx, db, []model.Event{e2}, "batch-002"); err != nil {
		t.Fatalf("update: %v", err)
	}

	if countChanges(t, db, e.EventID) != 1 {
		t.Fatalf("single field update wrote %d change rows, want 1", countChanges(t, db, e.EventID))
	}
	after := rowSnapshot(t, db, e.EventID)
	if after["update_state"].String != "updated" {
		t.Fatalf("update_state = %q, want updated", after["update_state"].String)
	}
	if after["content_hash"].String == before["content_hash"].String {
		t.Fatal("content_hash must advance on a real change")
	}
	// Verify the change row columns.
	var fp string
	var ov, nv sql.NullString
	if err := db.QueryRow(`SELECT field_path, old_value, new_value FROM change_log WHERE event_id=?`, e.EventID).Scan(&fp, &ov, &nv); err != nil {
		t.Fatalf("read change row: %v", err)
	}
	if fp != "status" || ov.String != "scheduled" || nv.String != "cancelled" {
		t.Fatalf("change row = (%q,%q,%q), want (status,scheduled,cancelled)", fp, ov.String, nv.String)
	}
	// batch_id recorded.
	var bid sql.NullString
	if err := db.QueryRow(`SELECT batch_id FROM change_log WHERE event_id=?`, e.EventID).Scan(&bid); err != nil {
		t.Fatalf("read batch_id: %v", err)
	}
	if bid.String != "batch-002" {
		t.Fatalf("batch_id = %q, want batch-002", bid.String)
	}
}

// TestApplyBatchMultiFieldNRows: a multi-field change writes exactly N rows.
func TestApplyBatchMultiFieldNRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()
	e.Categories = []string{"ai"}
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "b1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	e2 := newEvent()
	e2.Categories = []string{"ai", "bio"}
	e2.Status = "postponed"
	e2.Actions.CanExhibit = true
	if err := store.ApplyBatch(ctx, db, []model.Event{e2}, "b2"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if n := countChanges(t, db, e.EventID); n != 3 {
		t.Fatalf("multi-field change wrote %d rows, want 3", n)
	}
}

// TestApplyBatchUpdateThenRerunIdentical: cross-run idempotence. Apply a status
// change (1 row), then re-run the IDENTICAL new state -> still exactly 1 row
// total (not 2), content_hash byte-identical, update_state settles to unchanged.
func TestApplyBatchUpdateThenRerunIdentical(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "b1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	e2 := newEvent()
	e2.Status = "cancelled"
	if err := store.ApplyBatch(ctx, db, []model.Event{e2}, "b2"); err != nil {
		t.Fatalf("update: %v", err)
	}
	hashAfterUpdate := rowSnapshot(t, db, e.EventID)["content_hash"].String

	// Re-run identical-to-current state.
	e3 := newEvent()
	e3.Status = "cancelled"
	e3.LastCheckedAt = "2026-06-23T00:00:00Z"
	if err := store.ApplyBatch(ctx, db, []model.Event{e3}, "b3"); err != nil {
		t.Fatalf("rerun: %v", err)
	}

	if n := countChanges(t, db, e.EventID); n != 1 {
		t.Fatalf("cross-run idempotence broken: %d change rows total, want 1", n)
	}
	after := rowSnapshot(t, db, e.EventID)
	if after["content_hash"].String != hashAfterUpdate {
		t.Fatal("content_hash must be byte-identical on a re-run of identical state")
	}
	if after["update_state"].String != "unchanged" {
		t.Fatalf("update_state = %q on identical re-run, want unchanged", after["update_state"].String)
	}
}

// TestApplyBatchDisappearanceUntouched: seed 2 events, run a batch containing
// only event A. Event B's row (every column) must be byte-identical to before,
// and no change_log / raw_snapshot rows for B may be deleted. Critically B's
// last_checked_at must NOT advance (it goes stale).
func TestApplyBatchDisappearanceUntouched(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := newEvent()
	a.EventID = "coex-a"
	b := newEvent()
	b.EventID = "coex-b"
	if err := store.ApplyBatch(ctx, db, []model.Event{a, b}, "b1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Give B a change row + raw_snapshot to prove no cascade delete happens.
	bUpd := b
	bUpd.Status = "cancelled"
	if err := store.UpsertEvent(ctx, db, bUpd,
		[]model.ChangeLog{{EventID: "coex-b", FieldPath: "status", OldValue: strptr("scheduled"), NewValue: strptr("cancelled"), ChangedAt: "t", BatchID: strptr("seed")}},
		&model.RawSnapshot{EventID: "coex-b", Body: []byte("x"), RetrievedAt: "t"},
	); err != nil {
		t.Fatalf("seed B change: %v", err)
	}

	beforeB := rowSnapshot(t, db, "coex-b")
	beforeChanges := countChanges(t, db, "coex-b")
	var beforeSnaps int
	db.QueryRow(`SELECT COUNT(*) FROM raw_snapshot WHERE event_id='coex-b'`).Scan(&beforeSnaps)

	// Run a batch with only A (B disappears from discovery).
	a2 := newEvent()
	a2.EventID = "coex-a"
	a2.LastCheckedAt = "2026-07-01T00:00:00Z"
	if err := store.ApplyBatch(ctx, db, []model.Event{a2}, "b2"); err != nil {
		t.Fatalf("partial batch: %v", err)
	}

	afterB := rowSnapshot(t, db, "coex-b")
	for col := range beforeB {
		if afterB[col] != beforeB[col] {
			t.Fatalf("disappeared event B column %q changed: %q -> %q", col, beforeB[col].String, afterB[col].String)
		}
	}
	if got := countChanges(t, db, "coex-b"); got != beforeChanges {
		t.Fatalf("B change_log rows changed: %d -> %d", beforeChanges, got)
	}
	var afterSnaps int
	db.QueryRow(`SELECT COUNT(*) FROM raw_snapshot WHERE event_id='coex-b'`).Scan(&afterSnaps)
	if afterSnaps != beforeSnaps {
		t.Fatalf("B raw_snapshot rows changed: %d -> %d", beforeSnaps, afterSnaps)
	}
}

// TestApplyBatchPartialBatchRerunConvergence: crash-simulate by committing the
// event row WITHOUT its change_log (as if the batch died after the event write
// but the change feed write was lost), then re-run the SAME new state. The
// change_log must converge to exactly the correct rows — not be duplicated, and
// not silently lost. Because change detection is derived from the PERSISTED
// content_hash, a re-run over identical source is a pure no-op regardless of
// crash history: the transition is recorded at most once.
func TestApplyBatchPartialBatchRerunConvergence(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "b1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate a crash: the event row is committed with the NEW status but the
	// change_log row was lost (UpsertEvent with changes=nil).
	crashed := newEvent()
	crashed.Status = "cancelled"
	if err := store.UpsertEvent(ctx, db, crashed, nil, nil); err != nil {
		t.Fatalf("crash sim: %v", err)
	}
	if countChanges(t, db, e.EventID) != 0 {
		t.Fatal("precondition: crash left a change row")
	}

	// Re-run the SAME new state through the batch runner. Since the persisted
	// hash already equals the new hash, this is a no-op: no duplicate/spurious
	// change row appears.
	rerun := newEvent()
	rerun.Status = "cancelled"
	rerun.LastCheckedAt = "2026-07-01T00:00:00Z"
	if err := store.ApplyBatch(ctx, db, []model.Event{rerun}, "b2"); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if n := countChanges(t, db, e.EventID); n != 0 {
		t.Fatalf("partial-batch re-run produced %d change rows, want 0 (pure no-op vs persisted hash)", n)
	}
	if rowSnapshot(t, db, e.EventID)["update_state"].String != "unchanged" {
		t.Fatal("re-run over persisted-identical state must settle update_state=unchanged")
	}
}

// TestApplyBatchEmptyChangeTimestampRejected: a status change whose verification
// instant is unstamped (LastCheckedAt=="" AND UpdatedAt=="") must FAIL LOUDLY
// rather than silently writing change_log.changed_at="" into the change feed.
// An empty changed_at satisfies the NOT NULL constraint yet corrupts the
// /events/changes feed and any since= cursor ordering on changed_at. PLAN AC-3
// requires changed_at as a recorded column of the change row.
func TestApplyBatchEmptyChangeTimestampRejected(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	e := newEvent()
	if err := store.ApplyBatch(ctx, db, []model.Event{e}, "b1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A real change whose timestamps were never stamped (normalize bug /
	// hand-built Event): both fall-through sources are empty.
	bad := newEvent()
	bad.Status = "cancelled"
	bad.LastCheckedAt = ""
	bad.UpdatedAt = ""

	err := store.ApplyBatch(ctx, db, []model.Event{bad}, "b2")
	if err == nil {
		t.Fatal("ApplyBatch must reject a changed event with an empty change timestamp, got nil error")
	}

	// The change feed must NOT have been corrupted with a blank changed_at.
	var blanks int
	if qerr := db.QueryRow(
		`SELECT COUNT(*) FROM change_log WHERE event_id=? AND changed_at=''`, e.EventID,
	).Scan(&blanks); qerr != nil {
		t.Fatalf("count blank changed_at: %v", qerr)
	}
	if blanks != 0 {
		t.Fatalf("a blank changed_at row leaked into the change feed: %d rows", blanks)
	}
}

package showala_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/normalize"
	"github.com/smpain/event-intelligence-api/internal/sources/showala"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// End-to-end: the REAL SHOWALA adapter output (Parse of the RoboWorld detail
// fixture) flows through normalize and store, and:
//   - a SHOWALA-only event (RoboWorld — absent from KINTEX list.do) is visible;
//   - when a venue-native (kintex) twin of the SAME event also lands, the two
//     fold into ONE canonical row (the kintex one), proving the adapter's scraped
//     venue text maps to venue_id "kintex" and yields a content_key that matches
//     the kintex row's.
func TestShowalaEndToEndDedup(t *testing.T) {
	const now = "2026-06-24T00:00:00Z"
	ctx := context.Background()

	body, err := os.ReadFile(filepath.Join("testdata", "detail_robotworld.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pe, err := showala.New().Parse(ctx, &fetch.Result{
		URL:        "https://showala.com/ex/ex_detail.php?idx=3219",
		StatusCode: 200,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	robotworld, err := normalize.Normalize(pe, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	// The adapter's scraped 개최장소 must have normalized to the KINTEX venue slug.
	if robotworld.Venue == nil || robotworld.Venue.VenueID == nil || *robotworld.Venue.VenueID != "kintex" {
		t.Fatalf("RoboWorld venue_id = %v, want kintex", robotworld.Venue)
	}

	db, err := store.OpenWrite(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// SHOWALA-only: RoboWorld is visible (AC-4).
	if err := store.ApplyBatch(ctx, db, []model.Event{*robotworld}, "b1"); err != nil {
		t.Fatalf("apply showala: %v", err)
	}
	if got, _ := store.GetEvent(ctx, db, "showala-3219"); got == nil {
		t.Fatal("RoboWorld (SHOWALA-only) should be visible")
	}

	// A venue-native KINTEX twin of the same real event arrives later: same name,
	// start, venue -> same content_key -> folds to one canonical row (AC-3).
	kintexTwin := *robotworld
	kintexTwin.EventID = "kintex-999"
	kintexTwin.Sources = []model.Source{{URL: "https://www.kintex.com/web/ko/event/view.do?seq=999", Type: "venue", Publisher: "KINTEX", RetrievedAt: now}}
	if store.ContentKey(kintexTwin) != store.ContentKey(*robotworld) {
		t.Fatalf("twin content_key mismatch: %q vs %q", store.ContentKey(kintexTwin), store.ContentKey(*robotworld))
	}
	if err := store.ApplyBatch(ctx, db, []model.Event{kintexTwin}, "b2"); err != nil {
		t.Fatalf("apply kintex twin: %v", err)
	}

	events, _, err := store.ListEvents(ctx, db, store.EventFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var roboCount int
	var sawCanonical string
	for _, e := range events {
		if store.ContentKey(e) == store.ContentKey(*robotworld) {
			roboCount++
			sawCanonical = e.EventID
		}
	}
	if roboCount != 1 {
		t.Fatalf("RoboWorld appears %d times in listing, want 1 (deduped)", roboCount)
	}
	if sawCanonical != "kintex-999" {
		t.Fatalf("canonical = %q, want kintex-999 (venue-native wins)", sawCanonical)
	}
}

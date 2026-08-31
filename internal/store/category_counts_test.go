package store_test

import (
	"context"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// TestCategoryCounts proves the category facet: it counts non-excluded events
// per taxonomy slug, honours the SAME list/venue/date predicates as ListEvents,
// counts an event once per category it carries, and IGNORES the category filter
// itself (so a client filtered to one category still sees every category's size).
func TestCategoryCounts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	seed := func(id, venueID string, cats []string, start string, excluded bool) {
		e := newEvent()
		e.EventID = id
		e.Venue = &model.Venue{VenueID: strptr(venueID), Name: "V", City: "Seoul"}
		e.Categories = cats
		e.StartDate = strptr(start)
		e.Excluded = excluded
		if err := store.UpsertEvent(ctx, db, e, nil, nil); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// id, venue, categories, start_date, excluded
	seed("coex-a", "coex", []string{"ai"}, "2026-07-01", false)
	seed("coex-b", "coex", []string{"ai", "bio"}, "2026-07-15", false) // counts in ai AND bio
	seed("kintex-c", "kintex", []string{"humanoid-robotics"}, "2026-08-01", false)
	seed("coex-d", "coex", []string{"ai"}, "2026-05-01", false) // past
	seed("coex-x", "coex", []string{}, "2026-07-20", true)      // excluded → uncounted
	seed("benchmark-e", "websummit", []string{"medical-devices"}, "2026-09-01", false)
	if _, err := db.Exec(`UPDATE events SET country = 'FR' WHERE event_id = 'benchmark-e'`); err != nil {
		t.Fatalf("mark benchmark-e overseas: %v", err)
	}

	got := func(t *testing.T, f store.EventFilter) map[string]int {
		t.Helper()
		m, err := store.CategoryCounts(ctx, db, f)
		if err != nil {
			t.Fatalf("CategoryCounts: %v", err)
		}
		return m
	}
	want := func(t *testing.T, m map[string]int, slug string, n int) {
		t.Helper()
		if m[slug] != n {
			t.Fatalf("count[%s] = %d, want %d (full map: %v)", slug, m[slug], n, m)
		}
	}

	t.Run("all lists, no filter", func(t *testing.T) {
		m := got(t, store.EventFilter{})
		want(t, m, "ai", 3)                // coex-a, coex-b, coex-d
		want(t, m, "bio", 1)               // coex-b
		want(t, m, "humanoid-robotics", 1) // kintex-c
		want(t, m, "medical-devices", 1)   // benchmark-e
		want(t, m, "digital-health", 0)
	})

	t.Run("excluded event never counted", func(t *testing.T) {
		// coex-x is excluded with empty categories; it must not appear anywhere.
		m := got(t, store.EventFilter{})
		total := 0
		for _, v := range m {
			total += v
		}
		if total != 6 { // ai3 + bio1 + robotics1 + med1
			t.Fatalf("total category memberships = %d, want 6 (full map: %v)", total, m)
		}
	})

	t.Run("venue list contains Korean hosts", func(t *testing.T) {
		m := got(t, store.EventFilter{ListKind: "venue"})
		want(t, m, "ai", 3)
		want(t, m, "medical-devices", 0) // benchmark-e dropped
	})

	t.Run("benchmark list contains overseas hosts", func(t *testing.T) {
		m := got(t, store.EventFilter{ListKind: "benchmark"})
		want(t, m, "medical-devices", 1)
		want(t, m, "ai", 0)
	})

	t.Run("min start date drops past", func(t *testing.T) {
		m := got(t, store.EventFilter{ListKind: "venue", MinStartDate: "2026-06-23"})
		want(t, m, "ai", 2) // coex-d (2026-05-01) dropped
		want(t, m, "bio", 1)
	})

	t.Run("date range narrows both bounds", func(t *testing.T) {
		m := got(t, store.EventFilter{MinStartDate: "2026-07-01", MaxStartDate: "2026-07-31"})
		want(t, m, "ai", 2)                // coex-a (07-01), coex-b (07-15)
		want(t, m, "bio", 1)               // coex-b
		want(t, m, "humanoid-robotics", 0) // kintex-c is 08-01
	})

	t.Run("explicit venue filter", func(t *testing.T) {
		m := got(t, store.EventFilter{Venue: "kintex"})
		want(t, m, "humanoid-robotics", 1)
		want(t, m, "ai", 0)
	})

	t.Run("category filter is ignored by the facet", func(t *testing.T) {
		// Passing a Category must NOT narrow the counts — the facet always reports
		// every category so the UI can offer a switch.
		withCat := got(t, store.EventFilter{Category: "bio"})
		withoutCat := got(t, store.EventFilter{})
		if withCat["ai"] != withoutCat["ai"] || withCat["ai"] != 3 {
			t.Fatalf("category filter leaked into facet: ai with=%d without=%d, want 3/3", withCat["ai"], withoutCat["ai"])
		}
	})
}

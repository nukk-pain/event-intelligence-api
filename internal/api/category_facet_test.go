package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// facetEnvelope mirrors the events-list wire shape including the optional
// category_counts facet so these tests can assert it independently of the
// concrete handler types.
type facetEnvelope struct {
	Data           []model.Event `json:"data"`
	CategoryCounts []struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	} `json:"category_counts"`
}

func withStart(e model.Event, start string) model.Event {
	e.StartDate = strptr(start)
	return e
}

func withRange(e model.Event, start, end string) model.Event {
	e.StartDate = strptr(start)
	e.EndDate = strptr(end)
	return e
}

func getFacet(t *testing.T, url string) facetEnvelope {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	var env facetEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return env
}

func countOf(env facetEnvelope, slug string) int {
	for _, c := range env.CategoryCounts {
		if c.Category == slug {
			return c.Count
		}
	}
	return -1
}

func idSet(events []model.Event) map[string]bool {
	m := map[string]bool{}
	for _, e := range events {
		m[e.EventID] = true
	}
	return m
}

// TestListEvents_CategoryCounts asserts the events list carries a category facet
// in canonical taxonomy order, with every category present (including zeros) and
// excluded events never counted.
func TestListEvents_CategoryCounts(t *testing.T) {
	events := []model.Event{
		withStart(seedEvent("coex-ai-1", "coex", "ai", false), "2026-07-01"),
		withStart(seedEvent("coex-ai-2", "coex", "ai", false), "2026-07-10"),
		withStart(seedEvent("coex-bio", "coex", "bio", false), "2026-07-05"),
		withStart(seedEvent("coex-x", "coex", "ai", true), "2026-07-06"), // excluded → uncounted
	}
	srv := newServer(t, events)
	env := getFacet(t, srv.URL+"/api/v1/events?limit=100")

	wantOrder := []string{"ai", "humanoid-robotics", "bio", "medical-devices", "digital-health"}
	if len(env.CategoryCounts) != len(wantOrder) {
		t.Fatalf("category_counts len = %d, want %d", len(env.CategoryCounts), len(wantOrder))
	}
	for i, slug := range wantOrder {
		if env.CategoryCounts[i].Category != slug {
			t.Fatalf("category_counts[%d] = %s, want %s (canonical order)", i, env.CategoryCounts[i].Category, slug)
		}
	}
	if got := countOf(env, "ai"); got != 2 {
		t.Errorf("ai count = %d, want 2 (excluded coex-x must not count)", got)
	}
	if got := countOf(env, "bio"); got != 1 {
		t.Errorf("bio count = %d, want 1", got)
	}
	if got := countOf(env, "digital-health"); got != 0 {
		t.Errorf("digital-health count = %d, want 0 (present with zero)", got)
	}
}

// TestListEvents_BeforeFilter asserts the `before` query param bounds start_date
// from above, and that the category facet reflects the active date window.
func TestListEvents_BeforeFilter(t *testing.T) {
	events := []model.Event{
		withStart(seedEvent("ev-jun", "coex", "ai", false), "2026-06-15"),
		withStart(seedEvent("ev-jul", "coex", "ai", false), "2026-07-15"),
		withStart(seedEvent("ev-aug", "coex", "ai", false), "2026-08-15"),
	}
	srv := newServer(t, events)

	// before keeps start_date <= cutoff.
	env := getFacet(t, srv.URL+"/api/v1/events?limit=100&before=2026-07-31")
	got := idSet(env.Data)
	if got["ev-aug"] {
		t.Errorf("before=2026-07-31 returned ev-aug (start 2026-08-15)")
	}
	if !got["ev-jun"] || !got["ev-jul"] {
		t.Errorf("before=2026-07-31 dropped in-range events; got %v", got)
	}
	if c := countOf(env, "ai"); c != 2 {
		t.Errorf("facet ai under before filter = %d, want 2", c)
	}

	// since + before together form a closed range; only ev-jul falls inside.
	env2 := getFacet(t, srv.URL+"/api/v1/events?limit=100&since=2026-07-01&before=2026-07-31")
	if len(env2.Data) != 1 || env2.Data[0].EventID != "ev-jul" {
		t.Fatalf("since+before range returned %d events, want only ev-jul", len(env2.Data))
	}
	if c := countOf(env2, "ai"); c != 1 {
		t.Errorf("facet ai under since+before = %d, want 1", c)
	}
}

func TestListEvents_ActiveRangeOverlapAndUndated(t *testing.T) {
	undated := seedEvent("ev-tba", "coex", "ai", false)
	undated.StartDate = nil
	events := []model.Event{
		withRange(seedEvent("ev-cross", "coex", "ai", false), "2026-08-30", "2026-09-02"),
		withStart(seedEvent("ev-single", "coex", "bio", false), "2026-09-15"),
		withRange(seedEvent("ev-old", "coex", "ai", false), "2026-08-01", "2026-08-02"),
		withStart(seedEvent("ev-future", "coex", "ai", false), "2026-10-01"),
		undated,
	}
	srv := newServer(t, events)
	base := srv.URL + "/api/v1/events?limit=100&active_from=2026-09-01&active_before=2026-09-30"
	env := getFacet(t, base)
	got := idSet(env.Data)
	if !got["ev-cross"] || !got["ev-single"] || got["ev-old"] || got["ev-future"] || got["ev-tba"] {
		t.Fatalf("overlap filter returned unexpected events: %v", got)
	}
	if countOf(env, "ai") != 1 || countOf(env, "bio") != 1 {
		t.Fatalf("facet did not share overlap filter: ai=%d bio=%d", countOf(env, "ai"), countOf(env, "bio"))
	}
	withTBA := getFacet(t, base+"&include_undated=1")
	if !idSet(withTBA.Data)["ev-tba"] {
		t.Fatalf("include_undated=1 did not include undated event")
	}
}

// TestChangesFeed_NoCategoryCounts guards the omitempty contract: the shared
// Envelope must not leak a category_counts key onto the changes feed.
func TestChangesFeed_NoCategoryCounts(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-1", "coex", "ai", false)})
	resp, err := http.Get(srv.URL + "/api/v1/events/changes?limit=10")
	if err != nil {
		t.Fatalf("GET changes: %v", err)
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode changes: %v", err)
	}
	if _, present := raw["category_counts"]; present {
		t.Errorf("changes feed leaked category_counts; omitempty contract broken")
	}
}

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// detail_test.go covers Task 3.2: the single-event detail endpoint, its sources
// sub-resource, and the change feed. It reuses the seedEvent / newServer /
// strptr helpers defined in events_test.go (same package: api_test).

// detailResp mirrors the detail wire shape: a single event under "data".
type detailResp struct {
	Data model.Event `json:"data"`
}

// sourcesResp mirrors GET /events/{id}/sources: {"data":[Source...]}.
type sourcesResp struct {
	Data []model.Source `json:"data"`
}

// changeItem mirrors one change-feed entry shape from the PLAN:
// {event_id, field, old, new, update_state, changed_at}.
type changeItem struct {
	EventID     string  `json:"event_id"`
	Field       string  `json:"field"`
	Old         *string `json:"old"`
	New         *string `json:"new"`
	UpdateState string  `json:"update_state"`
	ChangedAt   string  `json:"changed_at"`
}

// changesResp mirrors the change feed: the shared cursor envelope with a typed
// Data slice.
type changesResp struct {
	Data []changeItem `json:"data"`
	Page struct {
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
		Limit      int     `json:"limit"`
	} `json:"page"`
}

func getJSON(t *testing.T, url string, v any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return resp
}

// seedServerWithChanges builds a temp DB with the supplied events and per-event
// change_log rows, then returns an httptest server fronting the read API. It is
// the change-feed analogue of newServer (which seeds events only).
func seedServerWithChanges(t *testing.T, events []model.Event, changes map[string][]model.ChangeLog) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	wdb, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if err := store.Migrate(wdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ctx := context.Background()
	for _, e := range events {
		if err := store.UpsertEvent(ctx, wdb, e, changes[e.EventID], nil); err != nil {
			t.Fatalf("UpsertEvent %s: %v", e.EventID, err)
		}
	}
	if err := wdb.Close(); err != nil {
		t.Fatalf("close write db: %v", err)
	}

	rdb, err := store.OpenRead(path)
	if err != nil {
		t.Fatalf("OpenRead: %v", err)
	}
	srv := httptest.NewServer(api.Routes(rdb))
	t.Cleanup(func() {
		srv.Close()
		rdb.Close()
	})
	return srv
}

// TestGetEvent_OK returns the event JSON with its provenance (sources) included.
func TestGetEvent_OK(t *testing.T) {
	event := seedEvent("ev-001", "coex", "ai", false)
	event.Summary = strptr("Event ev-001 — 2026-06-21 @ Venue")
	srv := newServer(t, []model.Event{event})

	var got detailResp
	resp := getJSON(t, srv.URL+"/api/v1/events/ev-001", &got)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got.Data.EventID != "ev-001" {
		t.Fatalf("event_id = %q, want ev-001", got.Data.EventID)
	}
	// Detail MUST carry provenance.
	if len(got.Data.Sources) < 1 {
		t.Fatalf("detail returned %d sources, want >= 1", len(got.Data.Sources))
	}
	if got.Data.Summary == nil || *got.Data.Summary != *event.Summary {
		t.Fatalf("summary = %v, want %q", got.Data.Summary, *event.Summary)
	}
}

// TestGetEvent_NotFound returns a 404 with the error envelope.
func TestGetEvent_NotFound(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	resp, err := http.Get(srv.URL + "/api/v1/events/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want not_found", body.Error.Code)
	}
}

// TestGetEventSources_OK returns >=1 source for an existing event.
func TestGetEventSources_OK(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	var got sourcesResp
	resp := getJSON(t, srv.URL+"/api/v1/events/ev-001/sources", &got)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(got.Data) < 1 {
		t.Fatalf("sources len = %d, want >= 1", len(got.Data))
	}
	s := got.Data[0]
	if s.URL == "" || s.Type == "" || s.Publisher == "" || s.RetrievedAt == "" {
		t.Errorf("source missing required field: %+v", s)
	}
}

// TestGetEventSources_NotFound returns 404 when the event is absent.
func TestGetEventSources_NotFound(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	resp, err := http.Get(srv.URL + "/api/v1/events/nope/sources")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want not_found", body.Error.Code)
	}
}

// TestChanges_FeedShape asserts the change feed returns the documented item
// shape {event_id, field, old, new, update_state, changed_at} under the shared
// cursor envelope, and that the static /events/changes route is NOT shadowed by
// the /events/{event_id} wildcard.
func TestChanges_FeedShape(t *testing.T) {
	ev := seedEvent("ev-001", "coex", "ai", false)
	changes := map[string][]model.ChangeLog{
		"ev-001": {{
			EventID:   "ev-001",
			FieldPath: "status",
			OldValue:  strptr("tentative"),
			NewValue:  strptr("scheduled"),
			ChangedAt: "2026-06-20T00:00:00Z",
			BatchID:   strptr("b1"),
		}},
	}
	srv := seedServerWithChanges(t, []model.Event{ev}, changes)

	var got changesResp
	resp := getJSON(t, srv.URL+"/api/v1/events/changes", &got)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (route shadowed by {event_id}?)", resp.StatusCode)
	}
	if len(got.Data) != 1 {
		t.Fatalf("changes len = %d, want 1", len(got.Data))
	}
	c := got.Data[0]
	if c.EventID != "ev-001" {
		t.Errorf("event_id = %q, want ev-001", c.EventID)
	}
	if c.Field != "status" {
		t.Errorf("field = %q, want status", c.Field)
	}
	if c.Old == nil || *c.Old != "tentative" {
		t.Errorf("old = %v, want tentative", c.Old)
	}
	if c.New == nil || *c.New != "scheduled" {
		t.Errorf("new = %v, want scheduled", c.New)
	}
	if c.ChangedAt != "2026-06-20T00:00:00Z" {
		t.Errorf("changed_at = %q, want 2026-06-20T00:00:00Z", c.ChangedAt)
	}
	// update_state is the event's current state (new event => "new").
	if c.UpdateState == "" {
		t.Errorf("update_state is empty, want the event's update_state")
	}
}

// TestChanges_SinceFilter asserts ?since drops rows older than the cutoff.
func TestChanges_SinceFilter(t *testing.T) {
	ev := seedEvent("ev-001", "coex", "ai", false)
	changes := map[string][]model.ChangeLog{
		"ev-001": {
			{EventID: "ev-001", FieldPath: "status", OldValue: strptr("a"), NewValue: strptr("b"), ChangedAt: "2026-06-10T00:00:00Z", BatchID: strptr("b1")},
			{EventID: "ev-001", FieldPath: "name", OldValue: strptr("c"), NewValue: strptr("d"), ChangedAt: "2026-06-20T00:00:00Z", BatchID: strptr("b1")},
		},
	}
	srv := seedServerWithChanges(t, []model.Event{ev}, changes)

	var all changesResp
	getJSON(t, srv.URL+"/api/v1/events/changes", &all)
	if len(all.Data) != 2 {
		t.Fatalf("unfiltered changes = %d, want 2", len(all.Data))
	}

	var filtered changesResp
	getJSON(t, srv.URL+"/api/v1/events/changes?since=2026-06-15T00:00:00Z", &filtered)
	if len(filtered.Data) != 1 {
		t.Fatalf("since-filtered changes = %d, want 1", len(filtered.Data))
	}
	if filtered.Data[0].Field != "name" {
		t.Errorf("filtered field = %q, want name", filtered.Data[0].Field)
	}
}

// TestChanges_Pagination walks the change feed by cursor and asserts every row
// is returned exactly once with no gaps or duplicates, reusing the same cursor
// envelope contract as the events list.
func TestChanges_Pagination(t *testing.T) {
	ev := seedEvent("ev-001", "coex", "ai", false)
	const total = 25
	var rows []model.ChangeLog
	for i := 0; i < total; i++ {
		// distinct, monotonically increasing changed_at to exercise ordering
		ts := "2026-06-20T00:00:00Z"
		rows = append(rows, model.ChangeLog{
			EventID:   "ev-001",
			FieldPath: "f",
			OldValue:  strptr("o"),
			NewValue:  strptr("n"),
			ChangedAt: ts,
			BatchID:   strptr("b1"),
		})
	}
	srv := seedServerWithChanges(t, []model.Event{ev}, map[string][]model.ChangeLog{"ev-001": rows})

	seen := 0
	cursor := ""
	pages := 0
	for {
		url := srv.URL + "/api/v1/events/changes?limit=10"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		var page changesResp
		resp := getJSON(t, url, &page)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if len(page.Data) > 10 {
			t.Fatalf("page returned %d rows, want <= 10", len(page.Data))
		}
		seen += len(page.Data)
		pages++
		if pages > total+5 {
			t.Fatalf("too many pages (%d): cursor not terminating", pages)
		}
		link := resp.Header.Get("Link")
		if !page.Page.HasMore {
			if page.Page.NextCursor != nil {
				t.Fatalf("has_more=false but next_cursor non-nil")
			}
			if link != "" {
				t.Fatalf("last page emitted a Link header: %q", link)
			}
			break
		}
		if page.Page.NextCursor == nil {
			t.Fatalf("has_more=true but next_cursor nil")
		}
		// Task 3.4 parity: a paginated (next != 0) change-feed page MUST carry the
		// rel="next" Link header, mirroring the events list (TestRender_LinkHeaderParity).
		if link == "" {
			t.Fatalf("multi-page change feed missing Link next header")
		}
		if !strings.Contains(link, `rel="next"`) || !strings.Contains(link, "cursor=") {
			t.Fatalf("change feed Link header malformed: %q", link)
		}
		if want := "cursor=" + *page.Page.NextCursor; !strings.Contains(link, want) {
			t.Fatalf("Link header cursor mismatch: %q does not contain %q", link, want)
		}
		cursor = *page.Page.NextCursor
	}
	if seen != total {
		t.Fatalf("walked %d change rows, want %d", seen, total)
	}
}

// TestChanges_PaginationKeysetTotalOrder is a regression test for the keyset
// drop bug: the feed must order on the SAME key as its cursor predicate (id), so
// that interleaving changed_at against id order never silently skips a row.
//
// It seeds a 'late' change FIRST (small id, changed_at far in the future) then an
// 'early' change (large id, changed_at in the past) — the exact shape that arises
// from re-ingest/backfill with preserved timestamps. Walking the feed at limit=1
// must return BOTH rows. The old ORDER BY changed_at ASC, id ASC with an id>cursor
// predicate returned only the 'early' row (the 'late' small-id row was excluded by
// id > cursor once the cursor advanced past the large id).
func TestChanges_PaginationKeysetTotalOrder(t *testing.T) {
	ev := seedEvent("ev-001", "coex", "ai", false)
	changes := map[string][]model.ChangeLog{
		"ev-001": {
			// Inserted first => smallest id, but the LATEST changed_at.
			{EventID: "ev-001", FieldPath: "late", OldValue: strptr("o"), NewValue: strptr("n"), ChangedAt: "2026-06-20T00:00:00Z", BatchID: strptr("b1")},
			// Inserted second => larger id, but an EARLIER changed_at.
			{EventID: "ev-001", FieldPath: "early", OldValue: strptr("o"), NewValue: strptr("n"), ChangedAt: "2026-06-10T00:00:00Z", BatchID: strptr("b1")},
		},
	}
	srv := seedServerWithChanges(t, []model.Event{ev}, changes)

	seenFields := map[string]int{}
	cursor := ""
	pages := 0
	for {
		url := srv.URL + "/api/v1/events/changes?limit=1"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		var page changesResp
		resp := getJSON(t, url, &page)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		for _, c := range page.Data {
			seenFields[c.Field]++
		}
		pages++
		if pages > 10 {
			t.Fatalf("cursor not terminating after %d pages", pages)
		}
		if !page.Page.HasMore {
			break
		}
		if page.Page.NextCursor == nil {
			t.Fatalf("has_more=true but next_cursor nil")
		}
		cursor = *page.Page.NextCursor
	}

	// BOTH rows must be walked exactly once — no gap, no duplicate.
	if seenFields["late"] != 1 {
		t.Errorf("'late' change walked %d times, want 1 (keyset drop?)", seenFields["late"])
	}
	if seenFields["early"] != 1 {
		t.Errorf("'early' change walked %d times, want 1", seenFields["early"])
	}
}

// TestGetEvent_MarkdownNegotiation asserts the detail route honours ?format=md /
// Accept: text/markdown (the OpenAPI text/markdown advertisement for this path),
// rendering the pinned event field set, while remaining JSON by default.
func TestGetEvent_MarkdownNegotiation(t *testing.T) {
	ev := seedEvent("ev-001", "coex", "ai", false)
	ev.Name = "AI Summit"
	ev.StartDate = strptr("2026-09-01")
	ev.EndDate = strptr("2026-09-03")
	ev.Status = "scheduled"
	srv := newServer(t, []model.Event{ev})

	// Default is JSON.
	resp := getJSON(t, srv.URL+"/api/v1/events/ev-001", nil)
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("default detail Content-Type = %q, want application/json", ct)
	}

	// ?format=md => markdown.
	mdQuery := getMarkdown(t, srv.URL+"/api/v1/events/ev-001?format=md")

	// Accept: text/markdown => markdown.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events/ev-001", nil)
	req.Header.Set("Accept", "text/markdown")
	mresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET md: %v", err)
	}
	defer mresp.Body.Close()
	if ct := mresp.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Fatalf("Accept md detail Content-Type = %q, want text/markdown", ct)
	}

	// The markdown must carry the pinned field set's values and column names.
	for _, want := range []string{"ev-001", "AI Summit", "2026-09-01", "2026-09-03", "scheduled"} {
		if !strings.Contains(mdQuery, want) {
			t.Errorf("detail md missing value %q\nmd:\n%s", want, mdQuery)
		}
	}
	for _, col := range []string{"event_id", "name", "start_date", "end_date", "venue_id", "categories", "status"} {
		if !strings.Contains(mdQuery, col) {
			t.Errorf("detail md missing field %q", col)
		}
	}
}

// TestChanges_InvalidCursor returns a bad_request error envelope.
func TestChanges_InvalidCursor(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	resp, err := http.Get(srv.URL + "/api/v1/events/changes?cursor=!!!not-valid!!!")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Error.Code != "invalid_cursor" {
		t.Errorf("error.code = %q, want invalid_cursor", body.Error.Code)
	}
}

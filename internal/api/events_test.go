package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

func strptr(s string) *string { return &s }

// envelopeResp mirrors the api.Envelope wire shape with a typed Data so tests
// can read event ids without depending on the concrete handler types.
type envelopeResp struct {
	Data []model.Event `json:"data"`
	Page struct {
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
		Limit      int     `json:"limit"`
	} `json:"page"`
}

// seedEvent builds a minimal valid event with overridable identity/classification.
func seedEvent(id, venueID, category string, excluded bool) model.Event {
	return model.Event{
		SchemaVersion:  model.SchemaVersion,
		EventID:        id,
		Name:           "Event " + id,
		StartDate:      strptr("2999-01-01"),
		DateConfidence: "high",
		Status:         "scheduled",
		Format:         "onsite",
		Venue:          &model.Venue{VenueID: strptr(venueID), Name: "Venue", City: "Seoul"},
		Country:        "KR",
		Categories:     []string{category},
		Audience:       []string{"b2b"},
		Actions:        model.Actions{},
		CostHint:       "unknown",
		Sources: []model.Source{
			{URL: "https://www.coex.co.kr/", Type: "venue", Publisher: "COEX", RetrievedAt: "2026-06-21T10:00:00Z"},
		},
		LastCheckedAt: "2026-06-21T10:00:00Z",
		UpdateState:   "new",
		Confidence:    "high",
		MissingFields: []string{},
		CreatedAt:     "2026-06-21T10:00:00Z",
		UpdatedAt:     "2026-06-21T10:00:00Z",
		Excluded:      excluded,
	}
}

// newServer seeds a temp SQLite DB with the given events and returns an
// httptest server fronting the read-only API plus a teardown.
func newServer(t *testing.T, events []model.Event) *httptest.Server {
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
		if err := store.UpsertEvent(ctx, wdb, e, nil, nil); err != nil {
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

func getEnvelope(t *testing.T, url string) (envelopeResp, *http.Response) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var env envelopeResp
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return env, resp
}

func TestListEvents_VenueDefaultsToUpcomingKST(t *testing.T) {
	past := withStart(seedEvent("ev-past", "coex", "ai", false), "2000-01-01")
	future := withStart(seedEvent("ev-future", "coex", "ai", false), "2999-01-01")
	srv := newServer(t, []model.Event{past, future})

	env, resp := getEnvelope(t, srv.URL+"/api/v1/events?list=venue&limit=100")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := eventIDs(env.Data); len(got) != 1 || got[0] != "ev-future" {
		t.Fatalf("default venue list returned %+v, want only upcoming ev-future", got)
	}

	env, _ = getEnvelope(t, srv.URL+"/api/v1/events?list=venue&since=1999-01-01&limit=100")
	if got := idSet(env.Data); !got["ev-past"] || !got["ev-future"] {
		t.Fatalf("explicit since should override default upcoming floor; got %v", got)
	}
}

// TestListEvents_FullIterationNoGapsNoDupes seeds more events than the page
// limit and walks every cursor page, asserting each event is returned exactly
// once with no gaps or duplicates (AC-4 core).
func TestListEvents_FullIterationNoGapsNoDupes(t *testing.T) {
	const total = 25
	var events []model.Event
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("ev-%03d", i)
		events = append(events, seedEvent(id, "coex", "ai", false))
		want[id] = true
	}
	srv := newServer(t, events)

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		url := srv.URL + "/api/v1/events?limit=10"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		env, resp := getEnvelope(t, url)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if len(env.Data) > 10 {
			t.Fatalf("page returned %d events, want <= limit 10", len(env.Data))
		}
		for _, e := range env.Data {
			seen[e.EventID]++
		}
		pages++
		if pages > total+5 {
			t.Fatalf("too many pages (%d): cursor not terminating", pages)
		}
		if !env.Page.HasMore {
			if env.Page.NextCursor != nil {
				t.Fatalf("has_more=false but next_cursor non-nil")
			}
			break
		}
		if env.Page.NextCursor == nil {
			t.Fatalf("has_more=true but next_cursor nil")
		}
		cursor = *env.Page.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("saw %d distinct events, want %d", len(seen), total)
	}
	for id := range want {
		if seen[id] != 1 {
			t.Errorf("event %s seen %d times, want exactly 1", id, seen[id])
		}
	}
}

// TestListEvents_LimitClampAndEcho asserts limit=101 is clamped to <=100 and the
// effective limit is echoed in page.limit.
func TestListEvents_LimitClampAndEcho(t *testing.T) {
	var events []model.Event
	for i := 0; i < 5; i++ {
		events = append(events, seedEvent(fmt.Sprintf("ev-%03d", i), "coex", "ai", false))
	}
	srv := newServer(t, events)

	env, resp := getEnvelope(t, srv.URL+"/api/v1/events?limit=101")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if env.Page.Limit > 100 {
		t.Fatalf("page.limit = %d, want <= 100", env.Page.Limit)
	}
	if env.Page.Limit != 100 {
		t.Errorf("page.limit = %d, want clamped 100", env.Page.Limit)
	}
}

// TestListEvents_DefaultLimitEcho asserts the default limit is echoed when no
// ?limit is supplied.
func TestListEvents_DefaultLimitEcho(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})
	env, _ := getEnvelope(t, srv.URL+"/api/v1/events")
	if env.Page.Limit != 20 {
		t.Errorf("default page.limit = %d, want 20", env.Page.Limit)
	}
}

func TestListEvents_DefaultIncludesExcluded(t *testing.T) {
	events := []model.Event{
		seedEvent("ev-keep", "coex", "ai", false),
		seedEvent("ev-excluded", "coex", "ai", true),
	}
	srv := newServer(t, events)

	env, _ := getEnvelope(t, srv.URL+"/api/v1/events?limit=100")
	got := map[string]bool{}
	for _, event := range env.Data {
		got[event.EventID] = true
	}
	for _, want := range []string{"ev-keep", "ev-excluded"} {
		if !got[want] {
			t.Fatalf("default list missing %s; got %v", want, got)
		}
	}
}

// TestListEvents_CategoryFilter narrows by taxonomy category.
func TestListEvents_CategoryFilter(t *testing.T) {
	events := []model.Event{
		seedEvent("ev-ai", "coex", "ai", false),
		seedEvent("ev-bio", "coex", "bio", false),
	}
	srv := newServer(t, events)

	env, _ := getEnvelope(t, srv.URL+"/api/v1/events?category=bio&limit=100")
	if len(env.Data) != 1 || env.Data[0].EventID != "ev-bio" {
		t.Fatalf("category=bio returned %d events, want only ev-bio", len(env.Data))
	}
}

// TestListEvents_VenueFilter narrows by venue_id.
func TestListEvents_VenueFilter(t *testing.T) {
	events := []model.Event{
		seedEvent("ev-coex", "coex", "ai", false),
		seedEvent("ev-kintex", "kintex", "ai", false),
	}
	srv := newServer(t, events)

	env, _ := getEnvelope(t, srv.URL+"/api/v1/events?venue=kintex&limit=100")
	if len(env.Data) != 1 || env.Data[0].EventID != "ev-kintex" {
		t.Fatalf("venue=kintex returned %d events, want only ev-kintex", len(env.Data))
	}
}

// TestListEvents_UpdatedSinceFilter asserts updated_since is applied as a >=
// boundary: a cutoff after all rows hides everything, a cutoff before all rows
// keeps everything, and the rows actually returned satisfy updated_at >= cutoff.
//
// Note: UpsertEvent stamps updated_at from SQL now() at sub-second resolution,
// so a tight seed loop can produce identical timestamps; the test therefore
// asserts boundary behavior rather than assuming intra-batch ordering gaps.
func TestListEvents_UpdatedSinceFilter(t *testing.T) {
	var events []model.Event
	for i := 0; i < 6; i++ {
		events = append(events, seedEvent(fmt.Sprintf("ev-%03d", i), "coex", "ai", false))
	}
	srv := newServer(t, events)

	all, _ := getEnvelope(t, srv.URL+"/api/v1/events?limit=100")
	if len(all.Data) != 6 {
		t.Fatalf("seeded 6 events, listed %d", len(all.Data))
	}
	minUpdated := all.Data[0].UpdatedAt // ascending order: row 0 is the earliest

	// Cutoff in the far future: nothing is >= it.
	future, _ := getEnvelope(t, srv.URL+"/api/v1/events?limit=100&updated_since=2999-01-01T00:00:00Z")
	if len(future.Data) != 0 {
		t.Fatalf("updated_since=2999 returned %d events, want 0", len(future.Data))
	}

	// Cutoff in the far past: everything is >= it.
	past, _ := getEnvelope(t, srv.URL+"/api/v1/events?limit=100&updated_since=2000-01-01T00:00:00Z")
	if len(past.Data) != 6 {
		t.Fatalf("updated_since=2000 returned %d events, want 6", len(past.Data))
	}

	// Cutoff at the observed minimum: the boundary is inclusive (>=), and every
	// returned row satisfies the predicate.
	atMin, _ := getEnvelope(t, srv.URL+"/api/v1/events?limit=100&updated_since="+minUpdated)
	if len(atMin.Data) == 0 {
		t.Fatalf("updated_since=min(%s) returned 0 events, want inclusive >=", minUpdated)
	}
	for _, e := range atMin.Data {
		if e.UpdatedAt < minUpdated {
			t.Errorf("event %s updated_at %s < cutoff %s", e.EventID, e.UpdatedAt, minUpdated)
		}
	}
}

// TestListEvents_ChangedSinceFilter narrows to events with a change_log row
// after the cutoff.
func TestListEvents_ChangedSinceFilter(t *testing.T) {
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

	// ev-changed has a change_log row; ev-static does not.
	changed := seedEvent("ev-changed", "coex", "ai", false)
	change := model.ChangeLog{
		EventID:   "ev-changed",
		FieldPath: "status",
		OldValue:  strptr("tentative"),
		NewValue:  strptr("scheduled"),
		ChangedAt: "2026-06-20T00:00:00Z",
		BatchID:   strptr("b1"),
	}
	if err := store.UpsertEvent(ctx, wdb, changed, []model.ChangeLog{change}, nil); err != nil {
		t.Fatalf("upsert changed: %v", err)
	}
	if err := store.UpsertEvent(ctx, wdb, seedEvent("ev-static", "coex", "ai", false), nil, nil); err != nil {
		t.Fatalf("upsert static: %v", err)
	}
	wdb.Close()

	rdb, err := store.OpenRead(path)
	if err != nil {
		t.Fatalf("OpenRead: %v", err)
	}
	srv := httptest.NewServer(api.Routes(rdb))
	t.Cleanup(func() { srv.Close(); rdb.Close() })

	env, _ := getEnvelope(t, srv.URL+"/api/v1/events?limit=100&changed_since=2026-06-19T00:00:00Z")
	if len(env.Data) != 1 || env.Data[0].EventID != "ev-changed" {
		t.Fatalf("changed_since returned %d events, want only ev-changed", len(env.Data))
	}
}

// TestListEvents_InvalidCursor returns a bad_request error envelope.
func TestListEvents_InvalidCursor(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})
	resp, err := http.Get(srv.URL + "/api/v1/events?cursor=!!!not-base64!!!")
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

// TestCursorRoundTrip is a unit check on the opaque cursor codec.
func TestCursorRoundTrip(t *testing.T) {
	in := api.Cursor{UpdatedAt: "2026-06-21T10:00:00.123Z", EventID: "coex-2026-ai-expo"}
	enc := api.EncodeCursor(in)
	out, err := api.DecodeCursor(enc)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if out != in {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
	if _, err := api.DecodeCursor("###"); err != api.ErrInvalidCursor {
		t.Errorf("DecodeCursor(garbage) err = %v, want ErrInvalidCursor", err)
	}
	if c, err := api.DecodeCursor(""); err != nil || c != (api.Cursor{}) {
		t.Errorf("DecodeCursor(empty) = (%+v, %v), want zero cursor, nil", c, err)
	}
}

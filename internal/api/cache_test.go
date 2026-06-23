package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// wantCacheControl is the exact edge-cache policy the read API must advertise on
// success responses. Locking the literal here makes the public cache contract a
// test, not an implementation detail.
const wantCacheControl = "public, max-age=120, s-maxage=3600, stale-while-revalidate=86400"

// TestCacheControlOnReadEndpoints asserts every read success response carries the
// shared edge-cache policy, so Cloudflare (Cache Everything) and browsers can
// serve them from cache instead of round-tripping to origin on every page open.
func TestCacheControlOnReadEndpoints(t *testing.T) {
	srv := newFullServer(t, []model.Event{seedEvent("ev-1", "coex", "ai", false)})

	paths := []string{
		"/",
		"/api/v1",
		"/api/v1/schema",
		"/api/v1/openapi.yaml",
		"/llms.txt",
		"/api/v1/events",
		"/api/v1/events?format=md",
		"/api/v1/events/ev-1",
		"/api/v1/events/ev-1/sources",
		"/api/v1/events/changes",
	}
	for _, p := range paths {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", p, resp.StatusCode)
			continue
		}
		if got := resp.Header.Get("Cache-Control"); got != wantCacheControl {
			t.Errorf("GET %s: Cache-Control = %q, want %q", p, got, wantCacheControl)
		}
	}
}

// TestMarkdownNegotiationHasCacheControl pins that the Markdown branch of Respond
// (a separate writer from writeJSON) also stamps the policy.
func TestMarkdownNegotiationHasCacheControl(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-1", "coex", "ai", false)})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events", nil)
	req.Header.Set("Accept", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET md: %v", err)
	}
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want markdown", ct)
	}
	if got := resp.Header.Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("md Cache-Control = %q, want %q", got, wantCacheControl)
	}
}

// TestCacheControlAbsentOnErrors guards the invariant that error responses are
// NOT cacheable — a cached 404/400/429 at the edge would be a correctness bug.
func TestCacheControlAbsentOnErrors(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-1", "coex", "ai", false)})

	cases := []struct {
		name string
		path string
		want int
	}{
		{"not_found", "/api/v1/events/does-not-exist", http.StatusNotFound},
		{"bad_cursor", "/api/v1/events?cursor=!!!notbase64!!!", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + c.path)
			if err != nil {
				t.Fatalf("GET %s: %v", c.path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.want)
			}
			if got := resp.Header.Get("Cache-Control"); got != "" {
				t.Errorf("error response carried Cache-Control = %q, want none", got)
			}
		})
	}
}

// newFullServer fronts the API with the COMPLETE middleware chain (api.Router),
// including the ETag conditional-GET middleware, so 304 behavior can be tested.
func newFullServer(t *testing.T, events []model.Event) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/test.db"

	wdb, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if err := store.Migrate(wdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, e := range events {
		if err := store.UpsertEvent(t.Context(), wdb, e, nil, nil); err != nil {
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
	h, err := api.Router(rdb, api.MiddlewareConfig{})
	if err != nil {
		t.Fatalf("Router: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		rdb.Close()
	})
	return srv
}

// TestConditionalGETPreservesCacheHeaders pins that a 304 (If-None-Match match)
// still carries Cache-Control and Vary: Accept. The ETag middleware copies the
// handler's buffered headers onto the real writer before short-circuiting to 304,
// so the cache directives survive — a client revalidating must still learn the
// freshness policy and the Vary key.
func TestConditionalGETPreservesCacheHeaders(t *testing.T) {
	srv := newFullServer(t, []model.Event{seedEvent("ev-1", "coex", "ai", false)})

	first, err := http.Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	first.Body.Close()
	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("first response has no ETag")
	}
	if got := first.Header.Get("Cache-Control"); got != wantCacheControl {
		t.Fatalf("first Cache-Control = %q, want %q", got, wantCacheControl)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("304 Cache-Control = %q, want %q", got, wantCacheControl)
	}
	if got := resp.Header.Get("Vary"); got != "Accept" {
		t.Errorf("304 Vary = %q, want Accept", got)
	}
}

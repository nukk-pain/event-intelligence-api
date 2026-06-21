package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// ---------------------------------------------------------------------------
// Test harness: a fake Source whose Discover returns canned Refs (independent
// of the fetcher) and whose Parse reads the fetched detail body. The detail
// pages are served by an httptest server; a real SSRF-guarded Fetcher (loopback
// allowed for the test) fetches them, so the full Fetch->Parse->Normalize path
// is exercised. One special body triggers a panic in Parse to prove per-item
// recover() isolation.
// ---------------------------------------------------------------------------

// fakeSource serves a fixed slug set. Body "PANIC" makes Parse panic; body
// "INVALID" makes Parse return a ParsedEvent that fails normalize validation.
type fakeSource struct {
	id      string
	base    string // httptest base URL
	slugs   []string
	discErr error
}

func (s *fakeSource) ID() string { return s.id }

func (s *fakeSource) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	if s.discErr != nil {
		return nil, s.discErr
	}
	refs := make([]sources.Ref, 0, len(s.slugs))
	for _, slug := range s.slugs {
		refs = append(refs, sources.Ref{
			EventID: s.id + "-" + slug,
			URL:     s.base + "/d/" + slug,
		})
	}
	return refs, nil
}

func (s *fakeSource) Parse(ctx context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	body := strings.TrimSpace(string(raw.Body))
	if body == "PANIC" {
		panic("boom: simulated parser panic")
	}
	if body == "INVALID" {
		// Name empty -> normalize rule1 fails -> per-item skip (not panic).
		return &sources.ParsedEvent{
			SourceID:     s.id,
			URL:          raw.URL,
			Name:         "",
			ClassifyText: "",
			RetrievedAt:  "2026-06-21T00:00:00Z",
		}, nil
	}
	// Good event. body is the display name; classify text drives a category.
	return &sources.ParsedEvent{
		SourceID:     s.id,
		URL:          raw.URL,
		Name:         body,
		StartRaw:     strptr("2026-06-18"),
		EndRaw:       strptr("2026-06-20"),
		VenueName:    strptr("코엑스"),
		City:         strptr("서울"),
		ClassifyText: body + " 인공지능 AI 컨퍼런스",
		RetrievedAt:  "2026-06-21T00:00:00Z",
	}, nil
}

func strptr(s string) *string { return &s }

var _ sources.Source = (*fakeSource)(nil)

// detailServer serves /d/<slug> with a body chosen by bodies[slug] (default the
// slug itself, which normalizes to a valid event).
func detailServer(t *testing.T, bodies map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/d/", func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/d/")
		body, ok := bodies[slug]
		if !ok {
			body = slug // valid by default
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// loopbackFetcher builds a Fetcher that may reach the httptest server (loopback
// + the server's host on the allowlist). Robots/crawl-delay are inert (the test
// server 404s robots.txt -> permissive).
func loopbackFetcher(t *testing.T, base string) *fetch.Fetcher {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base %q: %v", base, err)
	}
	f, err := fetch.NewFetcher(
		fetch.WithAllowLoopback(true),
		fetch.WithAllowedHosts(u.Hostname()),
		fetch.WithPerMinute(6000), // don't throttle the test
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	return f
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenWrite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open write: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func countEvents(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func countIngestErrors(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ingest_error`).Scan(&n); err != nil {
		t.Fatalf("count ingest_error: %v", err)
	}
	return n
}

func countAnomalies(t *testing.T, db *sql.DB, source string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM fetch_anomaly WHERE source=?`, source).Scan(&n); err != nil {
		t.Fatalf("count fetch_anomaly: %v", err)
	}
	return n
}

func eventExists(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE event_id=?`, id).Scan(&n); err != nil {
		t.Fatalf("event exists: %v", err)
	}
	return n > 0
}

// ---------------------------------------------------------------------------
// (a) per-item isolation: one panicking item + one invalid item are isolated;
//
//	the good items still store.
//
// ---------------------------------------------------------------------------
func TestRun_PerItemIsolation(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, map[string]string{
		"bad":     "PANIC",   // Parse panics
		"invalid": "INVALID", // normalize rejects
	})
	f := loopbackFetcher(t, srv.URL)

	src := &fakeSource{
		id:    "coex",
		base:  srv.URL,
		slugs: []string{"good-1", "bad", "good-2", "invalid", "good-3"},
	}

	p := New("batch-iso").WithClock(func() string { return "2026-06-21T00:00:00Z" })
	rep, err := p.Run(context.Background(), db, []sources.Source{src}, f)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(rep.Sources) != 1 {
		t.Fatalf("want 1 source report, got %d", len(rep.Sources))
	}
	sr := rep.Sources[0]
	if sr.Aborted {
		t.Fatalf("source unexpectedly aborted: %s", sr.AbortReason)
	}
	if sr.Discovered != 5 {
		t.Errorf("Discovered = %d, want 5", sr.Discovered)
	}
	if sr.Parsed != 3 {
		t.Errorf("Parsed = %d, want 3 (good-1/2/3)", sr.Parsed)
	}
	if sr.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (panic + invalid)", sr.Skipped)
	}
	if sr.Stored != 3 {
		t.Errorf("Stored = %d, want 3", sr.Stored)
	}

	if got := countEvents(t, db); got != 3 {
		t.Errorf("events in db = %d, want 3", got)
	}
	if got := countIngestErrors(t, db); got != 2 {
		t.Errorf("ingest_error rows = %d, want 2", got)
	}
	for _, id := range []string{"coex-good-1", "coex-good-2", "coex-good-3"} {
		if !eventExists(t, db, id) {
			t.Errorf("expected event %s stored", id)
		}
	}
	// The bad items must NOT be half-written.
	if eventExists(t, db, "coex-bad") {
		t.Errorf("panicking item coex-bad must not be stored")
	}
	if eventExists(t, db, "coex-invalid") {
		t.Errorf("invalid item coex-invalid must not be stored")
	}
}

// ---------------------------------------------------------------------------
// (b) circuit breaker: a source whose discovery returns 0 trips the breaker and
//
//	leaves prior rows intact (no delete / no mutation).
//
// ---------------------------------------------------------------------------
func TestRun_CircuitBreakerZeroDiscovery_PreservesPriorRows(t *testing.T) {
	db := testDB(t)

	// First run: 2 good events -> stored, baseline recorded.
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)
	good := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b"}}

	p1 := New("batch-1").WithClock(func() string { return "2026-06-21T00:00:00Z" })
	if _, err := p1.Run(context.Background(), db, []sources.Source{good}, f); err != nil {
		t.Fatalf("seed run error: %v", err)
	}
	if got := countEvents(t, db); got != 2 {
		t.Fatalf("after seed: events = %d, want 2", got)
	}

	// Second run: SAME source id but discovery returns 0 refs (e.g. site
	// restructure / parser break). Breaker must trip and write NOTHING.
	empty := &fakeSource{id: "coex", base: srv.URL, slugs: nil}
	p2 := New("batch-2").WithClock(func() string { return "2026-06-22T00:00:00Z" })
	rep, err := p2.Run(context.Background(), db, []sources.Source{empty}, f)
	if err != nil {
		t.Fatalf("second run error: %v", err)
	}

	sr := rep.Sources[0]
	if !sr.Aborted {
		t.Fatalf("expected breaker to trip on 0 discovery; report: %+v", sr)
	}
	if sr.Stored != 0 {
		t.Errorf("Stored = %d, want 0 when aborted", sr.Stored)
	}
	// Prior rows intact.
	if got := countEvents(t, db); got != 2 {
		t.Errorf("after aborted run: events = %d, want 2 (prior rows preserved)", got)
	}
	if !eventExists(t, db, "coex-a") || !eventExists(t, db, "coex-b") {
		t.Errorf("prior events must remain after breaker trip")
	}
	if got := countAnomalies(t, db, "coex"); got != 1 {
		t.Errorf("fetch_anomaly rows = %d, want 1", got)
	}
}

// Relative floor: discovery drops below 50% of last successful -> trips.
func TestRun_CircuitBreakerRelativeFloor(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)

	// Seed with 4 events.
	seed := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b", "c", "d"}}
	p1 := New("b1").WithClock(func() string { return "2026-06-21T00:00:00Z" })
	if _, err := p1.Run(context.Background(), db, []sources.Source{seed}, f); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Next run discovers 1 (< 50% of 4 = 2) -> trip, preserve 4 rows.
	drop := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a"}}
	p2 := New("b2").WithClock(func() string { return "2026-06-22T00:00:00Z" })
	rep, err := p2.Run(context.Background(), db, []sources.Source{drop}, f)
	if err != nil {
		t.Fatalf("drop run: %v", err)
	}
	if !rep.Sources[0].Aborted {
		t.Fatalf("expected relative-floor trip; got %+v", rep.Sources[0])
	}
	if got := countEvents(t, db); got != 4 {
		t.Errorf("events = %d, want 4 preserved", got)
	}
}

// Discovery ERROR (not empty) must also preserve rows and not masquerade as a reap.
func TestRun_DiscoveryError_PreservesRows(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)

	seed := &fakeSource{id: "kintex", base: srv.URL, slugs: []string{"x", "y"}}
	if _, err := New("b1").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{seed}, f); err != nil {
		t.Fatalf("seed: %v", err)
	}

	failing := &fakeSource{id: "kintex", base: srv.URL, discErr: fmt.Errorf("list.do status 503")}
	rep, err := New("b2").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{failing}, f)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	sr := rep.Sources[0]
	if !sr.Aborted || sr.Err == nil {
		t.Fatalf("expected discovery error to abort with Err set; got %+v", sr)
	}
	if got := countEvents(t, db); got != 2 {
		t.Errorf("events = %d, want 2 preserved on discovery error", got)
	}
}

func fixedClock() string { return "2026-06-21T00:00:00Z" }

// Multiple sources: one tripping must not stop the other from storing.
func TestRun_PerSourceIndependence(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)

	good := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b"}}
	empty := &fakeSource{id: "kintex", base: srv.URL, slugs: nil}

	rep, err := New("b").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{good, empty}, f)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var coexRep, kintexRep SourceReport
	for _, sr := range rep.Sources {
		switch sr.Source {
		case "coex":
			coexRep = sr
		case "kintex":
			kintexRep = sr
		}
	}
	if coexRep.Stored != 2 || coexRep.Aborted {
		t.Errorf("coex should store 2 unaffected by kintex; got %+v", coexRep)
	}
	if !kintexRep.Aborted {
		t.Errorf("kintex should abort on 0 discovery; got %+v", kintexRep)
	}
	if got := countEvents(t, db); got != 2 {
		t.Errorf("events = %d, want 2 (coex only)", got)
	}
}

// ---------------------------------------------------------------------------
// (c) flock single-flight: a second concurrent acquire is rejected cleanly.
// ---------------------------------------------------------------------------
func TestFileLock_SingleFlight(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "ingest.lock")

	first, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}

	// Second acquire while first is held must fail with ErrLocked (no block).
	second, err := AcquireLock(lockPath)
	if err == nil {
		second.Unlock()
		t.Fatalf("second AcquireLock succeeded; want ErrLocked")
	}
	if err != ErrLocked {
		t.Fatalf("second AcquireLock err = %v, want ErrLocked", err)
	}

	// After releasing the first, a fresh acquire succeeds.
	if err := first.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	third, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("third AcquireLock after release: %v", err)
	}
	if err := third.Unlock(); err != nil {
		t.Fatalf("Unlock third: %v", err)
	}
}

// ---------------------------------------------------------------------------
// (d) wall-clock deadline / cancellation: an already-cancelled ctx must stop the
//
//	run before any source is processed; the batch is marked Truncated and NO
//	source's discovery baseline is written (floor-poisoning guard).
//
// ---------------------------------------------------------------------------
func TestRun_CancelledBeforeStart_Truncated(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)
	src := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b", "c"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already out of time before Run starts

	rep, err := New("b-cancel").WithClock(fixedClock).Run(ctx, db, []sources.Source{src}, f)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rep.Truncated {
		t.Fatalf("expected Truncated=true on pre-cancelled ctx; got %+v", rep)
	}
	if len(rep.Sources) != 0 {
		t.Errorf("expected 0 source reports (no source started); got %d", len(rep.Sources))
	}
	if got := countEvents(t, db); got != 0 {
		t.Errorf("events = %d, want 0 (nothing processed)", got)
	}
	// No baseline must have been written for a run that never completed a source.
	if _, found, _ := store.LoadBatchStats(ctx, db, "coex"); found {
		t.Errorf("batch_stats baseline must NOT be recorded when run is truncated before any source")
	}
}

// Cancellation MID-SOURCE: the ref loop must stop and the source must be marked
// Cancelled, and crucially its (partial) Discovered count must NOT become the
// floor baseline -- otherwise a later run's 50% floor ratchets down.
func TestRun_CancelledMidSource_DoesNotPoisonFloor(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)

	// Seed a COMPLETE run with 4 events so a real baseline (Discovered=4) exists.
	seed := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b", "c", "d"}}
	if _, err := New("b-seed").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{seed}, f); err != nil {
		t.Fatalf("seed: %v", err)
	}
	prev, found, _ := store.LoadBatchStats(context.Background(), db, "coex")
	if !found || prev.Discovered != 4 {
		t.Fatalf("seed baseline = %+v found=%v, want Discovered=4", prev, found)
	}

	// Cancelling source cancels the run as soon as its Discover is observed,
	// before the ref loop processes items. Discover still returns a small ref
	// list (2), which under normal rules would NOT trip the breaker but WOULD,
	// if persisted, drop the floor from 4 to 2.
	src := &cancelOnDiscoverSource{
		fakeSource: fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	src.cancel = cancel

	rep, err := New("b-mid").WithClock(fixedClock).Run(ctx, db, []sources.Source{src}, f)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rep.Truncated {
		t.Fatalf("expected Truncated=true; got %+v", rep)
	}
	if len(rep.Sources) != 1 || !rep.Sources[0].Cancelled {
		t.Fatalf("expected the source marked Cancelled; got %+v", rep.Sources)
	}

	// THE GUARD: baseline must still be the COMPLETE run's 4, not the partial 2.
	after, _, _ := store.LoadBatchStats(context.Background(), db, "coex")
	if after.Discovered != 4 {
		t.Errorf("floor baseline poisoned: Discovered = %d, want 4 (cut-short run must not update baseline)", after.Discovered)
	}
}

// cancelOnDiscoverSource cancels the run ctx as soon as Discover is called (the
// refs are returned, but the ctx is dead so the ref loop bails on its first
// iteration). This deterministically exercises the mid-source cancellation path.
type cancelOnDiscoverSource struct {
	fakeSource
	cancel context.CancelFunc
}

func (s *cancelOnDiscoverSource) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	refs, err := s.fakeSource.Discover(ctx, f)
	if s.cancel != nil {
		s.cancel()
	}
	return refs, err
}

// Idempotence smoke: same source twice -> no new events, no new change_log rows.
func TestRun_IdempotentSecondRun(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)
	src := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b"}}

	if _, err := New("b1").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{src}, f); err != nil {
		t.Fatalf("run1: %v", err)
	}
	var changeRows1 int
	db.QueryRow(`SELECT COUNT(*) FROM change_log`).Scan(&changeRows1)

	if _, err := New("b2").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{src}, f); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if got := countEvents(t, db); got != 2 {
		t.Errorf("events = %d, want 2 after idempotent rerun", got)
	}
	var changeRows2 int
	db.QueryRow(`SELECT COUNT(*) FROM change_log`).Scan(&changeRows2)
	if changeRows2 != changeRows1 {
		t.Errorf("change_log rows changed on no-op rerun: %d -> %d", changeRows1, changeRows2)
	}
}

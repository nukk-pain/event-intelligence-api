package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/store"
)

type detailActivity struct {
	mu     sync.Mutex
	active int
	max    int
}

func (a *detailActivity) enter() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.active++
	if a.active > a.max {
		a.max = a.active
	}
}

func (a *detailActivity) leave() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.active--
}

func (a *detailActivity) maxActive() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.max
}

func TestRun_DetailRefsUseBoundedWorkers(t *testing.T) {
	// Given
	db := testDB(t)
	activity := &detailActivity{}
	srv := delayedDetailServer(t, 200*time.Millisecond, activity)
	f := loopbackFetcher(t, srv.URL)
	src := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"a", "b", "c", "d", "e", "f"}}

	// When
	started := time.Now()
	report, err := New("b-detail-workers").
		WithDetailWorkers(2).
		WithClock(fixedClock).
		Run(context.Background(), db, []sources.Source{src}, f)
	elapsed := time.Since(started)

	// Then
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if got := activity.maxActive(); got > 2 {
		t.Fatalf("max active detail workers = %d, want <= 2", got)
	}
	if got := activity.maxActive(); got < 2 {
		t.Fatalf("max active detail workers = %d, want evidence of concurrent refs", got)
	}
	if elapsed >= time.Second {
		t.Fatalf("elapsed = %s, want under 1s for 6 refs with 2 workers and 200ms detail delay", elapsed)
	}
	if got := countEvents(t, db); got != 6 {
		t.Fatalf("events in db = %d, want 6", got)
	}
	if len(report.Sources) != 1 || report.Sources[0].Stored != 6 {
		t.Fatalf("source report = %+v, want Stored=6", report.Sources)
	}
	t.Logf("detail_workers=2 max_active=%d elapsed=%s stored=%d", activity.maxActive(), elapsed, countEvents(t, db))
}

func TestRun_ConcurrentRefProcessingPreservesItemIsolation(t *testing.T) {
	// Given
	db := testDB(t)
	invalidStarted := make(chan struct{}, 1)
	goodFinished := make(chan struct{}, 1)
	releaseInvalid := make(chan struct{})
	srv := isolationDetailServer(t, invalidStarted, goodFinished, releaseInvalid)
	f := loopbackFetcher(t, srv.URL)
	src := &fakeSource{id: "coex", base: srv.URL, slugs: []string{"invalid-first", "good-second"}}
	done := make(chan runResult, 1)

	// When
	go func() {
		report, err := New("b-detail-isolation").
			WithDetailWorkers(2).
			WithClock(fixedClock).
			Run(context.Background(), db, []sources.Source{src}, f)
		done <- runResult{report: report, err: err}
	}()
	waitSignal(t, invalidStarted, "invalid detail start")
	waitSignal(t, goodFinished, "good detail finish")
	if got := countEvents(t, db); got != 0 {
		t.Fatalf("events before source finalization = %d, want 0", got)
	}
	if got := countIngestErrors(t, db); got != 0 {
		t.Fatalf("ingest errors before source finalization = %d, want 0", got)
	}
	close(releaseInvalid)
	outcome := waitRunResult(t, done)

	// Then
	if outcome.err != nil {
		t.Fatalf("Run error: %v", outcome.err)
	}
	if got := countEvents(t, db); got != 1 {
		t.Fatalf("events in db = %d, want 1 good survivor", got)
	}
	if !eventExists(t, db, "coex-good-second") {
		t.Fatalf("expected good survivor coex-good-second stored")
	}
	eventID, stage := ingestErrorRow(t, db)
	if eventID != "coex-invalid-first" || stage != "normalize" {
		t.Fatalf("ingest error = %s/%s, want coex-invalid-first/normalize", eventID, stage)
	}
	if len(outcome.report.Sources) != 1 || outcome.report.Sources[0].Skipped != 1 || outcome.report.Sources[0].Stored != 1 {
		t.Fatalf("source report = %+v, want Skipped=1 Stored=1", outcome.report.Sources)
	}
	t.Logf("good_event_stored=coex-good-second bad_event_error=%s/%s", eventID, stage)
}

func TestRun_CancelledConcurrentRunDoesNotPoisonFloor(t *testing.T) {
	// Given
	db := testDB(t)
	seedServer := detailServer(t, nil)
	seedFetcher := loopbackFetcher(t, seedServer.URL)
	seed := &fakeSource{id: "coex", base: seedServer.URL, slugs: []string{"a", "b", "c", "d"}}
	if _, err := New("b-seed").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{seed}, seedFetcher); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	prev, found, err := store.LoadBatchStats(context.Background(), db, "coex")
	if err != nil || !found || prev.Discovered != 4 {
		t.Fatalf("seed baseline = %+v found=%v err=%v, want discovered=4", prev, found, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	cancelServer := cancellingDetailServer(t, started, release, cancel)
	f := loopbackFetcher(t, cancelServer.URL)
	partial := &fakeSource{id: "coex", base: cancelServer.URL, slugs: []string{"a", "b", "c"}}
	done := make(chan runResult, 1)

	// When
	go func() {
		report, err := New("b-cancel-concurrent").
			WithDetailWorkers(2).
			WithClock(fixedClock).
			Run(ctx, db, []sources.Source{partial}, f)
		done <- runResult{report: report, err: err}
	}()
	waitSignal(t, started, "cancelled detail start")
	close(release)
	outcome := waitRunResult(t, done)

	// Then
	if outcome.err != nil {
		t.Fatalf("Run error: %v", outcome.err)
	}
	if !outcome.report.Truncated {
		t.Fatalf("expected truncated report after cancellation; got %+v", outcome.report)
	}
	if len(outcome.report.Sources) != 1 || !outcome.report.Sources[0].Cancelled {
		t.Fatalf("expected source Cancelled=true; report=%+v", outcome.report.Sources)
	}
	after, _, err := store.LoadBatchStats(context.Background(), db, "coex")
	if err != nil {
		t.Fatalf("load post-cancel baseline: %v", err)
	}
	if after.Discovered != 4 {
		t.Fatalf("floor baseline poisoned: discovered=%d, want 4", after.Discovered)
	}
	if got := countEvents(t, db); got != 4 {
		t.Fatalf("events after cancelled partial run = %d, want seeded 4 unchanged", got)
	}
	t.Logf("cancelled=true preserved_floor=%d stored_rows=%d", after.Discovered, countEvents(t, db))
}

type runResult struct {
	report Report
	err    error
}

func delayedDetailServer(t *testing.T, delay time.Duration, activity *detailActivity) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/d/", func(w http.ResponseWriter, r *http.Request) {
		activity.enter()
		defer activity.leave()
		time.Sleep(delay)
		fmt.Fprint(w, strings.TrimPrefix(r.URL.Path, "/d/"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func isolationDetailServer(t *testing.T, invalidStarted chan<- struct{}, goodFinished chan<- struct{}, releaseInvalid <-chan struct{}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/d/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/d/") {
		case "invalid-first":
			invalidStarted <- struct{}{}
			<-releaseInvalid
			fmt.Fprint(w, "INVALID")
		case "good-second":
			fmt.Fprint(w, "good-second")
			goodFinished <- struct{}{}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func cancellingDetailServer(t *testing.T, started chan<- struct{}, release <-chan struct{}, cancel context.CancelFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/d/", func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		cancel()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "cancelled-detail")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func ingestErrorRow(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	var eventID, stage string
	switch err := db.QueryRow(`SELECT event_id, stage FROM ingest_error ORDER BY id`).Scan(&eventID, &stage); {
	case err == nil:
		return eventID, stage
	default:
		t.Fatalf("query ingest_error: %v", err)
	}
	return "", ""
}

func waitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitRunResult(t *testing.T, done <-chan runResult) runResult {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pipeline run")
		return runResult{}
	}
}

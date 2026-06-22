package pipeline

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

type blockingDiscoverSource struct {
	id      string
	started chan<- string
	release <-chan struct{}
}

func (s *blockingDiscoverSource) ID() string { return s.id }

func (s *blockingDiscoverSource) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	select {
	case s.started <- s.id:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-s.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *blockingDiscoverSource) Parse(ctx context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	return nil, fmt.Errorf("unexpected parse for %s", s.id)
}

var _ sources.Source = (*blockingDiscoverSource)(nil)

type orderedDiscoverSource struct {
	id       string
	release  <-chan struct{}
	finished chan<- string
}

func (s *orderedDiscoverSource) ID() string { return s.id }

func (s *orderedDiscoverSource) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case s.finished <- s.id:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}

func (s *orderedDiscoverSource) Parse(ctx context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	return nil, fmt.Errorf("unexpected parse for %s", s.id)
}

var _ sources.Source = (*orderedDiscoverSource)(nil)

func TestRun_SourcesRunConcurrently(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)

	started := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan error, 1)
	srcs := []sources.Source{
		&blockingDiscoverSource{id: "coex", started: started, release: release},
		&blockingDiscoverSource{id: "kintex", started: started, release: release},
	}

	began := time.Now()
	go func() {
		_, err := New("b-concurrent").WithSourceConcurrency(2).WithClock(fixedClock).Run(context.Background(), db, srcs, f)
		done <- err
	}()

	first := waitForSourceStart(t, started)
	second, sawSecond := waitForOptionalSourceStart(started, 500*time.Millisecond)
	t.Logf("source discover overlap: first=%s second=%s elapsed_to_second=%s", first, second, time.Since(began))
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !sawSecond {
		t.Fatalf("expected both sources to enter Discover concurrently; only saw %q before release", first)
	}
	if first == second {
		t.Fatalf("expected two distinct sources to start, got %q twice", first)
	}
}

func TestRun_SourceReportsStayInputOrdered(t *testing.T) {
	db := testDB(t)
	srv := detailServer(t, nil)
	f := loopbackFetcher(t, srv.URL)

	coexRelease := make(chan struct{})
	kintexRelease := make(chan struct{})
	finished := make(chan string, 2)
	srcs := []sources.Source{
		&orderedDiscoverSource{id: "coex", release: coexRelease, finished: finished},
		&orderedDiscoverSource{id: "kintex", release: kintexRelease, finished: finished},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var rep Report
	var runErr error
	go func() {
		defer wg.Done()
		rep, runErr = New("b-ordered").WithSourceConcurrency(2).WithClock(fixedClock).Run(context.Background(), db, srcs, f)
	}()

	close(kintexRelease)
	if got := waitForSourceStart(t, finished); got != "kintex" {
		t.Fatalf("expected kintex to finish first, got %q", got)
	}
	close(coexRelease)
	wg.Wait()
	if runErr != nil {
		t.Fatalf("Run error: %v", runErr)
	}
	if len(rep.Sources) != 2 {
		t.Fatalf("source reports = %d, want 2", len(rep.Sources))
	}
	t.Logf("source completion order=[kintex coex] report order=[%s %s]", rep.Sources[0].Source, rep.Sources[1].Source)
	if rep.Sources[0].Source != "coex" || rep.Sources[1].Source != "kintex" {
		t.Fatalf("report order = [%s %s], want [coex kintex]", rep.Sources[0].Source, rep.Sources[1].Source)
	}
}

func waitForSourceStart(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case id := <-started:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for source to start")
		return ""
	}
}

func waitForOptionalSourceStart(started <-chan string, timeout time.Duration) (string, bool) {
	select {
	case id := <-started:
		return id, true
	case <-time.After(timeout):
		return "", false
	}
}

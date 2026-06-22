package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestRobotsDisallowSkips(t *testing.T) {
	var detailHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\nCrawl-delay: 0\n"))
		case "/private/secret":
			detailHits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("should not be fetched"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	_, err := f.Fetch(context.Background(), srv.URL+"/private/secret", Conditional{})
	if err == nil {
		t.Fatalf("expected robots-disallow skip error, got nil")
	}
	if !errors.Is(err, ErrRobotsDisallowed) {
		t.Fatalf("expected ErrRobotsDisallowed, got %v", err)
	}
	if detailHits.Load() != 0 {
		t.Fatalf("disallowed path was fetched %d times, want 0", detailHits.Load())
	}
}

func TestRobotsAllowsPermittedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("public ok"))
		}
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/public/event", Conditional{})
	if err != nil {
		t.Fatalf("allowed path should fetch: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestRobotsFetchSingleflightPerHost(t *testing.T) {
	var robotsHits atomic.Int32
	var detailHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHits.Add(1)
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\n"))
			return
		}
		detailHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := testFetcher(t,
		allowHost(t, srv),
		WithPerMinute(6000),
		WithMaxRetries(0),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		i := i
		go func() {
			<-start
			_, err := f.Fetch(ctx, srv.URL+"/event/"+strconv.Itoa(i), Conditional{})
			errs <- err
		}()
	}

	close(start)
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Fetch %d: %v", i, err)
		}
	}
	t.Logf("robots requests=%d detail requests=%d", robotsHits.Load(), detailHits.Load())
	if robotsHits.Load() != 1 {
		t.Fatalf("robots.txt stampede: got %d requests, want 1", robotsHits.Load())
	}
	if detailHits.Load() != 8 {
		t.Fatalf("detail hits = %d, want 8", detailHits.Load())
	}
}

func TestRobotsSingleflightLeaderCancellationDoesNotAllowDisallowedPath(t *testing.T) {
	var robotsHits atomic.Int32
	var secretHits atomic.Int32
	firstRobotsEntered := make(chan struct{})
	releaseFirstRobots := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			if robotsHits.Add(1) == 1 {
				close(firstRobotsEntered)
				select {
				case <-r.Context().Done():
					return
				case <-releaseFirstRobots:
				}
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
		case "/private/secret":
			secretHits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("secret"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	f := testFetcher(t,
		allowHost(t, srv),
		WithPerMinute(600000),
		WithMaxRetries(0),
		WithTimeout(2*time.Second),
	)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := f.Fetch(leaderCtx, srv.URL+"/private/secret", Conditional{})
		leaderDone <- err
	}()
	<-firstRobotsEntered

	const waiterCount = 20
	startWaiters := make(chan struct{})
	waitersStarted := make(chan struct{}, waiterCount)
	waiterErrs := make(chan error, waiterCount)
	for i := 0; i < waiterCount; i++ {
		go func() {
			<-startWaiters
			waitersStarted <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := f.Fetch(ctx, srv.URL+"/private/secret", Conditional{})
			waiterErrs <- err
		}()
	}

	close(startWaiters)
	for i := 0; i < waiterCount; i++ {
		<-waitersStarted
	}
	time.Sleep(50 * time.Millisecond)
	cancelLeader()

	var leaderErr error
	leaderReceived := false
	select {
	case leaderErr = <-leaderDone:
		leaderReceived = true
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirstRobots)
	if !leaderReceived {
		leaderErr = <-leaderDone
	}

	var allowedErrs int
	for i := 0; i < waiterCount; i++ {
		err := <-waiterErrs
		if !errors.Is(err, ErrRobotsDisallowed) {
			allowedErrs++
		}
	}
	t.Logf("leader error=%v robots requests=%d secret hits=%d healthy waiter non-disallow errors=%d", leaderErr, robotsHits.Load(), secretHits.Load(), allowedErrs)
	if secretHits.Load() != 0 {
		t.Fatalf("healthy waiters fetched robots-disallowed path after leader cancellation: secret hits=%d robots hits=%d", secretHits.Load(), robotsHits.Load())
	}
	if allowedErrs != 0 {
		t.Fatalf("healthy waiters should all receive ErrRobotsDisallowed, got %d non-disallow errors", allowedErrs)
	}
}

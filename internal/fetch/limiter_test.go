package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A positive Crawl-delay on one host must throttle only that host, never the
// shared base rate for other hosts. Two distinct hosts are served by two
// httptest servers; the first advertises a slow Crawl-delay, the second does
// not. After fetching the slow host, a back-to-back pair of fetches against the
// fast host must NOT be gated by the slow host's Crawl-delay.
func TestCrawlDelayIsPerHost(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\nCrawl-delay: 10\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("slow ok"))
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fast ok"))
	}))
	defer fast.Close()

	f := testFetcher(t, allowHost(t, slow), allowHost(t, fast))

	// Prime the slow host: this records its Crawl-delay=10s for that host only.
	if _, err := f.Fetch(context.Background(), slow.URL+"/a", Conditional{}); err != nil {
		t.Fatalf("slow host fetch: %v", err)
	}

	start := time.Now()
	if _, err := f.Fetch(context.Background(), fast.URL+"/1", Conditional{}); err != nil {
		t.Fatalf("fast host fetch 1: %v", err)
	}
	if _, err := f.Fetch(context.Background(), fast.URL+"/2", Conditional{}); err != nil {
		t.Fatalf("fast host fetch 2: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fast host throttled by other host's Crawl-delay: %v elapsed", elapsed)
	}
}

func TestRequestRateLimitIsPerHost(t *testing.T) {
	var mu sync.Mutex
	requestTimes := map[string][]time.Time{
		"127.0.0.1": {},
		"localhost": {},
	}
	newServer := func(label string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/robots.txt" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("User-agent: *\n"))
				return
			}
			mu.Lock()
			requestTimes[label] = append(requestTimes[label], time.Now())
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
	}
	first := newServer("127.0.0.1")
	defer first.Close()
	second := newServer("localhost")
	defer second.Close()

	firstURL := rewriteURLHost(t, first.URL, "127.0.0.1")
	secondURL := rewriteURLHost(t, second.URL, "localhost")
	f := testFetcher(t,
		WithAllowedHosts("127.0.0.1", "localhost"),
		WithPerMinute(60),
		WithMaxRetries(0),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, 4)
	for _, rawURL := range []string{
		firstURL + "/event/1",
		firstURL + "/event/2",
		secondURL + "/event/1",
		secondURL + "/event/2",
	} {
		go func() {
			<-start
			_, err := f.Fetch(ctx, rawURL, Conditional{})
			errs <- err
		}()
	}

	began := time.Now()
	close(start)
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Fetch %d: %v", i, err)
		}
	}
	elapsed := time.Since(began)

	mu.Lock()
	firstTimes := append([]time.Time(nil), requestTimes["127.0.0.1"]...)
	secondTimes := append([]time.Time(nil), requestTimes["localhost"]...)
	mu.Unlock()
	if len(firstTimes) != 2 || len(secondTimes) != 2 {
		t.Fatalf("detail hits = 127.0.0.1:%d localhost:%d, want 2 each", len(firstTimes), len(secondTimes))
	}
	firstGap := absDuration(firstTimes[1].Sub(firstTimes[0]))
	secondGap := absDuration(secondTimes[1].Sub(secondTimes[0]))
	crossHostGap := absDuration(firstTimes[0].Sub(secondTimes[0]))
	t.Logf("same-host pacing: 127.0.0.1 gap=%v localhost gap=%v", firstGap, secondGap)
	t.Logf("cross-host overlap: first detail gap=%v total elapsed=%v", crossHostGap, elapsed)
	if firstGap < 800*time.Millisecond || secondGap < 800*time.Millisecond {
		t.Fatalf("same-host requests were not paced: 127.0.0.1=%v localhost=%v", firstGap, secondGap)
	}
	if crossHostGap > 350*time.Millisecond {
		t.Fatalf("different hosts did not consume their allowances concurrently: first detail gap=%v", crossHostGap)
	}
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("different hosts shared a global request lane: elapsed=%v", elapsed)
	}
}

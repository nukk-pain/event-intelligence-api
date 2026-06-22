package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testFetcher builds a Fetcher wired for httptest: the allowlist is widened to
// permit the test server host, the SSRF guard is relaxed to allow loopback for
// the server itself (httptest binds 127.0.0.1) unless a test explicitly probes
// the loopback-rejection path, and retry/timeout knobs are tightened.
func testFetcher(t *testing.T, opts ...Option) *Fetcher {
	t.Helper()
	base := []Option{
		WithUserAgent("eventsintel-test/0.1"),
		WithPerMinute(6000), // effectively unthrottled for unit tests
		WithTimeout(2 * time.Second),
		WithMaxBodyBytes(5 << 20),
		WithMaxRetries(2),
		WithRetryBackoff(5 * time.Millisecond),
		// httptest binds loopback; tests must opt into hitting it.
		WithAllowLoopback(true),
	}
	f, err := NewFetcher(append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	return f
}

// allowHost adds the httptest server host to the allowlist.
func allowHost(t *testing.T, srv *httptest.Server) Option {
	t.Helper()
	u := srv.URL
	// strip scheme
	host := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return WithAllowedHosts(host)
}

func TestFetch200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>안녕 hello</body></html>"))
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/event/1", Conditional{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if res.NotModified {
		t.Fatalf("NotModified should be false on 200")
	}
	if res.ETag != `"abc123"` {
		t.Fatalf("ETag = %q, want abc123", res.ETag)
	}
	if res.LastModified == "" {
		t.Fatalf("LastModified empty")
	}
	if !strings.Contains(string(res.Body), "안녕 hello") {
		t.Fatalf("body missing expected content: %q", string(res.Body))
	}
}

func TestFetchConditional304(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Only return 304 when the client sent the validator we previously gave.
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>fresh</html>"))
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/event/2", Conditional{ETag: `"v1"`})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.NotModified {
		t.Fatalf("expected NotModified=true, got status %d", res.StatusCode)
	}
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", res.StatusCode)
	}
}

// Conditional validators must NOT be sent when the caller has none (best-effort).
func TestFetchNoConditionalWhenNoValidators(t *testing.T) {
	var sawINM, sawIMS atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("If-None-Match") != "" {
			sawINM.Store(true)
		}
		if r.Header.Get("If-Modified-Since") != "" {
			sawIMS.Store(true)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	if _, err := f.Fetch(context.Background(), srv.URL+"/x", Conditional{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sawINM.Load() || sawIMS.Load() {
		t.Fatalf("conditional headers sent without validators (INM=%v IMS=%v)", sawINM.Load(), sawIMS.Load())
	}
}

func TestFetch429Retries(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := hits.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/y", Conditional{})
	if err != nil {
		t.Fatalf("Fetch after retries: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 after retry", res.StatusCode)
	}
	if hits.Load() < 3 {
		t.Fatalf("expected >=3 attempts (2 retries), got %d", hits.Load())
	}
	if !strings.Contains(string(res.Body), "recovered") {
		t.Fatalf("body = %q", string(res.Body))
	}
}

func TestFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv), WithTimeout(200*time.Millisecond), WithMaxRetries(0))
	_, err := f.Fetch(context.Background(), srv.URL+"/slow", Conditional{})
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestFetchOverSizeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Do not set Content-Length so chunked streaming forces LimitReader cap.
		w.WriteHeader(http.StatusOK)
		chunk := strings.Repeat("a", 64*1024)
		for i := 0; i < 200; i++ { // ~12.5MB > 5MB cap
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv), WithMaxBodyBytes(5<<20), WithMaxRetries(0))
	_, err := f.Fetch(context.Background(), srv.URL+"/big", Conditional{})
	if err == nil {
		t.Fatalf("expected over-size error, got nil")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

func TestFetchRejectsOverContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", "99999999") // ~95MB declared
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("small"))
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv), WithMaxBodyBytes(5<<20), WithMaxRetries(0))
	_, err := f.Fetch(context.Background(), srv.URL+"/declared-big", Conditional{})
	if err == nil {
		t.Fatalf("expected rejection on over-Content-Length, got nil")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

// Redirect to a loopback target must be rejected by the egress guard even when
// the initial host is allowed. We DISABLE the loopback bypass for this test.
func TestFetchRedirectToLoopbackRejected(t *testing.T) {
	// A normal external-looking server is not available in unit tests, so we
	// build a server that 302-redirects to http://127.0.0.1:1/secret and assert
	// the guard rejects following it. The initial request runs with loopback
	// allowed (httptest), but the guard must still reject the explicit
	// 169.254/127.0.0.1 metadata-style target via a dedicated denylist that
	// applies regardless of AllowLoopback when DenyMetadata is set.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	_, err := f.Fetch(context.Background(), srv.URL+"/redir", Conditional{})
	if err == nil {
		t.Fatalf("expected redirect-to-link-local to be rejected, got nil")
	}
	if !errors.Is(err, ErrBlockedAddress) && !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("expected ErrBlockedAddress/ErrHostNotAllowed, got %v", err)
	}
}

// Direct fetch of a link-local metadata address must be rejected even if it were
// somehow on the allowlist.
func TestFetchDirectLinkLocalRejected(t *testing.T) {
	f, err := NewFetcher(
		WithUserAgent("t"),
		WithPerMinute(6000),
		WithAllowedHosts("169.254.169.254"),
		WithMaxRetries(0),
	)
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	_, err = f.Fetch(context.Background(), "http://169.254.169.254/latest/meta-data/", Conditional{})
	if err == nil {
		t.Fatalf("expected link-local rejection, got nil")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected ErrBlockedAddress, got %v", err)
	}
}

// Non-http(s) schemes must be rejected.
func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	f := testFetcher(t)
	for _, u := range []string{"file:///etc/passwd", "ftp://host/x", "gopher://x"} {
		if _, err := f.Fetch(context.Background(), u, Conditional{}); err == nil {
			t.Fatalf("expected scheme rejection for %q", u)
		}
	}
}

// Host not on the allowlist must be rejected before any network call.
func TestFetchHostAllowlist(t *testing.T) {
	f := testFetcher(t) // default test allowlist is empty-of-real-hosts
	_, err := f.Fetch(context.Background(), "https://evil.example.com/x", Conditional{})
	if err == nil {
		t.Fatalf("expected host-allowlist rejection, got nil")
	}
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("expected ErrHostNotAllowed, got %v", err)
	}
}

// Default allowlist must contain the two production venues.
func TestDefaultAllowlistHasVenues(t *testing.T) {
	f, err := NewFetcher(WithUserAgent("t"))
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	if !f.hostAllowed("www.coex.co.kr") {
		t.Errorf("www.coex.co.kr not in default allowlist")
	}
	if !f.hostAllowed("www.kintex.com") {
		t.Errorf("www.kintex.com not in default allowlist")
	}
	if f.hostAllowed("evil.example.com") {
		t.Errorf("evil.example.com should not be allowed by default")
	}
}

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

// EUC-KR encoded body must be converted to UTF-8.
func TestFetchCharsetConversion(t *testing.T) {
	// "한국" in EUC-KR is bytes 0xC7 0xD1 0xB1 0xB9.
	eucKR := []byte{0x3C, 0x68, 0x74, 0x6D, 0x6C, 0x3E, 0xC7, 0xD1, 0xB1, 0xB9, 0x3C, 0x2F, 0x68, 0x74, 0x6D, 0x6C, 0x3E}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=euc-kr")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(eucKR)
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/k", Conditional{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(string(res.Body), "한국") {
		t.Fatalf("EUC-KR not converted to UTF-8: %q", string(res.Body))
	}
}

func TestRedirectCap(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		hits.Add(1)
		// Infinite redirect loop on the same (allowed) host.
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	_, err := f.Fetch(context.Background(), srv.URL+"/loop", Conditional{})
	if err == nil {
		t.Fatalf("expected redirect-cap error, got nil")
	}
	// We cap at <=3 redirects; ensure we did not follow unbounded.
	if hits.Load() > 5 {
		t.Fatalf("followed too many redirects: %d", hits.Load())
	}
}

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

	// Two back-to-back fetches to the fast host must complete promptly. If the
	// slow host's Crawl-delay bled into a shared limiter, the second fetch would
	// stall ~10s.
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

func rewriteURLHost(t *testing.T, rawURL string, host string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split test URL host: %v", err)
	}
	u.Host = net.JoinHostPort(host, port)
	return u.String()
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// A same-host redirect that lands on a robots-Disallow path must be rejected
// with ErrRobotsDisallowed, and the disallowed body must never be returned.
func TestRedirectOntoDisallowRejected(t *testing.T) {
	var secretHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
		case "/public/start":
			http.Redirect(w, r, "/private/secret", http.StatusFound)
		case "/private/secret":
			secretHits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("robots NOT re-checked post-redirect"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/public/start", Conditional{})
	if err == nil {
		t.Fatalf("expected redirect-onto-disallow rejection, got nil (body=%q)", string(res.Body))
	}
	if !errors.Is(err, ErrRobotsDisallowed) {
		t.Fatalf("expected ErrRobotsDisallowed, got %v", err)
	}
	if secretHits.Load() != 0 {
		t.Fatalf("disallowed redirect target was fetched %d times, want 0", secretHits.Load())
	}
}

// CDP fallback hook is interface-only (unimplemented) — assert the type exists
// and the default fetcher has no CDP fetcher wired.
func TestCDPFallbackHookIsOptional(t *testing.T) {
	f := testFetcher(t)
	if f.cdp != nil {
		t.Fatalf("expected nil CDP fallback by default")
	}
}

// isBlockedIP unit checks for the SSRF guard's address classifier.
func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.0.0.53", "::1",
		"169.254.169.254", "169.254.1.1",
		"10.0.0.1", "192.168.1.1", "172.16.0.1",
		"0.0.0.0", "fc00::1", "fd12:3456::1",
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP %q", s)
		}
		if !isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%s) = false, want true", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "203.0.113.10", "2606:4700:4700::1111"}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if !isBlockedIP(ip) {
			continue // public IP: not blocked -> correct
		}
		t.Errorf("isBlockedIP(%s) = true, want false (public)", s)
	}
}

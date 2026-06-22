package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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

// CDP fallback hook is interface-only (unimplemented) - assert the type exists
// and the default fetcher has no CDP fetcher wired.
func TestCDPFallbackHookIsOptional(t *testing.T) {
	f := testFetcher(t)
	if f.cdp != nil {
		t.Fatalf("expected nil CDP fallback by default")
	}
}

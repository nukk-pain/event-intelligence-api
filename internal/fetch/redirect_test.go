package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRedirectCap(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		hits.Add(1)
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	_, err := f.Fetch(context.Background(), srv.URL+"/loop", Conditional{})
	if err == nil {
		t.Fatalf("expected redirect-cap error, got nil")
	}
	if hits.Load() > 5 {
		t.Fatalf("followed too many redirects: %d", hits.Load())
	}
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

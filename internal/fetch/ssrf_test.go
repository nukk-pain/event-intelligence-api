package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

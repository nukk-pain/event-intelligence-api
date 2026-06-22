package fetch

import (
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
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

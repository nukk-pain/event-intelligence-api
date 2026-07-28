package aiia

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func testFetcher(t *testing.T, srv *httptest.Server) *fetch.Fetcher {
	t.Helper()
	host := srv.URL[len("http://"):]
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	f, err := fetch.NewFetcher(
		fetch.WithUserAgent("event-intelligence-test/0.1"),
		fetch.WithAllowedHosts(host),
		fetch.WithAllowLoopback(true),
		fetch.WithMaxRetries(0),
		fetch.WithPerMinute(100000),
	)
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	return f
}

func serveFixture(t *testing.T, name string) http.HandlerFunc {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}
}

// The live board mixes event announcements with recruiting and administrative
// notices; only titles matching the event keyword filter may become Refs.
func TestDiscoverKeepsOnlyEventLikePosts(t *testing.T) {
	srv := httptest.NewServer(serveFixture(t, "list.html"))
	defer srv.Close()

	s := New(WithBaseURL(srv.URL))
	refs, err := s.Discover(context.Background(), testFetcher(t, srv))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) == 0 {
		t.Fatalf("no refs discovered")
	}

	byID := map[string]string{}
	for _, r := range refs {
		byID[r.EventID] = r.URL
	}
	// 408 Tech-Day 행사, 406 설명회 are events on the captured list page.
	for _, want := range []string{"aiia-408", "aiia-406"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing expected event ref %s (got %v)", want, byID)
		}
	}
	// 283 오픈채팅방 안내, 397/396 모집 posts must not be promoted.
	for _, no := range []string{"aiia-283", "aiia-397", "aiia-396"} {
		if _, ok := byID[no]; ok {
			t.Errorf("non-event post %s must be filtered out", no)
		}
	}
	for id, u := range byID {
		if !strings.HasPrefix(u, srv.URL) || !strings.Contains(u, "mode=VIEW") {
			t.Errorf("%s has non-absolute or non-detail URL %q", id, u)
		}
	}
}

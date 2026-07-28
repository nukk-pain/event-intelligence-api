package akei

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// Every month/page request answers with the same captured July listing; the
// wr_id dedupe must both collapse those repeats and stop paging early, so the
// unique refs equal exactly the fixture's distinct wr_ids.
func TestDiscoverDedupesAcrossMonthsAndPages(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "list.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	fixed := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	s := New(WithBaseURL(srv.URL), WithClock(func() time.Time { return fixed }))
	refs, err := s.Discover(context.Background(), testFetcher(t, srv))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(refs) != 15 {
		t.Fatalf("refs = %d, want 15 distinct wr_ids from fixture", len(refs))
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if !strings.HasPrefix(r.EventID, "akei-") {
			t.Errorf("event id %q lacks akei- prefix", r.EventID)
		}
		if seen[r.EventID] {
			t.Errorf("duplicate ref %s", r.EventID)
		}
		seen[r.EventID] = true
		if !strings.HasPrefix(r.URL, srv.URL) || !strings.Contains(r.URL, "wr_id=") {
			t.Errorf("ref URL %q not an absolute detail URL", r.URL)
		}
	}
	// 3 months, but every page repeats the same ids, so paging must stop after
	// page 1 + one no-new page per month: at most 2 requests for month one and
	// 1 no-new page for each later month.
	if requests > 6 {
		t.Errorf("requests = %d, early-stop paging failed", requests)
	}
}

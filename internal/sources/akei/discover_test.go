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
		// Count listing fetches only; the fetcher also pulls robots.txt.
		if strings.Contains(r.URL.Path, "board.php") {
			requests++
		}
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
	// Every page repeats the same ids, so paging must stop after page 1 + one
	// no-new page per month: 2 listing fetches for month one (page 1 yields the
	// ids, page 2 yields nothing new) and 1 for each later month.
	if requests > monthsAhead+1 {
		t.Errorf("requests = %d, early-stop paging failed", requests)
	}
}

// The window is what bounds lead time for aT/EXCO/BEXCO-class venues, which no
// other source covers. A month that falls inside monthsAhead must be reachable:
// 2026 데이터센터코리아 (11/04) was missed because a 3-month window starting in
// August stopped at October. Each month view here answers with its own wr_id, so
// a ref for month N proves that month was actually requested.
func TestDiscoverReachesTheFullWindow(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ym := q.Get("searchYear") + "-" + q.Get("searchMonth")
		id := strings.ReplaceAll(ym, "-", "")
		if q.Get("page") == "1" {
			asked = append(asked, ym)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<a href="/bbs/board.php?bo_table=schedule&wr_id=` + id + `">x</a>`))
			return
		}
		// Later pages repeat page 1, so discovery stops on the first no-new page.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<a href="/bbs/board.php?bo_table=schedule&wr_id=` + id + `">x</a>`))
	}))
	defer srv.Close()

	// August 2026 — the month the miss was found in.
	fixed := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	s := New(WithBaseURL(srv.URL), WithClock(func() time.Time { return fixed }))
	refs, err := s.Discover(context.Background(), testFetcher(t, srv))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	want := []string{"2026-08", "2026-09", "2026-10", "2026-11", "2026-12", "2027-01"}
	want = want[:monthsAhead]
	if len(asked) != len(want) {
		t.Fatalf("months requested = %v, want %v", asked, want)
	}
	for i, ym := range want {
		if asked[i] != ym {
			t.Errorf("month %d requested = %s, want %s", i, asked[i], ym)
		}
	}

	got := map[string]bool{}
	for _, r := range refs {
		got[r.EventID] = true
	}
	// The November board is where 데이터센터코리아 sits.
	if !got["akei-202611"] {
		t.Errorf("November 2026 not discovered; refs = %v", refs)
	}
	if len(refs) != len(want) {
		t.Errorf("refs = %d, want one per month in the window (%d)", len(refs), len(want))
	}
}

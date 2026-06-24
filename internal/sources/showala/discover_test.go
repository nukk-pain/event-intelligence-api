package showala

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func testFetcher(t *testing.T) *fetch.Fetcher {
	t.Helper()
	f, err := fetch.NewFetcher(
		fetch.WithAllowedHosts("127.0.0.1"),
		fetch.WithAllowLoopback(true),
		fetch.WithPerMinute(100000), // effectively no rate limiting in tests
	)
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	return f
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// Discover keeps only UPCOMING KINTEX rows, follows AJAX pagination with a
// Referer, and early-stops at the upcoming→past pivot (tolerating an isolated
// junk past row).
func TestDiscoverKintexScopeEarlyStopAndReferer(t *testing.T) {
	list := readFixture(t, "list_page1.html")
	proc := readFixture(t, "proc_page2.html")

	var sawReferer string
	var procPagesServed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusNotFound)
		case "/ex/ex_list.php":
			_, _ = w.Write(list)
		case "/ex/ex_proc.php":
			sawReferer = r.Header.Get("Referer")
			page := r.URL.Query().Get("page")
			procPagesServed = append(procPagesServed, page)
			if page == "2" {
				_, _ = w.Write(proc)
				return
			}
			// Any page beyond 2 must not be reached (early-stop). Serve an empty
			// fragment so a regression surfaces as extra refs/pages, not a hang.
			_, _ = w.Write([]byte(":::0:::5"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	listFull := srv.URL + "/ex/ex_list.php?place[]=1"
	s := New(
		WithListURL(listFull),
		WithProcURL(srv.URL+"/ex/ex_proc.php"),
		WithClock(func() time.Time { return time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC) }),
	)

	refs, err := s.Discover(context.Background(), testFetcher(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	gotIDs := make([]string, len(refs))
	for i, r := range refs {
		gotIDs[i] = r.EventID
	}
	want := []string{"showala-2106", "showala-3219", "showala-2107"}
	if len(gotIDs) != len(want) {
		t.Fatalf("refs = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("refs[%d] = %q, want %q (all: %v)", i, gotIDs[i], want[i], gotIDs)
		}
	}

	// Detail URL is the canonical ex_detail.php?idx= link.
	if refs[0].URL != "https://showala.com/ex/ex_detail.php?idx=2106" {
		t.Fatalf("ref URL = %q", refs[0].URL)
	}

	// The AJAX endpoint must have received the listing URL as Referer.
	if sawReferer != listFull {
		t.Fatalf("Referer = %q, want %q", sawReferer, listFull)
	}

	// Early-stop must keep paging shallow: only page 2 is fetched, page 3+ is not.
	if len(procPagesServed) != 1 || procPagesServed[0] != "2" {
		t.Fatalf("proc pages served = %v, want [2] (early-stop should prevent page 3)", procPagesServed)
	}
}

// A non-200 listing status is an error, never a silently-empty discovery.
func TestDiscoverListStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	s := New(
		WithListURL(srv.URL+"/ex/ex_list.php?place[]=1"),
		WithProcURL(srv.URL+"/ex/ex_proc.php"),
		WithClock(func() time.Time { return time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC) }),
	)
	if _, err := s.Discover(context.Background(), testFetcher(t)); err == nil {
		t.Fatal("expected error on 403 listing, got nil")
	}
}

func TestSplitFooter(t *testing.T) {
	html, next, total := splitFooter([]byte("<li>x</li>\n:::3:::163"))
	if next != 3 || total != 163 {
		t.Fatalf("next/total = %d/%d, want 3/163", next, total)
	}
	if string(html) != "<li>x</li>\n" {
		t.Fatalf("html part = %q", string(html))
	}
	// No footer (server-rendered first page): returned unchanged, zero counts.
	h2, n2, t2 := splitFooter([]byte("<li>y</li>"))
	if n2 != 0 || t2 != 0 || string(h2) != "<li>y</li>" {
		t.Fatalf("no-footer case: html=%q next=%d total=%d", string(h2), n2, t2)
	}
}

package kintex

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// Discover must satisfy sources.Source (compile-time + behavioral).
var _ sources.Source = (*Source)(nil)

// testFetcher builds a Fetcher allowed to hit the loopback httptest server.
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
		// Lift the rate limit so the loopback suite is fast; production keeps
		// the polite default.
		fetch.WithPerMinute(100000),
	)
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	return f
}

// Golden: the saved real list.do response yields exactly the 9 seqs we observed
// during the 2026-06-21 spike, each mapped to a kintex- event id and a
// view.do?seq= detail URL.
func TestParseListingGolden(t *testing.T) {
	refs, err := parseListing(readFixture(t, "list.html"))
	if err != nil {
		t.Fatalf("parseListing: %v", err)
	}

	wantSeqs := []string{
		"2025044109",
		"26030510",
		"26033102",
		"26040707",
		"26041303",
		"26051808",
		"26052601",
		"26060201",
		"26060206",
	}

	if len(refs) != len(wantSeqs) {
		t.Fatalf("got %d refs, want %d: %+v", len(refs), len(wantSeqs), refs)
	}

	gotIDs := make([]string, len(refs))
	byID := make(map[string]string, len(refs))
	for i, r := range refs {
		gotIDs[i] = r.EventID
		byID[r.EventID] = r.URL
	}
	sort.Strings(gotIDs)

	for _, seq := range wantSeqs {
		id := "kintex-" + seq
		url, ok := byID[id]
		if !ok {
			t.Errorf("missing ref for seq %s (id %s)", seq, id)
			continue
		}
		wantURL := detailBase + "?seq=" + seq
		if url != wantURL {
			t.Errorf("seq %s: url = %q, want %q", seq, url, wantURL)
		}
	}
}

// Dedup: the same seq appearing twice in the listing produces a single Ref.
func TestParseListingDedup(t *testing.T) {
	html := `<a href="javascript:fnView('./view.do', 26060201);">a</a>` +
		`<a href="javascript:fnView('./view.do', 26060201);">dup</a>`
	refs, err := parseListing([]byte(html))
	if err != nil {
		t.Fatalf("parseListing: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1 (deduped): %+v", len(refs), refs)
	}
	if refs[0].EventID != "kintex-26060201" {
		t.Errorf("EventID = %q, want kintex-26060201", refs[0].EventID)
	}
}

// Empty listing: a valid page with no events returns zero refs, no error.
func TestParseListingEmpty(t *testing.T) {
	refs, err := parseListing(readFixture(t, "empty.html"))
	if err != nil {
		t.Fatalf("parseListing on empty: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("got %d refs on empty listing, want 0: %+v", len(refs), refs)
	}
}

// Malformed: broken fnView calls / non-numeric seqs are skipped, not fatal.
func TestParseListingMalformed(t *testing.T) {
	refs, err := parseListing(readFixture(t, "malformed.html"))
	if err != nil {
		t.Fatalf("parseListing on malformed: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("got %d refs on malformed listing, want 0: %+v", len(refs), refs)
	}
}

// ID() is the stable adapter id used by the registry.
func TestID(t *testing.T) {
	if got := New().ID(); got != "kintex" {
		t.Errorf("ID() = %q, want kintex", got)
	}
}

// 404 negative case (PLAN Task 2.3, line 148; AC-10): the shared Fetcher returns
// err==nil with the error-page body for a 404, so without a status guard a hard
// fetch failure would silently yield zero refs and be indistinguishable from a
// legitimately empty listing — defeating the disappearance circuit breaker.
// Discover must instead surface the 404 as an error.
func TestDiscover404Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A real WAF/restructure 404 still returns a body that contains no
		// fnView handlers — exactly what would otherwise parse to zero refs.
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html><body>Not Found</body></html>"))
	}))
	t.Cleanup(srv.Close)

	s := New(WithListURL(srv.URL))
	f := testFetcher(t, srv)

	refs, err := s.Discover(context.Background(), f)
	if err == nil {
		t.Fatalf("Discover on 404 returned err=nil, refs=%+v; want error", refs)
	}
	if refs != nil {
		t.Errorf("Discover on 404 returned non-nil refs %+v, want nil", refs)
	}
}

// 403 negative case: a WAF block (403) likewise must error rather than yield
// zero refs.
func TestDiscover403Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><body>Forbidden</body></html>"))
	}))
	t.Cleanup(srv.Close)

	s := New(WithListURL(srv.URL))
	f := testFetcher(t, srv)

	if _, err := s.Discover(context.Background(), f); err == nil {
		t.Fatalf("Discover on 403 returned err=nil; want error")
	}
}

// 200 happy path through Discover (status-level): a real 200 listing served over
// loopback parses to the expected refs, proving the status guard does not reject
// the legitimate path.
func TestDiscover200ParsesListing(t *testing.T) {
	body := readFixture(t, "list.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	s := New(WithListURL(srv.URL))
	f := testFetcher(t, srv)

	refs, err := s.Discover(context.Background(), f)
	if err != nil {
		t.Fatalf("Discover on 200: %v", err)
	}
	if len(refs) != 9 {
		t.Fatalf("Discover on 200 returned %d refs, want 9: %+v", len(refs), refs)
	}
}

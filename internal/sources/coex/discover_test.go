package coex

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

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
		// Lift the outbound rate limit so the loopback fixture suite is fast;
		// production keeps the polite default per-minute rate.
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
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=UTF-8")
		_, _ = w.Write(body)
	}
}

// The six verbatim detail URLs (path only) the trimmed shard fixture contains.
var shardURLPaths = []string{
	"/exhibitions/%ec%a0%9c5%ed%9a%8c-%ea%b5%ad%ec%a0%9c%ec%b4%89%eb%a7%a4%ec%97%b0%ec%86%8c%ed%95%99%ec%88%a0%eb%8c%80%ed%9a%8ciwcc/",
	"/exhibitions/%ec%84%b8%ea%b3%84%ec%9b%90%ec%a0%84%ec%82%ac%ec%97%85%ec%9e%90%ed%98%91%ed%9a%8cwano-%ea%b2%a9%eb%85%84%ec%b4%9d%ed%9a%8c/",
	"/exhibitions/%ec%95%84%ec%8b%9c%ec%95%84%ed%83%9c%ed%8f%89%ec%96%91%ec%b9%98%ea%b3%bc%ec%97%b0%eb%a7%b9%ec%b4%9d%ed%9a%8capdc/",
	"/exhibitions/%ec%95%84%ec%8b%9c%ec%95%84%ec%84%9d%ec%9c%a0%ea%b3%b5%ec%97%85%ed%99%94%ed%95%99%ed%9a%8c%ec%9d%98apic/",
	"/exhibitions/2002-fira-congres/",
	"/exhibitions/%ec%95%84%ec%8b%9c%ec%95%84-%ed%83%9c%ed%8f%89%ec%96%91-its-%ec%84%9c%ec%9a%b8%eb%8c%80%ed%9a%8c/",
}

// expected slugs (last path segment) for the six fixture entries.
var shardSlugs = []string{
	"%ec%a0%9c5%ed%9a%8c-%ea%b5%ad%ec%a0%9c%ec%b4%89%eb%a7%a4%ec%97%b0%ec%86%8c%ed%95%99%ec%88%a0%eb%8c%80%ed%9a%8ciwcc",
	"%ec%84%b8%ea%b3%84%ec%9b%90%ec%a0%84%ec%82%ac%ec%97%85%ec%9e%90%ed%98%91%ed%9a%8cwano-%ea%b2%a9%eb%85%84%ec%b4%9d%ed%9a%8c",
	"%ec%95%84%ec%8b%9c%ec%95%84%ed%83%9c%ed%8f%89%ec%96%91%ec%b9%98%ea%b3%bc%ec%97%b0%eb%a7%b9%ec%b4%9d%ed%9a%8capdc",
	"%ec%95%84%ec%8b%9c%ec%95%84%ec%84%9d%ec%9c%a0%ea%b3%b5%ec%97%85%ed%99%94%ed%95%99%ed%9a%8c%ec%9d%98apic",
	"2002-fira-congres",
	"%ec%95%84%ec%8b%9c%ec%95%84-%ed%83%9c%ed%8f%89%ec%96%91-its-%ec%84%9c%ec%9a%b8%eb%8c%80%ed%9a%8c",
}

// refByEventID indexes refs by EventID for assertions.
func refByEventID(refs []sources.Ref) map[string]sources.Ref {
	m := make(map[string]sources.Ref, len(refs))
	for _, r := range refs {
		m[r.EventID] = r
	}
	return m
}

func TestCOEXSourceID(t *testing.T) {
	s := New()
	if s.ID() != "coex" {
		t.Fatalf("ID() = %q, want %q", s.ID(), "coex")
	}
}

// Discover must satisfy sources.Source (compile-time + behavioral).
var _ sources.Source = (*Source)(nil)

func TestDiscoverRealIndexFiveShards(t *testing.T) {
	srv := sitemapOnlyFixtureServer(t, "wp-sitemap-index-real.xml")
	s := New(
		WithBaseURL(srv.URL),
		WithClock(func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC) }),
	)
	f := testFetcher(t, srv)

	refs, err := s.Discover(context.Background(), f)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(refs) != len(shardSlugs) {
		t.Fatalf("len(refs) = %d, want %d", len(refs), len(shardSlugs))
	}

	// The real index lists 5 exhibitions POST shards. Only shard 1 and 2 are
	// served (mux); shards 3..5 return 404 and must be tolerated as the
	// secondary fallback without failing discovery, since shard 1 == shard 2
	// the deduped result is exactly the six fixture entries.

	byID := refByEventID(refs)
	for i, slug := range shardSlugs {
		id := "coex-" + slug
		r, ok := byID[id]
		if !ok {
			t.Fatalf("missing ref EventID=%q", id)
		}
		wantURL := srv.URL + shardURLPaths[i]
		if r.URL != wantURL {
			t.Errorf("EventID %q URL = %q, want %q", id, r.URL, wantURL)
		}
	}
}

func TestDiscoverIndexWithTwoShards(t *testing.T) {
	// N != 5: prove discovery follows the index <loc> entries dynamically and
	// ignores taxonomies-exhibitions decoys + non-exhibition shards.
	srv := sitemapOnlyFixtureServer(t, "wp-sitemap-index-2shards.xml")
	s := New(
		WithBaseURL(srv.URL),
		WithClock(func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC) }),
	)
	f := testFetcher(t, srv)

	refs, err := s.Discover(context.Background(), f)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(refs) != len(shardSlugs) {
		t.Fatalf("len(refs) = %d, want %d", len(refs), len(shardSlugs))
	}

	gotSlugs := make([]string, 0, len(refs))
	for _, r := range refs {
		if !strings.HasPrefix(r.EventID, "coex-") {
			t.Errorf("EventID %q lacks coex- prefix", r.EventID)
		}
		gotSlugs = append(gotSlugs, strings.TrimPrefix(r.EventID, "coex-"))
	}
	sort.Strings(gotSlugs)

	want := append([]string{}, shardSlugs...)
	sort.Strings(want)
	for i := range want {
		if gotSlugs[i] != want[i] {
			t.Fatalf("slug[%d] = %q, want %q", i, gotSlugs[i], want[i])
		}
	}
}

func TestCurrentScheduleRefs(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "full-schedules.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	refs := currentScheduleRefs("https://www.coex.co.kr", body)
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].EventID != "coex-2026-%ec%84%9c%ec%9a%b8-%ed%94%84%eb%a6%ac%eb%af%b8%ec%97%84-%ed%85%8d%ec%8a%a4%ed%83%80%ec%9d%bc" {
		t.Fatalf("refs[0].EventID = %q", refs[0].EventID)
	}
	if refs[0].URL != "https://www.coex.co.kr/exhibitions/2026-%ec%84%9c%ec%9a%b8-%ed%94%84%eb%a6%ac%eb%af%b8%ec%97%84-%ed%85%8d%ec%8a%a4%ed%83%80%ec%9d%bc/" {
		t.Fatalf("refs[0].URL = %q", refs[0].URL)
	}
}

func TestCurrentSchedulePaths(t *testing.T) {
	got := currentSchedulePaths(time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC))
	want := "/event/full-schedules/?search_start_date=2026.06.21&search_end_date=2027.06.21&list_type=LIST"
	if len(got) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(got))
	}
	if got[1] != want {
		t.Fatalf("range path = %q, want %q", got[1], want)
	}
}

func TestSchedulePagePaths(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "full-schedules.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	paths := schedulePagePaths("https://www.coex.co.kr", body)
	if len(paths) != 2 {
		t.Fatalf("len(paths) = %d, want 2", len(paths))
	}
	if paths[1] != "/event/full-schedules/?var_page=2&search_start_date=2026.08.01&search_end_date=2026.08.31&list_type=LIST" {
		t.Fatalf("paths[1] = %q", paths[1])
	}
}

func TestExhibitionsShardLocs(t *testing.T) {
	// Unit: shard parsing extracts exactly the six detail URLs.
	body, err := os.ReadFile(filepath.Join("testdata", "wp-sitemap-posts-exhibitions-1.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	locs, err := parseURLSet(body)
	if err != nil {
		t.Fatalf("parseURLSet: %v", err)
	}
	if len(locs) != len(shardURLPaths) {
		t.Fatalf("len(locs) = %d, want %d", len(locs), len(shardURLPaths))
	}
	for i, loc := range locs {
		want := "https://www.coex.co.kr" + shardURLPaths[i]
		if loc != want {
			t.Errorf("loc[%d] = %q, want %q", i, loc, want)
		}
	}
}

func TestExhibitionShardURLsFromIndex(t *testing.T) {
	// Unit: the index parser keeps only wp-sitemap-posts-exhibitions-N shards
	// and rejects taxonomies-exhibitions + other post types.
	body, err := os.ReadFile(filepath.Join("testdata", "wp-sitemap-index-real.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	shards, err := exhibitionShardLocs(body)
	if err != nil {
		t.Fatalf("exhibitionShardLocs: %v", err)
	}
	if len(shards) != 5 {
		t.Fatalf("len(shards) = %d, want 5", len(shards))
	}
	for _, loc := range shards {
		if !strings.Contains(loc, "wp-sitemap-posts-exhibitions-") {
			t.Errorf("unexpected shard kept: %q", loc)
		}
		if strings.Contains(loc, "taxonomies") {
			t.Errorf("taxonomy decoy not filtered: %q", loc)
		}
	}
}

func TestSlugFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://www.coex.co.kr/exhibitions/2002-fira-congres/", "2002-fira-congres"},
		{"https://www.coex.co.kr/exhibitions/2002-fira-congres", "2002-fira-congres"},
		{"https://www.coex.co.kr/exhibitions/%ec%a0%9c5/", "%ec%a0%9c5"},
	}
	for _, c := range cases {
		got, err := slugFromURL(c.in)
		if err != nil {
			t.Fatalf("slugFromURL(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("slugFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

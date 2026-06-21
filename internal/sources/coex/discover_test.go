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

// fixtureServer serves files from testdata/ at the paths the WordPress sitemap
// uses (mirroring www.coex.co.kr). Unknown paths return 404 so a 404 on a
// non-listed shard is exercised as the secondary fallback, not the primary path.
func fixtureServer(t *testing.T, indexFixture string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-sitemap.xml", serveFixture(t, indexFixture))
	mux.HandleFunc("/wp-sitemap-posts-exhibitions-1.xml", serveFixture(t, "wp-sitemap-posts-exhibitions-1.xml"))
	// exhibitions-2 deliberately reuses the same fixture body so the 2-shard
	// index yields the union (deduped) of detail URLs across both shards.
	mux.HandleFunc("/wp-sitemap-posts-exhibitions-2.xml", serveFixture(t, "wp-sitemap-posts-exhibitions-1.xml"))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
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
	srv := fixtureServer(t, "wp-sitemap-index-real.xml")
	s := New(WithBaseURL(srv.URL))
	f := testFetcher(t, srv)

	refs, err := s.Discover(context.Background(), f)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// The real index lists 5 exhibitions POST shards. Only shard 1 and 2 are
	// served (mux); shards 3..5 return 404 and must be tolerated as the
	// secondary fallback without failing discovery, since shard 1 == shard 2
	// the deduped result is exactly the six fixture entries.
	if len(refs) != len(shardSlugs) {
		t.Fatalf("len(refs) = %d, want %d", len(refs), len(shardSlugs))
	}

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
	srv := fixtureServer(t, "wp-sitemap-index-2shards.xml")
	s := New(WithBaseURL(srv.URL))
	f := testFetcher(t, srv)

	refs, err := s.Discover(context.Background(), f)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Two exhibitions shards served from the same body → deduped to six.
	if len(refs) != len(shardSlugs) {
		t.Fatalf("len(refs) = %d, want %d (deduped across 2 shards)", len(refs), len(shardSlugs))
	}

	gotSlugs := make([]string, 0, len(refs))
	for _, r := range refs {
		if !strings.HasPrefix(r.EventID, "coex-") {
			t.Errorf("EventID %q lacks coex- prefix", r.EventID)
		}
		gotSlugs = append(gotSlugs, strings.TrimPrefix(r.EventID, "coex-"))
	}
	sort.Strings(gotSlugs)

	want := append([]string(nil), shardSlugs...)
	sort.Strings(want)
	for i := range want {
		if gotSlugs[i] != want[i] {
			t.Fatalf("slug[%d] = %q, want %q", i, gotSlugs[i], want[i])
		}
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

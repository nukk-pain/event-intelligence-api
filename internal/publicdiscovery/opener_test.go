package publicdiscovery

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/fetch"
)

// openerFixture is one httptest origin plus the opener pointed at it. pageHits
// counts every request that was not for robots.txt, so a test can prove a
// refused open never reached the page at all.
type openerFixture struct {
	opener   *AgentPageOpener
	server   string
	pageHits *atomic.Int32
}

func newOpenerFixture(t *testing.T, robots string, page http.HandlerFunc) openerFixture {
	t.Helper()
	hits := &atomic.Int32{}
	server := newFixtureServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			writeDocument(t, w, "text/plain", robots)
			return
		}
		hits.Add(1)
		page(w, r)
	}))
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("fixture", server.URL)}, DefaultLimits())
	opener, err := NewAgentPageOpenerWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentPageOpenerWithProvider() error = %v", err)
	}
	return openerFixture{opener: opener, server: server.URL, pageHits: hits}
}

const allowAllRobots = "User-agent: *\nAllow: /\n"

func TestAgentPageOpener_returns_bounded_redacted_title_and_snippet(t *testing.T) {
	// Given
	fixture := newOpenerFixture(t, allowAllRobots, func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html; charset=utf-8", `<html><head><title>  Robot Expo 2027  </title></head>`+
			`<body><script>var leak = "script text";</script><style>body{color:red}</style>`+
			`<p>문의 desk@example.com 02-1234-5678</p>`+
			`<p>Official   expo   listing</p></body></html>`)
	})

	// When
	page, err := fixture.opener.Open(context.Background(), fixture.server+"/events/robot-expo")

	// Then
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if page.Title != "Robot Expo 2027" {
		t.Fatalf("title = %q, want the normalized document title", page.Title)
	}
	if !strings.Contains(page.Snippet, "Official expo listing") {
		t.Fatalf("snippet = %q, want whitespace-normalized body text", page.Snippet)
	}
	if strings.Contains(page.Snippet, "desk@example.com") || strings.Contains(page.Snippet, "02-1234-5678") {
		t.Fatalf("snippet leaked contact details: %q", page.Snippet)
	}
	if !strings.Contains(page.Snippet, "[email removed]") || !strings.Contains(page.Snippet, "[phone removed]") {
		t.Fatalf("snippet = %q, want StripContacts placeholders", page.Snippet)
	}
	if strings.Contains(page.Snippet, "script text") || strings.Contains(page.Snippet, "color:red") {
		t.Fatalf("snippet included script or style text: %q", page.Snippet)
	}
	// The page is not parsed for structure: inventing a date or a location from
	// a free-text page is exactly what the open action must not do.
	if page.Date != "" || page.Location != "" {
		t.Fatalf("page = %#v, want no invented date or location", page)
	}
}

func TestAgentPageOpener_bounds_the_snippet_it_returns(t *testing.T) {
	// Given
	fixture := newOpenerFixture(t, allowAllRobots, func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", "<html><head><title>"+strings.Repeat("가", 900)+"</title></head><body>"+
			strings.Repeat("행사 ", 4000)+"</body></html>")
	})

	// When
	page, err := fixture.opener.Open(context.Background(), fixture.server+"/long")

	// Then
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := utf8.RuneCountInString(page.Snippet); got != maxOpenedSnippetRunes {
		t.Fatalf("snippet runes = %d, want the opener bound %d", got, maxOpenedSnippetRunes)
	}
	if got := utf8.RuneCountInString(page.Title); got != maxOpenedTitleRunes {
		t.Fatalf("title runes = %d, want the opener bound %d", got, maxOpenedTitleRunes)
	}
}

func TestAgentPageOpener_refuses_a_robots_disallowed_page_without_fetching_it(t *testing.T) {
	// Given
	fixture := newOpenerFixture(t, "User-agent: *\nDisallow: /\n", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", "<html><head><title>Forbidden</title></head><body>secret</body></html>")
	})

	// When
	page, err := fixture.opener.Open(context.Background(), fixture.server+"/events/robot-expo")

	// Then
	if !errors.Is(err, fetch.ErrRobotsDisallowed) {
		t.Fatalf("error = %v, want fetch.ErrRobotsDisallowed", err)
	}
	if page != (agent.OpenedPage{}) {
		t.Fatalf("page = %#v, want the zero page on a refused open", page)
	}
	if hits := fixture.pageHits.Load(); hits != 0 {
		t.Fatalf("page hits = %d, want the page never fetched", hits)
	}
}

func TestAgentPageOpener_refuses_a_body_over_the_crawl_size_limit(t *testing.T) {
	// Given
	fixture := newOpenerFixture(t, allowAllRobots, func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", "<html><body>"+strings.Repeat("a", 600<<10)+"</body></html>")
	})

	// When
	_, err := fixture.opener.Open(context.Background(), fixture.server+"/huge")

	// Then
	if !errors.Is(err, fetch.ErrBodyTooLarge) {
		t.Fatalf("error = %v, want fetch.ErrBodyTooLarge", err)
	}
}

func TestAgentPageOpener_refuses_a_non_HTML_document(t *testing.T) {
	// Given
	fixture := newOpenerFixture(t, allowAllRobots, func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/json", `{"title":"Robot Expo"}`)
	})

	// When
	_, err := fixture.opener.Open(context.Background(), fixture.server+"/feed.json")

	// Then
	if !errors.Is(err, ErrOpenUnsupportedContent) {
		t.Fatalf("error = %v, want ErrOpenUnsupportedContent", err)
	}
}

// The opener owns the same SSRF boundary as the crawler: a loopback or private
// target is refused by URL, before any name resolution or connection.
func TestAgentPageOpener_refuses_private_targets_before_any_network_call(t *testing.T) {
	// Given
	hits := &atomic.Int32{}
	server := newFixtureServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeDocument(t, w, "text/html", "<html><head><title>Internal</title></head><body>internal</body></html>")
	}))
	provider := newPublicOnlyProvider(t)
	opener, err := NewAgentPageOpenerWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentPageOpenerWithProvider() error = %v", err)
	}
	targets := []string{
		server.URL + "/internal",
		"http://127.0.0.1/admin",
		"http://10.0.0.5/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://user:pass@events.example/calendar",
		"file:///etc/passwd",
		"http://intranet.local/events",
	}

	for _, target := range targets {
		// When
		_, err := opener.Open(context.Background(), target)

		// Then
		if !errors.Is(err, ErrOpenURLNotAllowed) {
			t.Fatalf("Open(%q) error = %v, want ErrOpenURLNotAllowed", target, err)
		}
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("origin hits = %d, want no network call at all", got)
	}
}

func TestNewAgentPageOpenerWithProvider_rejects_a_missing_provider(t *testing.T) {
	// When
	_, err := NewAgentPageOpenerWithProvider(nil)

	// Then
	if !errors.Is(err, ErrInvalidAgentPageOpener) {
		t.Fatalf("error = %v, want ErrInvalidAgentPageOpener", err)
	}
}

// newPublicOnlyProvider builds a provider whose candidate boundary refuses
// loopback and private hosts, which is the production setting.
func newPublicOnlyProvider(t *testing.T) *Provider {
	t.Helper()
	limits := DefaultLimits()
	budget, err := fetch.NewCrawlBudget(fetch.CrawlBudgetLimits{
		MaxTransportAttempts:  int64(limits.MaxHTTPAttempts),
		MaxAggregateBodyBytes: limits.MaxResponseBytes,
	})
	if err != nil {
		t.Fatalf("NewCrawlBudget() error = %v", err)
	}
	fetcher, err := fetch.NewFetcher(
		fetch.WithStrictPublicCrawl(budget),
		fetch.WithAnyPublicHost(true),
		fetch.WithAllowLoopback(true),
		fetch.WithPerMinute(60_000),
		fetch.WithTimeout(200*time.Millisecond),
		fetch.WithRetryBackoff(0),
	)
	if err != nil {
		t.Fatalf("NewFetcher() error = %v", err)
	}
	provider, err := newProvider(
		Catalog{Version: "opener-v1", Seeds: []Seed{fixtureSeed("public", "https://events.example/")}},
		limits,
		runtimeDeps{fetcher: fetcher, budget: budget, now: func() time.Time { return fixtureTime }},
	)
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}
	return provider
}

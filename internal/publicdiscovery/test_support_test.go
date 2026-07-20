package publicdiscovery

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

var fixtureTime = time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

func newFixtureProvider(t *testing.T, seeds []Seed, limits Limits) (*Provider, *fetch.CrawlBudget) {
	t.Helper()
	budget, err := fetch.NewCrawlBudget(fetch.CrawlBudgetLimits{
		MaxTransportAttempts:  int64(limits.MaxHTTPAttempts),
		MaxAggregateBodyBytes: limits.MaxResponseBytes,
	})
	if err != nil {
		t.Fatalf("NewCrawlBudget: %v", err)
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
		t.Fatalf("NewFetcher: %v", err)
	}
	provider, err := newProvider(Catalog{Version: "fixture-v1", Seeds: seeds}, limits, runtimeDeps{
		fetcher:              fetcher,
		budget:               budget,
		now:                  func() time.Time { return fixtureTime },
		allowLocalCandidates: true,
	})
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	return provider, budget
}

func fixtureSeed(name, rawURL string) Seed {
	return Seed{Name: name, URL: rawURL}
}

func newFixtureServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func writeDocument(t *testing.T, w http.ResponseWriter, contentType, body string) {
	t.Helper()
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write fixture response: %v", err)
	}
}

func candidateByURL(t *testing.T, candidates []Candidate, rawURL string) Candidate {
	t.Helper()
	canonical, err := CanonicalizeURL(rawURL)
	if err != nil {
		t.Fatalf("CanonicalizeURL(%q): %v", rawURL, err)
	}
	for _, candidate := range candidates {
		if candidate.URL == canonical {
			return candidate
		}
	}
	t.Fatalf("candidate %q not found in %+v", canonical, candidates)
	return Candidate{}
}

func hasReason(result Result, reason TruncationReason) bool {
	for _, existing := range result.Budget.TruncationReasons {
		if existing == reason {
			return true
		}
	}
	return false
}

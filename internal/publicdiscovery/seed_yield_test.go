package publicdiscovery

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

// Seed candidates are the only ones guaranteed a title (the seed name is the
// fallback), so a run where none survive the crawl looks identical to a run
// where the model rejected them. The snapshot separates the two.
func TestAgentSearchTool_yieldSnapshotCountsSeedCandidates(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/xml", fmt.Sprintf(
			`<urlset><url><loc>%s/expo-a</loc></url><url><loc>%s/expo-b</loc></url></urlset>`,
			server.URL, server.URL))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	snapshot := tool.YieldSnapshot()

	// Then
	if snapshot.SeedCandidates != 1 {
		t.Fatalf("seed candidates = %d, want the one seed-protocol candidate", snapshot.SeedCandidates)
	}
	if snapshot.ValidatedCandidates <= snapshot.SeedCandidates {
		t.Fatalf("validated = %d, want the sitemap children counted alongside %d seed candidate(s)",
			snapshot.ValidatedCandidates, snapshot.SeedCandidates)
	}
}

// A crawl that never produces a seed candidate must report zero rather than
// inheriting the previous request's count.
func TestAgentSearchTool_yieldSnapshotSeedCountIsRequestLocal(t *testing.T) {
	// Given
	tool, err := NewAgentSearchToolWithProvider(mustFixtureProvider(t))
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	snapshot := tool.YieldSnapshot()

	// Then
	if snapshot.SeedCandidates != 0 {
		t.Fatalf("seed candidates = %d, want zero before any crawl", snapshot.SeedCandidates)
	}
}

// Sitemap children used to die at the prefilter for want of a title, which is
// what starved the model. Now that the crawler backfills each fetched page's
// title, they reach the model alongside the seed, so discovery can return a
// source the catalog never named.
func TestPublicYield_sitemapChildrenReachTheModel(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/xml", fmt.Sprintf(
			`<urlset><url><loc>%s/expo-a</loc></url><url><loc>%s/expo-b</loc></url></urlset>`,
			server.URL, server.URL))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())
	if _, err := provider.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("fixture Search: %v", err)
	}
	for index := range provider.immutableCandidates {
		public := fmt.Sprintf("https://venue.example/c%d", index)
		provider.immutableCandidates[index].URL = public
		provider.immutableCandidates[index].Provenance.SeedURL = public
		provider.immutableCandidates[index].Provenance.RawURL = public
		provider.immutableCandidates[index].Provenance.CanonicalURL = public
	}
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}
	backend, _ := newFakeSolarBackend(t, "https://venue.example/c0")

	// When
	run, err := agent.DiscoverWithOptions(context.Background(), agent.DiscoverRequest{
		Backend: backend, Goal: "official AI event calendar",
		Tool: tool, Options: publicYieldOptions(t),
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions: %v", err)
	}
	reasons := run.YieldTrace.PrefilterReasons
	if reasons != (agent.PrefilterReasons{}) {
		t.Fatalf("prefilter reasons = %#v, want no candidate dropped once titles are backfilled", reasons)
	}
	if reasons.Total() != run.YieldTrace.PrefilterDropped {
		t.Fatalf("reason total = %d, want prefilter dropped = %d", reasons.Total(), run.YieldTrace.PrefilterDropped)
	}
	snapshot := tool.YieldSnapshot()
	if snapshot.SeedCandidates != 1 {
		t.Fatalf("seed candidates = %d, want the seed page stored", snapshot.SeedCandidates)
	}
	if run.YieldTrace.Offered != 3 {
		t.Fatalf("offered = %d, want the seed and both sitemap children offered to the model",
			run.YieldTrace.Offered)
	}
}

func mustFixtureProvider(t *testing.T) *Provider {
	t.Helper()
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())
	return provider
}

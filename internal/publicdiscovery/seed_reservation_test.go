package publicdiscovery

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Sitemap children are discovered before seed pages are fetched, so without a
// reservation they consume the whole candidate list and the seed page — the
// only candidate guaranteed a title — is rejected for want of a slot. The
// reservation must not raise the total cap.
func TestCrawl_reservesCandidateSlotsForPendingSeeds(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		body := "<urlset>"
		for index := range 10 {
			body += fmt.Sprintf(`<url><loc>%s/expo-%02d</loc></url>`, server.URL, index)
		}
		writeDocument(t, w, "text/xml", body+"</urlset>")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	limits := DefaultLimits()
	limits.MaxCandidates = 5
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, limits)
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	snapshot := tool.YieldSnapshot()
	candidates := tool.Snapshot().Candidates

	// Then
	if snapshot.SeedOutcomes.Candidate != 1 || snapshot.SeedOutcomes.CandidateCap != 0 {
		t.Fatalf("seed outcomes = %#v, want the seed stored rather than capped out", snapshot.SeedOutcomes)
	}
	if snapshot.SeedCandidates != 1 {
		t.Fatalf("seed candidates = %d, want the reserved seed slot to be used", snapshot.SeedCandidates)
	}
	if len(candidates) > limits.MaxCandidates {
		t.Fatalf("stored candidates = %d, want the total cap of %d respected", len(candidates), limits.MaxCandidates)
	}
	titled := 0
	for _, candidate := range candidates {
		if candidate.Title != "" {
			titled++
		}
	}
	if titled == 0 {
		t.Fatalf("no titled candidate survived; the model would receive nothing to judge")
	}
}

// The reservation is released once a seed is accounted for, so a crawl whose
// seeds all resolve early still fills the whole candidate list.
func TestCrawl_releasesSeedReservationOnceSeedsResolve(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeDocument(t, w, "text/html", `<title>Event</title>`)
			return
		}
		body := "<html><title>Venue Root</title>"
		for index := range 10 {
			body += fmt.Sprintf(`<a href="/event-%02d">Event %02d</a>`, index, index)
		}
		writeDocument(t, w, "text/html", body+"</html>")
	})
	limits := DefaultLimits()
	limits.MaxCandidates = 5
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, limits)
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	candidates := tool.Snapshot().Candidates

	// Then
	if len(candidates) != limits.MaxCandidates {
		t.Fatalf("stored candidates = %d, want the full cap of %d once no seed is pending",
			len(candidates), limits.MaxCandidates)
	}
}

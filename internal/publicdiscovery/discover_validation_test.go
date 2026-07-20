package publicdiscovery

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestDiscoverCandidateValidation_when_destination_fetch_fails_is_not_returned(t *testing.T) {
	// Given
	var missingHits atomic.Int32
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", fmt.Sprintf(
			`<urlset><url><loc>%s/available</loc></url><url><loc>%s/missing</loc></url></urlset>`,
			server.URL, server.URL,
		))
	})
	mux.HandleFunc("/available", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Available</title>`)
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) {
		missingHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("validation", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "available")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	candidateByURL(t, result.Candidates, server.URL+"/available")
	if missingHits.Load() != 1 {
		t.Fatalf("missing destination hits = %d, want 1 strict Fetch attempt", missingHits.Load())
	}
	for _, candidate := range result.Candidates {
		if candidate.URL == server.URL+"/missing" {
			t.Fatalf("unfetchable candidate returned: %+v", candidate)
		}
	}
}

func TestDiscoverCandidateValidation_when_page_budget_prevents_fetch_is_not_returned(t *testing.T) {
	// Given
	var unvisitedHits atomic.Int32
	limits := DefaultLimits()
	limits.MaxHTMLPages = 1
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/unvisited", func(w http.ResponseWriter, _ *http.Request) {
		unvisitedHits.Add(1)
		writeDocument(t, w, "text/html", `<title>Unvisited</title>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Root</title><a href="/unvisited">Unvisited</a>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("page-cap", server.URL+"/")}, limits)

	// When
	result, err := provider.Search(context.Background(), "unvisited")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if unvisitedHits.Load() != 0 || !hasReason(result, TruncationHTMLPageLimit) {
		t.Fatalf("unvisited hits=%d budget=%+v", unvisitedHits.Load(), result.Budget)
	}
	for _, candidate := range result.Candidates {
		if candidate.URL == server.URL+"/unvisited" {
			t.Fatalf("unvalidated candidate returned: %+v", candidate)
		}
	}
}

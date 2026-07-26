package publicdiscovery

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// A sitemap lists URLs with no titles, so those candidates are stored untitled
// and the events profile drops every one of them before the model sees it. The
// crawler already fetches and parses each of those pages, so the title is
// in hand and only needs to reach the candidate.
func TestCrawl_backfillsCandidateTitleFromFetchedPage(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/xml", fmt.Sprintf(
			`<urlset><url><loc>%s/expo-a</loc></url></urlset>`, server.URL))
	})
	mux.HandleFunc("/expo-a", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>2027 AI 로봇 산업전</title>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "official AI event calendar")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Then
	child := candidateByURL(t, result.Candidates, server.URL+"/expo-a")
	if child.Provenance.Protocol != ProtocolSitemapURLSet {
		t.Fatalf("protocol = %q, want a sitemap child", child.Provenance.Protocol)
	}
	if child.Title != "2027 AI 로봇 산업전" {
		t.Fatalf("title = %q, want the title parsed from the fetched page", child.Title)
	}
}

// A title the discovery protocol already supplied is authoritative. Feed
// entries carry their own titles, and the fetched page must not overwrite one.
func TestCrawl_keepsProtocolSuppliedTitleOverFetchedPage(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/feed.json", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/feed+json", fmt.Sprintf(
			`{"version":"https://jsonfeed.org/version/1","items":[{"title":"Feed Supplied Title","url":"%s/expo-b"}]}`,
			server.URL))
	})
	mux.HandleFunc("/expo-b", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Page Title That Must Not Win</title>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", fmt.Sprintf(
			`<html><head><title>Venue Root</title><link rel="alternate" type="application/feed+json" href="%s/feed.json"></head></html>`,
			server.URL))
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "official AI event calendar")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Then
	child := candidateByURL(t, result.Candidates, server.URL+"/expo-b")
	if child.Title != "Feed Supplied Title" {
		t.Fatalf("title = %q, want the feed-supplied title preserved", child.Title)
	}
}

// The backfilled title is candidate data and must carry the same contact
// redaction the rest of the crawl output does.
func TestCrawl_backfilledTitleIsBounded(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/xml", fmt.Sprintf(
			`<urlset><url><loc>%s/expo-c</loc></url></urlset>`, server.URL))
	})
	mux.HandleFunc("/expo-c", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>   </title>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "official AI event calendar")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Then
	child := candidateByURL(t, result.Candidates, server.URL+"/expo-c")
	if child.Title != "" {
		t.Fatalf("title = %q, want a blank page title to leave the candidate untitled", child.Title)
	}
}

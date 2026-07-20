package publicdiscovery

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDiscoverDedupe_when_equivalent_URLs_repeat(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		upper := strings.Replace(server.URL, "http://", "HTTP://", 1)
		writeDocument(t, w, "application/xml", fmt.Sprintf(
			`<urlset><url><loc>%s/event#one</loc></url><url><loc>%s/a/../event#two</loc></url><url><loc>%s/event?b=2&amp;a=1</loc></url><url><loc>%s/event?a=1&amp;b=2</loc></url></urlset>`,
			server.URL, upper, server.URL, server.URL,
		))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeDocument(t, w, "text/html", `<title>Root</title>`) })
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("dedupe", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	wantURLs := map[string]bool{
		server.URL + "/":              true,
		server.URL + "/event":         true,
		server.URL + "/event?b=2&a=1": true,
		server.URL + "/event?a=1&b=2": true,
	}
	if len(result.Candidates) != len(wantURLs) {
		t.Fatalf("candidate count = %d, want %d: %+v", len(result.Candidates), len(wantURLs), result.Candidates)
	}
	for _, candidate := range result.Candidates {
		if !wantURLs[candidate.URL] {
			t.Fatalf("unexpected candidate URL %q", candidate.URL)
		}
	}
}

func TestDiscoverCycle_when_sitemap_index_references_itself(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/plain", "User-agent: *\nSitemap: "+server.URL+"/index.xml\n")
	})
	mux.HandleFunc("/index.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", fmt.Sprintf(`<sitemapindex><sitemap><loc>%s/index.xml</loc></sitemap></sitemapindex>`, server.URL))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeDocument(t, w, "text/html", `<title>Root</title>`) })
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("cycle", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "root")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Budget.Usage.ProtocolDocuments != 1 {
		t.Fatalf("protocol documents = %d, want 1", result.Budget.Usage.ProtocolDocuments)
	}
}

func TestDiscoverPrivateLiteral_when_sitemap_points_to_nonpublic_host(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", `<urlset><url><loc>http://169.254.169.254/latest/meta-data</loc></url></urlset>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeDocument(t, w, "text/html", `<title>Root</title>`) })
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("private", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "metadata")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, candidate := range result.Candidates {
		if strings.Contains(candidate.URL, "169.254.169.254") {
			t.Fatalf("private candidate leaked: %+v", candidate)
		}
	}
}

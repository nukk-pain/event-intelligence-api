package publicdiscovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestDiscoverSitemapIndexURLSetProvenance_when_robots_declares_gzip_index(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/plain", "User-agent: *\nSitemap: "+server.URL+"/index.xml\n")
	})
	mux.HandleFunc("/index.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", fmt.Sprintf(
			`<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>%s/part.xml</loc></sitemap><sitemap><loc>%s/part.xml.gz</loc></sitemap></sitemapindex>`,
			server.URL, server.URL,
		))
	})
	mux.HandleFunc("/part.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", fmt.Sprintf(
			`<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>%s/event-a?b=2&amp;a=1</loc></url></urlset>`,
			server.URL,
		))
	})
	mux.HandleFunc("/part.xml.gz", func(w http.ResponseWriter, _ *http.Request) {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := fmt.Fprintf(writer, `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>%s/event-b/</loc></url></urlset>`, server.URL); err != nil {
			t.Fatalf("write gzip fixture: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close gzip fixture: %v", err)
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(compressed.Bytes()); err != nil {
			t.Fatalf("write gzip response: %v", err)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<html><title>Root</title></html>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("official", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	candidate := candidateByURL(t, result.Candidates, server.URL+"/event-b/")
	if candidate.Provenance.Protocol != ProtocolSitemapURLSet {
		t.Fatalf("protocol = %q, want %q", candidate.Provenance.Protocol, ProtocolSitemapURLSet)
	}
	if candidate.Provenance.DiscoveredFrom != server.URL+"/part.xml.gz" {
		t.Fatalf("discovered from = %q", candidate.Provenance.DiscoveredFrom)
	}
	if candidate.Provenance.SeedName != "official" || candidate.Provenance.FetchedAt != fixtureTime {
		t.Fatalf("provenance = %+v", candidate.Provenance)
	}
	if result.Budget.Usage.ProtocolDocuments != 3 {
		t.Fatalf("protocol documents = %d, want 3", result.Budget.Usage.ProtocolDocuments)
	}
}

func TestDiscoverSitemapFallback_when_robots_has_no_declaration(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/xml", fmt.Sprintf(`<urlset><url><loc>%s/fallback-event</loc></url></urlset>`, server.URL))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeDocument(t, w, "text/html", `<title>Root</title>`) })
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("fallback", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "fallback")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	candidate := candidateByURL(t, result.Candidates, server.URL+"/fallback-event")
	if candidate.Provenance.Protocol != ProtocolSitemapURLSet {
		t.Fatalf("protocol = %q", candidate.Provenance.Protocol)
	}
}

func TestDiscoverFeedAtomRSSJSONProvenance_when_HTML_declares_feed(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		feedPath    string
		feedBody    func(string) string
		wantPath    string
		wantTitle   string
		protocol    Protocol
	}{
		{
			name: "atom", contentType: "application/atom+xml", feedPath: "/feed.atom", wantPath: "/atom-event", wantTitle: "Atom Event", protocol: ProtocolAtom,
			feedBody: func(base string) string {
				return fmt.Sprintf(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Atom Event</title><link href="%s/atom-event"/></entry></feed>`, base)
			},
		},
		{
			name: "rss", contentType: "application/rss+xml", feedPath: "/feed.rss", wantPath: "/rss-event", wantTitle: "RSS Event", protocol: ProtocolRSS,
			feedBody: func(base string) string {
				return fmt.Sprintf(`<rss version="2.0"><channel><item><title>RSS Event</title><link>%s/rss-event</link></item></channel></rss>`, base)
			},
		},
		{
			name: "json", contentType: "application/feed+json", feedPath: "/feed.json", wantPath: "/json-event", wantTitle: "JSON Event", protocol: ProtocolJSONFeed,
			feedBody: func(base string) string {
				return fmt.Sprintf(`{"version":"https://jsonfeed.org/version/1.1","items":[{"title":"JSON Event","url":"%s/json-event"}]}`, base)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			mux := http.NewServeMux()
			server := newFixtureServer(t, mux)
			mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/":
					writeDocument(t, w, "text/html", fmt.Sprintf(`<html><head><title>Root</title><link rel="alternate" type="%s" href="%s"></head></html>`, tt.contentType, tt.feedPath))
				case tt.feedPath:
					writeDocument(t, w, tt.contentType, tt.feedBody(server.URL))
				default:
					writeDocument(t, w, "text/html", `<title>Event</title>`)
				}
			})
			provider, _ := newFixtureProvider(t, []Seed{fixtureSeed(tt.name, server.URL+"/")}, DefaultLimits())

			// When
			result, err := provider.Search(context.Background(), "event")

			// Then
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			candidate := candidateByURL(t, result.Candidates, server.URL+tt.wantPath)
			if candidate.Title != tt.wantTitle || candidate.Provenance.Protocol != tt.protocol {
				t.Fatalf("candidate = %+v", candidate)
			}
		})
	}
}

func TestDiscoverHTMLAndHTTPLinkProvenance_when_root_exposes_both(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Add("Link", `</header-feed>; rel="alternate"; type="application/feed+json"`)
			writeDocument(t, w, "text/html", `<html><title>Root</title><a href="/html-event">HTML Event</a></html>`)
		case "/header-feed":
			writeDocument(t, w, "application/feed+json", fmt.Sprintf(`{"version":"https://jsonfeed.org/version/1","items":[{"title":"Header Event","url":"%s/header-event"}]}`, server.URL))
		default:
			writeDocument(t, w, "text/html", `<title>Event</title>`)
		}
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("links", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	htmlCandidate := candidateByURL(t, result.Candidates, server.URL+"/html-event")
	if htmlCandidate.Provenance.Protocol != ProtocolHTMLLink || htmlCandidate.Title != "HTML Event" {
		t.Fatalf("HTML candidate = %+v", htmlCandidate)
	}
	if htmlCandidate.Provenance.RawURL != "/html-event" {
		t.Fatalf("HTML raw URL = %q, want source href", htmlCandidate.Provenance.RawURL)
	}
	headerCandidate := candidateByURL(t, result.Candidates, server.URL+"/header-event")
	if headerCandidate.Provenance.Protocol != ProtocolJSONFeed {
		t.Fatalf("header feed candidate = %+v", headerCandidate)
	}
}

func TestDiscoverHTTPLinkProvenance_when_protocol_document_exposes_relation(t *testing.T) {
	tests := []struct {
		name         string
		documentPath string
		contentType  string
		documentBody string
		rootBody     string
	}{
		{
			name: "sitemap", documentPath: "/sitemap.xml", contentType: "application/xml",
			documentBody: `<urlset></urlset>`, rootBody: `<title>Root</title>`,
		},
		{
			name: "atom feed", documentPath: "/feed.atom", contentType: "application/atom+xml",
			documentBody: `<feed xmlns="http://www.w3.org/2005/Atom"></feed>`,
			rootBody:     `<title>Root</title><link rel="alternate" type="application/atom+xml" href="/feed.atom">`,
		},
		{
			name: "JSON feed", documentPath: "/feed.json", contentType: "application/feed+json",
			documentBody: `{"version":"https://jsonfeed.org/version/1.1","items":[]}`,
			rootBody:     `<title>Root</title><link rel="alternate" type="application/feed+json" href="/feed.json">`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			var targetHits atomic.Int32
			mux := http.NewServeMux()
			server := newFixtureServer(t, mux)
			mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			mux.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case tt.documentPath:
					w.Header().Add("Link", `</linked-event>; rel="related"`)
					writeDocument(t, w, tt.contentType, tt.documentBody)
				case "/sitemap.xml":
					w.WriteHeader(http.StatusNotFound)
				case "/linked-event":
					targetHits.Add(1)
					writeDocument(t, w, "text/html", `<title>Linked Event</title>`)
				case "/":
					writeDocument(t, w, "text/html", tt.rootBody)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			provider, _ := newFixtureProvider(t, []Seed{fixtureSeed(tt.name, server.URL+"/")}, DefaultLimits())

			// When
			result, err := provider.Search(context.Background(), "linked")

			// Then
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			candidate := candidateByURL(t, result.Candidates, server.URL+"/linked-event")
			if targetHits.Load() != 1 || candidate.Provenance.Protocol != ProtocolHTTPLink ||
				candidate.Provenance.RawURL != "/linked-event" || candidate.Provenance.DiscoveredFrom != server.URL+tt.documentPath {
				t.Fatalf("target hits=%d candidate=%+v", targetHits.Load(), candidate)
			}
		})
	}
}

package publicdiscovery

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoverBudgetDefaults_when_provider_is_created(t *testing.T) {
	// Given / When
	limits := DefaultLimits()

	// Then
	if limits.MaxSeeds != 8 || limits.MaxDepth != 2 || limits.MaxProtocolDocuments != 12 || limits.MaxHTMLPages != 24 {
		t.Fatalf("frontier limits = %+v", limits)
	}
	if limits.MaxCandidates != 30 || limits.MaxHTTPAttempts != 64 || limits.MaxResponseBytes != 6<<20 || limits.Timeout != 60*time.Second {
		t.Fatalf("job limits = %+v", limits)
	}
}

func TestDiscoverBudgetCandidate_when_more_than_thirty_are_found(t *testing.T) {
	// Given
	var validatedPageHits atomic.Int32
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		validatedPageHits.Add(1)
		body := "<html><title>Root</title>"
		for index := range 31 {
			body += fmt.Sprintf(`<a href="/event-%02d">Event %02d</a>`, index, index)
		}
		writeDocument(t, w, "text/html", body+"</html>")
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("cap", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Candidates) != 24 || result.Budget.Usage.Candidates != 24 || validatedPageHits.Load() != 24 {
		t.Fatalf("candidate usage = %d/%d", len(result.Candidates), result.Budget.Usage.Candidates)
	}
	if !result.Budget.Truncated || !hasReason(result, TruncationCandidateLimit) || !hasReason(result, TruncationHTMLPageLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetHTML_when_more_than_twenty_four_pages_are_queued(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeDocument(t, w, "text/html", `<title>Leaf</title>`)
			return
		}
		body := "<html><title>Root</title>"
		for index := range 24 {
			body += fmt.Sprintf(`<a href="/page-%02d">Page %02d</a>`, index, index)
		}
		writeDocument(t, w, "text/html", body+"</html>")
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("html-cap", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "page")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Budget.Usage.HTMLPages != 24 || !hasReason(result, TruncationHTMLPageLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetProtocol_when_more_than_twelve_documents_are_queued(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/plain", "User-agent: *\nSitemap: "+server.URL+"/index.xml\n")
	})
	mux.HandleFunc("/index.xml", func(w http.ResponseWriter, _ *http.Request) {
		body := "<sitemapindex>"
		for index := range 12 {
			body += fmt.Sprintf("<sitemap><loc>%s/child-%02d.xml</loc></sitemap>", server.URL, index)
		}
		writeDocument(t, w, "application/xml", body+"</sitemapindex>")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			writeDocument(t, w, "text/html", `<title>Root</title>`)
			return
		}
		writeDocument(t, w, "application/xml", `<urlset></urlset>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("protocol-cap", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Budget.Usage.ProtocolDocuments != 12 || !hasReason(result, TruncationProtocolDocumentLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetAttempt_when_redirects_reach_transport_cap(t *testing.T) {
	// Given
	limits := DefaultLimits()
	limits.MaxHTTPAttempts = 5
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/redirect-one", http.StatusFound) })
	mux.HandleFunc("/redirect-one", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/redirect-two", http.StatusFound) })
	mux.HandleFunc("/redirect-two", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", `<urlset></urlset>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeDocument(t, w, "text/html", `<title>Root</title>`) })
	provider, budget := newFixtureProvider(t, []Seed{fixtureSeed("attempt-cap", server.URL+"/")}, limits)

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if usage := budget.Usage(); usage.TransportAttempts != 5 {
		t.Fatalf("transport attempts = %d, want 5", usage.TransportAttempts)
	}
	if !hasReason(result, TruncationHTTPAttemptLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetBody_when_aggregate_bytes_are_exhausted(t *testing.T) {
	// Given
	limits := DefaultLimits()
	limits.MaxResponseBytes = 32
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", `<urlset><url><loc>https://example.com/event</loc></url></urlset>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeDocument(t, w, "text/html", `<title>Root</title>`) })
	provider, budget := newFixtureProvider(t, []Seed{fixtureSeed("body-cap", server.URL+"/")}, limits)

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if usage := budget.Usage(); usage.AggregateBodyBytes != 32 {
		t.Fatalf("aggregate bytes = %d, want 32", usage.AggregateBodyBytes)
	}
	if !hasReason(result, TruncationResponseBodyLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetTime_when_upstream_hangs(t *testing.T) {
	// Given
	limits := DefaultLimits()
	limits.Timeout = 30 * time.Millisecond
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("time-cap", server.URL+"/")}, limits)

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !hasReason(result, TruncationTimeLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetTime_when_robots_hangs_stops_inflight_request(t *testing.T) {
	// Given
	limits := DefaultLimits()
	limits.Timeout = 30 * time.Millisecond
	requestStarted := make(chan struct{})
	requestStopped := make(chan struct{})
	server := newFixtureServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(requestStopped)
	}))
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("robots-time-cap", server.URL+"/")}, limits)

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("robots request did not start")
	}
	select {
	case <-requestStopped:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("robots request continued after the public discovery deadline")
	}
	if !hasReason(result, TruncationTimeLimit) {
		t.Fatalf("budget = %+v", result.Budget)
	}
}

func TestDiscoverBudgetDepth_when_link_exceeds_depth_two(t *testing.T) {
	// Given
	var tooDeepHits atomic.Int32
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			writeDocument(t, w, "text/html", `<a href="/one">One</a>`)
		case "/one":
			writeDocument(t, w, "text/html", `<a href="/two">Two</a>`)
		case "/two":
			writeDocument(t, w, "text/html", `<a href="/three">Three</a>`)
		case "/three":
			tooDeepHits.Add(1)
			writeDocument(t, w, "text/html", `<title>Too deep</title>`)
		}
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("depth", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if tooDeepHits.Load() != 0 || !hasReason(result, TruncationDepthLimit) {
		t.Fatalf("too-deep hits = %d budget=%+v", tooDeepHits.Load(), result.Budget)
	}
	for _, candidate := range result.Candidates {
		if candidate.URL == server.URL+"/three" {
			t.Fatalf("depth-three candidate leaked: %+v", candidate)
		}
	}
}

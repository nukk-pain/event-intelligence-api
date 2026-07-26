package publicdiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAgentSearchTool_resolves_relative_raw_url_for_agent_validation_and_keeps_literal(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<html><title>Root</title><a href="/event">Official event</a></html>`)
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Event</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("fixture", server.URL+"/")}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("new agent search tool: %v", err)
	}

	// When
	results, err := tool.Search(context.Background(), "event")

	// Then
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var eventResult agentSearchResultView
	for _, result := range results {
		if strings.HasSuffix(result.URL, "/event") {
			eventResult = agentSearchResultView{URL: result.URL, RawURL: result.Provenance.RawURL}
		}
	}
	if eventResult.URL == "" || eventResult.RawURL != server.URL+"/event" {
		t.Fatalf("agent result = %+v, want resolved raw URL", eventResult)
	}
	snapshot := tool.Snapshot()
	publicCandidate := candidateByURL(t, snapshot.Candidates, server.URL+"/event")
	if publicCandidate.Provenance.RawURL != "/event" {
		t.Fatalf("public raw URL = %q, want literal href", publicCandidate.Provenance.RawURL)
	}
}

func TestAgentSearchTool_search_reuses_one_frontier_per_request(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<html><title>Root</title></html>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("fixture", server.URL+"/")}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("new agent search tool: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "first"); err != nil {
		t.Fatalf("first search: %v", err)
	}
	first := tool.Snapshot().Budget.Usage.HTTPAttempts
	if _, err := tool.Search(context.Background(), "second"); err != nil {
		t.Fatalf("second search: %v", err)
	}
	second := tool.Snapshot().Budget.Usage.HTTPAttempts

	// Then
	if first == 0 || second != first {
		t.Fatalf("transport attempts changed across query ranking: first=%d second=%d", first, second)
	}
}

func TestAgentSearchToolYieldSnapshot(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		body := "<html><title>Root</title>"
		for index := range 31 {
			body += fmt.Sprintf(`<a href="/event-%02d">Event %02d</a>`, index, index)
		}
		writeDocument(t, w, "text/html", body+"</html>")
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	for index := range 31 {
		path := fmt.Sprintf("/event-%02d", index)
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			writeDocument(t, w, "text/html", `<html><title>Event</title></html>`)
		})
	}
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("capped", server.URL+"/")}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}
	emptyProvider, _ := newFixtureProvider(t, []Seed{fixtureSeed("empty", server.URL+"/empty")}, DefaultLimits())
	emptyTool, err := NewAgentSearchToolWithProvider(emptyProvider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}
	malformedMux := http.NewServeMux()
	malformedServer := newFixtureServer(t, malformedMux)
	malformedMux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	malformedMux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "application/xml", `<urlset><url>`)
	})
	malformedMux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	malformedProvider, _ := newFixtureProvider(t, []Seed{fixtureSeed("malformed", malformedServer.URL+"/")}, DefaultLimits())
	malformedTool, err := NewAgentSearchToolWithProvider(malformedProvider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "event"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	snapshot := tool.YieldSnapshot()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal snapshot: %v", err)
	}
	t.Logf("serialized_count_snapshot=%s", encoded)
	if _, err := emptyTool.Search(context.Background(), "event"); err != nil {
		t.Fatalf("empty Search: %v", err)
	}
	emptySnapshot := emptyTool.YieldSnapshot()
	if _, err := malformedTool.Search(context.Background(), "event"); err != nil {
		t.Fatalf("malformed Search: %v", err)
	}
	malformedSnapshot := malformedTool.YieldSnapshot()

	// Then
	if snapshot.ValidatedCandidates != 24 {
		t.Fatalf("validated candidates = %d, want 24", snapshot.ValidatedCandidates)
	}
	if !snapshot.Truncated || !hasTruncationReason(snapshot.TruncationReasons, TruncationCandidateLimit) || !hasTruncationReason(snapshot.TruncationReasons, TruncationHTMLPageLimit) {
		t.Fatalf("snapshot = %+v, want candidate and HTML truncation", snapshot)
	}
	if got := string(encoded); got != `{"validated_candidates":24,"seed_candidates":1,"skipped_documents":1,"malformed_documents":0,"seed_outcomes":{"candidate":1,"robots_disallowed":0,"http_status":0,"body_too_large":0,"unsupported_content":0,"transport_error":0,"duplicate":0,"candidate_cap":0,"not_attempted":0},"truncated":true,"truncation_reasons":["candidate_limit","html_page_limit"]}` {
		t.Fatalf("serialized snapshot = %s", got)
	}
	if strings.Contains(string(encoded), server.URL) {
		t.Fatalf("serialized snapshot leaked crawler URL: %s", encoded)
	}
	if emptySnapshot.ValidatedCandidates != 0 || emptySnapshot.Truncated || len(emptySnapshot.TruncationReasons) != 0 {
		t.Fatalf("fresh tool reused stale crawler state: %+v", emptySnapshot)
	}
	if malformedSnapshot.ValidatedCandidates != 0 || malformedSnapshot.Truncated || len(malformedSnapshot.TruncationReasons) != 0 {
		t.Fatalf("malformed crawl snapshot = %+v, want no count or truncation", malformedSnapshot)
	}
	if malformed := malformedTool.Snapshot().Budget.Usage.MalformedDocuments; malformed != 1 {
		t.Fatalf("malformed documents = %d, want 1", malformed)
	}
}

func hasTruncationReason(reasons []TruncationReason, want TruncationReason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

type agentSearchResultView struct {
	URL    string
	RawURL string
}

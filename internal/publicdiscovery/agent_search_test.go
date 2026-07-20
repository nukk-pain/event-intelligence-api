package publicdiscovery

import (
	"context"
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

type agentSearchResultView struct {
	URL    string
	RawURL string
}

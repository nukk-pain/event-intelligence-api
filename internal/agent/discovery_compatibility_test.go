package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDiscover_legacy_fixture_SearchTool_contract_remains_compatible(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	url := "https://events.example/calendar"
	backend, _ := newScriptedBackend(t, proposal("events"), judgment(options.Profile, url))
	fixture := &staticSearch{results: []SearchResult{{Title: "Fixture Calendar", URL: url, Snippet: "fixture"}}}

	// When
	sources, trace, err := Discover(context.Background(), backend, "events", fixture, 1, 100, time.Second)

	// Then
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(sources) != 1 || sources[0].URL != url || sources[0].Title != "Fixture Calendar" {
		t.Fatalf("sources = %#v, want legacy fixture candidate", sources)
	}
	if trace.Calls != 2 || sources[0].Provenance != nil {
		t.Fatalf("trace/provenance = %#v/%#v, want 2 calls and empty provenance", trace, sources[0].Provenance)
	}
}

func TestDiscover_Tavily_SearchTool_contract_remains_compatible(t *testing.T) {
	// Given
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"results":[{"title":"Tavily Calendar","url":"https://events.example/tavily","content":"calendar"}]}`), nil
		}),
	}
	search, err := NewTavilySearch("test-key", client)
	if err != nil {
		t.Fatalf("NewTavilySearch() error = %v", err)
	}
	options := testOptions(t, DiscoveryProfileEvents)
	backend, _ := newScriptedBackend(t, proposal("events"), judgment(options.Profile, "https://events.example/tavily"))

	// When
	sources, _, err := Discover(context.Background(), backend, "events", search, 1, 100, time.Second)

	// Then
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(sources) != 1 || sources[0].Title != "Tavily Calendar" || sources[0].Provenance != nil {
		t.Fatalf("sources = %#v, want provenance-free Tavily candidate", sources)
	}
}

func TestDiscoverWithOptions_preserves_provenance_additively(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	url := "https://events.example/calendar"
	provenance := &SourceProvenance{
		Provider: "public", SeedURL: "https://seed.example/", ParentURL: "https://seed.example/sitemap.xml",
		Relation: "sitemap", RawURL: "HTTPS://EVENTS.EXAMPLE:443/calendar#top",
	}
	backend, _ := newScriptedBackend(t, proposal("events"), judgment(options.Profile, url))
	search := &staticSearch{results: []SearchResult{{Title: "Calendar", URL: url, Provenance: provenance}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if len(run.Sources) != 1 || run.Sources[0].Provenance == nil || *run.Sources[0].Provenance != *provenance {
		t.Fatalf("provenance = %#v, want %#v", run.Sources, provenance)
	}
	wire, err := json.Marshal(run.Sources[0])
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	for _, field := range []string{"provider", "seed_url", "parent_url", "relation", "raw_url"} {
		if !strings.Contains(string(wire), `"`+field+`"`) {
			t.Fatalf("serialized provenance missing %q: %s", field, wire)
		}
	}
}

func TestDiscoverWithOptions_does_not_leak_state_between_requests(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxRounds = 1
	url := "https://events.example/calendar"
	backend, model := newScriptedBackend(t,
		proposal("first"), judgment(options.Profile, url), proposal("second"), judgment(options.Profile, url),
	)
	search := &staticSearch{results: []SearchResult{{Title: "Calendar", URL: url}}}

	// When
	first, firstErr := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "first-goal-marker", Tool: search, Options: options,
	})
	second, secondErr := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "second-goal-marker", Tool: search, Options: options,
	})

	// Then
	if firstErr != nil || secondErr != nil {
		t.Fatalf("request errors = %v / %v", firstErr, secondErr)
	}
	if len(first.Sources) != 1 || len(second.Sources) != 1 {
		t.Fatalf("source counts = %d / %d, want 1 / 1", len(first.Sources), len(second.Sources))
	}
	requests := model.capturedRequests()
	if len(requests) != 4 {
		t.Fatalf("requests = %d, want 4", len(requests))
	}
	for _, request := range requests[2:] {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "first-goal-marker") {
				t.Fatal("second request leaked first request goal")
			}
		}
	}
}

func TestDiscoverWithOptions_redacts_and_bounds_goal_per_request(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.GoalMaxRunes = 5
	backend, model := newScriptedBackend(t, doneProposal())

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "ABCDEFGHI person@example.com", Tool: &staticSearch{}, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	requests := model.capturedRequests()
	if len(requests) != 1 || len(requests[0].Messages) != 2 {
		t.Fatalf("requests = %#v, want one system/user exchange", requests)
	}
	user := requests[0].Messages[1].Content
	if !strings.Contains(user, "<untrusted-goal>\nABCDE\n</untrusted-goal>") ||
		strings.Contains(user, "FGHI") || strings.Contains(user, "person@example.com") {
		t.Fatalf("bounded redacted goal payload = %q", user)
	}
	if !containsTruncation(run.Metadata, TruncationGoalLength) {
		t.Fatalf("truncation reasons = %v, want goal length", run.Metadata.TruncationReasons)
	}
}

func TestDiscoverWithOptions_httptest_returns_only_offered_profile_valid_sources(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileConferences)
	options.ReferenceTime = time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	valid := "https://conf.example/conference/valid"
	missingLocation := "https://conf.example/conference/no-location"
	backend, _ := newScriptedBackend(t, proposal("conferences"), judgment(
		options.Profile, valid, missingLocation, "https://invented.example/conference",
	))
	search := &staticSearch{results: []SearchResult{
		{Title: "Valid Conference", URL: valid, Date: "2027-05-02", Location: "Seoul"},
		{Title: "Missing Location", URL: missingLocation, Date: "2027-05-03"},
	}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "AI conferences", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if len(run.Sources) != 1 || run.Sources[0].URL != valid || run.Sources[0].Location != "Seoul" {
		t.Fatalf("end-to-end response sources = %#v, want only valid offered conference", run.Sources)
	}
}

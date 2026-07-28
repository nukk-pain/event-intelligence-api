package publicdiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

func TestPublicYieldEndToEnd_exactSourceAccepted(t *testing.T) {
	// Given
	tool, candidateURL := newPublicYieldFixture(t)
	backend, modelCalls := newFakeSolarBackend(t, candidateURL)
	options := publicYieldOptions(t)

	// When
	run, err := agent.DiscoverWithOptions(context.Background(), agent.DiscoverRequest{
		Backend: backend, Goal: "official AI event calendar", Tool: tool, Options: options,
	})
	yield := run.YieldTrace.WithCrawlerValidated(tool.YieldSnapshot().ValidatedCandidates)

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions: %v", err)
	}
	if modelCalls.Load() != 2 {
		t.Fatalf("model calls = %d, want one search turn and one accept turn", modelCalls.Load())
	}
	wantYield := agent.YieldTrace{
		Outcome: agent.YieldOutcomeAccepted, TerminalReason: agent.YieldTerminalRoundLimit,
		CrawlerValidated: 1, Offered: 1, ProposalCalls: 1, JudgeCalls: 1,
		JudgeEntriesParsed: 1, Accepted: 1,
	}
	if yield != wantYield {
		t.Fatalf("yield trace = %#v, want %#v", yield, wantYield)
	}
	if len(run.Sources) != 1 || run.Sources[0].URL != candidateURL || run.Sources[0].Title != "Official AI Event Calendar" {
		t.Fatalf("sources = %#v, want exactly the offered safe source", run.Sources)
	}
}

func TestPublicYieldEndToEnd_rewrittenJudgeURLIsDropped(t *testing.T) {
	// Given
	tool, candidateURL := newPublicYieldFixture(t)
	backend, modelCalls := newFakeSolarBackend(t, candidateURL+"?utm_source=judge")
	options := publicYieldOptions(t)

	// When
	run, err := agent.DiscoverWithOptions(context.Background(), agent.DiscoverRequest{
		Backend: backend, Goal: "official AI event calendar", Tool: tool, Options: options,
	})
	yield := run.YieldTrace.WithCrawlerValidated(tool.YieldSnapshot().ValidatedCandidates)

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions: %v", err)
	}
	if modelCalls.Load() != 2 {
		t.Fatalf("model calls = %d, want one search turn and one accept turn", modelCalls.Load())
	}
	wantYield := agent.YieldTrace{
		Outcome: agent.YieldOutcomeBudgetStopped, TerminalReason: agent.YieldTerminalRoundLimit,
		CrawlerValidated: 1, Offered: 1, ProposalCalls: 1, JudgeCalls: 1,
		JudgeEntriesParsed: 1, JudgeEntriesDropped: 1,
	}
	if yield != wantYield {
		t.Fatalf("yield trace = %#v, want %#v", yield, wantYield)
	}
	if len(run.Sources) != 0 {
		t.Fatalf("sources = %#v, want rewritten judge URL safely dropped", run.Sources)
	}
}

func newPublicYieldFixture(t *testing.T) (*AgentSearchTool, string) {
	t.Helper()
	const candidateURL = "https://events.example/official-ai-calendar"
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/events", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<html><title>Official AI Event Calendar</title></html>`)
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("official-events", server.URL+"/events")}, DefaultLimits())
	result, err := provider.Search(context.Background(), "official AI event calendar")
	if err != nil {
		t.Fatalf("fixture Search: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("fixture candidates = %#v, want one strict-crawl candidate", result.Candidates)
	}
	// The strict crawler must fetch loopback in tests, while the imported agent
	// correctly rejects loopback candidates. Promote only the crawled fixture's
	// immutable identity to a reserved public hostname before agent selection.
	provider.immutableCandidates[0].URL = candidateURL
	provider.immutableCandidates[0].Provenance.SeedURL = candidateURL
	provider.immutableCandidates[0].Provenance.RawURL = candidateURL
	provider.immutableCandidates[0].Provenance.CanonicalURL = candidateURL
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}
	return tool, candidateURL
}

func newFakeSolarBackend(t *testing.T, judgeURL string) (agent.Backend, *atomic.Int32) {
	t.Helper()
	responses := []string{
		`{"action":"search","query":"official AI event calendar"}`,
		fmt.Sprintf(`{"action":"accept","selections":[{"url":%q,"reason":"official source"}]}`, judgeURL),
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(responses) {
			http.Error(w, "unexpected model call", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		response := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}{}
		response.Choices = make([]struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}, 1)
		response.Choices[0].Message.Content = responses[index]
		response.Usage.PromptTokens = 1
		response.Usage.CompletionTokens = 1
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "encode response", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return agent.Backend{Name: "solar", BaseURL: server.URL, Model: "solar-test"}, &calls
}

func publicYieldOptions(t *testing.T) agent.DiscoverOptions {
	t.Helper()
	profile, err := agent.NamedDiscoveryProfile(agent.DiscoveryProfileEvents)
	if err != nil {
		t.Fatalf("NamedDiscoveryProfile: %v", err)
	}
	options := agent.DefaultDiscoverOptions(profile)
	options.MaxSearches = 1
	// One search turn plus one accept turn, then the turn budget ends the run.
	options.MaxTurns = 2
	options.ReferenceTime = fixtureTime
	return options
}

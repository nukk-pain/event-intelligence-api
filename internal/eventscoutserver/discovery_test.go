package eventscoutserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/publicdiscovery"
)

type fixturePublicTool struct {
	originURL    string
	candidateURL string
	client       *http.Client
}

func (tool *fixturePublicTool) Search(ctx context.Context, _ string) ([]agent.SearchResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tool.originURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new public-origin request: %w", err)
	}
	response, err := tool.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch public origin: %w", err)
	}
	if err := response.Body.Close(); err != nil {
		return nil, fmt.Errorf("close public-origin response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public origin status %d", response.StatusCode)
	}
	return []agent.SearchResult{{
		Title: "Official Robotics Expo", URL: tool.candidateURL, Snippet: "official schedule",
		Provenance: &agent.SourceProvenance{
			Provider: "public", SeedURL: tool.candidateURL, RawURL: tool.candidateURL,
		},
	}}, nil
}

func (tool *fixturePublicTool) Snapshot() publicdiscovery.Result {
	return publicdiscovery.Result{Budget: publicdiscovery.BudgetState{
		Truncated: true, TruncationReasons: []publicdiscovery.TruncationReason{publicdiscovery.TruncationDepthLimit},
	}}
}

func (tool *fixturePublicTool) YieldSnapshot() publicdiscovery.PublicYieldSnapshot {
	return publicdiscovery.PublicYieldSnapshot{
		ValidatedCandidates: 1, Truncated: true,
		TruncationReasons: []publicdiscovery.TruncationReason{publicdiscovery.TruncationDepthLimit},
	}
}

func (tool *fixturePublicTool) RestoreProvenance(sources []agent.DiscoveredSource) []agent.DiscoveredSource {
	return append([]agent.DiscoveredSource(nil), sources...)
}

func TestSolarDiscoveryRunner_uses_fresh_public_tool_and_event_profile_per_request(t *testing.T) {
	// Given
	var modelCalls atomic.Int32
	var candidateURL string
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-operator-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		call := modelCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var content string
		switch call % 3 {
		case 1:
			content = `{"action":"search","query":"robotics official calendar"}`
		case 2:
			content = fmt.Sprintf(`{"action":"accept","selections":[{"url":%q,"reason":"official source"}]}`, candidateURL)
		default:
			content = `{"action":"done"}`
		}
		if _, err := fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`, content); err != nil {
			t.Errorf("write model response: %v", err)
		}
	}))
	defer modelServer.Close()
	var publicOriginHits atomic.Int32
	publicOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		publicOriginHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer publicOrigin.Close()
	candidateURL = "https://events.example/events/robotics"
	backend := agent.Backend{Name: "solar", BaseURL: modelServer.URL, APIKey: "test-operator-key", Model: "solar-open2"}
	var factoryCalls atomic.Int32
	unusedOpener := &countingOpener{}
	runner, err := newSolarDiscoveryRunner(backend, func() (publicSearchTool, error) {
		factoryCalls.Add(1)
		return &fixturePublicTool{
			originURL: publicOrigin.URL, candidateURL: candidateURL, client: publicOrigin.Client(),
		}, nil
	}, func() (agent.PageOpener, error) { return unusedOpener, nil }, fixedTestClock())
	if err != nil {
		t.Fatalf("newSolarDiscoveryRunner() error = %v", err)
	}
	goal, err := NewGoal("official robotics sources")
	if err != nil {
		t.Fatalf("NewGoal() error = %v", err)
	}

	// When
	first, err := runner.Discover(context.Background(), goal)
	if err != nil {
		t.Fatalf("first Discover() error = %v", err)
	}
	second, err := runner.Discover(context.Background(), goal)
	if err != nil {
		t.Fatalf("second Discover() error = %v", err)
	}

	// Then
	if factoryCalls.Load() != 2 {
		t.Fatalf("factory calls = %d, want 2", factoryCalls.Load())
	}
	if publicOriginHits.Load() != 2 {
		t.Fatalf("public origin hits = %d, want 2", publicOriginHits.Load())
	}
	for _, output := range []DiscoveryOutput{first, second} {
		if len(output.Sources) != 1 || output.Sources[0].URL != candidateURL {
			t.Fatalf("sources = %#v", output.Sources)
		}
		if output.ModelCalls != 3 || output.PromptTokens != 30 || output.CompletionTokens != 12 {
			t.Fatalf("model metadata = %#v", output)
		}
		if len(output.TruncationReasons) != 1 || output.TruncationReasons[0] != "depth_limit" {
			t.Fatalf("truncation reasons = %#v", output.TruncationReasons)
		}
		if output.YieldTrace.CrawlerValidated != 1 || output.YieldTrace.Accepted != 1 ||
			output.YieldTrace.Outcome != agent.YieldOutcomeAccepted {
			t.Fatalf("yield trace = %#v, want merged crawler and agent counts", output.YieldTrace)
		}
		if output.YieldTrace.OpenCalls != 0 {
			t.Fatalf("open calls = %d, want none when the model never opens a page", output.YieldTrace.OpenCalls)
		}
	}
	if opened := unusedOpener.opened(); len(opened) != 0 {
		t.Fatalf("opened = %#v, want no page fetched when the model never asks", opened)
	}
}

// countingOpener stands in for the live public page opener so the test can
// prove how many opens the server actually permits.
type countingOpener struct {
	mu    sync.Mutex
	calls []string
}

func (opener *countingOpener) Open(_ context.Context, url string) (agent.OpenedPage, error) {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	opener.calls = append(opener.calls, url)
	return agent.OpenedPage{Title: "Opened Robotics Expo", Snippet: "official schedule"}, nil
}

func (opener *countingOpener) opened() []string {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	return append([]string(nil), opener.calls...)
}

// The server wires an opener and holds it to two opens per request, so a live
// second hop can never eat the whole request deadline.
func TestSolarDiscoveryRunner_wires_an_opener_and_caps_it_at_two_opens(t *testing.T) {
	// Given
	candidateURL := "https://events.example/events/robotics"
	var modelCalls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var content string
		switch modelCalls.Add(1) {
		case 1:
			content = `{"action":"search","query":"robotics official calendar"}`
		case 2, 3, 4:
			content = fmt.Sprintf(`{"action":"open","url":%q}`, candidateURL)
		default:
			content = `{"action":"done"}`
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`, content); err != nil {
			t.Errorf("write model response: %v", err)
		}
	}))
	defer modelServer.Close()
	publicOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer publicOrigin.Close()
	backend := agent.Backend{Name: "solar", BaseURL: modelServer.URL, APIKey: "test-operator-key", Model: "solar-open2"}
	opener := &countingOpener{}
	var openerFactoryCalls atomic.Int32
	runner, err := newSolarDiscoveryRunner(backend, func() (publicSearchTool, error) {
		return &fixturePublicTool{
			originURL: publicOrigin.URL, candidateURL: candidateURL, client: publicOrigin.Client(),
		}, nil
	}, func() (agent.PageOpener, error) {
		openerFactoryCalls.Add(1)
		return opener, nil
	}, fixedTestClock())
	if err != nil {
		t.Fatalf("newSolarDiscoveryRunner() error = %v", err)
	}
	goal, err := NewGoal("official robotics sources")
	if err != nil {
		t.Fatalf("NewGoal() error = %v", err)
	}

	// When
	output, err := runner.Discover(context.Background(), goal)

	// Then
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if openerFactoryCalls.Load() != 1 {
		t.Fatalf("opener factory calls = %d, want one fresh opener per request", openerFactoryCalls.Load())
	}
	// The model asked to open three times; the server's ceiling is two, so the
	// third open must be refused without a fetch. The counts are written out
	// literally: a raised ceiling has to fail this test, not redefine it.
	opened := opener.opened()
	if len(opened) != 2 {
		t.Fatalf("opens = %#v, want exactly 2", opened)
	}
	for _, url := range opened {
		if url != candidateURL {
			t.Fatalf("opened %q, want only the offered candidate", url)
		}
	}
	if output.YieldTrace.OpenCalls != 2 {
		t.Fatalf("yield trace open calls = %d, want 2", output.YieldTrace.OpenCalls)
	}
	// The third open was answered with a correction rather than a fetch, so the
	// model still got its turn, and the per-call reservation (500) leaves room
	// for one more turn in which the model ends the run with done.
	if modelCalls.Load() != 5 {
		t.Fatalf("model calls = %d, want the refused open to cost a turn and the run to end on done", modelCalls.Load())
	}
}

func TestNewSolarDiscoveryRunner_rejects_non_solar_backend(t *testing.T) {
	// Given
	backend := agent.Backend{Name: "local", BaseURL: "http://127.0.0.1:18900/v1", Model: "local"}

	// When
	_, err := NewSolarDiscoveryRunner(backend)

	// Then
	if !errors.Is(err, ErrSolarBackendRequired) {
		t.Fatalf("error = %v, want ErrSolarBackendRequired", err)
	}
}

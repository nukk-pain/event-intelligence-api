package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiscoverWithOptions_returns_controlled_error_for_malformed_model_JSON(t *testing.T) {
	tests := []struct {
		name    string
		replies []modelReply
		results []SearchResult
		calls   int
	}{
		{
			name:    "first turn",
			replies: []modelReply{{content: `not JSON`}, {content: `still not JSON`}},
			calls:   2,
		},
		{
			name: "after a search",
			replies: []modelReply{
				searchAction("events"), modelReply{content: `{"sources":[`}, modelReply{content: `{"sources":[`},
			},
			results: []SearchResult{{Title: "Event", URL: "https://events.example/calendar"}}, calls: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			options := testOptions(t, DiscoveryProfileEvents)
			backend, model := newScriptedBackend(t, tt.replies...)

			// When
			_, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
				Backend: backend, Goal: "events", Tool: &staticSearch{results: tt.results}, Options: options,
			})

			// Then
			if !errors.Is(err, ErrMalformedModelResponse) {
				t.Fatalf("error = %v, want ErrMalformedModelResponse", err)
			}
			if got := len(model.capturedRequests()); got != tt.calls {
				t.Fatalf("model calls = %d, want %d", got, tt.calls)
			}
		})
	}
}

func TestDiscoverWithOptions_propagates_context_cancellation_before_model_call(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend, model := newScriptedBackend(t)

	// When
	_, err := DiscoverWithOptions(ctx, DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{}, Options: testOptions(t, DiscoveryProfileEvents),
	})

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := len(model.capturedRequests()); got != 0 {
		t.Fatalf("model calls = %d, want 0", got)
	}
}

func TestDiscoverWithOptions_propagates_per_call_deadline_to_hung_upstream(t *testing.T) {
	// Given
	requestStarted := make(chan struct{}, 1)
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-releaseHandler:
		}
	}))
	t.Cleanup(func() {
		close(releaseHandler)
		server.Close()
	})
	options := testOptions(t, DiscoveryProfileEvents)
	options.PerCallTimeout = 25 * time.Millisecond
	backend := Backend{Name: "hung", BaseURL: server.URL, Model: "test-model"}

	// When
	started := time.Now()
	_, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{}, Options: options,
	})
	duration := time.Since(started)

	// Then
	select {
	case <-requestStarted:
	default:
		t.Fatal("hung upstream did not receive request")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if duration > time.Second {
		t.Fatalf("deadline propagation took %s, want <= 1s", duration)
	}
}

func TestDiscoverWithOptions_stops_after_one_turn_when_call_budget_exhausted(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxModelCalls = 1
	backend, model := newScriptedBackend(t, searchAction("events"))
	search := &staticSearch{results: []SearchResult{{Title: "Event", URL: "https://events.example/calendar"}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if run.Trace.Calls != 1 || len(model.capturedRequests()) != 1 {
		t.Fatalf("calls trace=%d HTTP=%d, want 1", run.Trace.Calls, len(model.capturedRequests()))
	}
	if len(run.Sources) != 0 || !containsTruncation(run.Metadata, TruncationModelCallBudget) {
		t.Fatalf("run = %#v, want call-budget truncation without sources", run)
	}
}

func TestDiscoverWithOptions_caps_outbound_tokens_and_records_misleading_usage(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxCompletionTokens = 5
	options.MaxTokensPerCall = 4
	accept := acceptAction("https://events.example/calendar")
	accept.completionTokens = 3
	backend, model := newScriptedBackend(t,
		modelReply{content: `{"action":"search","query":"events"}`, completionTokens: 3}, accept,
	)
	search := &staticSearch{results: []SearchResult{{Title: "Event", URL: "https://events.example/calendar"}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	requests := model.capturedRequests()
	if len(requests) != 2 || requests[0].MaxTokens != 4 || requests[1].MaxTokens != 1 {
		t.Fatalf("max_tokens sequence = %#v, want [4 1]", requests)
	}
	if run.Trace.Usage.CompletionTokens != 6 {
		t.Fatalf("reported completion tokens = %d, want honest upstream total 6", run.Trace.Usage.CompletionTokens)
	}
	if !containsTruncation(run.Metadata, TruncationCompletionTokenBudget) {
		t.Fatalf("truncation reasons = %v, want completion budget", run.Metadata.TruncationReasons)
	}
}

func TestDiscoverWithOptions_clamps_hard_search_turn_call_and_completion_limits(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxSearches = 99
	options.MaxTurns = 99
	options.MaxModelCalls = 99
	options.MaxCompletionTokens = 99_999
	options.MaxTokensPerCall = 1000
	first := "https://events.example/one"
	second := "https://events.example/two"
	replies := []modelReply{
		{content: `{"action":"search","query":"one"}`, completionTokens: 1000},
		acceptAction(first),
		{content: `{"action":"search","query":"two"}`, completionTokens: 1000},
		acceptAction(second),
	}
	replies[1].completionTokens = 1000
	replies[3].completionTokens = 1000
	backend, model := newScriptedBackend(t, replies...)
	search := &sequenceSearch{batches: [][]SearchResult{
		{{Title: "One", URL: first}},
		{{Title: "Two", URL: second}},
	}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	requests := model.capturedRequests()
	// Four 1000-token replies exhaust the hard completion-token ceiling, so the
	// run stops there even though the raised call budget clamps to 8.
	if len(requests) != 4 || run.Trace.Calls != 4 {
		t.Fatalf("calls HTTP=%d trace=%d, want 4 before the token ceiling stops the run", len(requests), run.Trace.Calls)
	}
	wantMaxTokens := []int{1000, 1000, 1000, 1000}
	for i, request := range requests {
		if request.MaxTokens != wantMaxTokens[i] {
			t.Fatalf("request %d max_tokens = %d, want %d", i, request.MaxTokens, wantMaxTokens[i])
		}
	}
	if run.Trace.Usage.CompletionTokens != 4000 {
		t.Fatalf("completion tokens = %d, want 4000", run.Trace.Usage.CompletionTokens)
	}
	if run.Metadata.Budget.MaxSearches != 2 || run.Metadata.Budget.MaxTurns != 8 || run.Metadata.Budget.MaxModelCalls != 8 ||
		run.Metadata.Budget.MaxCompletionTokens != 4000 || run.Metadata.Budget.CompletionTokensReserved != 4000 {
		t.Fatalf("hard-clamped budget = %#v", run.Metadata.Budget)
	}
}

func TestValidateDiscoverOptions_clamps_caller_raised_candidate_limits(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxCandidates = 300
	options.MaxCandidatesPerSource = 300

	// When
	validated, err := validateDiscoverOptions(options)

	// Then
	if err != nil {
		t.Fatalf("validateDiscoverOptions() error = %v", err)
	}
	if validated.MaxCandidates != 30 || validated.MaxCandidatesPerSource != 30 {
		t.Fatalf("candidate limits = total %d per-source %d, want hard maximum 30 for both",
			validated.MaxCandidates, validated.MaxCandidatesPerSource)
	}
	t.Logf("normalized candidate limits: total=%d per_source=%d", validated.MaxCandidates, validated.MaxCandidatesPerSource)
}

func TestValidateDiscoverOptions_clamps_caller_raised_goal_and_timeout_limits(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.GoalMaxRunes = 8_000
	options.PerCallTimeout = 10 * time.Minute

	// When
	validated, err := validateDiscoverOptions(options)

	// Then
	if err != nil {
		t.Fatalf("validateDiscoverOptions() error = %v", err)
	}
	if validated.GoalMaxRunes != 800 {
		t.Fatalf("goal limit = %d, want hard maximum 800 runes", validated.GoalMaxRunes)
	}
	if validated.PerCallTimeout != time.Minute {
		t.Fatalf("per-call timeout = %s, want hard maximum 1m0s", validated.PerCallTimeout)
	}
	t.Logf("normalized goal/timeout limits: goal_runes=%d per_call_timeout=%s",
		validated.GoalMaxRunes, validated.PerCallTimeout)
}

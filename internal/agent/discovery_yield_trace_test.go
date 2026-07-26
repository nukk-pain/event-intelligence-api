package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDiscoverWithOptions_characterizesExactSelectionAndBudgets(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	candidateURL := "https://events.example/calendar"
	backend, _ := newScriptedBackend(t, proposal("events"), judgment(options.Profile, candidateURL))

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend,
		Goal:    "events",
		Tool:    &staticSearch{results: []SearchResult{{Title: "Calendar", URL: candidateURL}}},
		Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if len(run.Sources) != 1 || run.Sources[0].URL != candidateURL {
		t.Fatalf("sources = %#v, want exact offered URL", run.Sources)
	}
	if run.Metadata.CandidatesOffered != 1 || run.Trace.Calls != 2 {
		t.Fatalf("offered/calls = %d/%d, want 1/2", run.Metadata.CandidatesOffered, run.Trace.Calls)
	}
}

func TestDiscoverWithOptionsYieldTrace(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	candidateURL := "https://events.example/calendar"
	backend, _ := newScriptedBackend(t, proposal("events"), judgment(options.Profile, candidateURL), doneProposal())

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend,
		Goal:    "private-goal-marker operator-secret-marker",
		Tool: &staticSearch{results: []SearchResult{
			{Title: "Calendar", URL: candidateURL, Snippet: "private-page-marker"},
		}},
		Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	want := YieldTrace{
		Outcome: YieldOutcomeAccepted, TerminalReason: YieldTerminalRoundLimit,
		Offered: 1, ProposalCalls: 1, JudgeCalls: 1, JudgeEntriesParsed: 1, Accepted: 1,
	}
	if run.YieldTrace != want {
		t.Fatalf("yield trace = %#v, want %#v", run.YieldTrace, want)
	}
	wire, marshalErr := json.Marshal(run.YieldTrace)
	if marshalErr != nil {
		t.Fatalf("marshal yield trace: %v", marshalErr)
	}
	wantWire := `{"outcome":"accepted","terminal_reason":"round_limit","crawler_validated":0,"offered":1,"prefilter_dropped":0,"proposal_calls":1,"judge_calls":1,"judge_entries_parsed":1,"judge_entries_dropped":0,"accepted":1}`
	if string(wire) != wantWire {
		t.Fatalf("serialized yield trace = %s, want %s", wire, wantWire)
	}
	t.Logf("redacted_yield_trace=%s", wire)
	for _, sensitive := range []string{"private-goal-marker", "operator-secret-marker", "private-page-marker", candidateURL} {
		if strings.Contains(string(wire), sensitive) {
			t.Fatalf("serialized yield trace leaked sensitive value %q: %s", sensitive, wire)
		}
	}
	isolated, isolatedErr := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "stale_state", Tool: &staticSearch{}, Options: options,
	})
	if isolatedErr != nil {
		t.Fatalf("isolated DiscoverWithOptions() error = %v", isolatedErr)
	}
	wantIsolated := YieldTrace{
		Outcome: YieldOutcomeCandidateEmpty, TerminalReason: YieldTerminalProposalDone, ProposalCalls: 1,
	}
	if isolated.YieldTrace != wantIsolated {
		t.Fatalf("isolated yield trace = %#v, want %#v", isolated.YieldTrace, wantIsolated)
	}
	merged := isolated.YieldTrace.WithCrawlerValidated(4)
	if merged.CrawlerValidated != 4 || merged.Outcome != YieldOutcomeOfferedEmpty {
		t.Fatalf("merged crawler trace = %#v, want validated=4 offered_empty", merged)
	}
}

func TestDiscoverWithOptionsYieldTraceDropsMalformedSelection(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxRounds = 2
	backend, _ := newScriptedBackend(t,
		proposal("events"),
		modelReply{content: `{"sources":[{"url":"https://events.example/calendar","reason":"operator-secret-marker"}]}`},
		doneProposal(),
	)

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend,
		Goal:    "events",
		Tool:    &staticSearch{results: []SearchResult{{Title: "Calendar", URL: "https://events.example/calendar"}}},
		Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if run.YieldTrace.Outcome != YieldOutcomeJudgeEmpty || run.YieldTrace.TerminalReason != YieldTerminalProposalDone ||
		run.YieldTrace.JudgeEntriesParsed != 0 || run.YieldTrace.JudgeEntriesDropped != 1 || run.YieldTrace.Accepted != 0 {
		t.Fatalf("yield trace = %#v, want bounded malformed-entry drop", run.YieldTrace)
	}
	wire, marshalErr := json.Marshal(run.YieldTrace)
	if marshalErr != nil {
		t.Fatalf("marshal yield trace: %v", marshalErr)
	}
	if strings.Contains(string(wire), "operator-secret-marker") {
		t.Fatalf("serialized yield trace leaked judge content: %s", wire)
	}
}

func TestDiscoverWithOptionsYieldTraceRequestIsolation(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	backend, _ := newScriptedBackend(t,
		proposal("first"), judgment(options.Profile, "https://events.example/first"),
		doneProposal(),
	)

	// When
	first, firstErr := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "first", Tool: &staticSearch{results: []SearchResult{{Title: "First", URL: "https://events.example/first"}}}, Options: options,
	})
	second, secondErr := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "second", Tool: &staticSearch{}, Options: options,
	})

	// Then
	if firstErr != nil || secondErr != nil {
		t.Fatalf("request errors = %v / %v", firstErr, secondErr)
	}
	if first.YieldTrace.Accepted != 1 || second.YieldTrace.Accepted != 0 || second.YieldTrace.Offered != 0 ||
		second.YieldTrace.ProposalCalls != 1 || second.YieldTrace.Outcome != YieldOutcomeCandidateEmpty {
		t.Fatalf("request traces = %#v / %#v, want isolated counters", first.YieldTrace, second.YieldTrace)
	}
}

func TestDiscoverWithOptionsYieldTraceTopLevelMalformedJudgeIsError(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	backend, _ := newScriptedBackend(t, proposal("events"), modelReply{content: `{"sources":[`})

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{results: []SearchResult{{Title: "Calendar", URL: "https://events.example/calendar"}}}, Options: options,
	})

	// Then
	if !errors.Is(err, ErrMalformedModelResponse) {
		t.Fatalf("error = %v, want ErrMalformedModelResponse", err)
	}
	if run.YieldTrace.Outcome != YieldOutcomeError || run.YieldTrace.TerminalReason != YieldTerminalMalformedJudgeEnvelope ||
		run.YieldTrace.JudgeEntriesDropped != 0 {
		t.Fatalf("yield trace = %#v, want envelope error without entry drop", run.YieldTrace)
	}
}

func TestDiscoverWithOptionsYieldTraceSelectionBoundaries(t *testing.T) {
	tests := []struct {
		name          string
		replies       func(DiscoveryProfile) []modelReply
		results       []SearchResult
		configure     func(*DiscoverOptions)
		wantOutcome   YieldOutcome
		wantReason    YieldTerminalReason
		wantOffered   int
		wantPrefilter int
		wantParsed    int
		wantDropped   int
		wantAccepted  int
	}{
		{
			name: "candidate_empty", replies: func(DiscoveryProfile) []modelReply { return []modelReply{doneProposal()} },
			wantOutcome: YieldOutcomeCandidateEmpty, wantReason: YieldTerminalProposalDone,
		},
		{
			name: "prefilter_rejection", replies: func(DiscoveryProfile) []modelReply {
				return []modelReply{proposal("events"), doneProposal()}
			},
			results:     []SearchResult{{Title: "Private", URL: "http://127.0.0.1/private"}},
			configure:   func(options *DiscoverOptions) { options.MaxRounds = 2 },
			wantOutcome: YieldOutcomeOfferedEmpty, wantReason: YieldTerminalProposalDone, wantPrefilter: 1,
		},
		{
			name: "false_selection", replies: func(profile DiscoveryProfile) []modelReply {
				return []modelReply{
					proposal("events"),
					{content: `{"sources":[{"url":"https://events.example/calendar","` + profile.OutputLabel + `":false,"reason":"misleading_success_output"}]}`},
					doneProposal(),
				}
			},
			results:     []SearchResult{{Title: "Calendar", URL: "https://events.example/calendar"}},
			configure:   func(options *DiscoverOptions) { options.MaxRounds = 2 },
			wantOutcome: YieldOutcomeJudgeEmpty, wantReason: YieldTerminalProposalDone,
			wantOffered: 1, wantParsed: 1, wantDropped: 1,
		},
		{
			name: "non_exact_selection", replies: func(profile DiscoveryProfile) []modelReply {
				return []modelReply{
					proposal("events"), judgment(profile, "https://events.example/calendar?invented=true"), doneProposal(),
				}
			},
			results:     []SearchResult{{Title: "Calendar", URL: "https://events.example/calendar"}},
			configure:   func(options *DiscoverOptions) { options.MaxRounds = 2 },
			wantOutcome: YieldOutcomeJudgeEmpty, wantReason: YieldTerminalProposalDone,
			wantOffered: 1, wantParsed: 1, wantDropped: 1,
		},
		{
			name: "model_call_budget", replies: func(DiscoveryProfile) []modelReply { return []modelReply{proposal("events")} },
			configure:   func(options *DiscoverOptions) { options.MaxModelCalls = 1 },
			wantOutcome: YieldOutcomeBudgetStopped, wantReason: YieldTerminalModelCallBudget,
		},
		{
			name: "accepted_plus_token_budget", replies: func(profile DiscoveryProfile) []modelReply {
				return []modelReply{proposal("events"), judgment(profile, "https://events.example/calendar")}
			},
			results: []SearchResult{{Title: "Calendar", URL: "https://events.example/calendar"}},
			configure: func(options *DiscoverOptions) {
				options.MaxCompletionTokens = 2
				options.MaxTokensPerCall = 1
			},
			wantOutcome: YieldOutcomeAccepted, wantReason: YieldTerminalTokenBudget,
			wantOffered: 1, wantParsed: 1, wantAccepted: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			options := testOptions(t, DiscoveryProfileEvents)
			if tt.configure != nil {
				tt.configure(&options)
			}
			backend, _ := newScriptedBackend(t, tt.replies(options.Profile)...)

			// When
			run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
				Backend: backend, Goal: "events", Tool: &staticSearch{results: tt.results}, Options: options,
			})

			// Then
			if err != nil {
				t.Fatalf("DiscoverWithOptions() error = %v", err)
			}
			got := run.YieldTrace
			if got.Outcome != tt.wantOutcome || got.TerminalReason != tt.wantReason || got.Offered != tt.wantOffered ||
				got.PrefilterDropped != tt.wantPrefilter || got.JudgeEntriesParsed != tt.wantParsed ||
				got.JudgeEntriesDropped != tt.wantDropped || got.Accepted != tt.wantAccepted {
				t.Fatalf("yield trace = %#v, want outcome=%q reason=%q offered=%d prefilter=%d parsed=%d dropped=%d accepted=%d",
					got, tt.wantOutcome, tt.wantReason, tt.wantOffered, tt.wantPrefilter, tt.wantParsed, tt.wantDropped, tt.wantAccepted)
			}
		})
	}
}

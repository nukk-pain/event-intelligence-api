package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The loop must execute whatever the model returns, in whatever order it
// returns it. These tests script the model turn by turn and assert the run
// followed that script — the point is that no code path imposes an order.

func TestDiscoveryActionLoop_runs_search_search_accept_done_in_model_order(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxSearches = 2
	first := "https://events.example/one"
	second := "https://events.example/two"
	backend, model := newScriptedBackend(t,
		searchAction("first query"), searchAction("second query"), acceptAction(first, second), doneAction(),
	)
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
	if run.YieldTrace.TerminalReason != YieldTerminalProposalDone {
		t.Fatalf("terminal reason = %q, want %q", run.YieldTrace.TerminalReason, YieldTerminalProposalDone)
	}
	if len(run.Sources) != 2 || run.Sources[0].URL != first || run.Sources[1].URL != second {
		t.Fatalf("sources = %#v, want both searched candidates accepted in one turn", run.Sources)
	}
	// Two searches ran, and the second turn's prompt carried the first query
	// forward: the candidate table accumulates across searches.
	requests := model.capturedRequests()
	if len(requests) != 4 {
		t.Fatalf("model requests = %d, want one per scripted turn", len(requests))
	}
	if !strings.Contains(requests[1].Messages[1].Content, "first query") {
		t.Fatalf("second turn prompt lost the first tried query:\n%s", requests[1].Messages[1].Content)
	}
	acceptTurn := requests[2].Messages[1].Content
	if !strings.Contains(acceptTurn, first) || !strings.Contains(acceptTurn, second) {
		t.Fatalf("accept turn candidate table did not accumulate both searches:\n%s", acceptTurn)
	}
	if run.YieldTrace.ProposalCalls != 3 || run.YieldTrace.JudgeCalls != 1 {
		t.Fatalf("call split = proposal %d judge %d, want 3/1 (search+search+done vs accept)",
			run.YieldTrace.ProposalCalls, run.YieldTrace.JudgeCalls)
	}
	if run.YieldTrace.JudgeEntriesParsed != 2 || run.YieldTrace.Accepted != 2 {
		t.Fatalf("yield trace = %#v, want both selections parsed and accepted", run.YieldTrace)
	}
}

// Accept on the very first turn is legal: the loop does not require a search to
// have happened. The empty table simply drops the selection and the run
// continues, so the model can search afterwards and accept for real.
func TestDiscoveryActionLoop_accepts_before_any_search_without_forcing_an_order(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	candidateURL := "https://events.example/calendar"
	backend, _ := newScriptedBackend(t,
		acceptAction(candidateURL), searchAction("events"), acceptAction(candidateURL), doneAction(),
	)
	search := &staticSearch{results: []SearchResult{{Title: "Calendar", URL: candidateURL}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalProposalDone {
		t.Fatalf("terminal reason = %q, want the scripted done turn to end the run", run.YieldTrace.TerminalReason)
	}
	if len(run.Sources) != 1 || run.Sources[0].URL != candidateURL {
		t.Fatalf("sources = %#v, want the later accept to succeed", run.Sources)
	}
	if run.YieldTrace.JudgeCalls != 2 || run.YieldTrace.JudgeEntriesParsed != 2 {
		t.Fatalf("yield trace = %#v, want both accept turns counted", run.YieldTrace)
	}
	if run.YieldTrace.JudgeEntriesDropped != 1 || run.YieldTrace.Accepted != 1 {
		t.Fatalf("yield trace = %#v, want the pre-search accept dropped against an empty table", run.YieldTrace)
	}
}

func TestDiscoveryActionLoop_stops_at_the_turn_budget_when_the_model_never_stops(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxSearches = 2
	options.MaxTurns = 2
	backend, model := newScriptedBackend(t, searchAction("one"), searchAction("two"), searchAction("three"))
	search := &sequenceSearch{batches: [][]SearchResult{nil, nil}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if got := len(model.capturedRequests()); got != 2 {
		t.Fatalf("model calls = %d, want the turn budget to stop the loop at 2", got)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalRoundLimit || run.YieldTrace.Outcome != YieldOutcomeBudgetStopped {
		t.Fatalf("yield trace = %#v, want a budget-stopped turn-limit terminal", run.YieldTrace)
	}
	if !run.Metadata.Truncated || !containsTruncation(run.Metadata, TruncationRoundLimit) {
		t.Fatalf("truncation = %v, want the turn limit recorded", run.Metadata.TruncationReasons)
	}
}

func TestDiscoveryActionLoop_recovers_from_one_malformed_action(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	backend, model := newScriptedBackend(t, modelReply{content: `not an action`}, doneAction())

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{}, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v, want recovery after one correction", err)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalProposalDone {
		t.Fatalf("terminal reason = %q, want the corrected turn to be honored", run.YieldTrace.TerminalReason)
	}
	requests := model.capturedRequests()
	if len(requests) != 2 {
		t.Fatalf("model calls = %d, want the correction to cost exactly one extra turn", len(requests))
	}
	if strings.Contains(requests[0].Messages[0].Content, correctionMalformedAction) {
		t.Fatal("first turn carried a correction it had not yet earned")
	}
	if !strings.Contains(requests[1].Messages[0].Content, correctionMalformedAction) {
		t.Fatalf("retry turn missing the correction instruction:\n%s", requests[1].Messages[0].Content)
	}
	if run.YieldTrace.ProposalCalls != 2 {
		t.Fatalf("proposal calls = %d, want the malformed turn counted against the budget", run.YieldTrace.ProposalCalls)
	}
}

func TestDiscoveryActionLoop_ends_on_a_second_consecutive_malformed_action(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	backend, model := newScriptedBackend(t,
		modelReply{content: `not an action`}, modelReply{content: `{"action":"delete"}`},
	)

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{}, Options: options,
	})

	// Then
	if !errors.Is(err, ErrMalformedModelResponse) || !errors.Is(err, errMalformedAction) {
		t.Fatalf("error = %v, want a controlled malformed-action error", err)
	}
	if got := len(model.capturedRequests()); got != 2 {
		t.Fatalf("model calls = %d, want exactly one correction attempt", got)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalMalformedAction || run.YieldTrace.Outcome != YieldOutcomeError {
		t.Fatalf("yield trace = %#v, want the malformed-action terminal", run.YieldTrace)
	}
	if len(run.Sources) != 0 {
		t.Fatalf("sources = %#v, want none from a run that never produced an action", run.Sources)
	}
}

// A search with no search budget left is well formed but not currently
// available. It earns the same single correction, and a repeat ends the run —
// gracefully, so anything already accepted survives.
func TestDiscoveryActionLoop_ends_when_the_model_keeps_searching_with_no_searches_left(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxSearches = 1
	candidateURL := "https://events.example/calendar"
	backend, model := newScriptedBackend(t,
		searchAction("events"), acceptAction(candidateURL), searchAction("again"), searchAction("and again"),
	)
	search := &staticSearch{results: []SearchResult{{Title: "Calendar", URL: candidateURL}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v, want a graceful stop", err)
	}
	if got := len(search.queries); got != 1 {
		t.Fatalf("executed searches = %d, want the search budget enforced", got)
	}
	requests := model.capturedRequests()
	if len(requests) != 4 {
		t.Fatalf("model calls = %d, want one correction before stopping", len(requests))
	}
	if !strings.Contains(requests[3].Messages[0].Content, correctionSearchExhausted) {
		t.Fatalf("retry turn missing the search-exhausted correction:\n%s", requests[3].Messages[0].Content)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalMalformedAction {
		t.Fatalf("terminal reason = %q, want %q", run.YieldTrace.TerminalReason, YieldTerminalMalformedAction)
	}
	if len(run.Sources) != 1 || run.Sources[0].URL != candidateURL {
		t.Fatalf("sources = %#v, want the earlier accept preserved", run.Sources)
	}
}

// A request with no page opener cannot open anything, however large its open
// budget is, so choosing open is a contract violation that ends the run without
// any fetch.
func TestDiscoveryActionLoop_refuses_the_open_action_and_never_fetches(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 3
	open := modelReply{content: `{"action":"open","url":"https://events.example/calendar"}`, completionTokens: 1}
	backend, model := newScriptedBackend(t, open, open)

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{}, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v, want a graceful stop", err)
	}
	requests := model.capturedRequests()
	if len(requests) != 2 {
		t.Fatalf("model calls = %d, want one correction before stopping", len(requests))
	}
	if strings.Contains(requests[0].Messages[0].Content, `"action":"open"`) {
		t.Fatal("open action was offered in the menu while it is unavailable")
	}
	if !strings.Contains(requests[1].Messages[0].Content, correctionActionUnavailable) {
		t.Fatalf("retry turn missing the unavailable-action correction:\n%s", requests[1].Messages[0].Content)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalMalformedAction || len(run.Sources) != 0 {
		t.Fatalf("run = %#v, want a malformed-action terminal with no sources", run)
	}
}

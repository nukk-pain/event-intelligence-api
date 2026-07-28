package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeOpener records every URL it was asked to fetch, so a test can prove that a
// refused open never reached the network at all.
type fakeOpener struct {
	mu    sync.Mutex
	pages map[string]OpenedPage
	err   error
	calls []string
}

func (opener *fakeOpener) Open(ctx context.Context, url string) (OpenedPage, error) {
	if err := ctx.Err(); err != nil {
		return OpenedPage{}, err
	}
	opener.mu.Lock()
	defer opener.mu.Unlock()
	opener.calls = append(opener.calls, url)
	if opener.err != nil {
		return OpenedPage{}, opener.err
	}
	return opener.pages[url], nil
}

func (opener *fakeOpener) opened() []string {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	return append([]string(nil), opener.calls...)
}

func openAction(url string) modelReply {
	return modelReply{content: fmt.Sprintf(`{"action":"open","url":%q}`, url), completionTokens: 1}
}

// This is the whole reason the open action exists: a search hit that carries no
// title is unusable as a candidate, but it is not worthless — opening the page
// can supply the missing field and rescue it into the judgeable table.
func TestDiscoveryOpen_rescuesAPendingCandidateIntoTheCandidateTable(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 2
	candidateURL := "https://events.example/robot-expo"
	opener := &fakeOpener{pages: map[string]OpenedPage{
		candidateURL: {Title: "Robot Expo 2027", Snippet: "official expo page", Date: "2030-03-01", Location: "COEX"},
	}}
	backend, model := newScriptedBackend(t,
		searchAction("robot expo"), openAction(candidateURL), acceptAction(candidateURL), doneAction(),
	)
	search := &staticSearch{results: []SearchResult{{URL: candidateURL, Snippet: "listing"}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Opener: opener, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if got := opener.opened(); len(got) != 1 || got[0] != candidateURL {
		t.Fatalf("opened = %#v, want exactly the pending candidate URL", got)
	}
	if len(run.Sources) != 1 || run.Sources[0].URL != candidateURL {
		t.Fatalf("sources = %#v, want the rescued candidate accepted", run.Sources)
	}
	source := run.Sources[0]
	if source.Title != "Robot Expo 2027" || source.Date != "2030-03-01" || source.Location != "COEX" {
		t.Fatalf("source = %#v, want the opened page backfilled into the empty fields", source)
	}
	if run.YieldTrace.OpenCalls != 1 {
		t.Fatalf("open calls = %d, want 1", run.YieldTrace.OpenCalls)
	}
	// The rescue must show up on the offered counters, exactly like a candidate
	// that entered the table through search.
	if run.YieldTrace.Offered != 1 || run.Metadata.CandidatesOffered != 1 {
		t.Fatalf("offered = %d / metadata %d, want the promotion counted once",
			run.YieldTrace.Offered, run.Metadata.CandidatesOffered)
	}
	// Holding the candidate as pending must not rewrite the prefilter history:
	// it really was dropped at the prefilter boundary.
	if run.YieldTrace.PrefilterDropped != 1 || run.YieldTrace.PrefilterReasons != (PrefilterReasons{MissingTitle: 1}) {
		t.Fatalf("prefilter = %d %#v, want the original drop preserved",
			run.YieldTrace.PrefilterDropped, run.YieldTrace.PrefilterReasons)
	}
	if run.YieldTrace.Accepted != 1 || run.YieldTrace.TerminalReason != YieldTerminalProposalDone {
		t.Fatalf("yield trace = %#v, want an accepted run ending on done", run.YieldTrace)
	}
	// The pending row was offered to the model before the open, with its
	// missing field named.
	openTurn := model.capturedRequests()[1].Messages[1].Content
	if !strings.Contains(openTurn, "<untrusted-pending>") || !strings.Contains(openTurn, "[missing: title] "+candidateURL) {
		t.Fatalf("open turn prompt did not offer the pending candidate:\n%s", openTurn)
	}
	// After promotion the row lives in the candidate table, not in pending.
	acceptTurn := model.capturedRequests()[2].Messages[1].Content
	pendingBlock := acceptTurn[strings.Index(acceptTurn, "<untrusted-pending>"):]
	if strings.Contains(pendingBlock, candidateURL) {
		t.Fatalf("promoted candidate still listed as pending:\n%s", acceptTurn)
	}
	if !strings.Contains(acceptTurn, "1. Robot Expo 2027 — "+candidateURL) {
		t.Fatalf("promoted candidate missing from the candidate table:\n%s", acceptTurn)
	}
}

// An open for a URL the server never offered is an invented fetch target. It
// must be refused on the correction path without any network call at all.
func TestDiscoveryOpen_refusesAURLThatIsNeitherOfferedNorPending(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 2
	invented := "https://invented.example/calendar"
	opener := &fakeOpener{pages: map[string]OpenedPage{invented: {Title: "Should never be fetched"}}}
	backend, model := newScriptedBackend(t, openAction(invented), openAction(invented))

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: &staticSearch{}, Opener: opener, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v, want a graceful stop", err)
	}
	if got := opener.opened(); len(got) != 0 {
		t.Fatalf("opened = %#v, want no fetch for an unoffered URL", got)
	}
	if run.YieldTrace.OpenCalls != 0 {
		t.Fatalf("open calls = %d, want a refused open uncounted", run.YieldTrace.OpenCalls)
	}
	requests := model.capturedRequests()
	if len(requests) != 2 {
		t.Fatalf("model calls = %d, want exactly one correction before stopping", len(requests))
	}
	if !strings.Contains(requests[1].Messages[0].Content, correctionActionUnavailable) {
		t.Fatalf("retry turn missing the unavailable-action correction:\n%s", requests[1].Messages[0].Content)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalMalformedAction {
		t.Fatalf("terminal reason = %q, want %q", run.YieldTrace.TerminalReason, YieldTerminalMalformedAction)
	}
}

// A page that will not load is an ordinary fact of the open web, not a contract
// violation. The turn is spent, nothing changes, and the run keeps going.
func TestDiscoveryOpen_treatsAnOpenerErrorAsNonFatal(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 2
	candidateURL := "https://events.example/calendar"
	opener := &fakeOpener{err: errors.New("fetch failed")}
	backend, model := newScriptedBackend(t,
		searchAction("events"), openAction(candidateURL), acceptAction(candidateURL), doneAction(),
	)
	search := &staticSearch{results: []SearchResult{{Title: "Calendar", URL: candidateURL, Date: "2030-05-05"}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Opener: opener, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v, want a failed open to be non-fatal", err)
	}
	if run.YieldTrace.OpenCalls != 1 {
		t.Fatalf("open calls = %d, want an attempted fetch counted even when it failed", run.YieldTrace.OpenCalls)
	}
	if len(run.Sources) != 1 || run.Sources[0].Title != "Calendar" || run.Sources[0].Date != "2030-05-05" {
		t.Fatalf("sources = %#v, want the candidate untouched by the failed open", run.Sources)
	}
	if run.YieldTrace.TerminalReason != YieldTerminalProposalDone {
		t.Fatalf("terminal reason = %q, want the loop to have continued to done", run.YieldTrace.TerminalReason)
	}
	requests := model.capturedRequests()
	if len(requests) != 4 {
		t.Fatalf("model calls = %d, want the loop to continue after the failed open", len(requests))
	}
	if !strings.Contains(requests[2].Messages[0].Content, noteOpenFailed) {
		t.Fatalf("turn after the failed open missing the server-owned note:\n%s", requests[2].Messages[0].Content)
	}
	if strings.Contains(requests[3].Messages[0].Content, noteOpenFailed) {
		t.Fatalf("open-failure note outlived its one turn:\n%s", requests[3].Messages[0].Content)
	}
	// A failed open is not a contract violation, so it must not consume the
	// one-shot correction the loop reserves for those.
	if strings.Contains(requests[2].Messages[0].Content, correctionActionUnavailable) {
		t.Fatalf("failed open was treated as a contract violation:\n%s", requests[2].Messages[0].Content)
	}
}

// The open budget is a hard limit, so once it is spent the action and the
// pending list it exists to serve both disappear from the prompt.
func TestDiscoveryOpen_withdrawsTheActionAndPendingListWhenTheBudgetIsSpent(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 1
	pendingURL := "https://events.example/untitled"
	tableURL := "https://events.example/calendar"
	opener := &fakeOpener{pages: map[string]OpenedPage{tableURL: {Snippet: "official calendar"}}}
	backend, model := newScriptedBackend(t, searchAction("events"), openAction(tableURL), doneAction())
	search := &staticSearch{results: []SearchResult{
		{Title: "Calendar", URL: tableURL},
		{URL: pendingURL},
	}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Opener: opener, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if run.YieldTrace.OpenCalls != 1 {
		t.Fatalf("open calls = %d, want the single budgeted open", run.YieldTrace.OpenCalls)
	}
	requests := model.capturedRequests()
	if len(requests) != 3 {
		t.Fatalf("model calls = %d, want search, open, done", len(requests))
	}
	before := requests[1]
	if !strings.Contains(before.Messages[0].Content, `{"action":"open","url":`) {
		t.Fatalf("open turn lost the open action from the menu:\n%s", before.Messages[0].Content)
	}
	if !strings.Contains(before.Messages[1].Content, "<untrusted-pending>") ||
		!strings.Contains(before.Messages[1].Content, pendingURL) {
		t.Fatalf("open turn lost the pending block:\n%s", before.Messages[1].Content)
	}
	after := requests[2]
	if strings.Contains(after.Messages[0].Content, `{"action":"open","url":`) {
		t.Fatalf("open action still offered after the budget was spent:\n%s", after.Messages[0].Content)
	}
	if strings.Contains(after.Messages[1].Content, "<untrusted-pending>") ||
		strings.Contains(after.Messages[1].Content, "Remaining opens") {
		t.Fatalf("pending block still rendered after the budget was spent:\n%s", after.Messages[1].Content)
	}
}

// An opened page is a supplement, never an override: whatever the search
// provider already established about a candidate stands.
func TestDiscoveryOpen_neverOverwritesAFieldThatAlreadyHasAValue(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 1
	candidateURL := "https://events.example/calendar"
	opener := &fakeOpener{pages: map[string]OpenedPage{candidateURL: {
		Title: "Overwritten Title", Snippet: "overwritten snippet", Date: "1999-01-01", Location: "Seoul",
	}}}
	backend, _ := newScriptedBackend(t,
		searchAction("events"), openAction(candidateURL), acceptAction(candidateURL), doneAction(),
	)
	search := &staticSearch{results: []SearchResult{
		{Title: "Original Title", URL: candidateURL, Snippet: "original snippet", Date: "2030-05-05"},
	}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Opener: opener, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if len(run.Sources) != 1 {
		t.Fatalf("sources = %#v, want the opened candidate accepted", run.Sources)
	}
	source := run.Sources[0]
	if source.Title != "Original Title" || source.Date != "2030-05-05" {
		t.Fatalf("source = %#v, want the pre-existing title and date preserved", source)
	}
	if source.Location != "Seoul" {
		t.Fatalf("source location = %q, want the empty field filled from the opened page", source.Location)
	}
}

// Without an opener there is nothing to open, so the action must stay off the
// menu no matter how generous the open budget is.
func TestDiscoveryOpen_neverOpensWhenNoOpenerIsWired(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 3
	candidateURL := "https://events.example/calendar"
	backend, model := newScriptedBackend(t,
		searchAction("events"), openAction(candidateURL), openAction(candidateURL),
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
	if run.YieldTrace.OpenCalls != 0 {
		t.Fatalf("open calls = %d, want none without an opener", run.YieldTrace.OpenCalls)
	}
	requests := model.capturedRequests()
	if len(requests) != 3 {
		t.Fatalf("model calls = %d, want one correction before stopping", len(requests))
	}
	for i, request := range requests {
		if strings.Contains(request.Messages[0].Content, `{"action":"open","url":`) {
			t.Fatalf("turn %d offered the open action without an opener:\n%s", i, request.Messages[0].Content)
		}
		if strings.Contains(request.Messages[1].Content, "<untrusted-pending>") {
			t.Fatalf("turn %d rendered a pending block without an opener:\n%s", i, request.Messages[1].Content)
		}
	}
	if run.YieldTrace.TerminalReason != YieldTerminalMalformedAction {
		t.Fatalf("terminal reason = %q, want %q", run.YieldTrace.TerminalReason, YieldTerminalMalformedAction)
	}
}

// Only a metadata shortfall is recoverable by opening a page. A URL the profile
// rejects outright, or an event that already happened, stays rejected — holding
// it as pending would hand the model a fetch target the prefilter refused.
func TestDiscoveryOpen_neverHoldsCandidatesRejectedForNonMetadataReasons(t *testing.T) {
	tests := []struct {
		name    string
		profile DiscoveryProfileName
		result  SearchResult
		openURL string
	}{
		{
			name:    "unparseable URL",
			profile: DiscoveryProfileEvents,
			result:  SearchResult{Title: "Expo", URL: "://not-a-url"},
			openURL: "://not-a-url",
		},
		{
			name:    "URL outside the profile pattern",
			profile: DiscoveryProfileTraining,
			result:  SearchResult{Title: "Expo", URL: "https://events.example/expo-2026", Date: "2030-09-01"},
			openURL: "https://events.example/expo-2026",
		},
		{
			name:    "past date rejected by profile policy",
			profile: DiscoveryProfileConferences,
			result: SearchResult{
				Title: "Summit", URL: "https://events.example/summit/ai", Date: "2020-01-01", Location: "Seoul",
			},
			openURL: "https://events.example/summit/ai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			options := testOptions(t, tt.profile)
			options.MaxOpens = 2
			opener := &fakeOpener{pages: map[string]OpenedPage{tt.openURL: {Title: "Should never be fetched"}}}
			backend, model := newScriptedBackend(t,
				searchAction("events"), openAction(tt.openURL), openAction(tt.openURL),
			)

			// When
			run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
				Backend: backend, Goal: "events",
				Tool: &staticSearch{results: []SearchResult{tt.result}}, Opener: opener, Options: options,
			})

			// Then
			if err != nil {
				t.Fatalf("DiscoverWithOptions() error = %v, want a graceful stop", err)
			}
			if got := opener.opened(); len(got) != 0 {
				t.Fatalf("opened = %#v, want no fetch for a non-metadata rejection", got)
			}
			if run.YieldTrace.OpenCalls != 0 {
				t.Fatalf("open calls = %d, want none", run.YieldTrace.OpenCalls)
			}
			openTurn := model.capturedRequests()[1].Messages[1].Content
			if strings.Contains(openTurn, tt.openURL) {
				t.Fatalf("rejected candidate was offered as pending:\n%s", openTurn)
			}
			if run.YieldTrace.TerminalReason != YieldTerminalMalformedAction {
				t.Fatalf("terminal reason = %q, want %q", run.YieldTrace.TerminalReason, YieldTerminalMalformedAction)
			}
		})
	}
}

// open_calls joins the yield trace additively: a new count, no field removed,
// and still counts only.
func TestDiscoveryOpen_serializesOpenCallsIntoTheYieldTrace(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	options.MaxOpens = 1
	candidateURL := "https://events.example/calendar"
	opener := &fakeOpener{pages: map[string]OpenedPage{candidateURL: {Location: "private-page-marker"}}}
	backend, _ := newScriptedBackend(t, searchAction("events"), openAction(candidateURL), doneAction())
	search := &staticSearch{results: []SearchResult{{Title: "Calendar", URL: candidateURL}}}

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events", Tool: search, Opener: opener, Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	wire, marshalErr := json.Marshal(run.YieldTrace)
	if marshalErr != nil {
		t.Fatalf("marshal yield trace: %v", marshalErr)
	}
	if !strings.Contains(string(wire), `"open_calls":1`) {
		t.Fatalf("yield trace missing open_calls: %s", wire)
	}
	for _, leaked := range []string{"private-page-marker", candidateURL, "events.example"} {
		if strings.Contains(string(wire), leaked) {
			t.Fatalf("yield trace leaked %q: %s", leaked, wire)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("decode yield trace: %v", err)
	}
	wantKeys := []string{
		"outcome", "terminal_reason", "crawler_validated", "offered", "prefilter_dropped",
		"prefilter_reasons", "proposal_calls", "judge_calls", "open_calls",
		"judge_entries_parsed", "judge_entries_dropped", "accepted",
	}
	if len(decoded) != len(wantKeys) {
		t.Fatalf("yield trace keys = %v, want exactly %d", decoded, len(wantKeys))
	}
	for _, key := range wantKeys {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("yield trace missing key %q: %s", key, wire)
		}
	}
}

func TestValidateDiscoverOptions_boundsMaxOpens(t *testing.T) {
	tests := []struct {
		name    string
		opens   int
		want    int
		wantErr bool
	}{
		{name: "zero disables the action", opens: 0, want: 0},
		{name: "in range", opens: 2, want: 2},
		{name: "clamped to the hard cap", opens: 99, want: hardMaxDiscoveryOpens},
		{name: "negative is invalid", opens: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			options := testOptions(t, DiscoveryProfileEvents)
			options.MaxOpens = tt.opens

			// When
			validated, err := validateDiscoverOptions(options)

			// Then
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidDiscoverOptions) {
					t.Fatalf("error = %v, want ErrInvalidDiscoverOptions", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateDiscoverOptions() error = %v", err)
			}
			if validated.MaxOpens != tt.want {
				t.Fatalf("MaxOpens = %d, want %d", validated.MaxOpens, tt.want)
			}
		})
	}
}

func TestActionModelRequest_pendingBlockFollowsTheOpenAction(t *testing.T) {
	pending := []offeredCandidate{
		{canonicalURL: "https://example.com/p", result: SearchResult{Snippet: "no title, no date"}},
	}

	t.Run("open allowed renders the pending block with the missing fields named", func(t *testing.T) {
		// Given
		data := actionPromptData{
			goal: "g", remainingSearches: 1, remainingOpens: 2, remainingModelCalls: 3,
			openAllowed: true, pending: pending,
		}

		// When
		request := actionModelRequest(testActionProfile(), data)

		// Then
		if !strings.Contains(request.user, "1. [missing: title,date,location] https://example.com/p") {
			t.Errorf("pending row missing or malformed\ngot:\n%s", request.user)
		}
		if got := strings.Count(request.user, "</untrusted-pending>"); got != 1 {
			t.Errorf("pending closing delimiter count = %d, want 1\ngot:\n%s", got, request.user)
		}
		if !strings.Contains(request.system, "pending") {
			t.Errorf("system prompt does not tell the model pending URLs are openable\ngot:\n%s", request.system)
		}
	})

	t.Run("open disallowed omits the pending block entirely", func(t *testing.T) {
		// Given
		data := actionPromptData{
			goal: "g", remainingSearches: 1, remainingModelCalls: 3, openAllowed: false, pending: pending,
		}

		// When
		request := actionModelRequest(testActionProfile(), data)

		// Then
		if strings.Contains(request.user, "untrusted-pending") {
			t.Errorf("pending block rendered while open is unavailable\ngot:\n%s", request.user)
		}
		if strings.Contains(request.user, "https://example.com/p") {
			t.Errorf("pending URL leaked into a prompt that cannot open it\ngot:\n%s", request.user)
		}
	})
}

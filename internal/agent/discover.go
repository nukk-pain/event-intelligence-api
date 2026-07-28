package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// correction instructions are server-owned constants. They never echo model or
// crawled text back into the prompt, so a hostile response cannot use the
// correction channel to smuggle instructions into the next turn.
const (
	correctionMalformedAction = "Your previous response was not a single valid JSON action object. " +
		"Reply with exactly one JSON object in one of the listed action formats and nothing else."
	correctionSearchExhausted = "Your previous response asked for a search, but no searches remain. " +
		"Choose one of the remaining actions instead."
	correctionActionUnavailable = "Your previous response chose an action that is not available. " +
		"Choose one of the listed actions only."
	// noteOpenFailed is not a correction: a page that will not load is an
	// ordinary fact of the open web, not a contract violation. It rides along
	// for exactly one turn so the model does not spend another open retrying the
	// same URL, and it never echoes the model's or the page's text.
	noteOpenFailed = "Your previous open action could not retrieve that page. " +
		"Do not open the same url again; choose a different action."
)

type discoverySession struct {
	options    validatedDiscoverOptions
	model      discoveryModel
	tool       SearchTool
	opener     PageOpener
	result     DiscoveryResult
	goal       string
	tried      []string
	candidates []offeredCandidate
	// pending holds candidates the prefilter dropped only for missing metadata.
	// They are not judgeable and the model cannot accept them; an open can fill
	// the gap and promote one into candidates.
	pending  []offeredCandidate
	searches int
	opens    int
	turns    int
	// openNote carries a server-owned one-turn note about a failed open.
	openNote              string
	found                 map[string]DiscoveredSource
	order                 []string
	offered               map[string]struct{}
	perSourceCandidateUse map[string]int
}

// Discover runs the autonomous source-discovery loop: on every turn the model
// picks its own next action (search, accept, or done), the loop executes it and
// re-renders the state, and this repeats until the model stops or a budget is
// reached. maxRounds carries forward as the search budget. Discovered sources
// are de-duplicated by URL.
func Discover(ctx context.Context, be Backend, goal string, tool SearchTool, maxRounds, maxTokens int, timeout time.Duration) ([]DiscoveredSource, Trace, error) {
	if maxRounds <= 0 {
		return nil, Trace{}, nil
	}
	profile, err := NamedDiscoveryProfile(DiscoveryProfileEvents)
	if err != nil {
		return nil, Trace{}, err
	}
	options := DefaultDiscoverOptions(profile)
	options.MaxSearches = maxRounds
	options.MaxTokensPerCall = maxTokens
	options.PerCallTimeout = timeout
	run, err := DiscoverWithOptions(ctx, DiscoverRequest{Backend: be, Goal: goal, Tool: tool, Options: options})
	return run.Sources, run.Trace, err
}

// DiscoverWithOptions executes profile-driven discovery with request-local hard limits.
func DiscoverWithOptions(ctx context.Context, request DiscoverRequest) (DiscoveryResult, error) {
	validated, err := validateDiscoverOptions(request.Options)
	if err != nil {
		return DiscoveryResult{}, err
	}
	if request.Tool == nil {
		return DiscoveryResult{}, fmt.Errorf("search tool: %w", ErrInvalidDiscoverOptions)
	}
	redactedGoal := strings.TrimSpace(StripContacts(request.Goal))
	result := DiscoveryResult{Sources: []DiscoveredSource{}, Metadata: DiscoveryMetadata{
		Profile: validated.Profile.Name,
		Budget: DiscoveryBudget{
			MaxSearches: validated.MaxSearches, MaxTurns: validated.MaxTurns, MaxModelCalls: validated.MaxModelCalls,
			MaxCompletionTokens: validated.MaxCompletionTokens, MaxCandidates: validated.MaxCandidates,
			MaxCandidatesPerSource: validated.MaxCandidatesPerSource,
		},
	}}
	if utf8.RuneCountInString(redactedGoal) > validated.GoalMaxRunes {
		redactedGoal = string([]rune(redactedGoal)[:validated.GoalMaxRunes])
		result.addTruncation(TruncationGoalLength)
	}
	session := discoverySession{
		options: validated, tool: request.Tool, opener: request.Opener, result: result, goal: redactedGoal,
		found: map[string]DiscoveredSource{}, offered: map[string]struct{}{}, perSourceCandidateUse: map[string]int{},
	}
	session.model = discoveryModel{backend: request.Backend, options: validated, result: &session.result}
	return session.run(ctx)
}

// run is the action loop: every turn re-renders the current state, asks the
// model for exactly one next action, and executes it. Nothing here sequences
// the model — a search, an accept, or a stop is legal on any turn, in any
// order, any number of times. Termination is guaranteed by the turn, model-call
// and token budgets checked at the top of each iteration; every exit goes
// through complete().
func (session *discoverySession) run(ctx context.Context) (DiscoveryResult, error) {
	// correction holds the one-shot instruction added after a contract
	// violation. Non-empty means the previous turn already spent its single
	// correction, so a repeat violation ends the run.
	var correction string
	for {
		if session.turns >= session.options.MaxTurns {
			session.result.addTruncation(TruncationRoundLimit)
			return session.complete(YieldTerminalRoundLimit, nil)
		}
		if session.result.Trace.Calls >= session.options.MaxModelCalls {
			session.result.addTruncation(TruncationModelCallBudget)
			return session.complete(YieldTerminalModelCallBudget, nil)
		}
		if session.result.Metadata.Budget.CompletionTokensReserved >= session.options.MaxCompletionTokens {
			session.result.addTruncation(TruncationCompletionTokenBudget)
			return session.complete(YieldTerminalTokenBudget, nil)
		}

		request := actionModelRequest(session.options.Profile, session.promptData())
		if correction != "" {
			request.system += "\n" + correction
		}
		if session.openNote != "" {
			request.system += "\n" + session.openNote
			session.openNote = ""
		}
		callsBefore := session.result.Trace.Calls
		content, err := session.model.call(ctx, request)
		session.turns++
		calls := session.result.Trace.Calls - callsBefore
		if errors.Is(err, errModelBudgetReached) {
			return session.complete(budgetTerminalReason(session.result.Metadata), nil)
		}
		if err != nil {
			// A failed turn produced no action, so it cannot be an accept call.
			session.result.YieldTrace.ProposalCalls += calls
			return session.complete(terminalReasonForError(err, YieldTerminalProposalError), fmt.Errorf("action turn: %w", err))
		}
		action, parseErr := parseActionResponse(content)
		if parseErr != nil {
			// yield_trace: an unparseable turn is not an accept, so it lands in
			// proposal_calls with the search/done turns.
			session.result.YieldTrace.ProposalCalls += calls
			if correction != "" {
				return session.complete(YieldTerminalMalformedAction,
					fmt.Errorf("action turn: %w: %w", ErrMalformedModelResponse, parseErr))
			}
			correction = correctionMalformedAction
			continue
		}
		// yield_trace mapping: a turn that returned accept is a judging call;
		// every other turn (search, done, refused action) is a proposal call.
		if action.Kind == actionAccept {
			session.result.YieldTrace.JudgeCalls += calls
		} else {
			session.result.YieldTrace.ProposalCalls += calls
		}

		switch action.Kind {
		case actionSearch:
			if session.searches >= session.options.MaxSearches {
				if correction != "" {
					return session.complete(YieldTerminalMalformedAction, nil)
				}
				correction = correctionSearchExhausted
				continue
			}
			correction = ""
			session.searches++
			session.tried = append(session.tried, action.Query)
			results, searchErr := session.tool.Search(ctx, action.Query)
			if searchErr != nil {
				return session.complete(terminalReasonForError(searchErr, YieldTerminalSearchError),
					fmt.Errorf("search %q: %w", action.Query, searchErr))
			}
			session.candidates = append(session.candidates, session.offerCandidates(results)...)
		case actionOpen:
			// A URL the server never offered is an invented fetch target, and an
			// open with no opener or no budget left is an action that was not on
			// the menu. Both take the unavailable-action correction, and neither
			// touches the network.
			target, fromPending := session.openTarget(action.URL)
			if !session.openAllowed() || target == nil {
				if correction != "" {
					return session.complete(YieldTerminalMalformedAction, nil)
				}
				correction = correctionActionUnavailable
				continue
			}
			correction = ""
			session.opens++
			session.result.YieldTrace.OpenCalls++
			page, openErr := session.opener.Open(ctx, action.URL)
			if openErr != nil {
				// Non-fatal: the turn is spent, every candidate is unchanged, and
				// the next prompt says so once.
				session.openNote = noteOpenFailed
				continue
			}
			applyOpenedPage(&target.result, page)
			if fromPending {
				session.promotePending(action.URL)
			}
		case actionAccept:
			correction = ""
			// yield_trace: every selection the model returned counts as parsed;
			// acceptSelections counts the ones dropped against the table.
			// Parsed counts every entry the model actually sent; blank-url
			// entries dropped by the parser are accounted as dropped so
			// parsed == accepted + dropped still holds for the trace.
			session.result.YieldTrace.JudgeEntriesParsed += len(action.Selections) + action.DroppedSelections
			session.result.YieldTrace.JudgeEntriesDropped += action.DroppedSelections
			session.acceptSelections(action.Selections)
		case actionDone:
			return session.complete(YieldTerminalProposalDone, nil)
		}
	}
}

// promptData renders the current, order-free state the model chooses from.
func (session *discoverySession) promptData() actionPromptData {
	data := actionPromptData{
		goal:                session.goal,
		tried:               session.tried,
		candidates:          session.candidates,
		accepted:            session.order,
		remainingSearches:   session.options.MaxSearches - session.searches,
		remainingModelCalls: min(session.options.MaxModelCalls-session.result.Trace.Calls, session.options.MaxTurns-session.turns),
		openAllowed:         session.openAllowed(),
	}
	if data.openAllowed {
		data.remainingOpens = session.options.MaxOpens - session.opens
		data.pending = session.pending
	}
	return data
}

func (session *discoverySession) offerCandidates(results []SearchResult) []offeredCandidate {
	candidates := make([]offeredCandidate, 0, len(results))
	for _, result := range results {
		candidate, reason, ok := prepareCandidate(result, session.options)
		if !ok {
			session.result.YieldTrace.PrefilterDropped++
			session.result.YieldTrace.PrefilterReasons.add(reason)
			// Holding a metadata-short candidate for a later open does not undo
			// the drop, so the counters above stand as recorded.
			session.rememberPending(candidate, reason)
			continue
		}
		if _, seen := session.offered[candidate.canonicalURL]; seen {
			continue
		}
		if _, found := session.found[candidate.canonicalURL]; found {
			continue
		}
		if session.result.Metadata.CandidatesOffered >= session.options.MaxCandidates {
			session.result.addTruncation(TruncationTotalCandidateCap)
			break
		}
		if session.perSourceCandidateUse[candidate.sourceKey] >= session.options.MaxCandidatesPerSource {
			session.result.addTruncation(TruncationPerSourceCandidateCap)
			continue
		}
		session.offered[candidate.canonicalURL] = struct{}{}
		// A later search that returned this URL with complete metadata beat the
		// open to it, so it no longer belongs in pending.
		session.dropPending(candidate.canonicalURL)
		session.perSourceCandidateUse[candidate.sourceKey]++
		session.result.Metadata.CandidatesOffered++
		session.result.YieldTrace.Offered++
		candidates = append(candidates, candidate)
	}
	if session.result.Metadata.CandidatesOffered >= session.options.MaxCandidates {
		session.result.addTruncation(TruncationTotalCandidateCap)
	}
	return candidates
}

// acceptSelections admits the selections that name a candidate currently in the
// table, verbatim and canonical. Anything else — an invented URL, a rewritten
// one, or a duplicate — is dropped and counted, never fetched or trusted.
// Accepted candidates leave the table so later turns cannot re-offer them.
func (session *discoverySession) acceptSelections(selections []actionSelection) {
	offered := make(map[string]offeredCandidate, len(session.candidates))
	for _, candidate := range session.candidates {
		offered[candidate.canonicalURL] = candidate
	}
	accepted := make(map[string]struct{}, len(selections))
	for _, selection := range selections {
		canonicalURL, err := canonicalCandidateURL(selection.URL)
		if err != nil || selection.URL != canonicalURL {
			session.result.YieldTrace.JudgeEntriesDropped++
			continue
		}
		candidate, ok := offered[canonicalURL]
		if !ok {
			session.result.YieldTrace.JudgeEntriesDropped++
			continue
		}
		if _, seen := session.found[canonicalURL]; seen {
			session.result.YieldTrace.JudgeEntriesDropped++
			continue
		}
		result := candidate.result
		provenance := result.Provenance
		if provenance != nil {
			cloned := *provenance
			provenance = &cloned
		}
		source := DiscoveredSource{
			URL: canonicalURL, Title: result.Title, Reason: selection.Reason,
			Date: result.Date, Location: result.Location, Provenance: provenance,
		}
		session.found[canonicalURL] = source
		session.order = append(session.order, canonicalURL)
		session.result.YieldTrace.Accepted++
		accepted[canonicalURL] = struct{}{}
	}
	if len(accepted) == 0 {
		return
	}
	remaining := session.candidates[:0]
	for _, candidate := range session.candidates {
		if _, taken := accepted[candidate.canonicalURL]; taken {
			continue
		}
		remaining = append(remaining, candidate)
	}
	session.candidates = remaining
}

func (session *discoverySession) complete(reason YieldTerminalReason, err error) (DiscoveryResult, error) {
	result := session.finish()
	result.YieldTrace.finish(reason, err != nil)
	return result, err
}

func budgetTerminalReason(metadata DiscoveryMetadata) YieldTerminalReason {
	if containsTruncationReason(metadata.TruncationReasons, TruncationCompletionTokenBudget) {
		return YieldTerminalTokenBudget
	}
	return YieldTerminalModelCallBudget
}

func containsTruncationReason(reasons []TruncationReason, target TruncationReason) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}

func (session *discoverySession) finish() DiscoveryResult {
	session.result.Sources = make([]DiscoveredSource, 0, len(session.order))
	for _, canonicalURL := range session.order {
		session.result.Sources = append(session.result.Sources, session.found[canonicalURL])
	}
	return session.result
}

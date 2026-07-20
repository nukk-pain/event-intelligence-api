package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type discoverySession struct {
	options               validatedDiscoverOptions
	model                 discoveryModel
	tool                  SearchTool
	result                DiscoveryResult
	goal                  string
	tried                 []string
	found                 map[string]DiscoveredSource
	order                 []string
	offered               map[string]struct{}
	perSourceCandidateUse map[string]int
}

// Discover runs the autonomous source-discovery loop: the model proposes a
// search query, the tool runs it, the model judges which results are real event
// sources, and the loop repeats (deciding the next action itself) up to
// maxRounds. Discovered sources are de-duplicated by URL.
func Discover(ctx context.Context, be Backend, goal string, tool SearchTool, maxRounds, maxTokens int, timeout time.Duration) ([]DiscoveredSource, Trace, error) {
	if maxRounds <= 0 {
		return nil, Trace{}, nil
	}
	profile, err := NamedDiscoveryProfile(DiscoveryProfileEvents)
	if err != nil {
		return nil, Trace{}, err
	}
	options := DefaultDiscoverOptions(profile)
	options.MaxRounds = maxRounds
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
			MaxRounds: validated.MaxRounds, MaxModelCalls: validated.MaxModelCalls,
			MaxCompletionTokens: validated.MaxCompletionTokens, MaxCandidates: validated.MaxCandidates,
			MaxCandidatesPerSource: validated.MaxCandidatesPerSource,
		},
	}}
	if utf8.RuneCountInString(redactedGoal) > validated.GoalMaxRunes {
		redactedGoal = string([]rune(redactedGoal)[:validated.GoalMaxRunes])
		result.addTruncation(TruncationGoalLength)
	}
	session := discoverySession{
		options: validated, tool: request.Tool, result: result, goal: redactedGoal,
		found: map[string]DiscoveredSource{}, offered: map[string]struct{}{}, perSourceCandidateUse: map[string]int{},
	}
	session.model = discoveryModel{backend: request.Backend, options: validated, result: &session.result}
	return session.run(ctx)
}

func (session *discoverySession) run(ctx context.Context) (DiscoveryResult, error) {
	for round := 0; round < session.options.MaxRounds; round++ {
		request := proposalModelRequest(session.options.Profile, proposalData{
			goal: session.goal, tried: session.tried, found: session.order,
		})
		content, err := session.model.call(ctx, request)
		if errors.Is(err, errModelBudgetReached) {
			return session.finish(), nil
		}
		if err != nil {
			return session.finish(), fmt.Errorf("propose query: %w", err)
		}
		proposal, err := parseProposalResponse(content)
		if err != nil {
			return session.finish(), fmt.Errorf("propose query: %w", err)
		}
		if proposal.Done {
			return session.finish(), nil
		}
		if session.result.Trace.Calls >= session.options.MaxModelCalls {
			session.result.addTruncation(TruncationModelCallBudget)
			return session.finish(), nil
		}
		if session.result.Metadata.Budget.CompletionTokensReserved >= session.options.MaxCompletionTokens {
			session.result.addTruncation(TruncationCompletionTokenBudget)
			return session.finish(), nil
		}
		session.tried = append(session.tried, proposal.Query)
		results, err := session.tool.Search(ctx, proposal.Query)
		if err != nil {
			return session.finish(), fmt.Errorf("search %q: %w", proposal.Query, err)
		}
		candidates := session.offerCandidates(results)
		if len(candidates) == 0 {
			if session.result.Metadata.CandidatesOffered >= session.options.MaxCandidates {
				return session.finish(), nil
			}
			continue
		}
		request, err = judgeModelRequest(session.options.Profile, candidates)
		if err != nil {
			return session.finish(), err
		}
		content, err = session.model.call(ctx, request)
		if errors.Is(err, errModelBudgetReached) {
			return session.finish(), nil
		}
		if err != nil {
			return session.finish(), fmt.Errorf("judge results: %w", err)
		}
		selections, err := parseJudgeResponse(content, session.options.Profile.OutputLabel)
		if err != nil {
			return session.finish(), fmt.Errorf("judge results: %w", err)
		}
		session.acceptSelections(candidates, selections)
		if session.result.Metadata.Budget.CompletionTokensReserved >= session.options.MaxCompletionTokens ||
			session.result.Trace.Usage.CompletionTokens >= session.options.MaxCompletionTokens ||
			session.result.Metadata.CandidatesOffered >= session.options.MaxCandidates {
			return session.finish(), nil
		}
	}
	session.result.addTruncation(TruncationRoundLimit)
	return session.finish(), nil
}

func (session *discoverySession) offerCandidates(results []SearchResult) []offeredCandidate {
	candidates := make([]offeredCandidate, 0, len(results))
	for _, result := range results {
		candidate, ok := prepareCandidate(result, session.options)
		if !ok {
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
		session.perSourceCandidateUse[candidate.sourceKey]++
		session.result.Metadata.CandidatesOffered++
		candidates = append(candidates, candidate)
	}
	if session.result.Metadata.CandidatesOffered >= session.options.MaxCandidates {
		session.result.addTruncation(TruncationTotalCandidateCap)
	}
	return candidates
}

func (session *discoverySession) acceptSelections(candidates []offeredCandidate, selections []judgedSelection) {
	offered := make(map[string]offeredCandidate, len(candidates))
	for _, candidate := range candidates {
		offered[candidate.canonicalURL] = candidate
	}
	for _, selection := range selections {
		canonicalURL, err := canonicalCandidateURL(selection.url)
		if err != nil || selection.url != canonicalURL {
			continue
		}
		candidate, ok := offered[canonicalURL]
		if !ok {
			continue
		}
		if _, seen := session.found[canonicalURL]; seen {
			continue
		}
		result := candidate.result
		provenance := result.Provenance
		if provenance != nil {
			cloned := *provenance
			provenance = &cloned
		}
		source := DiscoveredSource{
			URL: canonicalURL, Title: result.Title, Reason: selection.reason,
			Date: result.Date, Location: result.Location, Provenance: provenance,
		}
		session.found[canonicalURL] = source
		session.order = append(session.order, canonicalURL)
	}
}

func (session *discoverySession) finish() DiscoveryResult {
	session.result.Sources = make([]DiscoveredSource, 0, len(session.order))
	for _, canonicalURL := range session.order {
		session.result.Sources = append(session.result.Sources, session.found[canonicalURL])
	}
	return session.result
}

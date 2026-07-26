package publicdiscovery

import (
	"context"
	"errors"
	"net/http"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func (provider *Provider) crawl(ctx context.Context) ([]Candidate, BudgetState) {
	jobContext, cancel := context.WithTimeout(ctx, provider.limits.Timeout)
	defer cancel()
	state := newCrawlState(provider)
	// The tally is anchored to the catalog, not to how far the crawl reached,
	// so a seed abandoned during bootstrap is still accounted for.
	state.seedsTotal = len(provider.catalog.Seeds)
	for _, seed := range provider.catalog.Seeds {
		if state.stopped {
			break
		}
		state.bootstrapSeed(jobContext, seed)
	}
	state.processProtocolQueue(jobContext)
	state.processHTMLQueue(jobContext)
	state.accountUnprocessedSeeds()
	state.syncFetchUsage()
	candidates := state.validatedCandidates()
	state.budget.Usage.Candidates = len(candidates)
	return candidates, cloneBudget(state.budget)
}

func (state *crawlState) bootstrapSeed(ctx context.Context, seed Seed) {
	state.budget.Usage.Seeds++
	robotsURL := originDocumentURL(seed.URL, "/robots.txt")
	result, err := state.provider.fetcher.Fetch(ctx, robotsURL, fetch.Conditional{})
	state.syncFetchUsage()
	declared := []string{}
	if err == nil {
		declared = parseRobotsSitemaps(result.Body)
	} else {
		var statusError *fetch.StatusError
		robotsNotFound := errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotFound
		if !robotsNotFound && !errors.Is(err, fetch.ErrRobotsDisallowed) {
			state.recordFetchError(ctx, err)
			if errors.Is(err, fetch.ErrRobotsUnavailable) {
				// Strict mode refuses an origin whose robots.txt cannot be read.
				// The seed is abandoned here and never reaches the HTML queue.
				state.budget.SeedOutcomes.add(SeedOutcomeRobotsUnavailable)
				return
			}
			if state.stopped {
				state.budget.SeedOutcomes.add(SeedOutcomeNotAttempted)
				return
			}
		}
	}
	queuedSitemap := false
	for _, rawURL := range declared {
		before := len(state.protocolQueue)
		state.enqueue(frontierItem{
			rawURL: rawURL, seed: seed, parentURL: robotsURL,
			relation: ProtocolRobotsSitemap, depth: 0, kind: documentSitemap,
		})
		queuedSitemap = queuedSitemap || len(state.protocolQueue) > before
	}
	if !queuedSitemap {
		state.enqueue(frontierItem{
			rawURL: originDocumentURL(seed.URL, "/sitemap.xml"), seed: seed,
			parentURL: robotsURL, relation: ProtocolRobotsSitemap, depth: 0, kind: documentSitemap,
		})
	}
	before := len(state.htmlQueue)
	state.enqueue(frontierItem{
		rawURL: seed.URL, seed: seed, relation: ProtocolSeed, depth: 0, kind: documentHTML,
	})
	if len(state.htmlQueue) == before {
		if state.stopped {
			state.budget.SeedOutcomes.add(SeedOutcomeNotAttempted)
			return
		}
		state.budget.SeedOutcomes.add(SeedOutcomeDuplicate)
	}
}

// recordSeedOutcome attributes one enqueued seed to exactly one category.
// Non-seed frontier items are ignored.
func (state *crawlState) recordSeedOutcome(item frontierItem, outcome SeedOutcome) {
	if item.relation != ProtocolSeed {
		return
	}
	state.budget.SeedOutcomes.add(outcome)
}

// accountUnprocessedSeeds attributes seeds the crawl never reached, whether it
// stopped on a budget or truncated the HTML queue.
func (state *crawlState) accountUnprocessedSeeds() {
	remaining := state.seedsTotal - state.budget.SeedOutcomes.Total()
	for range remaining {
		state.budget.SeedOutcomes.add(SeedOutcomeNotAttempted)
	}
}

func (state *crawlState) processProtocolQueue(ctx context.Context) {
	for len(state.protocolQueue) > 0 && !state.stopped {
		if state.budget.Usage.ProtocolDocuments >= state.provider.limits.MaxProtocolDocuments {
			state.addReason(TruncationProtocolDocumentLimit)
			state.protocolQueue = nil
			return
		}
		item := state.protocolQueue[0]
		state.protocolQueue = state.protocolQueue[1:]
		state.budget.Usage.ProtocolDocuments++
		result, err := state.fetchDocument(ctx, item)
		if err != nil {
			continue
		}
		state.processProtocolResult(item, result)
	}
}

func (state *crawlState) processHTMLQueue(ctx context.Context) {
	for len(state.htmlQueue) > 0 && !state.stopped {
		if state.budget.Usage.HTMLPages >= state.provider.limits.MaxHTMLPages {
			state.addReason(TruncationHTMLPageLimit)
			state.htmlQueue = nil
			return
		}
		item := state.htmlQueue[0]
		state.htmlQueue = state.htmlQueue[1:]
		state.budget.Usage.HTMLPages++
		result, err := state.fetchDocument(ctx, item)
		if err != nil {
			state.recordSeedOutcome(item, seedOutcomeForError(err))
			continue
		}
		state.processHTMLResult(item, result)
		state.processProtocolQueue(ctx)
	}
}

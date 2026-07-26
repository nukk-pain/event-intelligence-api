package publicdiscovery

import (
	"context"
	"errors"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func (state *crawlState) fetchDocument(ctx context.Context, item frontierItem) (*fetch.Result, error) {
	result, err := state.provider.fetcher.Fetch(ctx, item.canonicalURL, fetch.Conditional{})
	state.syncFetchUsage()
	if err == nil {
		state.validatedURLs[item.canonicalURL] = struct{}{}
		return result, nil
	}
	state.recordFetchError(ctx, err)
	return nil, err
}

// seedOutcomeForError maps one failed seed fetch to exactly one bounded
// category. It reads only error identity, never response content.
func seedOutcomeForError(err error) SeedOutcome {
	var statusError *fetch.StatusError
	switch {
	case errors.Is(err, fetch.ErrRobotsDisallowed), errors.Is(err, fetch.ErrRobotsUnavailable):
		return SeedOutcomeRobotsDisallowed
	case errors.Is(err, fetch.ErrBodyTooLarge), errors.Is(err, fetch.ErrAggregateBodyBudgetExhausted):
		return SeedOutcomeBodyTooLarge
	case errors.Is(err, fetch.ErrUnsupportedDocumentMIME):
		return SeedOutcomeUnsupportedContent
	case errors.As(err, &statusError), errors.Is(err, fetch.ErrUnexpectedStatus):
		return SeedOutcomeHTTPStatus
	default:
		return SeedOutcomeTransportError
	}
}

func (state *crawlState) recordFetchError(ctx context.Context, err error) {
	switch {
	case errors.Is(err, fetch.ErrTransportBudgetExhausted):
		state.addReason(TruncationHTTPAttemptLimit)
		state.stopped = true
	case errors.Is(err, fetch.ErrAggregateBodyBudgetExhausted):
		state.addReason(TruncationResponseBodyLimit)
		state.stopped = true
	case errors.Is(err, fetch.ErrBodyTooLarge):
		state.addReason(TruncationResponseBodyLimit)
		state.budget.Usage.SkippedDocuments++
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		state.addReason(TruncationTimeLimit)
		state.stopped = true
	case errors.Is(ctx.Err(), context.Canceled):
		state.addReason(TruncationContextCanceled)
		state.stopped = true
	default:
		state.budget.Usage.SkippedDocuments++
	}
}

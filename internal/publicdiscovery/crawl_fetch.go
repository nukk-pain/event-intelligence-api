package publicdiscovery

import (
	"context"
	"errors"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func (state *crawlState) fetchDocument(ctx context.Context, item frontierItem) (*fetch.Result, bool) {
	result, err := state.provider.fetcher.Fetch(ctx, item.canonicalURL, fetch.Conditional{})
	state.syncFetchUsage()
	if err == nil {
		state.validatedURLs[item.canonicalURL] = struct{}{}
		return result, true
	}
	state.recordFetchError(ctx, err)
	return nil, false
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

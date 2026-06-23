package pipeline

import (
	"context"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func (p *Pipeline) tryFallback(ctx context.Context, s sources.Source, f *fetch.Fetcher, ref sources.Ref, now string, cause error) (*model.Event, bool, error) {
	fallback, ok := s.(fallbackParser)
	if !ok {
		return nil, false, nil
	}
	parsed, err := fallback.ParseFallback(ctx, ref, cause)
	if err != nil {
		return nil, true, err
	}
	ev, _, err := p.normalizeParsed(ctx, f, ref, parsed, now)
	return ev, true, err
}

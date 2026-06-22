package pipeline

import (
	"context"

	"github.com/smpain/event-intelligence-api/internal/enrich"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func (p *Pipeline) enrichActions(ctx context.Context, f *fetch.Fetcher, parsed *sources.ParsedEvent, now string) {
	if parsed == nil || parsed.HomepageURL == nil || *parsed.HomepageURL == "" {
		return
	}
	officialFetcher := f
	if p.officialFetcher != nil {
		officialFetcher = p.officialFetcher
	}
	res, err := officialFetcher.Fetch(ctx, *parsed.HomepageURL, fetch.Conditional{})
	if err != nil || res.NotModified || res.StatusCode != 200 {
		return
	}
	signals, err := enrich.ExtractActions(res.URL, res.Body)
	if err != nil {
		return
	}
	parsed.Actions = signals
	parsed.ExtraSources = append(parsed.ExtraSources, sources.ParsedSource{
		URL:         res.URL,
		Type:        "organizer",
		Publisher:   "official event page",
		RetrievedAt: now,
	})
}

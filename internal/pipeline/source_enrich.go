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
	if parsed.SourceID == "benchmark" {
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
	parsed.Actions = mergeActionSignals(parsed.Actions, signals)
	// The deterministic extractor leaves most action fields empty on real venue
	// pages, which is exactly the gap the multi-hop agent was built to close.
	// It reads the page already in hand, so nothing is fetched twice.
	if p.actionEnricher != nil {
		if enriched, aerr := p.actionEnricher.EnrichActions(ctx, res.URL, res.Body, parsed.Actions); aerr == nil {
			parsed.Actions = mergeActionSignals(parsed.Actions, enriched)
		}
	}
	parsed.ExtraSources = append(parsed.ExtraSources, sources.ParsedSource{
		URL:         res.URL,
		Type:        "organizer",
		Publisher:   "official event page",
		RetrievedAt: now,
	})
}

func mergeActionSignals(base sources.ActionSignals, extra sources.ActionSignals) sources.ActionSignals {
	if base.CanRegister == nil {
		base.CanRegister = extra.CanRegister
	}
	if base.CanExhibit == nil {
		base.CanExhibit = extra.CanExhibit
	}
	if base.CanSponsor == nil {
		base.CanSponsor = extra.CanSponsor
	}
	if base.HasMatchmaking == nil {
		base.HasMatchmaking = extra.HasMatchmaking
	}
	if base.HasStartupProgram == nil {
		base.HasStartupProgram = extra.HasStartupProgram
	}
	if base.RegisterURL == nil {
		base.RegisterURL = extra.RegisterURL
	}
	if base.ExhibitURL == nil {
		base.ExhibitURL = extra.ExhibitURL
	}
	if base.RegistrationDeadline == nil {
		base.RegistrationDeadline = extra.RegistrationDeadline
	}
	if base.ExhibitorDeadline == nil {
		base.ExhibitorDeadline = extra.ExhibitorDeadline
	}
	if base.CostHint == nil {
		base.CostHint = extra.CostHint
	}
	return base
}

package pipeline

import (
	"context"

	"github.com/smpain/event-intelligence-api/internal/enrich"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/normalize"
	"github.com/smpain/event-intelligence-api/internal/render"
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
	body := p.officialPageText(ctx, res.URL, res.Body)
	signals, err := enrich.ExtractActions(res.URL, body)
	if err != nil {
		return
	}
	parsed.Actions = mergeActionSignals(parsed.Actions, signals)
	// The deterministic extractor leaves most action fields empty on real venue
	// pages, which is exactly the gap the multi-hop agent was built to close.
	// It reads the page already in hand, so nothing is fetched twice.
	if p.actionEnricher != nil {
		if enriched, aerr := p.actionEnricher.EnrichActions(ctx, res.URL, body, parsed.Actions); aerr == nil {
			before := parsed.Actions
			parsed.Actions = mergeActionSignals(parsed.Actions, enriched)
			// Every enriched claim carries provenance. Without this the
			// model-filled signals were indistinguishable from scraped ones,
			// and migration 0010 relies on the publisher string to tell the
			// evidence-gated generation apart from the pre-gate one.
			if parsed.Actions != before {
				parsed.ExtraSources = append(parsed.ExtraSources, sources.ParsedSource{
					URL:         res.URL,
					Type:        "organizer",
					Publisher:   EnrichmentPublisher,
					RetrievedAt: now,
				})
			}
		}
	}
	parsed.ExtraSources = append(parsed.ExtraSources, sources.ParsedSource{
		URL:         res.URL,
		Type:        "organizer",
		Publisher:   "official event page",
		RetrievedAt: now,
	})
}

// enrichDeadlinesFromActionPages runs the deterministic deadline extractor on
// the registration/exhibit pages the event already advertises. Deadlines live
// on enrollment pages far more often than on homepages (live finding
// 2026-07-29: kofurn's register page carries the deadline its homepage
// lacks). Only upcoming events with a still-nil deadline cost a fetch; values
// fill nil-only and every fill records the page as provenance.
func (p *Pipeline) enrichDeadlinesFromActionPages(ctx context.Context, f *fetch.Fetcher, parsed *sources.ParsedEvent, now string) {
	if parsed == nil {
		return
	}
	// Benchmark entries do not receive generic action enrichment. A reviewed
	// catalog entry may still declare an official registration or exhibit URL;
	// those URLs are eligible for the same evidence-gated deadline read as any
	// other source.
	if parsed.SourceID == "benchmark" && parsed.Actions.RegisterURL == nil && parsed.Actions.ExhibitURL == nil {
		return
	}
	if !eventUpcoming(parsed, now) {
		return
	}
	officialFetcher := f
	if p.officialFetcher != nil {
		officialFetcher = p.officialFetcher
	}

	try := func(pageURL *string, dst **string, kind enrich.ActionPageKind) {
		if *dst != nil || pageURL == nil || *pageURL == "" {
			return
		}
		res, err := officialFetcher.Fetch(ctx, *pageURL, fetch.Conditional{})
		if err != nil || res.NotModified || res.StatusCode != 200 {
			return
		}
		found := enrich.DeadlineOnActionPage(p.officialPageText(ctx, *pageURL, res.Body), kind)
		if found == nil {
			return
		}
		*dst = found
		parsed.ExtraSources = append(parsed.ExtraSources, sources.ParsedSource{
			URL:         res.URL,
			Type:        "organizer",
			Publisher:   "official action page",
			RetrievedAt: now,
		})
	}
	try(parsed.Actions.RegisterURL, &parsed.Actions.RegistrationDeadline, enrich.RegisterPage)
	try(parsed.Actions.ExhibitURL, &parsed.Actions.ExhibitorDeadline, enrich.ExhibitPage)
}

func (p *Pipeline) officialPageText(ctx context.Context, pageURL string, staticBody []byte) []byte {
	return render.StaticOrRendered(ctx, p.textSelector, pageURL, staticBody)
}

// eventUpcoming reports whether the event's end (or start) date parses and is
// today or later. Unparseable dates gate the fetch off — a deadline for an
// event we cannot even date is not worth a network request.
func eventUpcoming(parsed *sources.ParsedEvent, now string) bool {
	raw := parsed.EndRaw
	if raw == nil || *raw == "" {
		raw = parsed.StartRaw
	}
	if raw == nil || *raw == "" {
		return false
	}
	iso, ok := normalize.ParseDate(*raw)
	if !ok || len(now) < 10 {
		return false
	}
	return iso >= now[:10]
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

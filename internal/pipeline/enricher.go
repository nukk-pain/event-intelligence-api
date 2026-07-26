package pipeline

import (
	"context"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// EventEnricher resolves fields the deterministic parse could not. It runs
// inside ingest only. The read path stays LLM-free, and the pipeline never
// names a concrete backend, exactly as it never names a concrete source
// adapter.
//
// An enricher must fill only fields listed in MissingFields, must never
// overwrite a source-derived value, and must record its own provenance. A
// returned error is not fatal: the deterministic result stands.
type EventEnricher interface {
	Enrich(ctx context.Context, event *model.Event, sourceText string) error
}

// WithEnricher attaches an optional ingest-time enricher. A nil enricher keeps
// the pipeline fully deterministic.
func (p *Pipeline) WithEnricher(e EventEnricher) *Pipeline {
	p.enricher = e
	return p
}

// enrichmentText assembles the raw scraped strings an enricher may read. These
// are the very fields the normalizer failed to interpret, so they carry the
// unresolved information without a second fetch. Contact stripping happens in
// the enricher before anything leaves the process.
func enrichmentText(parsed *sources.ParsedEvent) string {
	if parsed == nil {
		return ""
	}
	parts := []string{parsed.Name}
	for _, field := range []*string{
		parsed.StartRaw, parsed.EndRaw, parsed.VenueName,
		parsed.Hall, parsed.City, parsed.Organizer, parsed.SummaryText,
	} {
		if field != nil && strings.TrimSpace(*field) != "" {
			parts = append(parts, strings.TrimSpace(*field))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// runEnricher is a no-op unless an enricher is attached and the event actually
// has unresolved fields, so a fully parsed event never costs a model call.
func (p *Pipeline) runEnricher(ctx context.Context, event *model.Event, parsed *sources.ParsedEvent) {
	if p.enricher == nil || event == nil || len(event.MissingFields) == 0 {
		return
	}
	text := enrichmentText(parsed)
	if text == "" {
		return
	}
	// A failure here is deliberately swallowed. Enrichment is additive, and a
	// model error must never cost the deterministic row.
	_ = p.enricher.Enrich(ctx, event, text)
}

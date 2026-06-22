package normalize

import (
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func buildSources(p *sources.ParsedEvent, now string) []model.Source {
	u := validURL(&p.URL)
	if u == nil {
		return nil
	}
	retrieved := strings.TrimSpace(p.RetrievedAt)
	if retrieved == "" {
		retrieved = now
	}
	publisher := derefTrim(p.VenueName)
	if publisher == "" {
		publisher = p.SourceID
	}
	out := []model.Source{{
		URL:         *u,
		Type:        sourceType,
		Publisher:   publisher,
		RetrievedAt: retrieved,
	}}
	for _, extra := range p.ExtraSources {
		if !isHTTPURL(extra.URL) {
			continue
		}
		if _, ok := validSourceTypes[extra.Type]; !ok {
			continue
		}
		if strings.TrimSpace(extra.Publisher) == "" || strings.TrimSpace(extra.RetrievedAt) == "" {
			continue
		}
		out = append(out, model.Source{
			URL:         strings.TrimSpace(extra.URL),
			Type:        strings.TrimSpace(extra.Type),
			Publisher:   strings.TrimSpace(extra.Publisher),
			RetrievedAt: strings.TrimSpace(extra.RetrievedAt),
		})
	}
	return out
}

func buildSummary(p *sources.ParsedEvent) string {
	if p == nil {
		return ""
	}
	return truncateRunes(strings.Join(strings.Fields(derefTrim(p.SummaryText)), " "), summaryMaxRunes)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

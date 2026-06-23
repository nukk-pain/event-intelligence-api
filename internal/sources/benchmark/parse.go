package benchmark

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func (s *Source) Parse(ctx context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("benchmark: nil fetch result")
	}
	if raw.StatusCode != 200 {
		return nil, fmt.Errorf("benchmark: status %d for %s", raw.StatusCode, raw.URL)
	}
	entry, ok := lookup(raw.URL)
	if !ok {
		return nil, fmt.Errorf("benchmark: URL %s not in catalog", raw.URL)
	}
	doc, err := fetch.ParseHTML(string(raw.Body))
	if err != nil {
		return nil, fmt.Errorf("benchmark: parse html %s: %w", raw.URL, err)
	}

	parsed := baseParsed(entry, raw.URL)
	if event, ok := firstSchemaEvent(doc); ok {
		applySchemaEvent(parsed, event, entry)
	} else {
		applyOpenGraph(parsed, doc, entry)
	}
	if parsed.Name == "" {
		return nil, fmt.Errorf("benchmark: no event name in %s", raw.URL)
	}
	parsed.ClassifyText = strings.Join(nonEmpty(parsed.Name, deref(parsed.SummaryText), entry.ClassifyHint), " ")
	return parsed, nil
}

func (s *Source) ParseFallback(_ context.Context, ref sources.Ref, _ error) (*sources.ParsedEvent, error) {
	entry, ok := lookup(ref.URL)
	if !ok {
		return nil, fmt.Errorf("benchmark: URL %s not in catalog", ref.URL)
	}
	parsed := baseParsed(entry, ref.URL)
	parsed.ClassifyText = strings.Join(nonEmpty(parsed.Name, deref(parsed.SummaryText), entry.ClassifyHint), " ")
	return parsed, nil
}

func baseParsed(entry catalogEvent, url string) *sources.ParsedEvent {
	sourceType := "organizer"
	return &sources.ParsedEvent{
		SourceID:    sourceID,
		EventID:     entry.EventID,
		URL:         url,
		Name:        entry.Name,
		StartRaw:    ptr(entry.StartRaw),
		EndRaw:      ptr(entry.EndRaw),
		VenueName:   ptr(entry.VenueName),
		City:        ptr(entry.City),
		Country:     ptr(entry.Country),
		Timezone:    ptr(entry.Timezone),
		Format:      ptr(entry.Format),
		Organizer:   ptr(entry.Organizer),
		Publisher:   ptr(entry.Organizer),
		SourceType:  &sourceType,
		HomepageURL: ptr(entry.URL),
		SummaryText: ptr(entry.Summary),
		Actions:     entry.Actions,
	}
}

func applySchemaEvent(parsed *sources.ParsedEvent, event schemaEvent, entry catalogEvent) {
	if event.Name != "" && entry.Name == "" {
		parsed.Name = event.Name
	}
	if event.StartDate != "" && entry.StartRaw == "" {
		parsed.StartRaw = ptr(event.StartDate)
	}
	if event.EndDate != "" && entry.EndRaw == "" {
		parsed.EndRaw = ptr(event.EndDate)
	}
	if event.URL != "" && !isRootURL(event.URL) {
		parsed.HomepageURL = ptr(event.URL)
	}
	if event.Description != "" && entry.Summary == "" {
		parsed.SummaryText = ptr(event.Description)
	}
	if event.Organizer.Name != "" {
		parsed.Organizer = ptr(event.Organizer.Name)
		parsed.Publisher = ptr(event.Organizer.Name)
	}
	if format := formatFromAttendanceMode(event.EventAttendanceMode); format != "" {
		parsed.Format = ptr(format)
	}
	if city, country := event.Location.place(); city != "" && entry.City == "" {
		parsed.City = ptr(city)
		if country != "" && entry.Country == "" {
			parsed.Country = ptr(country)
		}
	}
}

func applyOpenGraph(parsed *sources.ParsedEvent, doc *goquery.Document, entry catalogEvent) {
	title := metaContent(doc, "og:title")
	if title != "" {
		parsed.Name = entry.Name
	}
	if desc := metaContent(doc, "og:description"); desc != "" && entry.Summary == "" {
		parsed.SummaryText = ptr(desc)
	}
	if ogURL := metaContent(doc, "og:url"); ogURL != "" && !isRootURL(ogURL) {
		parsed.HomepageURL = ptr(ogURL)
	}
}

func metaContent(doc *goquery.Document, property string) string {
	return html.UnescapeString(strings.TrimSpace(doc.Find(`meta[property="`+property+`"]`).First().AttrOr("content", "")))
}

func isRootURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasSuffix(raw, "://"+hostOnly(raw)+"/") || strings.HasSuffix(raw, "://"+hostOnly(raw))
}

func hostOnly(raw string) string {
	withoutScheme := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if slash := strings.IndexByte(withoutScheme, '/'); slash >= 0 {
		return withoutScheme[:slash]
	}
	return withoutScheme
}

func formatFromAttendanceMode(mode string) string {
	lower := strings.ToLower(mode)
	if strings.Contains(lower, "mixed") {
		return "hybrid"
	}
	if strings.Contains(lower, "online") {
		return "online"
	}
	if strings.Contains(lower, "offline") {
		return "onsite"
	}
	return ""
}

func ptr(s string) *string { return &s }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

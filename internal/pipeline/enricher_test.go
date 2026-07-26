package pipeline

import (
	"context"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

type recordingEnricher struct {
	calls int
	text  string
	fill  string
}

func (r *recordingEnricher) Enrich(_ context.Context, event *model.Event, sourceText string) error {
	r.calls++
	r.text = sourceText
	if r.fill != "" {
		event.StartDate = &r.fill
	}
	return nil
}

// Ingest must stay exactly as deterministic as before unless an enricher is
// attached, so the default pipeline may never reach for a model.
func TestRunEnricher_isNoOpWithoutEnricher(t *testing.T) {
	// Given
	p := New("batch-test")
	event := &model.Event{MissingFields: []string{"start_date"}}

	// When
	p.runEnricher(context.Background(), event, &sources.ParsedEvent{Name: "행사"})

	// Then
	if event.StartDate != nil {
		t.Fatalf("start date = %q, want the deterministic result untouched", *event.StartDate)
	}
}

// A fully parsed event must not cost a model call.
func TestRunEnricher_skipsEventWithNothingMissing(t *testing.T) {
	// Given
	enricher := &recordingEnricher{}
	p := New("batch-test").WithEnricher(enricher)
	event := &model.Event{MissingFields: []string{}}

	// When
	p.runEnricher(context.Background(), event, &sources.ParsedEvent{Name: "행사"})

	// Then
	if enricher.calls != 0 {
		t.Fatalf("enricher calls = %d, want none for a complete event", enricher.calls)
	}
}

// The enricher reads the raw scraped strings, which is exactly where the
// information the normalizer could not interpret still lives.
func TestRunEnricher_passesRawScrapedText(t *testing.T) {
	// Given
	enricher := &recordingEnricher{fill: "2026-09-01"}
	p := New("batch-test").WithEnricher(enricher)
	event := &model.Event{MissingFields: []string{"start_date"}}
	raw := "2026.09.01(화)"
	parsed := &sources.ParsedEvent{Name: "AI 로봇 산업전", StartRaw: &raw}

	// When
	p.runEnricher(context.Background(), event, parsed)

	// Then
	if enricher.calls != 1 {
		t.Fatalf("enricher calls = %d, want one", enricher.calls)
	}
	if enricher.text == "" || !contains(enricher.text, raw) || !contains(enricher.text, "AI 로봇 산업전") {
		t.Fatalf("source text = %q, want the raw scraped date and name", enricher.text)
	}
	if event.StartDate == nil || *event.StartDate != "2026-09-01" {
		t.Fatalf("start date = %v, want the enricher result applied", event.StartDate)
	}
}

// An enricher failure is additive-only and must never cost the deterministic row.
func TestRunEnricher_survivesEnricherError(t *testing.T) {
	// Given
	p := New("batch-test").WithEnricher(failingEnricher{})
	event := &model.Event{MissingFields: []string{"start_date"}}

	// When
	p.runEnricher(context.Background(), event, &sources.ParsedEvent{Name: "행사"})

	// Then
	if event.StartDate != nil {
		t.Fatalf("start date = %v, want the row unchanged after an enricher error", event.StartDate)
	}
}

type failingEnricher struct{}

func (failingEnricher) Enrich(context.Context, *model.Event, string) error {
	return context.DeadlineExceeded
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

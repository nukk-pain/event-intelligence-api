// Package solarenrich resolves event dates the deterministic normalizer could
// not parse, using the Solar model inside batch ingest only.
//
// It is deliberately narrow. It fills start_date and end_date and nothing
// else, because those are objectively checkable against an ISO format and are
// the fields Korean venue pages express in the widest variety of shapes. It
// never overwrites a value the source supplied, never runs on the read path,
// and records its own provenance on every event it touches.
package solarenrich

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/model"
)

var ErrInvalidConfig = errors.New("solarenrich: invalid configuration")

// isoDate is the only shape an enriched date may take. Anything else is
// discarded rather than stored, so a model that answers in prose cannot put
// unparseable text into the event contract.
var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Config bounds one ingest run. Every field is required.
type Config struct {
	Backend     agent.Backend
	MaxCalls    int
	MaxTokens   int
	CallTimeout time.Duration
}

// Enricher is safe for concurrent use by the pipeline's detail workers.
type Enricher struct {
	config Config
	calls  atomic.Int64
	filled atomic.Int64
}

func New(config Config) (*Enricher, error) {
	if config.Backend.BaseURL == "" || config.Backend.Model == "" ||
		config.MaxCalls <= 0 || config.MaxTokens <= 0 || config.CallTimeout <= 0 {
		return nil, ErrInvalidConfig
	}
	return &Enricher{config: config}, nil
}

// Calls and Filled report what one ingest run spent and gained. Counts only.
func (e *Enricher) Calls() int64  { return e.calls.Load() }
func (e *Enricher) Filled() int64 { return e.filled.Load() }

// Enrich fills missing dates on one event. It returns nil when it declines to
// act, since declining is normal and must not be logged as a failure.
func (e *Enricher) Enrich(ctx context.Context, event *model.Event, sourceText string) error {
	if e == nil || event == nil || sourceText == "" {
		return nil
	}
	wantStart := missing(event.MissingFields, "start_date")
	wantEnd := missing(event.MissingFields, "end_date")
	if !wantStart && !wantEnd {
		return nil
	}
	if e.calls.Add(1) > int64(e.config.MaxCalls) {
		// The budget is a hard ceiling for the whole run, so an over-budget
		// event stays deterministic rather than waiting.
		e.calls.Add(-1)
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, e.config.CallTimeout)
	defer cancel()
	facts, _, _, err := agent.Extract(callCtx, e.config.Backend, sourceText, e.config.MaxTokens, e.config.CallTimeout)
	if err != nil {
		return err
	}

	changed := false
	if wantStart && event.StartDate == nil {
		if value, ok := isoValue(facts, "start_date"); ok {
			event.StartDate = &value
			changed = true
		}
	}
	if wantEnd && event.EndDate == nil {
		if value, ok := isoValue(facts, "end_date"); ok {
			event.EndDate = &value
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if event.StartDate != nil {
		event.MissingFields = without(event.MissingFields, "start_date")
	}
	if event.EndDate != nil {
		event.MissingFields = without(event.MissingFields, "end_date")
	}
	// An enriched claim without provenance would be indistinguishable from a
	// scraped one, so record where it came from before returning.
	event.Sources = append(event.Sources, model.Source{
		URL:       primarySourceURL(event),
		Type:      "venue",
		Publisher: "eventsintel/solar-enrich",
	})
	event.DateConfidence = "low"
	e.filled.Add(1)
	return nil
}

// primarySourceURL reuses the page the event was parsed from, so the
// enrichment record points at the same document the model read.
func primarySourceURL(event *model.Event) string {
	if len(event.Sources) == 0 {
		return ""
	}
	return event.Sources[0].URL
}

func isoValue(facts agent.Facts, key string) (string, bool) {
	raw, ok := facts[key]
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	if !ok || !isoDate.MatchString(text) {
		return "", false
	}
	return text, true
}

func missing(fields []string, key string) bool {
	for _, field := range fields {
		if field == key {
			return true
		}
	}
	return false
}

func without(fields []string, key string) []string {
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != key {
			kept = append(kept, field)
		}
	}
	return kept
}

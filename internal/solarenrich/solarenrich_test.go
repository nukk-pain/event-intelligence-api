package solarenrich

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/model"
)

func TestEnrich_fillsOnlyMissingDatesAndRecordsProvenance(t *testing.T) {
	// Given
	backend, calls, captured := fakeBackend(t, `{"start_date":"2026-09-01","end_date":"2026-09-03"}`)
	enricher := newEnricher(t, backend, 4)
	event := &model.Event{
		Sources: []model.Source{{URL: "https://venue.example/expo", Type: "venue"}}, MissingFields: []string{"end_date", "summary"},
		StartDate: strPtr("2026-08-01"),
	}

	// When
	if err := enricher.Enrich(context.Background(), event, "2026년 9월 1일부터 3일까지"); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// Then
	if got := *event.StartDate; got != "2026-08-01" {
		t.Fatalf("start date = %q, want the source-derived value untouched", got)
	}
	if event.EndDate == nil || *event.EndDate != "2026-09-03" {
		t.Fatalf("end date = %v, want the missing field filled", event.EndDate)
	}
	if hasField(event.MissingFields, "end_date") {
		t.Fatalf("missing fields = %v, want end_date cleared", event.MissingFields)
	}
	if !hasField(event.MissingFields, "summary") {
		t.Fatalf("missing fields = %v, want unrelated missing fields preserved", event.MissingFields)
	}
	if len(event.Sources) != 2 || event.Sources[1].Publisher != "eventsintel/solar-enrich" {
		t.Fatalf("sources = %#v, want the enrichment recorded as provenance", event.Sources)
	}
	if calls.Load() != 1 {
		t.Fatalf("model calls = %d, want exactly one", calls.Load())
	}
	if strings.Contains(captured(), "010-") {
		t.Fatalf("contact-like text reached the model: %s", captured())
	}
}

func TestEnrich_declinesWhenNothingIsMissing(t *testing.T) {
	// Given
	backend, calls, _ := fakeBackend(t, `{"start_date":"2026-09-01"}`)
	enricher := newEnricher(t, backend, 4)
	event := &model.Event{Sources: []model.Source{{URL: "https://venue.example/expo", Type: "venue"}}, MissingFields: []string{"summary"}}

	// When
	if err := enricher.Enrich(context.Background(), event, "행사 안내"); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// Then
	if calls.Load() != 0 {
		t.Fatalf("model calls = %d, want none when no date is missing", calls.Load())
	}
	if len(event.Sources) != 1 {
		t.Fatalf("sources = %#v, want no provenance for an untouched event", event.Sources)
	}
}

// A model that answers in prose must not put unparseable text into the event
// contract, and the row must stay exactly as the deterministic parse left it.
func TestEnrich_rejectsNonISODate(t *testing.T) {
	// Given
	backend, _, _ := fakeBackend(t, `{"start_date":"2026년 9월 1일"}`)
	enricher := newEnricher(t, backend, 4)
	event := &model.Event{Sources: []model.Source{{URL: "https://venue.example/expo", Type: "venue"}}, MissingFields: []string{"start_date"}}

	// When
	if err := enricher.Enrich(context.Background(), event, "2026년 9월 1일"); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// Then
	if event.StartDate != nil {
		t.Fatalf("start date = %q, want a non-ISO answer discarded", *event.StartDate)
	}
	if !hasField(event.MissingFields, "start_date") {
		t.Fatalf("missing fields = %v, want start_date still reported missing", event.MissingFields)
	}
	if len(event.Sources) != 1 {
		t.Fatalf("sources = %#v, want no provenance when nothing was filled", event.Sources)
	}
}

// The call budget is a ceiling for the whole run, so an over-budget event stays
// deterministic rather than blocking or overspending.
func TestEnrich_stopsAtCallBudget(t *testing.T) {
	// Given
	backend, calls, _ := fakeBackend(t, `{"start_date":"2026-09-01"}`)
	enricher := newEnricher(t, backend, 2)

	// When
	for range 5 {
		event := &model.Event{Sources: []model.Source{{URL: "https://venue.example/expo", Type: "venue"}}, MissingFields: []string{"start_date"}}
		if err := enricher.Enrich(context.Background(), event, "행사 일정"); err != nil {
			t.Fatalf("Enrich: %v", err)
		}
	}

	// Then
	if calls.Load() != 2 {
		t.Fatalf("model calls = %d, want the budget of 2 respected", calls.Load())
	}
	if enricher.Calls() != 2 || enricher.Filled() != 2 {
		t.Fatalf("calls/filled = %d/%d, want 2/2", enricher.Calls(), enricher.Filled())
	}
}

func TestNew_rejectsIncompleteConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil, want an invalid-configuration error")
	}
}

func newEnricher(t *testing.T, backend agent.Backend, maxCalls int) *Enricher {
	t.Helper()
	enricher, err := New(Config{
		Backend: backend, MaxCalls: maxCalls, MaxTokens: 256, CallTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return enricher
}

func fakeBackend(t *testing.T, content string) (agent.Backend, *atomic.Int32, func() string) {
	t.Helper()
	var calls atomic.Int32
	var lastBody atomic.Value
	lastBody.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err == nil {
			if encoded, err := json.Marshal(request); err == nil {
				lastBody.Store(string(encoded))
			}
		}
		response := map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "encode", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return agent.Backend{Name: "solar", BaseURL: server.URL, Model: "solar-test"},
		&calls, func() string { return lastBody.Load().(string) }
}

// scriptedBackend answers each call in order, which the multi-hop agent loop
// needs: extraction, then link selection, then action enrichment.
func scriptedBackend(t *testing.T, responses ...string) (agent.Backend, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		content := "{}"
		if index < len(responses) {
			content = responses[index]
		}
		_, _ = io.Copy(io.Discard, r.Body)
		response := map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "encode", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return agent.Backend{Name: "solar", BaseURL: server.URL, Model: "solar-test"}, &calls
}

func hasField(fields []string, key string) bool {
	for _, field := range fields {
		if field == key {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }

// The English name is missing on nine events out of ten, and it is what makes
// an event findable by an English query. The extraction prompt already returns
// it, so it costs no call beyond one the event may already need.
func TestEnrich_fillsEnglishName(t *testing.T) {
	// Given
	backend, calls, _ := fakeBackend(t, `{"name_en":"AI Robot Industry Expo 2027"}`)
	enricher := newEnricher(t, backend, 4)
	event := &model.Event{
		Name:          "2027 AI 로봇 산업전",
		Sources:       []model.Source{{URL: "https://venue.example/expo", Type: "venue"}},
		MissingFields: []string{"name_en", "start_date"},
	}

	// When
	if err := enricher.Enrich(context.Background(), event, "2027 AI 로봇 산업전"); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// Then
	if event.NameEn == nil || *event.NameEn != "AI Robot Industry Expo 2027" {
		t.Fatalf("name_en = %v, want the extracted English name", event.NameEn)
	}
	if hasField(event.MissingFields, "name_en") {
		t.Fatalf("missing fields = %v, want name_en cleared", event.MissingFields)
	}
	if calls.Load() != 1 {
		t.Fatalf("model calls = %d, want one", calls.Load())
	}
}

// A model that echoes the Korean title back has added nothing, and accepting it
// would wrongly mark the field resolved.
func TestEnrich_rejectsEchoedOrNonLatinName(t *testing.T) {
	for _, answer := range []string{`{"name_en":"2027 AI 로봇 산업전"}`, `{"name_en":"에이아이 로봇 산업전"}`, `{"name_en":""}`} {
		backend, _, _ := fakeBackend(t, answer)
		enricher := newEnricher(t, backend, 4)
		event := &model.Event{
			Name:          "2027 AI 로봇 산업전",
			Sources:       []model.Source{{URL: "https://venue.example/expo", Type: "venue"}},
			MissingFields: []string{"name_en"},
		}
		if err := enricher.Enrich(context.Background(), event, "2027 AI 로봇 산업전"); err != nil {
			t.Fatalf("Enrich: %v", err)
		}
		if event.NameEn != nil {
			t.Fatalf("answer %s gave name_en = %q, want it rejected", answer, *event.NameEn)
		}
		if !hasField(event.MissingFields, "name_en") {
			t.Fatalf("answer %s cleared name_en without filling it", answer)
		}
	}
}

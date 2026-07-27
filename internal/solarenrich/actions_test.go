package solarenrich

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

const officialPage = `<html><body><h1>2027 AI 로봇 산업전</h1>
<a href="/register">참가 등록</a><a href="/booth">부스 신청</a></body></html>`

func TestEnrichActions_fillsOnlyUnknownSignals(t *testing.T) {
	// Given
	backend, calls := scriptedBackend(t,
		`{"name":"2027 AI 로봇 산업전"}`,
		`{"pick":["https://venue.example/register","https://venue.example/booth"]}`,
		`{"register_url":"https://venue.example/register","register_deadline":"2027-01-31","booth_info":"부스 신청 접수 중","startup_program":null}`)
	enricher := newActionEnricher(t, backend, 4)
	already := "https://venue.example/already"
	current := sources.ActionSignals{RegisterURL: &already}

	// When
	out, err := enricher.EnrichActions(context.Background(), "https://venue.example/event", []byte(officialPage), current)

	// Then
	if err != nil {
		t.Fatalf("EnrichActions: %v", err)
	}
	if out.RegisterURL != nil {
		t.Fatalf("register url = %v, want the deterministic value left alone", *out.RegisterURL)
	}
	if out.RegistrationDeadline == nil || *out.RegistrationDeadline != "2027-01-31" {
		t.Fatalf("deadline = %v, want the unknown signal filled", out.RegistrationDeadline)
	}
	if out.CanExhibit == nil || !*out.CanExhibit {
		t.Fatalf("can exhibit = %v, want booth info to imply true", out.CanExhibit)
	}
	if out.HasStartupProgram != nil {
		t.Fatalf("startup program = %v, want an absent signal left unknown", *out.HasStartupProgram)
	}
	if calls.Load() == 0 {
		t.Fatal("model calls = 0, want the agent loop to run")
	}
	if enricher.Filled() != 1 {
		t.Fatalf("filled = %d, want 1", enricher.Filled())
	}
}

// Korean venue pages write deadlines in their own shape, and those carry a
// real date that must reach the event contract in ISO form.
func TestEnrichActions_acceptsKoreanDeadline(t *testing.T) {
	// Given
	backend, _ := scriptedBackend(t, `{"name":"2027 AI 로봇 산업전"}`, `{"pick":["https://venue.example/register","https://venue.example/booth"]}`,
		`{"register_deadline":"2027년 1월 31일까지"}`)
	enricher := newActionEnricher(t, backend, 4)

	// When
	out, err := enricher.EnrichActions(context.Background(), "https://venue.example/event", []byte(officialPage), sources.ActionSignals{})

	// Then
	if err != nil {
		t.Fatalf("EnrichActions: %v", err)
	}
	if out.RegistrationDeadline == nil || *out.RegistrationDeadline != "2027-01-31" {
		t.Fatalf("deadline = %v, want the Korean date normalized to ISO", out.RegistrationDeadline)
	}
}

// Text with no date in it must still be rejected rather than guessed at.
func TestEnrichActions_rejectsDatelessDeadline(t *testing.T) {
	// Given
	backend, _ := scriptedBackend(t, `{"name":"2027 AI 로봇 산업전"}`, `{"pick":["https://venue.example/register"]}`,
		`{"register_deadline":"선착순 마감"}`)
	enricher := newActionEnricher(t, backend, 4)

	// When
	out, err := enricher.EnrichActions(context.Background(), "https://venue.example/event", []byte(officialPage), sources.ActionSignals{})

	// Then
	if err != nil {
		t.Fatalf("EnrichActions: %v", err)
	}
	if out.RegistrationDeadline != nil {
		t.Fatalf("deadline = %q, want text without a date discarded", *out.RegistrationDeadline)
	}
}

// An event whose signals are all known must not cost a model call.
func TestEnrichActions_skipsFullyKnownEvent(t *testing.T) {
	// Given
	backend, calls := scriptedBackend(t, `{}`)
	enricher := newActionEnricher(t, backend, 4)
	value := "https://venue.example/x"
	yes := true
	current := sources.ActionSignals{
		RegisterURL: &value, RegistrationDeadline: &value, ExhibitorDeadline: &value,
		CanExhibit: &yes, HasStartupProgram: &yes,
	}

	// When
	if _, err := enricher.EnrichActions(context.Background(), "https://venue.example/event", []byte(officialPage), current); err != nil {
		t.Fatalf("EnrichActions: %v", err)
	}

	// Then
	if calls.Load() != 0 {
		t.Fatalf("model calls = %d, want none for a fully known event", calls.Load())
	}
}

func TestEnrichActions_stopsAtCallBudget(t *testing.T) {
	// Given
	backend, calls := scriptedBackend(t,
		`{"name":"2027 AI 로봇 산업전"}`, `{"pick":["https://venue.example/register","https://venue.example/booth"]}`, `{"booth_info":"부스"}`,
		`{"name":"2027 AI 로봇 산업전"}`, `{"pick":["https://venue.example/register","https://venue.example/booth"]}`, `{"booth_info":"부스"}`)
	enricher := newActionEnricher(t, backend, 2)

	// When
	for range 5 {
		if _, err := enricher.EnrichActions(context.Background(), "https://venue.example/event", []byte(officialPage), sources.ActionSignals{}); err != nil {
			t.Fatalf("EnrichActions: %v", err)
		}
	}

	// Then
	if enricher.Calls() != 2 {
		t.Fatalf("enricher calls = %d, want the budget of 2 respected", enricher.Calls())
	}
	if calls.Load() > 2*3 {
		t.Fatalf("model calls = %d, want the budget to bound the agent loop", calls.Load())
	}
}

func TestNewActionEnricher_requiresFetcher(t *testing.T) {
	backend, _ := scriptedBackend(t, `{}`)
	if _, err := NewActionEnricher(Config{
		Backend: backend, MaxCalls: 1, MaxTokens: 1, CallTimeout: time.Second,
	}, nil); err == nil {
		t.Fatal("NewActionEnricher() error = nil, want a missing-fetcher error")
	}
}

func TestPageTextAndLinks_resolvesRelativeHrefs(t *testing.T) {
	// When
	text, links, err := pageTextAndLinks("https://venue.example/event/", []byte(officialPage))

	// Then
	if err != nil {
		t.Fatalf("pageTextAndLinks: %v", err)
	}
	if text == "" {
		t.Fatal("text = empty, want the page body collapsed")
	}
	if len(links) != 2 || links[0].URL != "https://venue.example/register" {
		t.Fatalf("links = %#v, want relative hrefs resolved against the page", links)
	}
}

func newActionEnricher(t *testing.T, backend agent.Backend, maxCalls int) *ActionEnricher {
	t.Helper()
	enricher, err := NewActionEnricher(Config{
		Backend: backend, MaxCalls: maxCalls, MaxTokens: 256, CallTimeout: 5 * time.Second,
	}, func(context.Context, string) (string, error) { return "링크 페이지 본문", nil })
	if err != nil {
		t.Fatalf("NewActionEnricher: %v", err)
	}
	return enricher
}

// Full coverage runs eight detail units at once, which sits within a few
// percent of Solar's per-minute token ceiling. The enricher must hold its own
// concurrency below that rather than relying on the model to reject requests.
func TestEnrichActions_boundsConcurrentModelWork(t *testing.T) {
	// Given
	var live, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		now := live.Add(1)
		for {
			high := peak.Load()
			if now <= high || peak.CompareAndSwap(high, now) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		live.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": `{"booth_info":"부스"}`}}},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	t.Cleanup(server.Close)
	enricher, err := NewActionEnricher(Config{
		Backend:  agent.Backend{Name: "solar", BaseURL: server.URL, Model: "solar-test"},
		MaxCalls: 100, MaxTokens: 256, CallTimeout: 5 * time.Second, MaxConcurrent: 2,
	}, func(context.Context, string) (string, error) { return "본문", nil })
	if err != nil {
		t.Fatalf("NewActionEnricher: %v", err)
	}

	// When
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = enricher.EnrichActions(context.Background(),
				"https://venue.example/event", []byte(officialPage), sources.ActionSignals{})
		}()
	}
	wg.Wait()

	// Then
	if got := peak.Load(); got > 2 {
		t.Fatalf("peak concurrent model requests = %d, want the bound of 2 respected", got)
	}
	if enricher.Throttled() == 0 {
		t.Fatal("throttled = 0, want the bound to have made some attempts wait")
	}
}

// The exhibitor cutoff is a different date from the attendee one. A company
// weighing a booth needs it, and it was the one action field the enricher
// never filled because the agent loop did not ask for it.
func TestEnrichActions_fillsExhibitorDeadline(t *testing.T) {
	// Given
	backend, _ := scriptedBackend(t,
		`{"name":"2027 AI 로봇 산업전"}`,
		`{"pick":["https://venue.example/booth"]}`,
		`{"booth_info":"부스 신청 접수 중","booth_deadline":"2026년 12월 15일까지"}`)
	enricher := newActionEnricher(t, backend, 4)

	// When
	out, err := enricher.EnrichActions(context.Background(),
		"https://venue.example/event", []byte(officialPage), sources.ActionSignals{})

	// Then
	if err != nil {
		t.Fatalf("EnrichActions: %v", err)
	}
	if out.ExhibitorDeadline == nil || *out.ExhibitorDeadline != "2026-12-15" {
		t.Fatalf("exhibitor deadline = %v, want the Korean booth cutoff normalized", out.ExhibitorDeadline)
	}
	if out.CanExhibit == nil || !*out.CanExhibit {
		t.Fatalf("can exhibit = %v, want booth info to still imply true", out.CanExhibit)
	}
}

// The two deadlines must not be conflated.
func TestEnrichActions_keepsDeadlinesDistinct(t *testing.T) {
	// Given
	backend, _ := scriptedBackend(t,
		`{"name":"2027 AI 로봇 산업전"}`,
		`{"pick":["https://venue.example/register"]}`,
		`{"register_deadline":"2027-01-31","booth_deadline":"2026-12-15"}`)
	enricher := newActionEnricher(t, backend, 4)

	// When
	out, err := enricher.EnrichActions(context.Background(),
		"https://venue.example/event", []byte(officialPage), sources.ActionSignals{})

	// Then
	if err != nil {
		t.Fatalf("EnrichActions: %v", err)
	}
	if out.RegistrationDeadline == nil || *out.RegistrationDeadline != "2027-01-31" {
		t.Fatalf("registration deadline = %v, want the attendee date", out.RegistrationDeadline)
	}
	if out.ExhibitorDeadline == nil || *out.ExhibitorDeadline != "2026-12-15" {
		t.Fatalf("exhibitor deadline = %v, want the booth date", out.ExhibitorDeadline)
	}
}

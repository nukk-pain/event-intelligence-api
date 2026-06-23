package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

type catalogFallbackSource struct{}

func (s *catalogFallbackSource) ID() string { return "benchmark" }

func (s *catalogFallbackSource) Discover(_ context.Context, _ *fetch.Fetcher) ([]sources.Ref, error) {
	return []sources.Ref{{EventID: "benchmark-fallback", URL: "https://blocked.example/event"}}, nil
}

func (s *catalogFallbackSource) Parse(_ context.Context, _ *fetch.Result) (*sources.ParsedEvent, error) {
	return nil, fmt.Errorf("Parse should not run when fetch fails")
}

func (s *catalogFallbackSource) ParseFallback(_ context.Context, ref sources.Ref, _ error) (*sources.ParsedEvent, error) {
	return &sources.ParsedEvent{
		SourceID:     s.ID(),
		EventID:      ref.EventID,
		URL:          ref.URL,
		Name:         "Catalog Fallback AI Expo",
		StartRaw:     strptr("2027-01-06"),
		EndRaw:       strptr("2027-01-09"),
		VenueName:    strptr("Las Vegas, NV"),
		City:         strptr("Las Vegas"),
		Country:      strptr("US"),
		Timezone:     strptr("America/Los_Angeles"),
		Format:       strptr("onsite"),
		Publisher:    strptr("Catalog Organizer"),
		SourceType:   strptr("organizer"),
		HomepageURL:  strptr("https://blocked.example/event"),
		SummaryText:  strptr("AI and robotics event with source-backed catalog fallback fields."),
		ClassifyText: "AI robotics startup",
	}, nil
}

var _ sources.Source = (*catalogFallbackSource)(nil)

func TestRun_UsesSourceFallbackWhenFetchFails(t *testing.T) {
	db := testDB(t)
	f, err := fetch.NewFetcher()
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}

	rep, err := New("batch-fallback").
		WithClock(func() string { return "2026-06-23T00:00:00Z" }).
		Run(context.Background(), db, []sources.Source{&catalogFallbackSource{}}, f)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	sr := rep.Sources[0]
	if sr.Discovered != 1 || sr.Parsed != 1 || sr.Stored != 1 || sr.Skipped != 0 {
		t.Fatalf("source report = %+v, want discovered/parsed/stored=1 skipped=0", sr)
	}
	if !eventExists(t, db, "benchmark-fallback") {
		t.Fatal("fallback event was not stored")
	}
	if got := countIngestErrors(t, db); got != 0 {
		t.Fatalf("ingest_error rows = %d, want 0", got)
	}
}

func TestRun_UsesSourceFallbackWhenDetailStatusIsNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	db := testDB(t)
	f := loopbackFetcher(t, srv.URL)
	source := &statusFallbackSource{url: srv.URL}

	rep, err := New("batch-fallback-status").
		WithClock(func() string { return "2026-06-23T00:00:00Z" }).
		Run(context.Background(), db, []sources.Source{source}, f)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	sr := rep.Sources[0]
	if sr.Discovered != 1 || sr.Parsed != 1 || sr.Stored != 1 || sr.Skipped != 0 {
		t.Fatalf("source report = %+v, want discovered/parsed/stored=1 skipped=0", sr)
	}
	if !eventExists(t, db, "benchmark-status-fallback") {
		t.Fatal("status fallback event was not stored")
	}
}

type statusFallbackSource struct {
	url string
}

func (s *statusFallbackSource) ID() string { return "benchmark" }

func (s *statusFallbackSource) Discover(_ context.Context, _ *fetch.Fetcher) ([]sources.Ref, error) {
	return []sources.Ref{{EventID: "benchmark-status-fallback", URL: s.url}}, nil
}

func (s *statusFallbackSource) Parse(_ context.Context, _ *fetch.Result) (*sources.ParsedEvent, error) {
	return nil, fmt.Errorf("Parse should not run for non-200 detail responses")
}

func (s *statusFallbackSource) ParseFallback(_ context.Context, ref sources.Ref, _ error) (*sources.ParsedEvent, error) {
	return &sources.ParsedEvent{
		SourceID:     s.ID(),
		EventID:      ref.EventID,
		URL:          ref.URL,
		Name:         "Catalog Status Fallback AI Expo",
		StartRaw:     strptr("2027-02-01"),
		EndRaw:       strptr("2027-02-02"),
		VenueName:    strptr("Doha Exhibition and Convention Center"),
		City:         strptr("Doha"),
		Country:      strptr("QA"),
		Timezone:     strptr("Asia/Qatar"),
		Format:       strptr("onsite"),
		Publisher:    strptr("Catalog Organizer"),
		SourceType:   strptr("organizer"),
		HomepageURL:  strptr(ref.URL),
		SummaryText:  strptr("Catalog fallback fields survive a blocked official detail page."),
		ClassifyText: "AI startup",
	}, nil
}

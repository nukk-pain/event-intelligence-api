package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func TestMergeActionSignalsPreservesCatalogTruth(t *testing.T) {
	base := sources.ActionSignals{
		CanRegister:       boolptr(false),
		HasMatchmaking:    boolptr(true),
		HasStartupProgram: boolptr(true),
		CostHint:          strptr("unknown"),
	}
	extra := sources.ActionSignals{
		CanRegister:       boolptr(true),
		CanExhibit:        boolptr(true),
		CanSponsor:        boolptr(true),
		HasMatchmaking:    boolptr(false),
		HasStartupProgram: boolptr(false),
		RegisterURL:       strptr("https://example.com/register"),
		ExhibitURL:        strptr("https://example.com/exhibit"),
		CostHint:          strptr("paid"),
	}

	got := mergeActionSignals(base, extra)

	if got.CanRegister == nil || *got.CanRegister {
		t.Fatalf("CanRegister = %v, want preserved false", got.CanRegister)
	}
	if got.HasMatchmaking == nil || !*got.HasMatchmaking {
		t.Fatalf("HasMatchmaking = %v, want preserved true", got.HasMatchmaking)
	}
	if got.HasStartupProgram == nil || !*got.HasStartupProgram {
		t.Fatalf("HasStartupProgram = %v, want preserved true", got.HasStartupProgram)
	}
	if got.CanExhibit == nil || !*got.CanExhibit {
		t.Fatalf("CanExhibit = %v, want filled true", got.CanExhibit)
	}
	if got.CanSponsor == nil || !*got.CanSponsor {
		t.Fatalf("CanSponsor = %v, want filled true", got.CanSponsor)
	}
	if got.RegisterURL == nil || *got.RegisterURL != "https://example.com/register" {
		t.Fatalf("RegisterURL = %v, want filled URL", got.RegisterURL)
	}
	if got.CostHint == nil || *got.CostHint != "unknown" {
		t.Fatalf("CostHint = %v, want preserved unknown", got.CostHint)
	}
}

func boolptr(v bool) *bool { return &v }

type benchmarkActionSource struct {
	base string
}

func (s *benchmarkActionSource) ID() string { return "benchmark" }

func (s *benchmarkActionSource) Discover(context.Context, *fetch.Fetcher) ([]sources.Ref, error) {
	return []sources.Ref{{EventID: "benchmark-bio-international-2027", URL: s.base + "/detail"}}, nil
}

func (s *benchmarkActionSource) Parse(_ context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	return &sources.ParsedEvent{
		SourceID:     "benchmark",
		EventID:      "benchmark-bio-international-2027",
		URL:          raw.URL,
		Name:         "BIO International Convention 2027",
		StartRaw:     strptr("2027-06-07"),
		EndRaw:       strptr("2027-06-10"),
		VenueName:    strptr("Philadelphia, PA"),
		City:         strptr("Philadelphia"),
		Country:      strptr("US"),
		Timezone:     strptr("America/New_York"),
		Format:       strptr("onsite"),
		Publisher:    strptr("Biotechnology Innovation Organization"),
		SourceType:   strptr("organizer"),
		HomepageURL:  strptr(s.base + "/future-dates"),
		SummaryText:  strptr("BIO International Convention 2027 is a global biotechnology convention."),
		ClassifyText: "bio biotechnology digital health AI investors partnering",
		Actions: sources.ActionSignals{
			HasMatchmaking: boolptr(true),
			CostHint:       strptr("unknown"),
		},
	}, nil
}

func TestRun_SkipsGenericActionEnrichmentForBenchmarkCatalog(t *testing.T) {
	db := testDB(t)
	srv := benchmarkActionServer(t)
	f := loopbackFetcher(t, srv.URL)

	rep, err := New("batch-benchmark-action").
		WithClock(func() string { return "2026-06-23T00:00:00Z" }).
		Run(context.Background(), db, []sources.Source{&benchmarkActionSource{base: srv.URL}}, f)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if rep.Sources[0].Stored != 1 {
		t.Fatalf("stored = %d, want 1", rep.Sources[0].Stored)
	}

	assertBenchmarkCatalogActionHonesty(t, db)
}

func benchmarkActionServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/detail", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "BIO International Convention 2027")
	})
	mux.HandleFunc("/future-dates", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<a href="/media-resource-center">Register media</a><a href="/exhibit">Exhibit</a><a href="/sponsor">Sponsor</a>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func assertBenchmarkCatalogActionHonesty(t *testing.T, db *sql.DB) {
	t.Helper()
	actions := stringColumn(t, db, "benchmark-bio-international-2027", "actions")
	if !strings.Contains(actions.String, `"has_matchmaking":true`) {
		t.Fatalf("actions = %s, want catalog matchmaking preserved", actions.String)
	}
	for _, unexpected := range []string{`"can_register":true`, `"can_exhibit":true`, `"can_sponsor":true`} {
		if strings.Contains(actions.String, unexpected) {
			t.Fatalf("actions = %s, did not want generic %s", actions.String, unexpected)
		}
	}
	for _, column := range []string{"register_url", "exhibit_url"} {
		if value := stringColumn(t, db, "benchmark-bio-international-2027", column); value.Valid {
			t.Fatalf("%s = %q, want NULL", column, value.String)
		}
	}
	missing := stringColumn(t, db, "benchmark-bio-international-2027", "missing_fields")
	for _, want := range []string{"register_url", "exhibit_url", "registration_deadline", "exhibitor_deadline"} {
		if !strings.Contains(missing.String, want) {
			t.Fatalf("missing_fields = %s, want %q", missing.String, want)
		}
	}
}

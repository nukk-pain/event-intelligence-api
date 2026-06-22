package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

type actionSource struct {
	base string
}

func (s *actionSource) ID() string { return "coex" }

func (s *actionSource) Discover(context.Context, *fetch.Fetcher) ([]sources.Ref, error) {
	return []sources.Ref{{EventID: "coex-action", URL: s.base + "/d/action"}}, nil
}

func (s *actionSource) Parse(_ context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	return &sources.ParsedEvent{
		SourceID:     "coex",
		EventID:      "coex-action",
		URL:          raw.URL,
		Name:         strings.TrimSpace(string(raw.Body)),
		StartRaw:     strptr("2026-06-18"),
		EndRaw:       strptr("2026-06-20"),
		VenueName:    strptr("코엑스"),
		City:         strptr("서울"),
		HomepageURL:  strptr(s.base + "/official/action"),
		ClassifyText: "AI action 인공지능",
		RetrievedAt:  "2026-06-21T00:00:00Z",
	}, nil
}

func actionServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/d/action", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "AI Action Expo")
	})
	mux.HandleFunc("/official/action", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<a href="/register">사전등록</a><a href="/exhibit">전시참가</a><p>입장료 무료</p><p>참가 신청 마감: 2026.07.31</p><p>부스 신청 마감: 2026-08-15</p>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRun_EnrichesActionFieldsFromOfficialPage(t *testing.T) {
	db := testDB(t)
	srv := actionServer(t)
	f := loopbackFetcher(t, srv.URL)

	rep, err := New("batch-action").WithClock(fixedClock).Run(context.Background(), db, []sources.Source{&actionSource{base: srv.URL}}, f)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if rep.Sources[0].Stored != 1 {
		t.Fatalf("stored = %d, want 1", rep.Sources[0].Stored)
	}

	assertStoredActionFields(t, db, srv.URL)
	assertAPIDetailActionFields(t, db, srv.URL)
}

func assertStoredActionFields(t *testing.T, db *sql.DB, baseURL string) {
	t.Helper()
	actions := stringColumn(t, db, "coex-action", "actions")
	if !strings.Contains(actions.String, `"can_register":true`) || !strings.Contains(actions.String, `"can_exhibit":true`) {
		t.Fatalf("actions = %s, want register/exhibit true", actions.String)
	}
	assertColumn(t, db, "register_url", baseURL+"/register")
	assertColumn(t, db, "exhibit_url", baseURL+"/exhibit")
	assertColumn(t, db, "cost_hint", "free")
	assertColumn(t, db, "registration_deadline", "2026-07-31")
	assertColumn(t, db, "exhibitor_deadline", "2026-08-15")
	if sourcesJSON := stringColumn(t, db, "coex-action", "sources"); !strings.Contains(sourcesJSON.String, `"type":"organizer"`) {
		t.Fatalf("sources = %s, want organizer provenance", sourcesJSON.String)
	}
}

func assertAPIDetailActionFields(t *testing.T, db *sql.DB, baseURL string) {
	t.Helper()
	handler, err := api.Router(db, api.MiddlewareConfig{})
	if err != nil {
		t.Fatalf("api router: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/coex-action", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET detail = %d, body=%s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Data struct {
			RegisterURL          string `json:"register_url"`
			ExhibitURL           string `json:"exhibit_url"`
			RegistrationDeadline string `json:"registration_deadline"`
			ExhibitorDeadline    string `json:"exhibitor_deadline"`
			CostHint             string `json:"cost_hint"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Data.RegisterURL != baseURL+"/register" || detail.Data.ExhibitURL != baseURL+"/exhibit" {
		t.Fatalf("api detail links = %q / %q", detail.Data.RegisterURL, detail.Data.ExhibitURL)
	}
	if detail.Data.RegistrationDeadline != "2026-07-31" || detail.Data.ExhibitorDeadline != "2026-08-15" {
		t.Fatalf("api detail deadlines = %q / %q", detail.Data.RegistrationDeadline, detail.Data.ExhibitorDeadline)
	}
	if detail.Data.CostHint != "free" {
		t.Fatalf("api detail cost_hint = %q, want free", detail.Data.CostHint)
	}
}

func stringColumn(t *testing.T, db *sql.DB, id string, column string) sql.NullString {
	t.Helper()
	var value sql.NullString
	if err := db.QueryRow(`SELECT `+column+` FROM events WHERE event_id=?`, id).Scan(&value); err != nil {
		t.Fatalf("read %s: %v", column, err)
	}
	return value
}

func assertColumn(t *testing.T, db *sql.DB, column string, want string) {
	t.Helper()
	value := stringColumn(t, db, "coex-action", column)
	if !value.Valid || value.String != want {
		t.Fatalf("%s = %q valid=%v, want %q", column, value.String, value.Valid, want)
	}
}

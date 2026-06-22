package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/store"
)

func routerHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	wdb, err := store.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if err := store.Migrate(wdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := wdb.Close(); err != nil {
		t.Fatalf("close write db: %v", err)
	}

	rdb, err := store.OpenRead(path)
	if err != nil {
		t.Fatalf("OpenRead: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })

	h, err := api.Router(rdb, api.MiddlewareConfig{})
	if err != nil {
		t.Fatalf("Router: %v", err)
	}
	return h
}

func TestRootIndex(t *testing.T) {
	h := routerHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("root not valid JSON: %v", err)
	}
	if body["service"] == nil || body["endpoints"] == nil {
		t.Errorf("root index missing service/endpoints keys: %v", body)
	}
}

func TestRootIndexServesInteractiveHTML_whenBrowserAcceptsHTML(t *testing.T) {
	h := routerHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="modal-overlay"`) {
		t.Fatalf("root HTML missing modal overlay")
	}
	if !strings.Contains(body, `href="/assets/detail.css"`) {
		t.Fatalf("root HTML missing detail stylesheet")
	}
}

func TestRootIndexHTMLUsesHumanFriendlyCopy(t *testing.T) {
	h := routerHandler(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, slop := range []string{
		"Free, read-only API",
		"JSON-first",
		"agent-friendly",
		">Meta<",
		">Schema<",
		">OpenAPI<",
		">llms.txt<",
		"Rate limited",
		"Loading",
		"Failed to load",
		"Date TBD",
		" shown",
		"\" events\"",
		"retrieved ",
		"Close dialog",
		"Search loaded events",
		"Data sourced from COEX and KINTEX. API:",
		"서비스 정보",
		"데이터 항목",
		"지난 행사",
		"전체 기간",
		"accent-dot",
		"COEX·KINTEX 행사 모아보기<span",
		"<title>COEX·KINTEX 행사 모아보기</title>",
	} {
		if strings.Contains(body, slop) {
			t.Fatalf("root HTML still contains developer/AI slop copy %q", slop)
		}
	}
	for _, want := range []string{
		"행사 모아보기",
		"주요 분야",
		"모든 행사 보기",
		"행사명이나 설명 검색",
		"연동 안내",
		"요약 문서",
		"더 보기",
		"공개 예정 일정을 정리해 보여줍니다",
		`href="/assets/index.css"`,
		`src="/assets/events.js"`,
		`src="/assets/app.js?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("root HTML missing human-friendly copy %q", want)
		}
	}
}

func TestRootIndexServesSplitFrontendAssets(t *testing.T) {
	h := routerHandler(t)
	for _, tc := range []struct {
		path        string
		contentType string
		want        string
	}{
		{path: "/assets/theme.css", contentType: "text/css", want: "--bg"},
		{path: "/assets/index.css", contentType: "text/css", want: ".card"},
		{path: "/assets/list.css", contentType: "text/css", want: ".load-more"},
		{path: "/assets/detail.css", contentType: "text/css", want: ".detail-hero"},
		{path: "/assets/ui.js", contentType: "application/javascript", want: "EventIntelUI"},
		{path: "/assets/events.js", contentType: "application/javascript", want: "EventIntelEvents"},
		{path: "/assets/detail.js", contentType: "application/javascript", want: "EventIntelDetail"},
		{path: "/assets/app.js", contentType: "application/javascript", want: "loadEvents"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			// Given
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()

			// When
			h.ServeHTTP(rec, req)

			// Then
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.path, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.contentType) {
				t.Fatalf("content-type = %q, want %s", ct, tc.contentType)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("%s missing %q", tc.path, tc.want)
			}
		})
	}
}

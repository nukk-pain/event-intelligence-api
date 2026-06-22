package coex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// strptr is a small helper for building *string expectations.
func strptr(s string) *string { return &s }

// derefOr renders a *string for failure messages.
func derefOr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// loadFixtureResult reads a saved COEX detail HTML fixture and wraps it in a
// fetch.Result as the fetcher would hand it to Parse.
func loadFixtureResult(t *testing.T, name, url string) *fetch.Result {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return &fetch.Result{
		URL:         url,
		StatusCode:  200,
		ContentType: "text/html; charset=UTF-8",
		Body:        body,
	}
}

// TestParseGoldenAIFesta is the Golden test: parse the saved COEX detail page
// and assert the extracted ParsedEvent matches the expected struct field-by-field.
// Provenance for the fixture lives in testdata/ai-festa-2026.html.meta.
func TestParseGoldenAIFesta(t *testing.T) {
	const detailURL = "https://www.coex.co.kr/exhibitions/%ec%9d%b8%ea%b3%b5%ec%a7%80%eb%8a%a5-%ed%8e%98%ec%8a%a4%ed%83%80-2026/"

	s := New()
	raw := loadFixtureResult(t, "ai-festa-2026.html", detailURL)
	// EventID would come from the discovering Ref; Parse derives it from the URL
	// slug the same way Discover does.
	got, err := s.Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := &sources.ParsedEvent{
		SourceID:    "coex",
		EventID:     "coex-%ec%9d%b8%ea%b3%b5%ec%a7%80%eb%8a%a5-%ed%8e%98%ec%8a%a4%ed%83%80-2026",
		URL:         detailURL,
		Name:        "인공지능 페스타 2026",
		StartRaw:    strptr("2026.10.06"),
		EndRaw:      strptr("2026.10.08"),
		VenueName:   strptr("코엑스"),
		Hall:        strptr("Hall C"),
		City:        strptr("서울"),
		Organizer:   strptr("과학기술정보통신부"),
		Host:        strptr("한국인공지능•소프트웨어산업협회, AI페스타 조직위원회"),
		HomepageURL: strptr("https://www.aifesta.kr/"),
		SummaryText: strptr("AI Festa는 과학기술정보통신부 인공지능주간 공식 행사입니다.인공지능 혁신 생태계의 최접점에 위치한 국내 최대 규모 '비즈니스 플랫폼’ 으로, 'AI 핵심 리더'들과 함께 AI 기술의 실제 비즈니스 적용 사례를 공유합니다."),
	}

	if got.SourceID != want.SourceID {
		t.Errorf("SourceID = %q, want %q", got.SourceID, want.SourceID)
	}
	if got.EventID != want.EventID {
		t.Errorf("EventID = %q, want %q", got.EventID, want.EventID)
	}
	if got.URL != want.URL {
		t.Errorf("URL = %q, want %q", got.URL, want.URL)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if derefOr(got.StartRaw) != derefOr(want.StartRaw) {
		t.Errorf("StartRaw = %q, want %q", derefOr(got.StartRaw), derefOr(want.StartRaw))
	}
	if derefOr(got.EndRaw) != derefOr(want.EndRaw) {
		t.Errorf("EndRaw = %q, want %q", derefOr(got.EndRaw), derefOr(want.EndRaw))
	}
	if derefOr(got.VenueName) != derefOr(want.VenueName) {
		t.Errorf("VenueName = %q, want %q", derefOr(got.VenueName), derefOr(want.VenueName))
	}
	if derefOr(got.Hall) != derefOr(want.Hall) {
		t.Errorf("Hall = %q, want %q", derefOr(got.Hall), derefOr(want.Hall))
	}
	if derefOr(got.City) != derefOr(want.City) {
		t.Errorf("City = %q, want %q", derefOr(got.City), derefOr(want.City))
	}
	if derefOr(got.Organizer) != derefOr(want.Organizer) {
		t.Errorf("Organizer = %q, want %q", derefOr(got.Organizer), derefOr(want.Organizer))
	}
	if derefOr(got.Host) != derefOr(want.Host) {
		t.Errorf("Host = %q, want %q", derefOr(got.Host), derefOr(want.Host))
	}
	if derefOr(got.HomepageURL) != derefOr(want.HomepageURL) {
		t.Errorf("HomepageURL = %q, want %q", derefOr(got.HomepageURL), derefOr(want.HomepageURL))
	}
	if derefOr(got.SummaryText) != derefOr(want.SummaryText) {
		t.Errorf("SummaryText = %q, want %q", derefOr(got.SummaryText), derefOr(want.SummaryText))
	}

	// ClassifyText must carry title + organizer/host so the classify stage stays
	// source-agnostic. Assert it is non-empty and contains the title + organizer.
	if got.ClassifyText == "" {
		t.Errorf("ClassifyText is empty; want title+organizer text")
	}
	if !contains(got.ClassifyText, want.Name) {
		t.Errorf("ClassifyText %q does not contain name %q", got.ClassifyText, want.Name)
	}
	if !contains(got.ClassifyText, "과학기술정보통신부") {
		t.Errorf("ClassifyText %q does not contain organizer", got.ClassifyText)
	}
	if !contains(got.ClassifyText, "AI 기술의 실제 비즈니스 적용 사례") {
		t.Errorf("ClassifyText %q does not contain source summary text", got.ClassifyText)
	}

	// RetrievedAt is stamped by the pipeline/caller (the fetch.Result carries no
	// timestamp), so Parse leaves it empty.
	if got.RetrievedAt != "" {
		t.Errorf("RetrievedAt = %q, want empty (set by caller, not Parse)", got.RetrievedAt)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 ||
		(len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestParseRejectsNon200 ensures Parse does not try to parse an error page body.
func TestParseRejectsNon200(t *testing.T) {
	s := New()
	raw := &fetch.Result{URL: "https://www.coex.co.kr/exhibitions/x/", StatusCode: 404, Body: []byte("<html></html>")}
	if _, err := s.Parse(context.Background(), raw); err == nil {
		t.Fatalf("Parse on 404 should error, got nil")
	}
}

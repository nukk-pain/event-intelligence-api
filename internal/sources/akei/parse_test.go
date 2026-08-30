package akei

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func TestParseStructuredDetailTable(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "detail.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	s := New()
	ev, err := s.Parse(context.Background(), &fetch.Result{
		URL:        "https://www.akei.or.kr/bbs/board.php?bo_table=schedule&wr_id=104742",
		StatusCode: 200,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if ev.SourceID != "akei" || ev.EventID != "akei-104742" {
		t.Errorf("ids = %s/%s", ev.SourceID, ev.EventID)
	}
	if ev.Name != "제주 베이비&키즈페어" {
		t.Errorf("name = %q", ev.Name)
	}
	if ev.NameEn == nil || *ev.NameEn != "Jeju Baby & Kids Fair" {
		t.Errorf("name_en = %v", ev.NameEn)
	}
	if ev.Organizer == nil || *ev.Organizer != "제주전람" {
		t.Errorf("organizer = %v", ev.Organizer)
	}
	if ev.StartRaw == nil || *ev.StartRaw != "2026-07-02" {
		t.Errorf("start = %v", ev.StartRaw)
	}
	if ev.EndRaw == nil || *ev.EndRaw != "2026-07-05" {
		t.Errorf("end = %v", ev.EndRaw)
	}
	if ev.VenueName == nil || *ev.VenueName != "한라체육관" {
		t.Errorf("venue = %v", ev.VenueName)
	}
	if !strings.Contains(ev.ClassifyText, "임신/출산/육아") {
		t.Errorf("classify text missing 전시분야: %q", ev.ClassifyText)
	}
	if ev.SourceType == nil || *ev.SourceType != "aggregator" {
		t.Errorf("source type = %v, want aggregator", ev.SourceType)
	}

	// Data policy: contact details on the page must never be extracted.
	all := strings.Join([]string{
		ev.Name, ev.ClassifyText,
		strDeref(ev.NameEn), strDeref(ev.Organizer), strDeref(ev.SummaryText),
		strDeref(ev.HomepageURL),
	}, " ")
	for _, contact := range []string{"010-2665-1005", "38178318@daum.net"} {
		if strings.Contains(all, contact) {
			t.Errorf("parsed event leaked contact %q", contact)
		}
	}
}

func TestForeignLocation(t *testing.T) {
	tests := []struct {
		venue    string
		country  string
		timezone string
	}{
		{venue: "자카르타(Jakarta)", country: "ID", timezone: "Asia/Jakarta"},
		{venue: "타이페이 World Trade center", country: "TW", timezone: "Asia/Taipei"},
		{venue: "베트남(Vietnam)", country: "VN", timezone: "Asia/Ho_Chi_Minh"},
		{venue: "VEC(Vietnam Exposition Center)", country: "VN", timezone: "Asia/Ho_Chi_Minh"},
		{venue: "베튼마 호치민 SECC 전시장", country: "VN", timezone: "Asia/Ho_Chi_Minh"},
		{venue: "Magic Box, LA, USA", country: "US", timezone: "America/Los_Angeles"},
		{venue: "American Dream, New Jesey, USA", country: "US", timezone: "America/New_York"},
		{venue: "코엑스(COEX)"},
		{venue: "한라체육관"},
	}

	for _, tt := range tests {
		t.Run(tt.venue, func(t *testing.T) {
			country, timezone := foreignLocation(tt.venue)
			if country != tt.country || timezone != tt.timezone {
				t.Fatalf("foreignLocation(%q) = %q/%q, want %q/%q", tt.venue, country, timezone, tt.country, tt.timezone)
			}
		})
	}
}

func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

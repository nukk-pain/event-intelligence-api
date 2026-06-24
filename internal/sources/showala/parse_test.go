package showala

import (
	"context"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// Parse extracts the RAW detail fields for RoboWorld (idx=3219) from the real
// ba_info markup.
func TestParseRobotworldDetail(t *testing.T) {
	body := readFixture(t, "detail_robotworld.html")
	raw := &fetch.Result{
		URL:        "https://showala.com/ex/ex_detail.php?idx=3219",
		StatusCode: 200,
		Body:       body,
	}

	s := New()
	pe, err := s.Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"SourceID", pe.SourceID, "showala"},
		{"EventID", pe.EventID, "showala-3219"},
		{"Name", pe.Name, "2026 로보월드"},
		{"NameKo", deref(pe.NameKo), "2026 로보월드"},
		{"NameEn", deref(pe.NameEn), "ROBOTWORLD 2026"},
		{"StartRaw", deref(pe.StartRaw), "2026-11-04"},
		{"EndRaw", deref(pe.EndRaw), "2026-11-07"},
		{"VenueName", deref(pe.VenueName), "킨텍스 (KINTEX)"},
		{"Hall", deref(pe.Hall), "제 1전시장 3~5 Hall"},
		{"Host", deref(pe.Host), "한국AI•로봇산업협회"},
		{"HomepageURL", deref(pe.HomepageURL), "https://www.robotworld.or.kr/"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}

	if pe.Organizer == nil || !contains(*pe.Organizer, "산업통상") {
		t.Errorf("Organizer = %q, want contains 산업통상", deref(pe.Organizer))
	}
	// ClassifyText must carry the name so the keyword classifier can see it.
	if !contains(pe.ClassifyText, "로보월드") {
		t.Errorf("ClassifyText = %q, want contains 로보월드", pe.ClassifyText)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

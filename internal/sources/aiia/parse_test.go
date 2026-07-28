package aiia

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func parseFixture(t *testing.T, name, url string) *fetch.Result {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return &fetch.Result{URL: url, StatusCode: 200, Body: body}
}

// The captured Tech-Day post carries its content as one poster image, so the
// only honest date value is nil; the posting date in the header (2026-07-09)
// must never leak into StartRaw.
func TestParseImageOnlyEventPost(t *testing.T) {
	s := New()
	res := parseFixture(t, "detail-event.html", "https://k-ai.or.kr/bbs/board.php?tbl=bbs41&mode=VIEW&num=408")

	ev, err := s.Parse(context.Background(), res)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ev.SourceID != "aiia" || ev.EventID != "aiia-408" {
		t.Errorf("ids = %s/%s", ev.SourceID, ev.EventID)
	}
	if ev.Name != "[고려대학교] Human-Inspired AI연구원 (HIAI연구원) 2026 Tech-Day 행사 안내" {
		t.Errorf("name = %q", ev.Name)
	}
	if ev.StartRaw != nil {
		t.Errorf("image-only body must yield nil StartRaw, got %q", *ev.StartRaw)
	}
	if ev.Organizer == nil || *ev.Organizer != "고려대학교" {
		t.Errorf("organizer = %v, want 고려대학교", ev.Organizer)
	}
	if ev.ClassifyText == "" || ev.RetrievedAt == "" {
		t.Errorf("classify/retrieved missing: %q %q", ev.ClassifyText, ev.RetrievedAt)
	}
}

// Synthetic fixture with a dated text body: the explicit event date in the
// content container becomes StartRaw while the header posting date is ignored.
func TestParseDatedTextBody(t *testing.T) {
	s := New()
	res := parseFixture(t, "detail-event-dated.html", "https://k-ai.or.kr/bbs/board.php?tbl=bbs41&mode=VIEW&num=999")

	ev, err := s.Parse(context.Background(), res)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ev.StartRaw == nil || *ev.StartRaw != "2026년 8월 12일" {
		t.Errorf("StartRaw = %v, want 2026년 8월 12일", ev.StartRaw)
	}
	if ev.EndRaw == nil || *ev.EndRaw != "2026년 8월 13일" {
		t.Errorf("EndRaw = %v, want 2026년 8월 13일", ev.EndRaw)
	}
}

// A body that references a past event before the real announcement must not
// leak the stale date: the 일시/기간-labeled line wins, and when no label
// exists multiple scattered dates are ambiguous and stay nil.
func TestParseDatePrefersLabeledLineAndRefusesAmbiguity(t *testing.T) {
	s := New()

	labeled := `<div class="basic_board_view"><table class="bbs_view"><tr><th><div class="tit"><div class="left"><em>[AIIA] 후속 컨퍼런스 안내</em><p>관리자 │ 2026-07-21</p></div></div></th></tr><tr><td><div id="DivContents">
<p>지난 2025년 3월 1일 행사에 이어 후속 행사를 안내드립니다.</p>
<p>행사 일시: 2026년 9월 1일 ~ 2026년 9월 2일</p>
</div></td></tr></table></div>`
	res := &fetch.Result{URL: "https://k-ai.or.kr/bbs/board.php?tbl=bbs41&mode=VIEW&num=901", StatusCode: 200, Body: []byte(labeled)}
	ev, err := s.Parse(context.Background(), res)
	if err != nil {
		t.Fatalf("Parse labeled: %v", err)
	}
	if ev.StartRaw == nil || *ev.StartRaw != "2026년 9월 1일" {
		t.Errorf("labeled StartRaw = %v, want 2026년 9월 1일", ev.StartRaw)
	}
	if ev.EndRaw == nil || *ev.EndRaw != "2026년 9월 2일" {
		t.Errorf("labeled EndRaw = %v, want 2026년 9월 2일", ev.EndRaw)
	}

	ambiguous := `<div class="basic_board_view"><table class="bbs_view"><tr><th><div class="tit"><div class="left"><em>[AIIA] 세미나 안내</em></div></div></th></tr><tr><td><div id="DivContents">
<p>2025년 3월 1일에 1회를 열었습니다.</p>
<p>다음 회차는 2026년 10월 5일에 논의 중입니다.</p>
</div></td></tr></table></div>`
	res2 := &fetch.Result{URL: "https://k-ai.or.kr/bbs/board.php?tbl=bbs41&mode=VIEW&num=902", StatusCode: 200, Body: []byte(ambiguous)}
	ev2, err := s.Parse(context.Background(), res2)
	if err != nil {
		t.Fatalf("Parse ambiguous: %v", err)
	}
	if ev2.StartRaw != nil {
		t.Errorf("ambiguous multi-date body must stay nil, got %q", *ev2.StartRaw)
	}
}

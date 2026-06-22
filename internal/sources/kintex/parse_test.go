package kintex

import (
	"context"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

func strp(s string) *string { return &s }

func gotWant(t *testing.T, field string, got *string, want *string) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil && want != nil:
		t.Errorf("%s = nil, want %q", field, *want)
	case got != nil && want == nil:
		t.Errorf("%s = %q, want nil", field, *got)
	case *got != *want:
		t.Errorf("%s = %q, want %q", field, *got, *want)
	}
}

// Golden: the saved real view.do?seq=26060201 page parses into the exact raw
// ParsedEvent we observed during the 2026-06-21 capture. Raw strings only —
// the normalize stage owns ISO/date validation.
func TestParseGolden(t *testing.T) {
	res := &fetch.Result{
		URL:        "https://www.kintex.com/web/ko/event/view.do?seq=26060201",
		StatusCode: 200,
		Body:       readFixture(t, "view-26060201.html"),
	}

	pe, err := New().Parse(context.Background(), res)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if pe.SourceID != "kintex" {
		t.Errorf("SourceID = %q, want kintex", pe.SourceID)
	}
	if pe.EventID != "kintex-26060201" {
		t.Errorf("EventID = %q, want kintex-26060201", pe.EventID)
	}
	if pe.URL != res.URL {
		t.Errorf("URL = %q, want %q", pe.URL, res.URL)
	}
	if pe.Name != "2026고양가구박람회" {
		t.Errorf("Name = %q, want 2026고양가구박람회", pe.Name)
	}

	gotWant(t, "StartRaw", pe.StartRaw, strp("2026-06-18"))
	gotWant(t, "EndRaw", pe.EndRaw, strp("2026-06-21"))
	gotWant(t, "VenueName", pe.VenueName, strp("KINTEX"))
	gotWant(t, "Hall", pe.Hall, strp("전시홀 7 , 전시홀 8"))
	gotWant(t, "City", pe.City, strp("고양"))
	gotWant(t, "Organizer", pe.Organizer, strp("고양시가구협동조합, 경기도고양시 일산가구협동조합"))
	// 주관사(host) is empty on this page → nil, not "".
	gotWant(t, "Host", pe.Host, nil)
	gotWant(t, "HomepageURL", pe.HomepageURL, strp("http://고양가구박람회.com"))
	gotWant(t, "SummaryText", pe.SummaryText, strp("한 공간에서 브랜드별 가격·품질·디자인·서비스를 직접 비교·선택할 수 있어 합리적 소비 환경 조성 프리미엄 가구 전시를 통해 소비자에게 고품질 제품을 직접 체험할 수 있는 기회 제공 국내외 가구 트렌드와 신제품을 한자리에서 만나볼 수 있는 종합 정보 플랫폼 구현 가정·사무·주방·인테리어 등 다양한 품목의 전시로 폭넓은 볼거리와 정보 제공"))

	if pe.RetrievedAt == "" {
		t.Errorf("RetrievedAt is empty, want an RFC3339 timestamp")
	}

	if want := "2026고양가구박람회 고양시가구협동조합, 경기도고양시 일산가구협동조합 한 공간에서 브랜드별 가격·품질·디자인·서비스를 직접 비교·선택할 수 있어 합리적 소비 환경 조성 프리미엄 가구 전시를 통해 소비자에게 고품질 제품을 직접 체험할 수 있는 기회 제공 국내외 가구 트렌드와 신제품을 한자리에서 만나볼 수 있는 종합 정보 플랫폼 구현 가정·사무·주방·인테리어 등 다양한 품목의 전시로 폭넓은 볼거리와 정보 제공"; pe.ClassifyText != want {
		t.Errorf("ClassifyText = %q, want %q", pe.ClassifyText, want)
	}
}

// A single-date period ("YYYY-MM-DD" with no "~") yields equal start/end raw.
func TestParseSingleDayPeriod(t *testing.T) {
	html := `<h3>단일행사</h3><ul class="view-info-list">` +
		`<li><div class="title">기 간</div><div class="info">2026-07-01</div></li>` +
		`</ul>`
	pe, err := New().Parse(context.Background(), &fetch.Result{
		URL:  detailBase + "?seq=99",
		Body: []byte(html),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	gotWant(t, "StartRaw", pe.StartRaw, strp("2026-07-01"))
	gotWant(t, "EndRaw", pe.EndRaw, strp("2026-07-01"))
}

// A page missing the period/place/organizer rows leaves those fields nil
// (absent), never fabricated. Name still comes from <h3>.
func TestParseMissingFields(t *testing.T) {
	html := `<h3>정보없는행사</h3><ul class="view-info-list"></ul>`
	pe, err := New().Parse(context.Background(), &fetch.Result{
		URL:  detailBase + "?seq=1",
		Body: []byte(html),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pe.Name != "정보없는행사" {
		t.Errorf("Name = %q, want 정보없는행사", pe.Name)
	}
	gotWant(t, "StartRaw", pe.StartRaw, nil)
	gotWant(t, "EndRaw", pe.EndRaw, nil)
	gotWant(t, "Organizer", pe.Organizer, nil)
	gotWant(t, "Hall", pe.Hall, nil)
	gotWant(t, "HomepageURL", pe.HomepageURL, nil)
	// EventID still derives from the seq in the URL.
	if pe.EventID != "kintex-1" {
		t.Errorf("EventID = %q, want kintex-1", pe.EventID)
	}
}

package akei

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

var detailWrIDRe = regexp.MustCompile(`wr_id=(\d+)`)

// dateRangeRe matches the 기간 cell's "YYYY-MM-DD ~ YYYY-MM-DD" shape.
var dateRangeRe = regexp.MustCompile(`(20\d{2}-\d{2}-\d{2})\s*~\s*(20\d{2}-\d{2}-\d{2})`)

// Parse reads the structured 국내전시일정 detail table. Fields are copied raw;
// the 전화번호/이메일 rows are deliberately never read (data policy: no
// contact details in the store).
func (s *Source) Parse(_ context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	m := detailWrIDRe.FindStringSubmatch(raw.URL)
	if m == nil {
		return nil, fmt.Errorf("akei parse: no wr_id in URL %q", raw.URL)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw.Body))
	if err != nil {
		return nil, fmt.Errorf("akei parse: %w", err)
	}

	fields := map[string]string{}
	doc.Find(".bbs_schedule_view table tr").Each(func(_ int, tr *goquery.Selection) {
		th := strings.Join(strings.Fields(tr.Find("th").Text()), "")
		td := strings.TrimSpace(tr.Find("td").Text())
		if th != "" && td != "" {
			fields[th] = td
		}
	})

	name := fields["전시회명(한글)"]
	if name == "" {
		return nil, fmt.Errorf("akei parse: empty 전시회명 at %s", raw.URL)
	}

	ev := &sources.ParsedEvent{
		SourceID:    sourceID,
		EventID:     sourceID + "-" + m[1],
		URL:         raw.URL,
		Name:        name,
		RetrievedAt: time.Now().UTC().Format(time.RFC3339),
		// AKEI aggregates every venue's schedule; it is not the venue's own
		// site, so label provenance the way showala does.
		SourceType: ptr("aggregator"),
	}
	if v := fields["전시회명(영문)"]; v != "" {
		ev.NameEn = ptr(v)
	}
	if v := fields["주최"]; v != "" {
		ev.Organizer = ptr(v)
	}
	if v := fields["장소"]; v != "" {
		ev.VenueName = ptr(v)
	}
	if v := fields["기간"]; v != "" {
		if dm := dateRangeRe.FindStringSubmatch(v); dm != nil {
			ev.StartRaw = ptr(dm[1])
			ev.EndRaw = ptr(dm[2])
		} else {
			ev.StartRaw = ptr(v) // let normalize judge the raw text
		}
	}
	if v := fields["홈페이지"]; v != "" {
		ev.HomepageURL = ptr(v)
	}

	ev.ClassifyText = strings.TrimSpace(strings.Join([]string{
		name, strFrom(ev.NameEn), fields["전시분야"], fields["세부품목"], strFrom(ev.Organizer),
	}, " "))
	return ev, nil
}

func ptr(s string) *string { return &s }

func strFrom(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

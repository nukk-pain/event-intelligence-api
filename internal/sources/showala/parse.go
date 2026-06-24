package showala

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// periodSep splits a 전시기간 cell ("2026-11-04 ~ 2026-11-07") on the tilde. A
// cell without a tilde is treated as a single-day event (start == end).
const periodSep = "~"

// wsRe collapses any run of whitespace to a single space.
var wsRe = regexp.MustCompile(`\s+`)

// idxRe captures the numeric idx from a /ex/ex_detail.php?idx=<n> href (the
// durable per-event identifier the portal assigns).
var idxRe = regexp.MustCompile(`[?&]idx=(\d+)`)

// Detail-page 개최장소 / 세부장소 labels (li.where carries both; disambiguated by
// the p.tit label).
const (
	labelVenue = "개최장소"
	labelHall  = "세부장소"
)

// Parse turns one fetched SHOWALA ex_detail.php?idx= page into a ParsedEvent of
// RAW strings. It is deterministic and side-effect free: dates are split but not
// validated and absent fields stay nil. Date/taxonomy/missing_fields handling
// belongs to the normalize stage. Scope (KINTEX-only) is enforced upstream in
// Discover, so Parse does not re-filter by venue.
func (s *Source) Parse(_ context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	doc, err := fetch.ParseHTML(string(raw.Body))
	if err != nil {
		return nil, err
	}

	pe := &sources.ParsedEvent{
		SourceID:    sourceID,
		EventID:     eventIDFromURL(raw.URL),
		URL:         raw.URL,
		RetrievedAt: retrievedAt(),
		// SHOWALA is an exhibition aggregator, not the venue's own site: label its
		// provenance honestly so sources[].type reflects that (the API enum already
		// has "aggregator"). When a venue-native row also exists, dedup hides this
		// one anyway; when it doesn't (e.g. RoboWorld), the aggregator IS the source.
		SourceType: ptr("aggregator"),
		Publisher:  ptr("SHOWALA"),
	}

	// Titles: ba_info carries the Korean name in li.kor_tit and the English name
	// in li.eng_tit (each as the li's direct text).
	pe.Name = squish(doc.Find("li.kor_tit").First().Text())
	pe.NameKo = nilIfEmpty(pe.Name)
	pe.NameEn = nilIfEmpty(squish(doc.Find("li.eng_tit").First().Text()))

	// 전시기간: li.date > p.des, "start ~ end".
	start, end := splitPeriod(squish(doc.Find("li.date p.des").First().Text()))
	pe.StartRaw = nilIfEmpty(start)
	pe.EndRaw = nilIfEmpty(end)

	// 개최장소 / 세부장소 both render as li.where; disambiguate by the p.tit label.
	doc.Find("li.where").Each(func(_ int, li *goquery.Selection) {
		label := squish(li.Find("p.tit").First().Text())
		value := squish(li.Find("p.des").First().Text())
		switch label {
		case labelVenue:
			pe.VenueName = nilIfEmpty(value)
		case labelHall:
			pe.Hall = nilIfEmpty(value)
		}
	})

	// 주최 (li.opener) / 주관 (li.opener2). The label cells are malformed in the
	// source (opened <p>, closed </dt>), so target p.des by the li class — never by
	// label-text equality.
	pe.Organizer = nilIfEmpty(squish(doc.Find("li.opener p.des").First().Text()))
	pe.Host = nilIfEmpty(squish(doc.Find("li.opener2 p.des").First().Text()))

	// 홈페이지: li.homp > a[href].
	if href, ok := doc.Find("li.homp a[href]").First().Attr("href"); ok {
		pe.HomepageURL = nilIfEmpty(strings.TrimSpace(href))
	}

	// Short factual summary: 전시분야 (exhibition field, e.g. "종합기계류"). Kept short
	// and factual — never the full 전시품목 blob — per the copyright constraint.
	if cat := squish(doc.Find("li.ex_cate p.des").First().Text()); cat != "" {
		pe.SummaryText = ptr(cat)
	}

	pe.ClassifyText = strings.Join(nonEmpty(
		pe.Name,
		derefOr(pe.Organizer, ""),
		squish(doc.Find("li.work_cate p.des").First().Text()), // 산업분야
		squish(doc.Find("li.ex_cate p.des").First().Text()),   // 전시분야
	), " ")

	return pe, nil
}

// eventIDFromURL builds the durable "showala-<idx>" id from the detail URL's idx
// query param, matching the Ref ids Discover assigns. Falls back to the raw URL
// only if idx is absent (so the id is never silently empty).
func eventIDFromURL(rawURL string) string {
	if idx := idxFromHref(rawURL); idx != "" {
		return sourceID + "-" + idx
	}
	return sourceID + "-" + rawURL
}

// idxFromHref extracts the idx query value from an ex_detail href/URL.
func idxFromHref(href string) string {
	if m := idxRe.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	// Fall back to a real query parse for unusual encodings.
	if u, err := url.Parse(href); err == nil {
		return u.Query().Get("idx")
	}
	return ""
}

// retrievedAt stamps the fetch time in RFC3339 (informational provenance,
// excluded from content_hash).
func retrievedAt() string { return time.Now().UTC().Format(time.RFC3339) }

// splitPeriod splits a 전시기간 cell on the tilde into raw start/end. A cell with
// no tilde is a single-day event: end mirrors start.
func splitPeriod(period string) (start, end string) {
	if i := strings.Index(period, periodSep); i >= 0 {
		return strings.TrimSpace(period[:i]), strings.TrimSpace(period[i+len(periodSep):])
	}
	period = strings.TrimSpace(period)
	return period, period
}

// squish trims and collapses internal whitespace to single spaces.
func squish(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// nilIfEmpty returns a *string, or nil when the squished value is empty.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptr(s string) *string { return &s }

func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

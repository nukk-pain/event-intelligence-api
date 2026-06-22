package kintex

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

// venueName is the fixed display name for every KINTEX detail page; the page
// itself does not repeat it (the 장소 row carries only the hall).
const venueName = "KINTEX"

// venueCity is KINTEX's fixed location (Goyang). The detail page never states
// the city, so the adapter supplies it deterministically.
const venueCity = "고양"

// periodSep splits a 기간 cell ("2026-06-18 ~ 2026-06-21") on the tilde. A cell
// without a tilde is treated as a single-day event (start == end).
const periodSep = "~"

// wsRe collapses any run of whitespace (incl. the literal newlines/tabs the
// KINTEX template emits) to a single space.
var wsRe = regexp.MustCompile(`\s+`)

// labelKeys maps a normalized view-info-list label (spaces stripped, e.g.
// "기 간" -> "기간") to the ParsedEvent slot it feeds.
const (
	labelPeriod    = "기간" // start ~ end
	labelOrganizer = "주최"
	labelPlace     = "장소" // hall text
)

// Parse turns one fetched KINTEX view.do?seq= page into a ParsedEvent of RAW
// strings. It is deterministic and side-effect free: dates are split but not
// validated, labels are matched by their space-stripped text, and absent fields
// stay nil (never fabricated). Date/taxonomy/missing_fields handling belongs to
// the normalize stage.
func (s *Source) Parse(_ context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	doc, err := fetch.ParseHTML(string(raw.Body))
	if err != nil {
		return nil, err
	}

	pe := &sources.ParsedEvent{
		SourceID:    sourceID,
		EventID:     eventIDFromURL(raw.URL),
		URL:         raw.URL,
		RetrievedAt: retrievedAt(raw),
	}

	pe.Name = squish(doc.Find("h3").First().Text())
	pe.VenueName = ptr(venueName)
	pe.City = ptr(venueCity)

	// view-info-list: 기간 / 주최 / 장소 rows.
	doc.Find("ul.view-info-list > li").Each(func(_ int, li *goquery.Selection) {
		label := strings.ReplaceAll(squish(li.Find(".title").Text()), " ", "")
		info := li.Find(".info").First()
		switch label {
		case labelPeriod:
			start, end := splitPeriod(squish(info.Text()))
			pe.StartRaw = nilIfEmpty(start)
			pe.EndRaw = nilIfEmpty(end)
		case labelOrganizer:
			pe.Organizer = nilIfEmpty(squish(info.Text()))
		case labelPlace:
			// Drop the "위치안내" location button so only hall text remains.
			place := info.Clone()
			place.Find(".btn-table-info").Remove()
			pe.Hall = nilIfEmpty(squish(place.Text()))
		}
	})

	// read-table: 주관사(host) and 홈페이지(homepage link).
	details := map[string]string{}
	doc.Find("div.read-table th[scope=row]").Each(func(_ int, th *goquery.Selection) {
		td := th.Next()
		label := squish(th.Text())
		value := squish(td.Text())
		switch label {
		case "주관사":
			pe.Host = nilIfEmpty(value)
		case "홈페이지":
			if href, ok := td.Find("a[href]").First().Attr("href"); ok {
				pe.HomepageURL = nilIfEmpty(strings.TrimSpace(href))
			}
		case "행사내용", "행사목적", "행사품목":
			if value != "" {
				details[label] = value
			}
		}
	})
	if v := summaryText(details); v != "" {
		pe.SummaryText = ptr(v)
	}

	pe.ClassifyText = strings.Join(nonEmpty(pe.Name, derefOr(pe.Organizer, ""), summaryText(details)), " ")

	return pe, nil
}

// eventIDFromURL builds the durable "kintex-<seq>" id from the detail URL's seq
// query param, matching the Ref ids Discover assigns. Falls back to the raw URL
// only if seq is absent (so the id is never silently empty).
func eventIDFromURL(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if seq := u.Query().Get("seq"); seq != "" {
			return sourceID + "-" + seq
		}
	}
	return sourceID + "-" + rawURL
}

// retrievedAt returns the fetch timestamp. fetch.Result carries no timestamp, so
// the adapter stamps now() in RFC3339; the value is informational provenance and
// excluded from content_hash.
func retrievedAt(_ *fetch.Result) string {
	return time.Now().UTC().Format(time.RFC3339)
}

// splitPeriod splits a 기간 cell on the tilde into raw start/end. A cell with no
// tilde is a single-day event: end mirrors start.
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

// nilIfEmpty returns a *string, or nil when the (already-squished) value is
// empty so "absent" is distinguishable from "present but empty".
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ptr returns a *string for a known-non-empty constant.
func ptr(s string) *string { return &s }

// derefOr returns *p or def when p is nil.
func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func summaryText(details map[string]string) string {
	for _, label := range []string{"행사내용", "행사목적", "행사품목"} {
		if v := details[label]; v != "" {
			return v
		}
	}
	return ""
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

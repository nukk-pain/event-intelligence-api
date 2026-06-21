package coex

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// Static venue facts for COEX. The venue page is always COEX in Seoul; only the
// hall varies per event, so VenueName/City are adapter constants and Hall is
// scraped.
const (
	venueName = "코엑스"
	venueCity = "서울"
)

// Parse turns one fetched COEX exhibition detail page into a sources.ParsedEvent.
// It extracts ONLY raw scraped strings — date parsing, taxonomy mapping and
// missing_fields are the normalize stage's job (see ParsedEvent doc). Dates are
// kept verbatim (e.g. "2026.10.06"). The COEX detail markup uses stable, mostly
// unique class hooks:
//
//	h2.SingleTitle                       -> primary name
//	p.EventDetailBoxHeader-date          -> "<start> - <end>" raw period
//	.EventPlaceItem .EventPlaceItemTitle -> hall ("Hall" + letter)
//	.EventDetailBoxBody-item             -> labelled info rows (주최/주관/담당자/...)
//	a.EventDetailThumbLink-link.url      -> official homepage link
//
// Parse is deterministic and side-effect free. EventID is derived from the page
// URL slug exactly as Discover assigns it, so a Parse on a fetched detail page
// re-creates the same durable id without needing the discovering Ref.
func (s *Source) Parse(ctx context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("coex: nil fetch result")
	}
	if raw.StatusCode != 200 {
		return nil, fmt.Errorf("coex: parse: unexpected status %d for %s", raw.StatusCode, raw.URL)
	}

	doc, err := fetch.ParseHTML(string(raw.Body))
	if err != nil {
		return nil, fmt.Errorf("coex: parse html %s: %w", raw.URL, err)
	}

	name := firstText(doc, "h2.SingleTitle")
	if name == "" {
		// og:title is the deterministic fallback for the display name.
		name = strings.TrimSpace(doc.Find(`meta[property="og:title"]`).AttrOr("content", ""))
	}
	if name == "" {
		return nil, fmt.Errorf("coex: parse: no event name in %s", raw.URL)
	}

	pe := &sources.ParsedEvent{
		SourceID:  s.ID(),
		URL:       raw.URL,
		Name:      name,
		VenueName: ptr(venueName),
		City:      ptr(venueCity),
	}

	// Durable id from the URL slug (matches Discover's "coex-<slug>").
	if slug, serr := slugFromURL(raw.URL); serr == nil && slug != "" {
		pe.EventID = "coex-" + slug
	}

	// Period: "2026.10.06 - 2026.10.08" -> StartRaw / EndRaw, kept verbatim.
	if start, end, ok := splitPeriod(firstText(doc, "p.EventDetailBoxHeader-date")); ok {
		if start != "" {
			pe.StartRaw = ptr(start)
		}
		if end != "" {
			pe.EndRaw = ptr(end)
		}
	}

	// Hall: the 관람 장소 block renders "<label> <value>" pairs, e.g. "Hall" + "C".
	if hall := scrapeHall(doc); hall != "" {
		pe.Hall = ptr(hall)
	}

	// Labelled info rows (주최 / 주관 / 담당자 / 입장료 / ...).
	info := scrapeInfoRows(doc)
	if v := info["주최"]; v != "" {
		pe.Organizer = ptr(v)
	}
	if v := info["주관"]; v != "" {
		pe.Host = ptr(v)
	}

	// Homepage: the official-site link carries the "url" class hook.
	if href := strings.TrimSpace(doc.Find("a.EventDetailThumbLink-link.url").First().AttrOr("href", "")); href != "" {
		pe.HomepageURL = ptr(href)
	}

	// ClassifyText feeds the source-agnostic keyword classifier: title + organizer
	// + host + exhibit-items so categories can be inferred downstream.
	pe.ClassifyText = buildClassifyText(name, info)

	return pe, nil
}

// splitPeriod splits a "<start> - <end>" raw period string into its two halves,
// trimmed. A single date (no separator) is treated as start==end. ok is false
// when the input has no date text at all.
func splitPeriod(s string) (start, end string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	// The COEX separator is " - " (spaced hyphen). Fall back to a bare hyphen.
	parts := strings.SplitN(s, " - ", 2)
	if len(parts) == 1 {
		parts = strings.SplitN(s, "-", 2)
	}
	start = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		end = strings.TrimSpace(parts[1])
	} else {
		end = start
	}
	return start, end, true
}

// scrapeHall reads the 관람 장소 EventPlaceItem and joins its label/value pieces
// ("Hall" + "C" -> "Hall C"). Empty when no place block is present.
func scrapeHall(doc *goquery.Document) string {
	var halls []string
	doc.Find(".EventPlaceItem .EventPlaceItemTitle-item").Each(func(_ int, sel *goquery.Selection) {
		var parts []string
		sel.Find(".EventPlaceItemTitle-txt, .EventPlaceItemTitle-tit").Each(func(_ int, p *goquery.Selection) {
			if t := strings.TrimSpace(p.Text()); t != "" {
				parts = append(parts, t)
			}
		})
		if joined := strings.Join(parts, " "); joined != "" {
			halls = append(halls, joined)
		}
	})
	return strings.Join(halls, ", ")
}

// scrapeInfoRows reads the .EventDetailBoxBody-item rows into a label->value map.
// Each row has a .EventDetailBoxBodyTitle (label) and one or more
// .EventDetailBoxBodyText-txt values; multiple value lines are joined with " ".
func scrapeInfoRows(doc *goquery.Document) map[string]string {
	out := make(map[string]string)
	doc.Find(".EventDetailBoxBody-item").Each(func(_ int, sel *goquery.Selection) {
		label := strings.TrimSpace(sel.Find(".EventDetailBoxBodyTitle").First().Text())
		if label == "" {
			return
		}
		var vals []string
		sel.Find(".EventDetailBoxBodyText-txt").Each(func(_ int, v *goquery.Selection) {
			if t := normalizeWS(v.Text()); t != "" {
				vals = append(vals, t)
			}
		})
		if v := strings.Join(vals, " "); v != "" {
			out[label] = v
		}
	})
	return out
}

// buildClassifyText concatenates the title and the most discriminative info rows
// (organizer, host, exhibit items) into one keyword-search string.
func buildClassifyText(name string, info map[string]string) string {
	parts := []string{name}
	for _, label := range []string{"주최", "주관", "전시품목", "행사 소개"} {
		if v := info[label]; v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

// firstText returns the trimmed text of the first node matching sel, or "".
func firstText(doc *goquery.Document, sel string) string {
	return strings.TrimSpace(doc.Find(sel).First().Text())
}

// normalizeWS collapses runs of whitespace (incl. the literal newlines goquery
// emits for <br/>) into single spaces and trims the ends.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ptr returns a pointer to s. Used to build the nullable *string fields.
func ptr(s string) *string { return &s }

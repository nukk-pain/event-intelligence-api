package showala

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// kstZone is the listing's calendar zone; "today" for the upcoming/past pivot is
// computed in KST so a row that starts today is never wrongly judged past.
var kstZone = time.FixedZone("KST", 9*60*60)

// footerRe matches the AJAX fragment's trailing ":::<next>:::<total>" pagination
// token (next page number, total page count).
var footerRe = regexp.MustCompile(`:::(\d+):::(\d+)\s*$`)

// kintexRe matches a 개최장소 (venue) string that denotes KINTEX, in either the
// Korean (킨텍스) or Latin (KINTEX, any case) form.
var kintexRe = regexp.MustCompile(`(?i)kintex|킨텍스`)

// listRow is one parsed listing entry. The listing already carries venue and
// dates, so the KINTEX/future scope is decided here without fetching the detail.
type listRow struct {
	idx      string
	venue    string
	startISO string // raw "YYYY-MM-DD" start as shown (already ISO on this portal)
}

// scanState threads the early-stop bookkeeping across listing pages.
type scanState struct {
	today           string // KST today, "YYYY-MM-DD"
	consecutivePast int
}

// Discover walks the SHOWALA 경기 listing (page 1 server-rendered, later pages via
// the AJAX ex_proc.php endpoint that needs a same-origin Referer) and returns one
// Ref per UPCOMING event held at KINTEX.
//
// Cost control (project efficiency mandate): the listing is upcoming-ascending
// for the first ~16 pages, then pivots to a long descending tail of PAST events.
// Discover early-stops at that pivot — detected as a run of pastRunToStop
// CONSECUTIVE past-dated rows so an isolated junk/test row (the portal carries a
// few, e.g. an "online" row dated 2021) does not end discovery prematurely. So a
// normal run reads ~16 pages, not the full ~163, and fetches detail pages only
// for the KINTEX rows it kept.
//
// Like the kintex adapter, a hard fetch failure is returned as an error (never a
// silently-empty discovery) so the circuit breaker can tell a fetch failure apart
// from a genuinely empty listing.
func (s *Source) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	st := &scanState{today: s.now().In(kstZone).Format("2006-01-02")}
	var refs []sources.Ref
	seen := make(map[string]struct{})

	// Page 1: server-rendered listing at listURL.
	res, err := f.Fetch(ctx, s.listURL, fetch.Conditional{})
	if err != nil {
		return nil, err
	}
	if !res.NotModified {
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("showala: list status %d", res.StatusCode)
		}
		htmlPart, _, _ := splitFooter(res.Body) // page 1 has no footer; harmless
		stop, _, err := collectRefs(&refs, seen, htmlPart, st)
		if err != nil {
			return nil, err
		}
		if stop {
			return refs, nil
		}
	}

	// Pages 2..N: AJAX fragments from ex_proc.php (require the listURL as Referer).
	for page := 2; page <= maxListingPages; page++ {
		u, err := s.procPageURL(page)
		if err != nil {
			return nil, err
		}
		res, err := f.Fetch(ctx, u, fetch.Conditional{Referer: s.listURL})
		if err != nil {
			return nil, err
		}
		if res.NotModified {
			continue
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("showala: proc page %d status %d", page, res.StatusCode)
		}
		htmlPart, next, total := splitFooter(res.Body)
		stop, n, err := collectRefs(&refs, seen, htmlPart, st)
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
		// Stop conditions from the pagination footer / empty page (avoid looping
		// past the end of the list).
		if n == 0 {
			break
		}
		if total > 0 && page >= total {
			break
		}
		if next > 0 && next <= page {
			break
		}
	}

	return refs, nil
}

// collectRefs parses one listing fragment's rows and appends a Ref for every
// UPCOMING KINTEX event, in document order, deduping by event id. It returns
// stop=true once a run of pastRunToStop consecutive past-dated rows confirms the
// upcoming→past pivot, and the number of rows parsed (0 => an empty/末尾 page).
func collectRefs(dst *[]sources.Ref, seen map[string]struct{}, body []byte, st *scanState) (stop bool, n int, err error) {
	rows, err := parseListingRows(body)
	if err != nil {
		return false, 0, err
	}
	for _, r := range rows {
		// Rows with no parseable start date can't be placed on the timeline: skip
		// without disturbing the consecutive-past run (don't let them trip or reset
		// the pivot detector).
		if r.startISO == "" {
			continue
		}
		if r.startISO < st.today {
			st.consecutivePast++
			if st.consecutivePast >= pastRunToStop {
				return true, len(rows), nil
			}
			continue
		}
		// An upcoming row: the pivot run is broken.
		st.consecutivePast = 0
		if r.idx == "" || !kintexRe.MatchString(r.venue) {
			continue
		}
		id := sourceID + "-" + r.idx
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		*dst = append(*dst, sources.Ref{
			EventID: id,
			URL:     "https://showala.com/ex/ex_detail.php?idx=" + r.idx,
		})
	}
	return false, len(rows), nil
}

// parseListingRows extracts (idx, venue, start) from every li.ex_item in a
// listing fragment. It never errors on missing/malformed rows — an empty or junk
// page simply yields fewer rows.
func parseListingRows(body []byte) ([]listRow, error) {
	doc, err := fetch.ParseHTML(string(body))
	if err != nil {
		return nil, err
	}
	var rows []listRow
	doc.Find("li.ex_item").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a.ex_tit_a").First()
		href, _ := a.Attr("href")
		idx := idxFromHref(href)

		date := li.Find("div.ex_date").First().Clone()
		date.Find("span").Remove()
		start, _ := splitPeriod(squish(date.Text()))

		place := li.Find("div.ex_place").First().Clone()
		place.Find("span").Remove()
		venue := squish(place.Text())

		rows = append(rows, listRow{idx: idx, venue: venue, startISO: start})
	})
	return rows, nil
}

// splitFooter peels the trailing ":::next:::total" pagination token off an AJAX
// fragment, returning the HTML portion plus the parsed page numbers (0,0 when no
// token is present, e.g. the server-rendered first page).
func splitFooter(body []byte) (html []byte, next, total int) {
	m := footerRe.FindSubmatchIndex(body)
	if m == nil {
		return body, 0, 0
	}
	next, _ = strconv.Atoi(string(body[m[2]:m[3]]))
	total, _ = strconv.Atoi(string(body[m[4]:m[5]]))
	return body[:m[0]], next, total
}

// procPageURL builds the AJAX pagination URL for the given page.
func (s *Source) procPageURL(page int) (string, error) {
	u, err := url.Parse(s.procURL)
	if err != nil {
		return "", fmt.Errorf("showala: parse proc URL: %w", err)
	}
	q := u.Query()
	q.Set("action", "exPagingNew")
	q.Set("page", strconv.Itoa(page))
	q.Set("qstr", procQStr) // url.Values.Encode escapes place[]=1 -> place%5B%5D%3D1
	u.RawQuery = q.Encode()
	return u.String(), nil
}

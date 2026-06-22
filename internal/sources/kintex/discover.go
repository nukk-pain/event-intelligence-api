// Package kintex implements the KINTEX venue Source adapter.
//
// Discovery endpoint (resolved by the 2026-06-21 spike — PREFLIGHT finding I4):
// the canonical listing is GET /web/ko/event/list.do with searchStartDt,
// searchEndDt, and pageIndex. Each event card anchors a JS handler
// href="javascript:fnView('./view.do', <seq>);" carrying the durable numeric
// seq. The clist.do?searchType=… variants return 200 but render rows via AJAX,
// so list.do remains the deterministic, browser-free source.
package kintex

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

const (
	// sourceID is the registry key and the kintex- event-id prefix root.
	sourceID = "kintex"

	// listURL is the canonical event listing (spike: WORKING over plain HTTP).
	listURL = "https://www.kintex.com/web/ko/event/list.do"

	// detailBase is the absolute detail-page base; Discover appends ?seq=<seq>.
	detailBase = "https://www.kintex.com/web/ko/event/view.do"

	lookaheadDays   = 365
	maxListingPages = 50
)

// seqRe matches the listing's fnView('./view.do', <seq>) handler and captures
// the numeric seq. Whitespace around the args is tolerated; a non-numeric or
// empty argument simply does not match (malformed rows are skipped, not fatal).
var seqRe = regexp.MustCompile(`fnView\(\s*'\./view\.do'\s*,\s*(\d+)\s*\)`)
var pagingRe = regexp.MustCompile(`fnPaging\(\s*'(\d+)'\s*\)`)

// Source is the KINTEX adapter.
type Source struct {
	listURL string
	now     func() time.Time
}

// Option configures a Source.
type Option func(*Source)

// WithListURL overrides the listing URL Discover fetches. Intended for tests
// (httptest server); production uses the live list.do constant.
func WithListURL(u string) Option {
	return func(s *Source) { s.listURL = u }
}

// WithClock overrides the clock used to build KINTEX date-range listing URLs.
// Intended for deterministic discovery tests.
func WithClock(now func() time.Time) Option {
	return func(s *Source) {
		if now != nil {
			s.now = now
		}
	}
}

// New returns a KINTEX Source. By default it targets the live list.do listing.
func New(opts ...Option) *Source {
	s := &Source{listURL: listURL, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ID returns the adapter id ("kintex").
func (s *Source) ID() string { return sourceID }

// Discover fetches the KINTEX listing and returns one Ref per discovered event.
//
// A hard fetch failure must never masquerade as a legitimately empty listing:
// the shared Fetcher returns err==nil for every non-304 2xx/3xx AND every 4xx
// (it hands back the error-page body), so Discover guards res.StatusCode
// explicitly — mirroring the COEX adapter — and a 4xx (e.g. 404/403 from a site
// restructure or WAF block) is reported as an error rather than yielding zero
// refs. This keeps the disappearance policy / circuit breaker able to tell a
// fetch failure apart from a genuinely empty page (AC-10). A 304 Not Modified
// has nothing to re-parse and yields no refs.
func (s *Source) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	var refs []sources.Ref
	seenRefs := make(map[string]struct{})
	seenPages := make(map[int]struct{})
	queue := []int{1}
	seenPages[1] = struct{}{}

	for len(queue) > 0 {
		page := queue[0]
		queue = queue[1:]

		listPageURL, err := s.listPageURL(page)
		if err != nil {
			return nil, err
		}
		res, err := f.Fetch(ctx, listPageURL, fetch.Conditional{})
		if err != nil {
			return nil, err
		}
		if res.NotModified {
			continue
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("kintex: list.do page %d status %d", page, res.StatusCode)
		}

		pageRefs, err := parseListing(res.Body)
		if err != nil {
			return nil, err
		}
		appendRefs(&refs, seenRefs, pageRefs)

		for _, next := range parseListingPageNumbers(res.Body) {
			if next < 1 || next > maxListingPages {
				continue
			}
			if _, ok := seenPages[next]; ok {
				continue
			}
			seenPages[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	return refs, nil
}

// parseListing extracts every unique event seq from a list.do response body and
// builds a Ref per event. It is deterministic, dedupes seqs (preserving first
// occurrence order), and never errors on missing/malformed rows — an empty or
// junk page yields zero refs. Kept separate from Discover so tests can exercise
// it against saved fixtures with no network.
func parseListing(body []byte) ([]sources.Ref, error) {
	matches := seqRe.FindAllSubmatch(body, -1)
	refs := make([]sources.Ref, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seq := string(m[1])
		if _, dup := seen[seq]; dup {
			continue
		}
		seen[seq] = struct{}{}
		refs = append(refs, sources.Ref{
			EventID: sourceID + "-" + seq,
			URL:     detailBase + "?seq=" + seq,
		})
	}
	return refs, nil
}

func appendRefs(dst *[]sources.Ref, seen map[string]struct{}, refs []sources.Ref) {
	for _, ref := range refs {
		if _, dup := seen[ref.EventID]; dup {
			continue
		}
		seen[ref.EventID] = struct{}{}
		*dst = append(*dst, ref)
	}
}

func parseListingPageNumbers(body []byte) []int {
	matches := pagingRe.FindAllSubmatch(body, -1)
	pages := make([]int, 0, len(matches))
	seen := make(map[int]struct{}, len(matches))
	for _, m := range matches {
		n, err := strconv.Atoi(string(m[1]))
		if err != nil {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		pages = append(pages, n)
	}
	sort.Ints(pages)
	return pages
}

func (s *Source) listPageURL(page int) (string, error) {
	u, err := url.Parse(s.listURL)
	if err != nil {
		return "", fmt.Errorf("kintex: parse list URL: %w", err)
	}
	q := u.Query()
	q.Set("pageIndex", strconv.Itoa(page))
	start := s.now().In(time.FixedZone("KST", 9*60*60))
	q.Set("searchStartDt", start.Format("2006-01-02"))
	q.Set("searchEndDt", start.AddDate(0, 0, lookaheadDays).Format("2006-01-02"))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

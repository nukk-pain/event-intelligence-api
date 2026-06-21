// Package kintex implements the KINTEX venue Source adapter.
//
// Discovery endpoint (resolved by the 2026-06-21 spike — PREFLIGHT finding I4):
// the canonical listing is GET /web/ko/event/list.do. Each event card anchors a
// JS handler href="javascript:fnView('./view.do', <seq>);" carrying the durable
// numeric seq. The clist.do?searchType=… variants return 200 but render their
// rows via AJAX, so the initial HTML has zero seqs; the homepage has only a
// promo subset. list.do is therefore the deterministic, browser-free source.
package kintex

import (
	"context"
	"fmt"
	"regexp"

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
)

// seqRe matches the listing's fnView('./view.do', <seq>) handler and captures
// the numeric seq. Whitespace around the args is tolerated; a non-numeric or
// empty argument simply does not match (malformed rows are skipped, not fatal).
var seqRe = regexp.MustCompile(`fnView\(\s*'\./view\.do'\s*,\s*(\d+)\s*\)`)

// Source is the KINTEX adapter.
type Source struct {
	listURL string
}

// Option configures a Source.
type Option func(*Source)

// WithListURL overrides the listing URL Discover fetches. Intended for tests
// (httptest server); production uses the live list.do constant.
func WithListURL(u string) Option {
	return func(s *Source) { s.listURL = u }
}

// New returns a KINTEX Source. By default it targets the live list.do listing.
func New(opts ...Option) *Source {
	s := &Source{listURL: listURL}
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
	res, err := f.Fetch(ctx, s.listURL, fetch.Conditional{})
	if err != nil {
		return nil, err
	}
	if res.NotModified {
		return nil, nil
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("kintex: list.do status %d", res.StatusCode)
	}
	return parseListing(res.Body)
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

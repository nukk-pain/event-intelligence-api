// Package showala implements the SHOWALA (showala.com) exhibition-portal Source
// adapter, scoped to events held at KINTEX.
//
// Why this adapter exists: KINTEX's own calendar (the kintex adapter's list.do)
// only lists events KINTEX has published on its public calendar. Externally
// organized exhibitions that rent KINTEX halls — e.g. "2026 로보월드" / RoboWorld
// (robotworld.or.kr) — are absent from list.do until KINTEX publishes them, yet
// appear on the SHOWALA exhibition portal with full date/venue/homepage data.
// This adapter discovers those events so they are not missed; cross-source dedup
// (store layer) folds any event that ALSO appears under the kintex adapter into a
// single canonical row, preferring the venue-native source.
//
// HTTP-first / static-HTML only (project efficiency mandate): both the listing
// and the detail pages are server-rendered HTML parsed with goquery. The only
// non-static piece is the listing's "더보기" pagination, served by an AJAX
// endpoint (ex_proc.php) that requires a same-origin Referer header — supplied
// via fetch.Conditional.Referer. No headless browser is used.
package showala

import (
	"time"
)

const (
	// sourceID is the registry key and the "showala-" event-id prefix root.
	sourceID = "showala"

	// listURL is the first listing page: the 경기(Gyeonggi) region bucket, which
	// is a SUPERSET of KINTEX (it also holds 수원메쎄/수원컨벤션/일산 등). The adapter
	// narrows to KINTEX by each listing row's 개최장소 (venue) text — there is no
	// venue-exact filter on the portal. This URL is also the Referer the AJAX
	// pagination endpoint requires.
	listURL = "https://showala.com/ex/ex_list.php?place[]=1"

	// procURL is the AJAX pagination endpoint. Discover appends
	// action=exPagingNew, page=N and qstr=place%5B%5D%3D1, and sends the listURL
	// as Referer (the endpoint rejects requests without it).
	procURL = "https://showala.com/ex/ex_proc.php"

	// procQStr is the url-encoded filter echoed back to ex_proc.php (place[]=1).
	procQStr = "place[]=1"

	// maxListingPages caps how deep Discover pages before giving up. The listing
	// is upcoming-ascending for ~16 pages then flips to a long past-events tail;
	// Discover early-stops at the pivot (see discover.go), so this is only a
	// safety backstop against a layout change that never yields the pivot.
	maxListingPages = 25

	// pastRunToStop is how many CONSECUTIVE past-dated rows confirm the
	// upcoming→past pivot. A single isolated past row (the portal carries junk/test
	// rows, e.g. an "online" row dated 2021) must NOT end discovery, so one stray
	// past row is tolerated; a run of this many ends it.
	pastRunToStop = 3
)

// Source is the SHOWALA adapter.
type Source struct {
	listURL string
	procURL string
	now     func() time.Time
}

// Option configures a Source.
type Option func(*Source)

// WithListURL overrides the first listing page URL (also used as the AJAX
// Referer). Intended for tests (httptest server).
func WithListURL(u string) Option {
	return func(s *Source) { s.listURL = u }
}

// WithProcURL overrides the AJAX pagination endpoint URL. Intended for tests.
func WithProcURL(u string) Option {
	return func(s *Source) { s.procURL = u }
}

// WithClock overrides the clock used to decide which listing rows are upcoming
// (the KINTEX/future scope and the early-stop pivot). Intended for deterministic
// discovery tests.
func WithClock(now func() time.Time) Option {
	return func(s *Source) {
		if now != nil {
			s.now = now
		}
	}
}

// New returns a SHOWALA Source. By default it targets the live showala.com
// listing scoped to the 경기 region.
func New(opts ...Option) *Source {
	s := &Source{listURL: listURL, procURL: procURL, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ID returns the adapter id ("showala").
func (s *Source) ID() string { return sourceID }

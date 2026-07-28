// Package akei implements the AKEI (한국전시산업진흥회, www.akei.or.kr) Source
// adapter. AKEI maintains the nationwide domestic exhibition schedule — the
// closest thing Korea has to a single directory of trade fairs — so this one
// adapter reaches every venue in the country without per-venue crawling.
// Promoted from the first automated eventscout discovery packet on 2026-07-28.
package akei

import (
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/sources"
)

const sourceID = "akei"

// DefaultBaseURL is the live origin.
const DefaultBaseURL = "https://www.akei.or.kr"

// Source is the AKEI adapter. It implements sources.Source.
type Source struct {
	baseURL string
	now     func() time.Time
}

// Option configures a Source.
type Option func(*Source)

// WithBaseURL overrides the origin (tests: httptest server).
func WithBaseURL(u string) Option {
	return func(s *Source) { s.baseURL = strings.TrimRight(u, "/") }
}

// WithClock overrides the clock the month window is computed from (tests).
func WithClock(now func() time.Time) Option {
	return func(s *Source) { s.now = now }
}

// New builds the adapter against the live origin unless overridden.
func New(opts ...Option) *Source {
	s := &Source{baseURL: DefaultBaseURL, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ID implements sources.Source.
func (s *Source) ID() string { return sourceID }

var _ sources.Source = (*Source)(nil)

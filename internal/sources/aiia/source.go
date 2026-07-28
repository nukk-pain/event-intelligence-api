// Package aiia implements the AIIA (한국인공지능산업협회, k-ai.or.kr) Source
// adapter. The association publishes AI industry events on a plain PHP notice
// board that mixes event announcements with recruiting and administrative
// posts, so discovery applies a deterministic title keyword filter and only
// event-like posts become Refs. Promoted from an eventscout discovery run on
// 2026-07-28 (see .tasks/260728-scout-promote/candidates-fixture/).
package aiia

import (
	"strings"

	"github.com/smpain/event-intelligence-api/internal/sources"
)

const sourceID = "aiia"

// DefaultBaseURL is the live origin. The www subdomain does not respond, so
// the bare domain is the canonical (and allowlisted) host.
const DefaultBaseURL = "https://k-ai.or.kr"

// boardPath is the 공지사항 board where event announcements are posted.
const boardPath = "/bbs/board.php?tbl=bbs41"

// Source is the AIIA adapter. It implements sources.Source.
type Source struct {
	baseURL string
}

// Option configures a Source.
type Option func(*Source)

// WithBaseURL overrides the origin discovery fetches against. Intended for
// tests (httptest server).
func WithBaseURL(u string) Option {
	return func(s *Source) { s.baseURL = strings.TrimRight(u, "/") }
}

// New builds the adapter with the live origin unless overridden.
func New(opts ...Option) *Source {
	s := &Source{baseURL: DefaultBaseURL}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ID implements sources.Source.
func (s *Source) ID() string { return sourceID }

var _ sources.Source = (*Source)(nil)

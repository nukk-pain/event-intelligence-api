// Package sources defines the Source adapter contract and registry. Concrete
// COEX/KINTEX adapters land in Phase 2; this file is the contract every adapter
// implements plus the single registration point that the pipeline iterates.
package sources

import (
	"context"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

// Ref is a discovered detail reference: a URL to fetch together with the durable
// event id the adapter assigns (e.g. "coex-<slug>", "kintex-<seq>").
type Ref struct {
	EventID string
	URL     string
}

// ParsedEvent holds the RAW fields extracted from a venue detail page, BEFORE
// normalization. Dates, names and venue text are kept exactly as scraped so the
// normalize stage owns all parsing/validation (date invariants, taxonomy,
// missing_fields). Nullable fields use *string so "absent" is distinguishable
// from "present but empty".
type ParsedEvent struct {
	SourceID string // adapter id that produced this ("coex" / "kintex")
	EventID  string // durable id (matches the discovering Ref.EventID)
	URL      string // detail page URL this was parsed from
	Name     string // primary display name as scraped

	NameKo  *string // Korean name, if distinguishable
	NameEn  *string // English name, if present
	Edition *string // e.g. "제5회", "5th"

	StartRaw *string // raw start-date text, unparsed (e.g. "2024.08.24")
	EndRaw   *string // raw end-date text, unparsed

	VenueName *string // venue label as scraped (e.g. "코엑스", "KINTEX")
	Hall      *string // hall/room text, if present
	City      *string // city text, if present

	Organizer *string // 주최
	Host      *string // 주관

	HomepageURL  *string // event homepage link, if present
	SummaryText  *string
	Actions      ActionSignals
	ExtraSources []ParsedSource

	// ClassifyText is title + organizer concatenated, fed to the keyword
	// classifier. Adapters build this so the classify stage stays source-agnostic.
	ClassifyText string

	RetrievedAt string // RFC3339 timestamp of the fetch
}

// ActionSignals are optional second-hop fields parsed from an official event or
// organizer page. Nil booleans mean "unknown"; true/false values mean the
// source page supplied a signal.
type ActionSignals struct {
	CanRegister       *bool
	CanExhibit        *bool
	CanSponsor        *bool
	HasMatchmaking    *bool
	HasStartupProgram *bool

	RegisterURL          *string
	ExhibitURL           *string
	RegistrationDeadline *string
	ExhibitorDeadline    *string
	CostHint             *string
}

// ParsedSource is provenance discovered outside the venue detail page, such as
// an official/organizer homepage used for action-field enrichment.
type ParsedSource struct {
	URL         string
	Type        string
	Publisher   string
	RetrievedAt string
}

// Source is implemented by each venue adapter. Discover lists detail references
// (using the shared, SSRF-guarded Fetcher), and Parse turns one fetched detail
// page into a ParsedEvent. Both are deterministic and side-effect free.
type Source interface {
	ID() string
	Discover(ctx context.Context, f *fetch.Fetcher) ([]Ref, error)
	Parse(ctx context.Context, raw *fetch.Result) (*ParsedEvent, error)
}

// registry is the single registration point for sources. Adding a new venue is
// "implement Source + Register it + one config row" — the pipeline iterates All()
// and never names a concrete adapter.
var registry = map[string]Source{}

// Register adds a source under its ID. A later registration with the same ID
// overwrites the earlier one.
func Register(s Source) { registry[s.ID()] = s }

// Get returns a registered source by id, and whether it was found.
func Get(id string) (Source, bool) {
	s, ok := registry[id]
	return s, ok
}

// All returns the live registry map of every registered source keyed by id.
func All() map[string]Source { return registry }

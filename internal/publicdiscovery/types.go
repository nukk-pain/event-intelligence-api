package publicdiscovery

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

const (
	MaxSeeds             = 6
	MaxDepth             = 2
	MaxProtocolDocuments = 12
	MaxHTMLPages         = 24
	MaxCandidates        = 30
	MaxHTTPAttempts      = 64
	MaxResponseBytes     = 6 << 20
	JobTimeout           = 60 * time.Second
)

var (
	ErrInvalidURL     = errors.New("public discovery: invalid URL")
	ErrURLUserinfo    = errors.New("public discovery: URL userinfo is forbidden")
	ErrInvalidCatalog = errors.New("public discovery: invalid seed catalog")
	ErrInvalidLimits  = errors.New("public discovery: invalid limits")
	ErrQueryRequired  = errors.New("public discovery: query is required")
)

type Seed struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Catalog struct {
	Version string `json:"version"`
	Seeds   []Seed `json:"seeds"`
}

type Limits struct {
	MaxSeeds             int           `json:"max_seeds"`
	MaxDepth             int           `json:"max_depth"`
	MaxProtocolDocuments int           `json:"max_protocol_documents"`
	MaxHTMLPages         int           `json:"max_html_pages"`
	MaxCandidates        int           `json:"max_candidates"`
	MaxHTTPAttempts      int           `json:"max_http_attempts"`
	MaxResponseBytes     int64         `json:"max_response_bytes"`
	Timeout              time.Duration `json:"timeout"`
}

type Protocol string

const (
	ProtocolSeed          Protocol = "seed"
	ProtocolRobotsSitemap Protocol = "robots_sitemap"
	ProtocolSitemapIndex  Protocol = "sitemap_index"
	ProtocolSitemapURLSet Protocol = "sitemap_urlset"
	ProtocolAtom          Protocol = "atom"
	ProtocolRSS           Protocol = "rss"
	ProtocolJSONFeed      Protocol = "json_feed"
	ProtocolHTMLLink      Protocol = "html_link"
	ProtocolHTMLFeed      Protocol = "html_feed"
	ProtocolHTTPLink      Protocol = "http_link"
)

type Provenance struct {
	Provider       string    `json:"provider"`
	CatalogVersion string    `json:"catalog_version"`
	SeedName       string    `json:"seed_name"`
	SeedURL        string    `json:"seed_url"`
	Protocol       Protocol  `json:"protocol"`
	DiscoveredFrom string    `json:"discovered_from,omitempty"`
	RawURL         string    `json:"raw_url"`
	CanonicalURL   string    `json:"canonical_url"`
	FetchedAt      time.Time `json:"fetched_at"`
	Depth          int       `json:"depth"`
}

type Candidate struct {
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	Snippet    string     `json:"snippet,omitempty"`
	Provenance Provenance `json:"provenance"`
}

type TruncationReason string

const (
	TruncationDepthLimit            TruncationReason = "depth_limit"
	TruncationProtocolDocumentLimit TruncationReason = "protocol_document_limit"
	TruncationHTMLPageLimit         TruncationReason = "html_page_limit"
	TruncationCandidateLimit        TruncationReason = "candidate_limit"
	TruncationHTTPAttemptLimit      TruncationReason = "http_attempt_limit"
	TruncationResponseBodyLimit     TruncationReason = "response_body_limit"
	TruncationTimeLimit             TruncationReason = "time_limit"
	TruncationContextCanceled       TruncationReason = "context_canceled"
)

type Usage struct {
	Seeds              int   `json:"seeds"`
	ProtocolDocuments  int   `json:"protocol_documents"`
	HTMLPages          int   `json:"html_pages"`
	Candidates         int   `json:"candidates"`
	HTTPAttempts       int64 `json:"http_attempts"`
	ResponseBytes      int64 `json:"response_bytes"`
	MalformedDocuments int   `json:"malformed_documents"`
	SkippedDocuments   int   `json:"skipped_documents"`
}

type BudgetState struct {
	Limits            Limits             `json:"limits"`
	Usage             Usage              `json:"usage"`
	SeedOutcomes      SeedOutcomes       `json:"seed_outcomes"`
	Truncated         bool               `json:"truncated"`
	TruncationReasons []TruncationReason `json:"truncation_reasons,omitempty"`
}

// SeedOutcome classifies what became of one catalog seed page. Seed pages are
// fetched before any sitemap child and are the only crawl output guaranteed a
// title, so an empty offer set is only explainable once each seed is accounted
// for.
type SeedOutcome string

const (
	SeedOutcomeCandidate          SeedOutcome = "candidate"
	SeedOutcomeRobotsDisallowed   SeedOutcome = "robots_disallowed"
	SeedOutcomeRobotsUnavailable  SeedOutcome = "robots_unavailable"
	SeedOutcomeHTTPStatus         SeedOutcome = "http_status"
	SeedOutcomeBodyTooLarge       SeedOutcome = "body_too_large"
	SeedOutcomeUnsupportedContent SeedOutcome = "unsupported_content"
	SeedOutcomeTransportError     SeedOutcome = "transport_error"
	SeedOutcomeDuplicate          SeedOutcome = "duplicate"
	SeedOutcomeCandidateCap       SeedOutcome = "candidate_cap"
	SeedOutcomeNotAttempted       SeedOutcome = "not_attempted"
)

// SeedOutcomes is a fixed-key, count-only tally with exactly one entry per
// enqueued seed. It holds no URL, host, or document content.
type SeedOutcomes struct {
	Candidate          int `json:"candidate"`
	RobotsDisallowed   int `json:"robots_disallowed"`
	RobotsUnavailable  int `json:"robots_unavailable"`
	HTTPStatus         int `json:"http_status"`
	BodyTooLarge       int `json:"body_too_large"`
	UnsupportedContent int `json:"unsupported_content"`
	TransportError     int `json:"transport_error"`
	Duplicate          int `json:"duplicate"`
	CandidateCap       int `json:"candidate_cap"`
	NotAttempted       int `json:"not_attempted"`
}

// Total returns the number of accounted seeds.
func (outcomes SeedOutcomes) Total() int {
	return outcomes.Candidate + outcomes.RobotsDisallowed + outcomes.RobotsUnavailable + outcomes.HTTPStatus +
		outcomes.BodyTooLarge + outcomes.UnsupportedContent + outcomes.TransportError +
		outcomes.Duplicate + outcomes.CandidateCap + outcomes.NotAttempted
}

func (outcomes *SeedOutcomes) add(outcome SeedOutcome) {
	switch outcome {
	case SeedOutcomeCandidate:
		outcomes.Candidate++
	case SeedOutcomeRobotsDisallowed:
		outcomes.RobotsDisallowed++
	case SeedOutcomeRobotsUnavailable:
		outcomes.RobotsUnavailable++
	case SeedOutcomeHTTPStatus:
		outcomes.HTTPStatus++
	case SeedOutcomeBodyTooLarge:
		outcomes.BodyTooLarge++
	case SeedOutcomeUnsupportedContent:
		outcomes.UnsupportedContent++
	case SeedOutcomeTransportError:
		outcomes.TransportError++
	case SeedOutcomeDuplicate:
		outcomes.Duplicate++
	case SeedOutcomeCandidateCap:
		outcomes.CandidateCap++
	case SeedOutcomeNotAttempted:
		outcomes.NotAttempted++
	}
}

type Result struct {
	Candidates []Candidate `json:"candidates"`
	Budget     BudgetState `json:"budget"`
}

type documentFetcher interface {
	Fetch(ctx context.Context, rawURL string, conditional fetch.Conditional) (*fetch.Result, error)
}

type runtimeDeps struct {
	fetcher              documentFetcher
	budget               *fetch.CrawlBudget
	now                  func() time.Time
	allowLocalCandidates bool
}

type Provider struct {
	mu                   sync.Mutex
	catalog              Catalog
	limits               Limits
	fetcher              documentFetcher
	budget               *fetch.CrawlBudget
	now                  func() time.Time
	allowLocalCandidates bool
	crawled              bool
	immutableCandidates  []Candidate
	immutableBudget      BudgetState
}

func DefaultLimits() Limits {
	return Limits{
		MaxSeeds: MaxSeeds, MaxDepth: MaxDepth,
		MaxProtocolDocuments: MaxProtocolDocuments, MaxHTMLPages: MaxHTMLPages,
		MaxCandidates: MaxCandidates, MaxHTTPAttempts: MaxHTTPAttempts,
		MaxResponseBytes: MaxResponseBytes, Timeout: JobTimeout,
	}
}

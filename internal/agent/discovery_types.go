package agent

import (
	"context"
	"errors"
	"time"
)

const (
	// hardMaxSearches caps how many searches one request may execute. It carries
	// the retired MaxRounds budget forward unchanged: the action loop has no
	// rounds, so the search count is what the old round budget actually bounded.
	hardMaxSearches = 2
	// hardMaxDiscoveryTurns caps the total number of model turns across every
	// action (search, accept, done, and one-shot malformed corrections).
	hardMaxDiscoveryTurns = 8
	// hardMaxDiscoveryOpens caps how many candidate pages one request may fetch
	// through a PageOpener. Each open is a live second-hop fetch inside the
	// request deadline, so the ceiling stays low.
	hardMaxDiscoveryOpens = 3
	// hardMaxDiscoveryModelCalls caps how many model calls one request may spend.
	// It is 8, not the 4 that covered a search/accept/done run, because every open
	// spends a turn of its own: a run that searches, opens the pages it needs, and
	// then judges cannot reach the accept turn on a budget of 4.
	hardMaxDiscoveryModelCalls       = 8
	hardMaxDiscoveryCompletionTokens = 4000
	hardMaxDiscoveryCandidates       = 30
	hardMaxDiscoveryGoalRunes        = 800
	hardMaxDiscoveryPerCallTimeout   = time.Minute
)

var (
	ErrInvalidDiscoveryProfile = errors.New("agent discovery: invalid profile")
	ErrInvalidDiscoverOptions  = errors.New("agent discovery: invalid options")
	ErrInvalidCandidateURL     = errors.New("agent discovery: invalid candidate URL")
	ErrMalformedModelResponse  = errors.New("agent discovery: malformed model response")
	ErrInvalidModelUsage       = errors.New("agent discovery: invalid model usage")
	errModelBudgetReached      = errors.New("agent discovery: model budget reached")
)

// DiscoveryProfileName identifies a server-selected discovery purpose.
type DiscoveryProfileName string

const (
	DiscoveryProfileEvents        DiscoveryProfileName = "events"
	DiscoveryProfileTraining      DiscoveryProfileName = "training"
	DiscoveryProfileConferences   DiscoveryProfileName = "conferences"
	DiscoveryProfileAnnouncements DiscoveryProfileName = "announcements"
)

// PastDatePolicy controls whether dated candidates before the reference day remain admissible.
type PastDatePolicy string

const (
	PastDatesRejected PastDatePolicy = "reject"
	PastDatesAllowed  PastDatePolicy = "allow"
)

// CandidateRequirements declares which offered fields a profile requires.
type CandidateRequirements struct {
	Title    bool `json:"title"`
	Date     bool `json:"date"`
	Location bool `json:"location"`
	Source   bool `json:"source"`
}

// DiscoveryProfile is the immutable request-local rubric used for proposal and judging.
type DiscoveryProfile struct {
	Name                  DiscoveryProfileName
	Purpose               string
	QueryTemplates        []string
	AdmissibilityRubric   string
	OutputLabel           string
	Requirements          CandidateRequirements
	AllowedURLPatterns    []string
	AllowedDomainPatterns []string
	PastDatePolicy        PastDatePolicy
	LocaleHints           []string
	LanguageHints         []string
}

// SourceProvenance records the public traversal path that yielded a candidate.
type SourceProvenance struct {
	Provider  string `json:"provider,omitempty"`
	SeedURL   string `json:"seed_url,omitempty"`
	ParentURL string `json:"parent_url,omitempty"`
	Relation  string `json:"relation,omitempty"`
	RawURL    string `json:"raw_url,omitempty"`
}

// SearchResult is one hit returned through the stable SearchTool contract.
type SearchResult struct {
	Title      string            `json:"title"`
	URL        string            `json:"url"`
	Snippet    string            `json:"snippet"`
	Date       string            `json:"date,omitempty"`
	Location   string            `json:"location,omitempty"`
	Locale     string            `json:"locale,omitempty"`
	Language   string            `json:"language,omitempty"`
	Provenance *SourceProvenance `json:"provenance,omitempty"`
}

// SearchTool supplies candidates without coupling discovery to a search provider.
type SearchTool interface {
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// DiscoveredSource is a profile-valid offered candidate selected by the model.
type DiscoveredSource struct {
	URL        string            `json:"url"`
	Title      string            `json:"title"`
	Reason     string            `json:"reason"`
	Date       string            `json:"date,omitempty"`
	Location   string            `json:"location,omitempty"`
	Provenance *SourceProvenance `json:"provenance,omitempty"`
}

// TruncationReason identifies a hard request budget that stopped or reduced discovery.
type TruncationReason string

const (
	TruncationRoundLimit            TruncationReason = "round_limit"
	TruncationModelCallBudget       TruncationReason = "model_call_budget"
	TruncationCompletionTokenBudget TruncationReason = "completion_token_budget"
	TruncationTotalCandidateCap     TruncationReason = "total_candidate_cap"
	TruncationPerSourceCandidateCap TruncationReason = "per_source_candidate_cap"
	TruncationGoalLength            TruncationReason = "goal_length"
)

// DiscoverOptions holds server-owned profile and budget controls for one request.
type DiscoverOptions struct {
	Profile DiscoveryProfile
	// MaxSearches bounds how many search actions the model may execute.
	MaxSearches int
	// MaxTurns bounds the total model turns spent across every action.
	MaxTurns int
	// MaxOpens bounds how many candidate pages the model may open. Zero (the
	// default) disables the open action, as does a request with no PageOpener.
	MaxOpens               int
	MaxModelCalls          int
	MaxCompletionTokens    int
	MaxTokensPerCall       int
	MaxCandidates          int
	MaxCandidatesPerSource int
	GoalMaxRunes           int
	PerCallTimeout         time.Duration
	ReferenceTime          time.Time
}

// DiscoverRequest groups one request's backend, goal, provider, and bounded options.
type DiscoverRequest struct {
	Backend Backend
	Goal    string
	Tool    SearchTool
	// Opener, when set, lets the model open an offered candidate page to fill in
	// fields the search provider left empty. A nil Opener disables the open
	// action entirely — the model is never offered it and nothing is fetched.
	Opener  PageOpener
	Options DiscoverOptions
}

// DiscoveryMetadata reports profile, candidate, budget, and truncation state.
type DiscoveryMetadata struct {
	Profile           DiscoveryProfileName `json:"profile"`
	CandidatesOffered int                  `json:"candidates_offered"`
	Budget            DiscoveryBudget      `json:"budget"`
	Truncated         bool                 `json:"truncated"`
	TruncationReasons []TruncationReason   `json:"truncation_reasons,omitempty"`
}

// DiscoveryBudget reports normalized hard limits and reserved completion tokens.
type DiscoveryBudget struct {
	MaxSearches              int `json:"max_searches"`
	MaxTurns                 int `json:"max_turns"`
	MaxModelCalls            int `json:"max_model_calls"`
	MaxCompletionTokens      int `json:"max_completion_tokens"`
	CompletionTokensReserved int `json:"completion_tokens_reserved"`
	MaxCandidates            int `json:"max_candidates"`
	MaxCandidatesPerSource   int `json:"max_candidates_per_source"`
}

// DiscoveryResult contains accepted sources plus additive trace and budget metadata.
type DiscoveryResult struct {
	Sources    []DiscoveredSource `json:"sources"`
	Trace      Trace              `json:"trace"`
	YieldTrace YieldTrace         `json:"yield_trace"`
	Metadata   DiscoveryMetadata  `json:"metadata"`
}

func (result *DiscoveryResult) addTruncation(reason TruncationReason) {
	for _, existing := range result.Metadata.TruncationReasons {
		if existing == reason {
			return
		}
	}
	result.Metadata.Truncated = true
	result.Metadata.TruncationReasons = append(result.Metadata.TruncationReasons, reason)
}

// DefaultDiscoverOptions returns the hard-bounded defaults for a server-selected profile.
func DefaultDiscoverOptions(profile DiscoveryProfile) DiscoverOptions {
	return DiscoverOptions{
		Profile: profile, MaxSearches: hardMaxSearches, MaxTurns: hardMaxDiscoveryTurns,
		MaxOpens: 0, MaxModelCalls: hardMaxDiscoveryModelCalls,
		// MaxTokensPerCall is a per-call reservation against the completion
		// budget, so effective turns = MaxCompletionTokens / MaxTokensPerCall.
		// Action responses are one small JSON object (~120 tokens observed with
		// reasoning_effort=minimal); 500 keeps all 8 turns reachable without
		// raising the completion-token cost ceiling.
		MaxCompletionTokens: hardMaxDiscoveryCompletionTokens, MaxTokensPerCall: 500,
		MaxCandidates: hardMaxDiscoveryCandidates, MaxCandidatesPerSource: 5, GoalMaxRunes: hardMaxDiscoveryGoalRunes,
		PerCallTimeout: 20 * time.Second, ReferenceTime: time.Now().UTC(),
	}
}

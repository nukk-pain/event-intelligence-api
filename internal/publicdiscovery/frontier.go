package publicdiscovery

import (
	"net/url"
	"strings"
	"time"
)

type frontierItem struct {
	rawURL       string
	canonicalURL string
	seed         Seed
	parentURL    string
	relation     Protocol
	depth        int
	kind         documentKind
}

type candidateDiscovery struct {
	rawURL           string
	provenanceRawURL string
	title            string
	seed             Seed
	parentURL        string
	protocol         Protocol
	depth            int
	fetchedAt        time.Time
}

type crawlState struct {
	provider       *Provider
	budget         BudgetState
	candidates     []Candidate
	seenCandidates map[string]struct{}
	seenDocuments  map[string]struct{}
	validatedURLs  map[string]struct{}
	protocolQueue  []frontierItem
	htmlQueue      []frontierItem
	seedsTotal     int
	stopped        bool
}

func newCrawlState(provider *Provider) *crawlState {
	return &crawlState{
		provider:       provider,
		budget:         BudgetState{Limits: provider.limits, TruncationReasons: []TruncationReason{}},
		candidates:     []Candidate{},
		seenCandidates: map[string]struct{}{},
		seenDocuments:  map[string]struct{}{},
		validatedURLs:  map[string]struct{}{},
	}
}

func (state *crawlState) addReason(reason TruncationReason) {
	for _, existing := range state.budget.TruncationReasons {
		if existing == reason {
			return
		}
	}
	state.budget.Truncated = true
	state.budget.TruncationReasons = append(state.budget.TruncationReasons, reason)
}

func (state *crawlState) syncFetchUsage() {
	usage := state.provider.budget.Usage()
	state.budget.Usage.HTTPAttempts = usage.TransportAttempts
	state.budget.Usage.ResponseBytes = usage.AggregateBodyBytes
}

func (state *crawlState) enqueue(item frontierItem) {
	if state.stopped {
		return
	}
	if item.depth > state.provider.limits.MaxDepth {
		state.addReason(TruncationDepthLimit)
		return
	}
	canonical, err := CanonicalizeURL(item.rawURL)
	if err != nil || !candidateURLAllowed(canonical, state.provider.allowLocalCandidates) {
		return
	}
	if _, seen := state.seenDocuments[canonical]; seen {
		return
	}
	state.seenDocuments[canonical] = struct{}{}
	item.canonicalURL = canonical
	switch item.kind {
	case documentSitemap, documentFeed:
		state.protocolQueue = append(state.protocolQueue, item)
	case documentHTML, documentAuto:
		state.htmlQueue = append(state.htmlQueue, item)
	}
}

// addCandidate reports whether the candidate was stored and, when it was not,
// which bounded category explains the rejection.
func (state *crawlState) addCandidate(discovery candidateDiscovery) (bool, SeedOutcome) {
	if state.stopped || discovery.depth > state.provider.limits.MaxDepth {
		if discovery.depth > state.provider.limits.MaxDepth {
			state.addReason(TruncationDepthLimit)
		}
		return false, SeedOutcomeNotAttempted
	}
	canonical, err := CanonicalizeURL(discovery.rawURL)
	if err != nil || !candidateURLAllowed(canonical, state.provider.allowLocalCandidates) {
		return false, SeedOutcomeUnsupportedContent
	}
	if _, seen := state.seenCandidates[canonical]; seen {
		return false, SeedOutcomeDuplicate
	}
	if len(state.candidates) >= state.candidateLimitFor(discovery.protocol) {
		state.addReason(TruncationCandidateLimit)
		return false, SeedOutcomeCandidateCap
	}
	state.seenCandidates[canonical] = struct{}{}
	provenanceRawURL := discovery.provenanceRawURL
	if provenanceRawURL == "" {
		provenanceRawURL = discovery.rawURL
	}
	state.candidates = append(state.candidates, Candidate{
		Title: discovery.title,
		URL:   canonical,
		Provenance: Provenance{
			Provider: "public", CatalogVersion: state.provider.catalog.Version,
			SeedName: discovery.seed.Name, SeedURL: discovery.seed.URL,
			Protocol: discovery.protocol, DiscoveredFrom: discovery.parentURL,
			RawURL: provenanceRawURL, CanonicalURL: canonical,
			FetchedAt: discovery.fetchedAt, Depth: discovery.depth,
		},
	})
	return true, ""
}

// candidateLimitFor holds a slot open for each seed the crawl has enqueued but
// not yet accounted for. Sitemap children are discovered before seed pages are
// fetched, so without this they fill the whole list and the seed page — the
// only candidate guaranteed a title — is rejected for want of a slot, leaving
// the model nothing it can judge. The total cap is never raised: seeds spend
// the very slots this withholds.
func (state *crawlState) candidateLimitFor(protocol Protocol) int {
	limit := state.provider.limits.MaxCandidates
	if protocol == ProtocolSeed {
		return limit
	}
	return max(limit-state.pendingSeeds(), 0)
}

// pendingSeeds counts enqueued seeds that have not yet reached an outcome.
func (state *crawlState) pendingSeeds() int {
	return max(state.seedsTotal-state.budget.SeedOutcomes.Total(), 0)
}

// backfillCandidateTitle gives a stored candidate the title of the page the
// crawler already fetched. Sitemaps and bare links carry no title, so without
// this those candidates stay untitled and a profile that requires a title
// discards every one of them before the model can judge it. A title the
// discovery protocol already supplied is authoritative and is never replaced.
func (state *crawlState) backfillCandidateTitle(canonicalURL, title string) {
	title = strings.TrimSpace(title)
	if title == "" || canonicalURL == "" {
		return
	}
	for index := range state.candidates {
		if state.candidates[index].URL != canonicalURL {
			continue
		}
		if state.candidates[index].Title == "" {
			state.candidates[index].Title = title
		}
		return
	}
}

func (state *crawlState) validatedCandidates() []Candidate {
	result := make([]Candidate, 0, len(state.candidates))
	for _, candidate := range state.candidates {
		if _, validated := state.validatedURLs[candidate.URL]; validated {
			result = append(result, candidate)
		}
	}
	return result
}

func (state *crawlState) addAndQueue(discovery candidateDiscovery, kind documentKind) {
	if stored, _ := state.addCandidate(discovery); !stored {
		return
	}
	state.enqueue(frontierItem{
		rawURL: discovery.rawURL, seed: discovery.seed, parentURL: discovery.parentURL,
		relation: discovery.protocol, depth: discovery.depth, kind: kind,
	})
}

func originDocumentURL(seedURL, path string) string {
	parsed, err := url.Parse(seedURL)
	if err != nil {
		return ""
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

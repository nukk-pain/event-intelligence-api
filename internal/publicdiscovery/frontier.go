package publicdiscovery

import (
	"net/url"
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

func (state *crawlState) addCandidate(discovery candidateDiscovery) bool {
	if state.stopped || discovery.depth > state.provider.limits.MaxDepth {
		if discovery.depth > state.provider.limits.MaxDepth {
			state.addReason(TruncationDepthLimit)
		}
		return false
	}
	canonical, err := CanonicalizeURL(discovery.rawURL)
	if err != nil || !candidateURLAllowed(canonical, state.provider.allowLocalCandidates) {
		return false
	}
	if _, seen := state.seenCandidates[canonical]; seen {
		return false
	}
	if len(state.candidates) >= state.provider.limits.MaxCandidates {
		state.addReason(TruncationCandidateLimit)
		return false
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
	return true
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
	if !state.addCandidate(discovery) {
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

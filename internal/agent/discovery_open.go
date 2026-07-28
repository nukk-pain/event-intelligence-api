package agent

import "context"

// maxPendingCandidates bounds how many metadata-short candidates one request may
// hold for a possible open. It is a small fixed number because pending rows are
// rendered into every open-enabled prompt and only MaxOpens of them can ever be
// acted on.
const maxPendingCandidates = 10

// OpenedPage is the bounded view of one candidate page returned by a PageOpener.
// Every field is untrusted crawled text; the discovery loop redacts and bounds
// it before it reaches a candidate or a prompt.
type OpenedPage struct {
	Title    string
	Snippet  string
	Date     string
	Location string
}

// PageOpener fetches one already-offered candidate URL so the discovery loop can
// fill in fields the search provider left empty. Implementations own their own
// SSRF, robots, and size policy; the loop only ever passes a URL it offered.
type PageOpener interface {
	Open(ctx context.Context, url string) (OpenedPage, error)
}

// openRecoverablePrefilterReason reports whether opening the page could plausibly
// rescue a dropped candidate. Only a metadata shortfall qualifies: a URL the
// profile rejects, or an event that already happened, is not made admissible by
// reading the page, and holding it would hand the model a fetch target the
// prefilter already refused.
func openRecoverablePrefilterReason(reason PrefilterReason) bool {
	switch reason {
	case PrefilterMissingTitle, PrefilterMissingDate, PrefilterMissingLocation:
		return true
	default:
		return false
	}
}

// applyOpenedPage fills only the empty fields of a candidate. An opened page
// supplements what the search provider found; it never overrides it, so a
// hostile page cannot rewrite a title or date the provider already established.
func applyOpenedPage(result *SearchResult, page OpenedPage) {
	if result.Title == "" {
		result.Title = boundedRedactedText(page.Title, maxCandidateTitleRunes)
	}
	if result.Snippet == "" {
		result.Snippet = boundedRedactedText(page.Snippet, maxCandidateSnippetRunes)
	}
	if result.Date == "" {
		result.Date = boundedRedactedText(page.Date, maxCandidateMetadataRunes)
	}
	if result.Location == "" {
		result.Location = boundedRedactedText(page.Location, maxCandidateMetadataRunes)
	}
}

// missingRequiredFields names the profile-required fields a candidate still
// lacks. The names are server-owned strings, never crawled text, so rendering
// them into a prompt cannot smuggle instructions.
func missingRequiredFields(profile DiscoveryProfile, result SearchResult) []string {
	missing := make([]string, 0, 3)
	if profile.Requirements.Title && result.Title == "" {
		missing = append(missing, "title")
	}
	if _, hasDate := parseCandidateDate(result.Date); profile.Requirements.Date && !hasDate {
		missing = append(missing, "date")
	}
	if profile.Requirements.Location && result.Location == "" {
		missing = append(missing, "location")
	}
	return missing
}

// openAllowed reports whether the open action may be offered this turn: an
// opener must exist and the open budget must have room left. When it is false
// the action, its budget line, and the pending list all vanish from the prompt.
func (session *discoverySession) openAllowed() bool {
	return session.opener != nil && session.opens < session.options.MaxOpens
}

// rememberPending holds a metadata-short candidate so a later open can rescue
// it. The candidate is still dropped — it is not in the table and the model
// cannot accept it — and the prefilter counters recorded that drop already.
func (session *discoverySession) rememberPending(candidate offeredCandidate, reason PrefilterReason) {
	if session.opener == nil || session.options.MaxOpens <= 0 {
		return
	}
	if !openRecoverablePrefilterReason(reason) || candidate.canonicalURL == "" {
		return
	}
	if len(session.pending) >= maxPendingCandidates {
		return
	}
	if _, offered := session.offered[candidate.canonicalURL]; offered {
		return
	}
	if _, found := session.found[candidate.canonicalURL]; found {
		return
	}
	if session.pendingIndex(candidate.canonicalURL) >= 0 {
		return
	}
	session.pending = append(session.pending, candidate)
}

func (session *discoverySession) pendingIndex(canonicalURL string) int {
	for index := range session.pending {
		if session.pending[index].canonicalURL == canonicalURL {
			return index
		}
	}
	return -1
}

func (session *discoverySession) dropPending(canonicalURL string) {
	if index := session.pendingIndex(canonicalURL); index >= 0 {
		session.pending = append(session.pending[:index], session.pending[index+1:]...)
	}
}

// openTarget resolves the URL an open action named to the candidate it refers
// to, in the table or in pending. A URL that is neither is an invented fetch
// target and resolves to nil, so nothing is ever fetched for it.
func (session *discoverySession) openTarget(url string) (target *offeredCandidate, pending bool) {
	for index := range session.candidates {
		if session.candidates[index].canonicalURL == url {
			return &session.candidates[index], false
		}
	}
	for index := range session.pending {
		if session.pending[index].canonicalURL == url {
			return &session.pending[index], true
		}
	}
	return nil, false
}

// promotePending moves a rescued candidate from pending into the judgeable
// table once it satisfies the profile. It reuses the same counters as the search
// path so a promotion is indistinguishable from a candidate that arrived already
// complete. A candidate that still falls short simply stays pending, keeping the
// fields the open did fill in.
func (session *discoverySession) promotePending(canonicalURL string) {
	index := session.pendingIndex(canonicalURL)
	if index < 0 {
		return
	}
	candidate := session.pending[index]
	if _, ok := candidateMeetsRequirements(candidate.result, session.options); !ok {
		return
	}
	if session.result.Metadata.CandidatesOffered >= session.options.MaxCandidates {
		session.result.addTruncation(TruncationTotalCandidateCap)
		return
	}
	if session.perSourceCandidateUse[candidate.sourceKey] >= session.options.MaxCandidatesPerSource {
		session.result.addTruncation(TruncationPerSourceCandidateCap)
		return
	}
	session.pending = append(session.pending[:index], session.pending[index+1:]...)
	session.offered[candidate.canonicalURL] = struct{}{}
	session.perSourceCandidateUse[candidate.sourceKey]++
	session.result.Metadata.CandidatesOffered++
	session.result.YieldTrace.Offered++
	session.candidates = append(session.candidates, candidate)
}

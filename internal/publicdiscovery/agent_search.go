package publicdiscovery

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

var ErrInvalidAgentSearchTool = errors.New("public discovery: invalid agent search tool")

// PublicYieldSnapshot is the count-only crawler state that public discovery
// orchestration may merge with its agent-side yield trace. It intentionally
// excludes candidates and all crawl URLs.
type PublicYieldSnapshot struct {
	ValidatedCandidates int                `json:"validated_candidates"`
	Truncated           bool               `json:"truncated"`
	TruncationReasons   []TruncationReason `json:"truncation_reasons,omitempty"`
}

// AgentSearchTool adapts one request-local public crawl to agent.SearchTool.
// The provider is intentionally owned by the adapter so callers can create a
// fresh frontier for each discovery request.
type AgentSearchTool struct {
	provider *Provider

	mu   sync.RWMutex
	last Result
}

var _ agent.SearchTool = (*AgentSearchTool)(nil)

// NewAgentSearchTool creates a public search tool with the server-owned seed
// catalog and fixed public-crawl budgets.
func NewAgentSearchTool() (*AgentSearchTool, error) {
	provider, err := New()
	if err != nil {
		return nil, err
	}
	return NewAgentSearchToolWithProvider(provider)
}

// NewAgentSearchToolWithProvider wraps an already-created provider. It is
// useful for deterministic local fixtures while keeping production creation
// on NewAgentSearchTool.
func NewAgentSearchToolWithProvider(provider *Provider) (*AgentSearchTool, error) {
	if provider == nil {
		return nil, ErrInvalidAgentSearchTool
	}
	return &AgentSearchTool{provider: provider}, nil
}

// Search advances this adapter's frontier once and returns safe agent hits.
// Repeated calls rank the same immutable candidate set without another crawl.
func (tool *AgentSearchTool) Search(ctx context.Context, query string) ([]agent.SearchResult, error) {
	if tool == nil || tool.provider == nil {
		return nil, ErrInvalidAgentSearchTool
	}
	result, err := tool.provider.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	tool.mu.Lock()
	tool.last = cloneResult(result)
	tool.mu.Unlock()

	hits := make([]agent.SearchResult, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		hits = append(hits, toAgentSearchResult(candidate))
	}
	return hits, nil
}

// Snapshot returns the most recent public-crawl result, including budget and
// truncation metadata, without exposing mutable provider state.
func (tool *AgentSearchTool) Snapshot() Result {
	if tool == nil {
		return Result{}
	}
	tool.mu.RLock()
	defer tool.mu.RUnlock()
	return cloneResult(tool.last)
}

// YieldSnapshot returns the bounded, request-local crawler metadata needed by
// public discovery orchestration without exposing candidates or crawl URLs.
func (tool *AgentSearchTool) YieldSnapshot() PublicYieldSnapshot {
	snapshot := tool.Snapshot()
	return PublicYieldSnapshot{
		ValidatedCandidates: snapshot.Budget.Usage.Candidates,
		Truncated:           snapshot.Budget.Truncated,
		TruncationReasons:   append([]TruncationReason(nil), snapshot.Budget.TruncationReasons...),
	}
}

// RestoreProvenance replaces validated adapter provenance with the original
// public record for final JSON output. Agent validation sees absolute URLs;
// callers still receive the literal href/feed value in raw_url.
func (tool *AgentSearchTool) RestoreProvenance(sources []agent.DiscoveredSource) []agent.DiscoveredSource {
	if tool == nil {
		return sources
	}
	snapshot := tool.Snapshot()
	byURL := make(map[string]Provenance, len(snapshot.Candidates))
	for _, candidate := range snapshot.Candidates {
		byURL[candidate.URL] = candidate.Provenance
	}
	output := make([]agent.DiscoveredSource, len(sources))
	copy(output, sources)
	for index := range output {
		provenance, ok := byURL[output[index].URL]
		if !ok {
			continue
		}
		output[index].Provenance = &agent.SourceProvenance{
			Provider:  provenance.Provider,
			SeedURL:   provenance.SeedURL,
			ParentURL: provenance.DiscoveredFrom,
			Relation:  string(provenance.Protocol),
			RawURL:    provenance.RawURL,
		}
	}
	return output
}

func toAgentSearchResult(candidate Candidate) agent.SearchResult {
	provenance := candidate.Provenance
	return agent.SearchResult{
		Title:      candidate.Title,
		URL:        candidate.URL,
		Snippet:    candidate.Snippet,
		Provenance: toAgentProvenance(provenance, candidate.URL),
	}
}

func toAgentProvenance(provenance Provenance, candidateURL string) *agent.SourceProvenance {
	if provenance.Provider == "" && provenance.SeedURL == "" && provenance.DiscoveredFrom == "" && provenance.RawURL == "" {
		return nil
	}
	rawURL := provenance.RawURL
	if rawURL != "" {
		resolved, ok := resolveAgentRawURL(rawURL, provenance.DiscoveredFrom)
		if !ok || resolved != candidateURL {
			rawURL = ""
		} else {
			rawURL = resolved
		}
	}
	return &agent.SourceProvenance{
		Provider: provenance.Provider, SeedURL: provenance.SeedURL,
		ParentURL: provenance.DiscoveredFrom, Relation: string(provenance.Protocol), RawURL: rawURL,
	}
}

func resolveAgentRawURL(rawURL, parentURL string) (string, bool) {
	if canonical, err := CanonicalizeURL(rawURL); err == nil {
		return canonical, true
	}
	if strings.TrimSpace(parentURL) == "" {
		return "", false
	}
	resolved, ok := resolveURLReference(parentURL, rawURL)
	if !ok {
		return "", false
	}
	canonical, err := CanonicalizeURL(resolved)
	return canonical, err == nil
}

func cloneResult(result Result) Result {
	result.Candidates = append([]Candidate(nil), result.Candidates...)
	result.Budget.TruncationReasons = append([]TruncationReason(nil), result.Budget.TruncationReasons...)
	return result
}

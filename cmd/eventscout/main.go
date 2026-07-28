// Command eventscout runs the autonomous source-discovery loop: given a goal,
// the model proposes search queries, a search tool runs them, and the model
// judges which results are real event-listing sources worth crawling — deciding
// the next action itself each round.
//
// Search is pluggable (agent.SearchTool). This CLI uses a fixture-backed search
// so the loop runs offline today; a real web-search tool drops in later without
// changing the agent logic. Backend selection matches cmd/eventagent.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/publicdiscovery"
)

func main() {
	goal := flag.String("goal", "국내 AI·로봇·바이오·의료기기 산업 행사를 목록으로 제공하는 소스 찾기", "discovery goal")
	searchFile := flag.String("search", "cmd/eventscout/fixtures/search.json", "fixture search dataset")
	searchProvider := flag.String("search-provider", "public", "search provider (public, fixture or tavily)")
	backendName := flag.String("backend", "", "backend name (default: first configured)")
	rounds := flag.Int("rounds", 3, "max discovery searches")
	opens := flag.Int("opens", maxCLIOpens, "max candidate pages the model may open (0-3, public provider only)")
	maxTokens := flag.Int("max-tokens", 500, "max completion tokens reserved per model call (effective turns = total budget / this)")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	promoteDir := flag.String("promote", "", "write review artifacts (seed candidates, catalog snippet, new allowlist hosts) for accepted sources into this directory")
	flag.Parse()

	searchCfg := searchConfig{
		Provider: *searchProvider, FixturePath: *searchFile,
		Client: &http.Client{Timeout: *timeout},
	}
	if *searchProvider == "tavily" {
		searchCfg.TavilyKey = os.Getenv("EVENTSINTEL_TAVILY_API_KEY")
	}
	tool, err := newSearchTool(searchCfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	opener, err := newPageOpener(*searchProvider)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	maxOpens := clampOpens(*opens)
	be, err := pickBackend(*backendName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("source discovery — backend=%s(%s) search=%s rounds=%d opens=%d\ngoal: %s\n\n",
		be.Name, be.Model, *searchProvider, *rounds, maxOpens, *goal)
	start := time.Now()
	profile, err := agent.NamedDiscoveryProfile(agent.DiscoveryProfileEvents)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	options := agent.DefaultDiscoverOptions(profile)
	options.MaxSearches = *rounds
	// A provider with no opener ignores MaxOpens: the loop never offers the open
	// action without one, so nothing is fetched for it.
	options.MaxOpens = maxOpens
	options.MaxTokensPerCall = *maxTokens
	options.PerCallTimeout = *timeout
	result, err := agent.DiscoverWithOptions(context.Background(), agent.DiscoverRequest{
		Backend: be, Goal: *goal, Tool: tool, Opener: opener, Options: options,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
	}
	out := marshalDiscovery(tool, result, err == nil)
	fmt.Printf("discovered %d source(s):\n%s\n", len(result.Sources), string(out))
	fmt.Printf("\n%d model call(s), %d in + %d out tokens, %dms total\n",
		result.Trace.Calls, result.Trace.Usage.PromptTokens, result.Trace.Usage.CompletionTokens, time.Since(start).Milliseconds())
	if err != nil {
		os.Exit(1)
	}
	if *promoteDir != "" {
		wrote, perr := writePromotionFiles(*promoteDir, result.Sources, fetch.ProductionAllowedHosts)
		if perr != nil {
			fmt.Fprintln(os.Stderr, "promote:", perr)
			os.Exit(1)
		}
		if !wrote {
			fmt.Fprintln(os.Stderr, "promote: no accepted sources, nothing written")
		}
	}
}

type publicDiscoveryOutput struct {
	Sources         []agent.DiscoveredSource            `json:"sources"`
	Provider        string                              `json:"provider"`
	PublicDiscovery publicdiscovery.PublicYieldSnapshot `json:"public_discovery"`
	YieldTrace      *agent.YieldTrace                   `json:"yield_trace,omitempty"`
}

func marshalDiscovery(tool agent.SearchTool, result agent.DiscoveryResult, completed bool) []byte {
	if publicTool, ok := tool.(*publicdiscovery.AgentSearchTool); ok {
		snapshot := publicTool.YieldSnapshot()
		var yieldTrace *agent.YieldTrace
		if completed {
			merged := result.YieldTrace.WithCrawlerValidated(snapshot.ValidatedCandidates)
			yieldTrace = &merged
		}
		output := publicDiscoveryOutput{
			Sources: publicTool.RestoreProvenance(result.Sources), Provider: "public",
			PublicDiscovery: snapshot, YieldTrace: yieldTrace,
		}
		encoded, err := json.MarshalIndent(output, "", "  ")
		if err == nil {
			return encoded
		}
	}
	encoded, err := json.MarshalIndent(result.Sources, "", "  ")
	if err != nil {
		return []byte("[]")
	}
	return encoded
}

// maxCLIOpens is the CLI ceiling on page opens. It matches the agent's own hard
// cap, which clamps anything larger anyway.
const maxCLIOpens = 3

func clampOpens(opens int) int {
	return min(max(opens, 0), maxCLIOpens)
}

// newPageOpener returns a live page opener for the public provider only. The
// fixture provider is offline by design and the Tavily provider owns no fetch
// policy of its own, so neither gets a second hop; a nil opener disables the
// open action entirely.
func newPageOpener(provider string) (agent.PageOpener, error) {
	if provider != "" && provider != "public" {
		return nil, nil
	}
	opener, err := publicdiscovery.NewAgentPageOpener()
	if err != nil {
		return nil, fmt.Errorf("public page opener: %w", err)
	}
	return opener, nil
}

type searchConfig struct {
	Provider    string
	FixturePath string
	TavilyKey   string
	Client      *http.Client
}

func newSearchTool(cfg searchConfig) (agent.SearchTool, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = "public"
	}
	switch provider {
	case "fixture":
		tool, err := loadFixtureSearch(cfg.FixturePath)
		if err != nil {
			return nil, fmt.Errorf("load search fixture: %w", err)
		}
		return tool, nil
	case "public":
		return publicdiscovery.NewAgentSearchTool()
	case "tavily":
		return agent.NewTavilySearch(cfg.TavilyKey, cfg.Client)
	default:
		return nil, fmt.Errorf("unknown search provider %q (want public, fixture, or tavily)", cfg.Provider)
	}
}

func pickBackend(name string) (agent.Backend, error) {
	return selectBackend(name, agent.LoadBackends())
}

func selectBackend(name string, backends []agent.Backend) (agent.Backend, error) {
	if len(backends) == 0 {
		return agent.Backend{}, fmt.Errorf("no backends configured (set EVENTSINTEL_LOCAL_BASE_URL or EVENTSINTEL_SOLAR_API_KEY)")
	}
	if name == "" {
		return backends[0], nil
	}
	for _, b := range backends {
		if b.Name == name {
			if name == "solar" && strings.TrimSpace(b.APIKey) == "" {
				return agent.Backend{}, fmt.Errorf("backend %q not configured (set EVENTSINTEL_SOLAR_API_KEY)", name)
			}
			return b, nil
		}
	}
	return agent.Backend{}, fmt.Errorf("backend %q not configured", name)
}

// fixtureSearch matches a query against keyword groups and returns their canned
// results — a stand-in for a real search index/API for offline testing.
type fixtureSearch struct {
	Groups []struct {
		Keywords []string             `json:"keywords"`
		Results  []agent.SearchResult `json:"results"`
	} `json:"groups"`
}

func loadFixtureSearch(path string) (*fixtureSearch, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fs fixtureSearch
	if err := json.Unmarshal(raw, &fs); err != nil {
		return nil, err
	}
	return &fs, nil
}

func (f *fixtureSearch) Search(_ context.Context, query string) ([]agent.SearchResult, error) {
	q := strings.ToLower(query)
	var out []agent.SearchResult
	seen := map[string]bool{}
	for _, g := range f.Groups {
		matched := false
		for _, kw := range g.Keywords {
			if strings.Contains(q, strings.ToLower(kw)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, r := range g.Results {
			if !seen[r.URL] {
				seen[r.URL] = true
				out = append(out, r)
			}
		}
	}
	return out, nil
}

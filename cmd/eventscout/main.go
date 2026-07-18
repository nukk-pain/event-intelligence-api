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
)

func main() {
	goal := flag.String("goal", "국내 AI·로봇·바이오·의료기기 산업 행사를 목록으로 제공하는 소스 찾기", "discovery goal")
	searchFile := flag.String("search", "cmd/eventscout/fixtures/search.json", "fixture search dataset")
	searchProvider := flag.String("search-provider", "fixture", "search provider (fixture or tavily)")
	backendName := flag.String("backend", "", "backend name (default: first configured)")
	rounds := flag.Int("rounds", 3, "max discovery rounds")
	maxTokens := flag.Int("max-tokens", 3000, "max completion tokens")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	flag.Parse()

	tool, err := newSearchTool(searchConfig{
		Provider:    *searchProvider,
		FixturePath: *searchFile,
		TavilyKey:   os.Getenv("EVENTSINTEL_TAVILY_API_KEY"),
		Client:      &http.Client{Timeout: *timeout},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	be, err := pickBackend(*backendName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("source discovery — backend=%s(%s) search=%s rounds=%d\ngoal: %s\n\n", be.Name, be.Model, *searchProvider, *rounds, *goal)
	start := time.Now()
	sources, tr, err := agent.Discover(context.Background(), be, *goal, tool, *rounds, *maxTokens, *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
	}
	out, _ := json.MarshalIndent(sources, "", "  ")
	fmt.Printf("discovered %d source(s):\n%s\n", len(sources), string(out))
	fmt.Printf("\n%d model call(s), %d in + %d out tokens, %dms total\n",
		tr.Calls, tr.Usage.PromptTokens, tr.Usage.CompletionTokens, time.Since(start).Milliseconds())
	if err != nil {
		os.Exit(1)
	}
}

type searchConfig struct {
	Provider    string
	FixturePath string
	TavilyKey   string
	Client      *http.Client
}

func newSearchTool(cfg searchConfig) (agent.SearchTool, error) {
	switch cfg.Provider {
	case "fixture":
		tool, err := loadFixtureSearch(cfg.FixturePath)
		if err != nil {
			return nil, fmt.Errorf("load search fixture: %w", err)
		}
		return tool, nil
	case "tavily":
		return agent.NewTavilySearch(cfg.TavilyKey, cfg.Client)
	default:
		return nil, fmt.Errorf("unknown search provider %q", cfg.Provider)
	}
}

func pickBackend(name string) (agent.Backend, error) {
	backends := agent.LoadBackends()
	if len(backends) == 0 {
		return agent.Backend{}, fmt.Errorf("no backends configured (set EVENTSINTEL_LOCAL_BASE_URL or EVENTSINTEL_SOLAR_API_KEY)")
	}
	if name == "" {
		return backends[0], nil
	}
	for _, b := range backends {
		if b.Name == name {
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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/publicdiscovery"
)

func TestLoadFixtureSearch_returns_matching_unique_results(t *testing.T) {
	// Given
	path := filepath.Join("fixtures", "search.json")
	fixture, err := loadFixtureSearch(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	// When
	results, err := fixture.Search(context.Background(), "AI 행사 컨퍼런스")

	// Then
	if err != nil {
		t.Fatalf("search fixture: %v", err)
	}
	if got, want := len(results), 4; got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	if got, want := results[0].URL, "https://www.coex.co.kr/exhibitions"; got != want {
		t.Fatalf("first result URL = %q, want %q", got, want)
	}
}

func TestNewSearchTool_selects_fixture_and_tavily(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	tests := []struct {
		name        string
		provider    string
		key         string
		wantFixture bool
	}{
		{name: "fixture", provider: "fixture", wantFixture: true},
		{name: "tavily", provider: "tavily", key: "key"},
		{name: "public", provider: "public"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			tool, err := newSearchTool(searchConfig{
				Provider: tt.provider, FixturePath: filepath.Join("fixtures", "search.json"),
				TavilyKey: tt.key, Client: client,
			})

			// Then
			if err != nil {
				t.Fatalf("select search tool: %v", err)
			}
			if tt.wantFixture {
				if _, ok := tool.(*fixtureSearch); !ok {
					t.Fatalf("tool type = %T, want *fixtureSearch", tool)
				}
				return
			}
			if tt.provider == "public" {
				if _, ok := tool.(*publicdiscovery.AgentSearchTool); !ok {
					t.Fatalf("tool type = %T, want *publicdiscovery.AgentSearchTool", tool)
				}
				return
			}
			if _, ok := tool.(*agent.TavilySearch); !ok {
				t.Fatalf("tool type = %T, want *agent.TavilySearch", tool)
			}
		})
	}
}

func TestNewSearchTool_public_does_not_require_or_read_tavily_key(t *testing.T) {
	// Given
	config := searchConfig{Provider: "public", TavilyKey: "not-a-tavily-key"}

	// When
	tool, err := newSearchTool(config)

	// Then
	if err != nil {
		t.Fatalf("select public search tool: %v", err)
	}
	if _, ok := tool.(*publicdiscovery.AgentSearchTool); !ok {
		t.Fatalf("tool type = %T, want public adapter", tool)
	}
}

func TestNewSearchTool_defaults_to_public_provider(t *testing.T) {
	// Given
	config := searchConfig{}

	// When
	tool, err := newSearchTool(config)

	// Then
	if err != nil {
		t.Fatalf("select default search tool: %v", err)
	}
	if _, ok := tool.(*publicdiscovery.AgentSearchTool); !ok {
		t.Fatalf("tool type = %T, want public adapter", tool)
	}
}

func TestMarshalSources_public_reports_provider_and_budget(t *testing.T) {
	// Given
	tool, err := publicdiscovery.NewAgentSearchTool()
	if err != nil {
		t.Fatalf("new public tool: %v", err)
	}

	// When
	wire := marshalDiscovery(tool, agent.DiscoveryResult{
		Sources: []agent.DiscoveredSource{{URL: "https://events.example/calendar"}},
		YieldTrace: agent.YieldTrace{
			Outcome: agent.YieldOutcomeAccepted, TerminalReason: agent.YieldTerminalProposalDone,
			Offered: 1, ProposalCalls: 2, JudgeCalls: 1, JudgeEntriesParsed: 1, Accepted: 1,
		},
	}, true)

	// Then
	var output struct {
		Provider        string `json:"provider"`
		PublicDiscovery struct {
			Truncated bool `json:"truncated"`
		} `json:"public_discovery"`
	}
	if err := json.Unmarshal(wire, &output); err != nil {
		t.Fatalf("decode public output: %v (%s)", err, wire)
	}
	if output.Provider != "public" {
		t.Fatalf("provider = %q, want public", output.Provider)
	}
	if output.PublicDiscovery.Truncated {
		t.Fatal("fresh public budget unexpectedly truncated")
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "public-yield-trace.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(wire, bytes.TrimSuffix(golden, []byte("\n"))) {
		t.Fatalf("public output differs from golden\ngot:  %s\nwant: %s", wire, golden)
	}
}

func TestMarshalSources_public_omits_yield_trace_when_discovery_fails(t *testing.T) {
	// Given
	tool, err := publicdiscovery.NewAgentSearchTool()
	if err != nil {
		t.Fatalf("new public tool: %v", err)
	}

	// When
	wire := marshalDiscovery(tool, agent.DiscoveryResult{YieldTrace: agent.YieldTrace{
		Outcome: agent.YieldOutcomeError, TerminalReason: agent.YieldTerminalJudgeError,
	}}, false)

	// Then
	if bytes.Contains(wire, []byte(`"yield_trace"`)) {
		t.Fatalf("failed discovery exposed yield trace: %s", wire)
	}
}

func TestNewSearchTool_rejects_missing_tavily_key(t *testing.T) {
	// When
	_, err := newSearchTool(searchConfig{Provider: "tavily", Client: &http.Client{Timeout: time.Second}})

	// Then
	if err == nil {
		t.Fatal("select search tool error = nil, want error")
	}
}

func TestNewSearchTool_rejects_unknown_provider_without_fallback(t *testing.T) {
	// When
	_, err := newSearchTool(searchConfig{Provider: "unknown"})

	// Then
	if err == nil {
		t.Fatal("unknown provider error = nil, want explicit selection error")
	}
}

func TestSelectBackend_explicitly_chooses_configured_solar(t *testing.T) {
	// Given
	backends := []agent.Backend{
		{Name: "local", BaseURL: "http://local.invalid/v1", Model: "local"},
		{Name: "solar", BaseURL: "https://solar.invalid/v1", APIKey: "redacted", Model: "solar-open2"},
	}

	// When
	got, err := selectBackend("solar", backends)

	// Then
	if err != nil {
		t.Fatalf("select solar backend: %v", err)
	}
	if got.Name != "solar" || got.APIKey != "redacted" {
		t.Fatalf("backend = %+v, want configured solar backend", got)
	}
}

func TestSelectBackend_rejects_unconfigured_solar_even_when_local_exists(t *testing.T) {
	// Given
	backends := []agent.Backend{{Name: "local", BaseURL: "http://local.invalid/v1", Model: "local"}}

	// When
	_, err := selectBackend("solar", backends)

	// Then
	if err == nil {
		t.Fatal("select solar backend error = nil, want explicit configuration error")
	}
}

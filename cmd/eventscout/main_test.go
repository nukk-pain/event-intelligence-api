package main

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
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
			if _, ok := tool.(*agent.TavilySearch); !ok {
				t.Fatalf("tool type = %T, want *agent.TavilySearch", tool)
			}
		})
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

package publicdiscovery

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSeedCatalog_when_read_by_caller_is_curated_and_isolated(t *testing.T) {
	// Given / When
	first := SeedCatalog()

	// Then
	if first.Version == "" {
		t.Fatal("catalog version is empty")
	}
	if len(first.Seeds) != MaxSeeds {
		t.Fatalf("seed count = %d, want exactly %d", len(first.Seeds), MaxSeeds)
	}
	seenNames := map[string]struct{}{}
	for _, seed := range first.Seeds {
		if strings.TrimSpace(seed.Name) == "" {
			t.Fatal("catalog contains unnamed seed")
		}
		if _, duplicate := seenNames[seed.Name]; duplicate {
			t.Fatalf("duplicate seed name %q", seed.Name)
		}
		seenNames[seed.Name] = struct{}{}
		if _, err := CanonicalizeURL(seed.URL); err != nil {
			t.Fatalf("catalog seed %q URL %q: %v", seed.Name, seed.URL, err)
		}
		if !candidateURLAllowed(seed.URL, false) {
			t.Fatalf("catalog seed %q is not a public literal URL: %q", seed.Name, seed.URL)
		}
	}

	for _, want := range []string{"AKEI 한국전시산업진흥회", "EXCO"} {
		if _, ok := seenNames[want]; !ok {
			t.Fatalf("catalog missing promoted domestic hub seed %q", want)
		}
	}

	first.Seeds[0].URL = "https://attacker.invalid/"
	second := SeedCatalog()
	if second.Seeds[0].URL == first.Seeds[0].URL {
		t.Fatal("caller mutation changed the server-owned catalog")
	}
}

func TestDiscoverQueryRequired_when_query_is_blank(t *testing.T) {
	// Given
	var hits atomic.Int32
	server := newFixtureServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("catalog", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), " \n\t")

	// Then
	if !errors.Is(err, ErrQueryRequired) || len(result.Candidates) != 0 || hits.Load() != 0 {
		t.Fatalf("result=%+v error=%v hits=%d", result, err, hits.Load())
	}
}

func TestDiscoverNoArbitrarySeedInjection_when_query_contains_URL(t *testing.T) {
	// Given
	var injectedHits atomic.Int32
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/injected" {
			injectedHits.Add(1)
		}
		writeDocument(t, w, "text/html", `<html><title>Catalog root</title></html>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("catalog", server.URL+"/")}, DefaultLimits())

	// When
	result, err := provider.Search(context.Background(), server.URL+"/injected")

	// Then
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if injectedHits.Load() != 0 {
		t.Fatalf("query URL was crawled %d times", injectedHits.Load())
	}
	if len(result.Candidates) != 1 || result.Candidates[0].URL != server.URL+"/" {
		t.Fatalf("candidates = %+v, want only catalog root", result.Candidates)
	}
}

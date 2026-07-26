package publicdiscovery

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A zero seed-candidate count is only actionable if it names its own cause, so
// every enqueued seed must land in exactly one bounded category.
func TestCrawl_accountsForEverySeedOutcome(t *testing.T) {
	tests := []struct {
		name    string
		handler func(t *testing.T, w http.ResponseWriter)
		want    SeedOutcomes
	}{
		{
			name: "reachable HTML seed becomes a titled candidate",
			handler: func(t *testing.T, w http.ResponseWriter) {
				writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
			},
			want: SeedOutcomes{Candidate: 1},
		},
		{
			name: "seed refused by status never becomes a candidate",
			handler: func(_ *testing.T, w http.ResponseWriter) {
				w.WriteHeader(http.StatusForbidden)
			},
			want: SeedOutcomes{HTTPStatus: 1},
		},
		{
			name: "seed served as a non-HTML document is unsupported",
			handler: func(t *testing.T, w http.ResponseWriter) {
				writeDocument(t, w, "application/pdf", "%PDF-1.4")
			},
			want: SeedOutcomes{UnsupportedContent: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			mux := http.NewServeMux()
			server := newFixtureServer(t, mux)
			mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { tt.handler(t, w) })
			provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())
			tool, err := NewAgentSearchToolWithProvider(provider)
			if err != nil {
				t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
			}

			// When
			if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
				t.Fatalf("Search: %v", err)
			}
			snapshot := tool.YieldSnapshot()

			// Then
			if snapshot.SeedOutcomes != tt.want {
				t.Fatalf("seed outcomes = %#v, want %#v", snapshot.SeedOutcomes, tt.want)
			}
			if snapshot.SeedOutcomes.Total() != 1 {
				t.Fatalf("accounted seeds = %d, want exactly one per enqueued seed", snapshot.SeedOutcomes.Total())
			}
			if snapshot.SeedCandidates != tt.want.Candidate {
				t.Fatalf("seed candidates = %d, want %d", snapshot.SeedCandidates, tt.want.Candidate)
			}
		})
	}
}

// Robots enforcement is the one rejection the crawler applies before any
// request body exists, so it must be distinguishable from a transport failure.
func TestCrawl_attributesRobotsDisallowedSeed(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/plain", "User-agent: *\nDisallow: /\n")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	snapshot := tool.YieldSnapshot()

	// Then
	if snapshot.SeedOutcomes.RobotsDisallowed != 1 || snapshot.SeedOutcomes.Total() != 1 {
		t.Fatalf("seed outcomes = %#v, want one robots_disallowed", snapshot.SeedOutcomes)
	}
	if snapshot.SeedCandidates != 0 {
		t.Fatalf("seed candidates = %d, want none from a disallowed seed", snapshot.SeedCandidates)
	}
}

// The tally must hold no host, URL, or document text.
func TestCrawl_seedOutcomesSerializeCountsOnly(t *testing.T) {
	// Given
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>PRIVATE_SEED_TITLE_MARKER</title>`)
	})
	provider, _ := newFixtureProvider(t, []Seed{fixtureSeed("venue", server.URL+"/")}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	encoded, err := json.Marshal(tool.YieldSnapshot().SeedOutcomes)
	if err != nil {
		t.Fatalf("marshal seed outcomes: %v", err)
	}

	// Then
	for _, leaked := range []string{"PRIVATE_SEED_TITLE_MARKER", server.URL, "127.0.0.1"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("seed outcomes leaked %q: %s", leaked, encoded)
		}
	}
}

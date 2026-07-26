package publicdiscovery

import (
	"context"
	"net/http"
	"testing"
)

// Every catalog seed must reach exactly one outcome. A seed abandoned during
// robots bootstrap is still a seed that produced no candidate, and leaving it
// unaccounted makes the tally silently under-report the very loss it exists to
// explain.
func TestCrawl_accountsForSeedsAbandonedDuringRobotsBootstrap(t *testing.T) {
	tests := []struct {
		name       string
		robotsCode int
		want       SeedOutcomes
	}{
		{
			name:       "robots unavailable in strict mode",
			robotsCode: http.StatusInternalServerError,
			want:       SeedOutcomes{RobotsUnavailable: 1},
		},
		{
			name:       "robots refused outright",
			robotsCode: http.StatusForbidden,
			want:       SeedOutcomes{RobotsUnavailable: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			mux := http.NewServeMux()
			server := newFixtureServer(t, mux)
			mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.robotsCode)
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
			if snapshot.SeedOutcomes.Total() != 1 {
				t.Fatalf("accounted seeds = %d (%#v), want every catalog seed accounted exactly once",
					snapshot.SeedOutcomes.Total(), snapshot.SeedOutcomes)
			}
			if snapshot.SeedOutcomes != tt.want {
				t.Fatalf("seed outcomes = %#v, want %#v", snapshot.SeedOutcomes, tt.want)
			}
		})
	}
}

// The tally is anchored to the catalog, not to how far the crawl happened to
// get, so a mixed catalog still balances.
func TestCrawl_seedOutcomesBalanceAcrossMixedCatalog(t *testing.T) {
	// Given
	reachable := newSeedServer(t, http.StatusNotFound)
	blocked := newSeedServer(t, http.StatusInternalServerError)
	provider, _ := newFixtureProvider(t, []Seed{
		fixtureSeed("reachable", reachable+"/"),
		fixtureSeed("blocked", blocked+"/"),
	}, DefaultLimits())
	tool, err := NewAgentSearchToolWithProvider(provider)
	if err != nil {
		t.Fatalf("NewAgentSearchToolWithProvider: %v", err)
	}

	// When
	if _, err := tool.Search(context.Background(), "official AI event calendar"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	outcomes := tool.YieldSnapshot().SeedOutcomes

	// Then
	if outcomes.Total() != 2 {
		t.Fatalf("accounted seeds = %d (%#v), want both catalog seeds", outcomes.Total(), outcomes)
	}
	if outcomes.Candidate != 1 || outcomes.RobotsUnavailable != 1 {
		t.Fatalf("seed outcomes = %#v, want one candidate and one robots_unavailable", outcomes)
	}
}

func newSeedServer(t *testing.T, robotsCode int) string {
	t.Helper()
	mux := http.NewServeMux()
	server := newFixtureServer(t, mux)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(robotsCode) })
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeDocument(t, w, "text/html", `<title>Venue Root</title>`)
	})
	return server.URL
}

package publicdiscovery

import (
	"fmt"
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

const catalogVersion = "2026-07-28.v2"

func catalogSeeds() []Seed {
	return []Seed{
		{Name: "COEX", URL: "https://www.coex.co.kr/"},
		{Name: "KINTEX", URL: "https://www.kintex.com/"},
		{Name: "CES", URL: "https://www.ces.tech/"},
		{Name: "NVIDIA GTC", URL: "https://www.nvidia.com/gtc/"},
		{Name: "BIO International", URL: "https://convention.bio.org/"},
		{Name: "MEDICA", URL: "https://www.medica-tradefair.com/"},
		// Domestic hub seeds promoted 2026-07-28 (keyless discovery expansion).
		// robots verified: AKEI 200 allow, EXCO 200 with Googlebot-only rules.
		// GEP excluded (robots disallows all), BEXCO deferred (robots 302).
		{Name: "AKEI 한국전시산업진흥회", URL: "https://www.akei.or.kr/"},
		{Name: "EXCO", URL: "https://exco.co.kr/"},
	}
}

func SeedCatalog() Catalog {
	return Catalog{Version: catalogVersion, Seeds: catalogSeeds()}
}

func New() (*Provider, error) {
	limits := DefaultLimits()
	budget := fetch.NewPublicCrawlBudget()
	fetcher, err := fetch.NewFetcher(
		fetch.WithStrictPublicCrawl(budget),
		fetch.WithAnyPublicHost(true),
		fetch.WithUserAgent("eventsintel-public-discovery/0.1 (+https://events.nukk.net)"),
		fetch.WithPerMinute(30),
	)
	if err != nil {
		return nil, fmt.Errorf("public discovery fetcher: %w", err)
	}
	return newProvider(SeedCatalog(), limits, runtimeDeps{
		fetcher: fetcher,
		budget:  budget,
		now:     time.Now,
	})
}

func newProvider(catalog Catalog, limits Limits, deps runtimeDeps) (*Provider, error) {
	if err := validateLimits(limits); err != nil {
		return nil, err
	}
	parsedCatalog, err := parseCatalog(catalog, limits.MaxSeeds, deps.allowLocalCandidates)
	if err != nil {
		return nil, err
	}
	if deps.fetcher == nil || deps.budget == nil || deps.now == nil {
		return nil, fmt.Errorf("runtime dependencies: %w", ErrInvalidCatalog)
	}
	return &Provider{
		catalog: parsedCatalog, limits: limits, fetcher: deps.fetcher,
		budget: deps.budget, now: deps.now, allowLocalCandidates: deps.allowLocalCandidates,
	}, nil
}

func parseCatalog(catalog Catalog, maxSeeds int, allowLocal bool) (Catalog, error) {
	if strings.TrimSpace(catalog.Version) == "" || len(catalog.Seeds) == 0 || len(catalog.Seeds) > maxSeeds {
		return Catalog{}, ErrInvalidCatalog
	}
	parsed := Catalog{Version: catalog.Version, Seeds: make([]Seed, 0, len(catalog.Seeds))}
	seenNames := make(map[string]struct{}, len(catalog.Seeds))
	for _, seed := range catalog.Seeds {
		name := strings.TrimSpace(seed.Name)
		canonical, err := CanonicalizeURL(seed.URL)
		if err != nil || name == "" || !candidateURLAllowed(canonical, allowLocal) {
			return Catalog{}, ErrInvalidCatalog
		}
		if _, exists := seenNames[name]; exists {
			return Catalog{}, ErrInvalidCatalog
		}
		seenNames[name] = struct{}{}
		parsed.Seeds = append(parsed.Seeds, Seed{Name: name, URL: canonical})
	}
	return parsed, nil
}

func validateLimits(limits Limits) error {
	if limits.MaxSeeds <= 0 || limits.MaxSeeds > MaxSeeds || limits.MaxDepth < 0 ||
		limits.MaxProtocolDocuments <= 0 || limits.MaxHTMLPages <= 0 || limits.MaxCandidates <= 0 ||
		limits.MaxHTTPAttempts <= 0 || limits.MaxResponseBytes <= 0 || limits.Timeout <= 0 {
		return ErrInvalidLimits
	}
	return nil
}

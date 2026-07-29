// Command eventsintel is the COEX/KINTEX event intelligence binary. It exposes
// two subcommands: `ingest` (batch crawl) and `serve` (read-only HTTP API).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/cfpurge"
	"github.com/smpain/event-intelligence-api/internal/config"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/pipeline"
	"github.com/smpain/event-intelligence-api/internal/render"
	"github.com/smpain/event-intelligence-api/internal/solarenrich"
	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/sources/aiia"
	"github.com/smpain/event-intelligence-api/internal/sources/akei"
	"github.com/smpain/event-intelligence-api/internal/sources/benchmark"
	"github.com/smpain/event-intelligence-api/internal/sources/coex"
	"github.com/smpain/event-intelligence-api/internal/sources/kintex"
	"github.com/smpain/event-intelligence-api/internal/sources/showala"
	"github.com/smpain/event-intelligence-api/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cfg := config.FromEnv()

	switch os.Args[1] {
	case "ingest":
		if err := runIngest(cfg); err != nil {
			log.Fatalf("ingest: %v", err)
		}
	case "serve":
		if err := runServe(cfg); err != nil {
			log.Fatalf("serve: %v", err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: eventsintel <ingest|serve>\n")
}

// runIngest runs one ingest batch: single-flight lock -> open writer + migrate
// -> build the SSRF-guarded fetcher (production host allowlist) -> register the
// COEX + KINTEX sources -> run the orchestrator -> print per-source counts.
//
// SINGLE-FLIGHT: the whole run is wrapped in a flock. An overlapping cron run
// that cannot take the lock is skipped cleanly (log + exit 0) so runs never pile
// up.
func runIngest(cfg config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.IngestDeadline)
	defer cancel()

	lock, err := pipeline.AcquireLock(cfg.LockPath)
	if err != nil {
		if errors.Is(err, pipeline.ErrLocked) {
			log.Printf("ingest: another run holds %s; skipping this run", cfg.LockPath)
			return nil // clean skip, exit 0
		}
		return err
	}
	defer lock.Unlock()

	db, err := store.OpenWrite(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		return err
	}

	f, err := fetch.NewFetcher(
		fetch.WithUserAgent(cfg.UserAgent),
		fetch.WithPerMinute(cfg.RateLimitPerMinute),
		// Production allowlist, shared with eventscout's promotion diff. The
		// list itself lives in fetch.ProductionAllowedHosts so there is one
		// audited place a host can enter the crawl boundary.
		fetch.WithAllowedHosts(fetch.ProductionAllowedHosts...),
		// k-ai.or.kr offers neither ECDHE nor TLS 1.3; see WithLegacyTLSHosts.
		fetch.WithLegacyTLSHosts("k-ai.or.kr"),
	)
	if err != nil {
		return err
	}
	officialFetcher, err := fetch.NewFetcher(
		fetch.WithUserAgent(cfg.UserAgent),
		fetch.WithPerMinute(cfg.RateLimitPerMinute),
		fetch.WithAnyPublicHost(true),
	)
	if err != nil {
		return err
	}
	renderConfig := render.DefaultConfig(cfg.UserAgent)
	// Chrome receives every resource through the reviewed production-host
	// fetcher. The broader officialFetcher remains limited to server-side
	// action-page enrichment; it must not become browser-reachable through the
	// loopback renderer proxy.
	renderConfig.ResourceFetcher = officialResourceFetcher{fetcher: f}
	textSelector, err := render.New(ctx, renderConfig)
	if err != nil {
		return fmt.Errorf("headless renderer: %w", err)
	}
	renderStats := func() {
		st := textSelector.Stats()
		log.Printf("headless render: shell_detected=%d succeeded=%d failed=%d cap_skipped=%d (cap=%d)",
			st.ShellDetected, st.RenderSucceeded, st.RenderFailed, st.CapSkipped, renderConfig.MaxPages)
	}
	defer func() {
		renderStats()
		textSelector.Close()
	}()

	// Register the venue adapters (single registration point).
	sources.Register(coex.New())
	sources.Register(kintex.New())
	sources.Register(benchmark.New())
	// SHOWALA aggregator, scoped to KINTEX events — catches externally organized
	// rentals (e.g. RoboWorld) that KINTEX's own list.do has not yet published.
	// Cross-source dups fold into the venue-native canonical row (store dedup).
	sources.Register(showala.New())
	sources.Register(aiia.New())
	// AKEI is the nationwide exhibition schedule aggregator: one adapter covers
	// every domestic venue; cross-source dups fold into venue-native rows.
	sources.Register(akei.New())

	registeredSources := sources.All()
	sourceIDs := make([]string, 0, len(registeredSources))
	for id := range registeredSources {
		sourceIDs = append(sourceIDs, id)
	}
	sort.Strings(sourceIDs)
	srcs := make([]sources.Source, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		srcs = append(srcs, registeredSources[id])
	}

	batchID := fmt.Sprintf("batch-%s", time.Now().UTC().Format("20060102T150405Z"))
	p := pipeline.New(batchID).
		WithMaxDiscover(cfg.MaxDiscoverPerSource).
		WithSourceConcurrency(cfg.SourceConcurrency).
		WithDetailWorkers(cfg.DetailWorkers).
		WithOfficialFetcher(officialFetcher).
		WithTextSelector(textSelector)

	// Solar enrichment is opt-in and ingest-only. Without both the operator key
	// and the explicit switch, ingest stays exactly as deterministic as before.
	// A misconfiguration must not silently disable it, so it is a hard error.
	if cfg.SolarEnrich {
		enricher, err := solarenrich.New(solarenrich.Config{
			Backend: agent.Backend{
				Name: "solar", BaseURL: cfg.SolarBaseURL,
				Model: cfg.SolarModel, APIKey: cfg.SolarAPIKey,
			},
			MaxCalls: cfg.SolarMaxCalls, MaxTokens: 256, CallTimeout: 20 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("solar enrichment: %w", err)
		}
		p = p.WithEnricher(enricher)

		// The action fields are where the deterministic extractor actually
		// falls short on live venue pages, so this is the enrichment that
		// carries the run. It reuses the official fetcher and therefore the
		// same allowlist, robots policy, and rate limits.
		actions, err := solarenrich.NewActionEnricher(solarenrich.Config{
			Backend: agent.Backend{
				Name: "solar", BaseURL: cfg.SolarBaseURL,
				Model: cfg.SolarModel, APIKey: cfg.SolarAPIKey,
			},
			MaxCalls: cfg.SolarMaxCalls, MaxTokens: 512, CallTimeout: 20 * time.Second,
			MaxConcurrent: cfg.SolarMaxConcurrent,
		}, func(fetchCtx context.Context, pageURL string) (string, error) {
			return readOfficialPageText(fetchCtx, officialFetcher, textSelector, pageURL)
		})
		if err != nil {
			return fmt.Errorf("solar action enrichment: %w", err)
		}
		p = p.WithActionEnricher(actions)

		defer func() {
			log.Printf("solar enrichment: dates attempts=%d filled=%d, actions attempts=%d filled=%d throttled=%d budget=%d",
				enricher.Calls(), enricher.Filled(), actions.Calls(), actions.Filled(), actions.Throttled(), cfg.SolarMaxCalls)
		}()
	}

	// WALL-CLOCK DEADLINE: cap the whole crawl so a hung Discover or a slow
	// cumulative detail fetch can never let one run exceed the cron interval.
	// Without this, the flock single-flight lock would mean the next scheduled
	// run is silently skipped indefinitely. The pipeline checks ctx between
	// items/sources and marks the run truncated (it will NOT poison the
	// discovery floor by recording a cut-short Discovered count as the baseline).
	rep, err := p.Run(ctx, db, srcs, f)
	if err != nil {
		return err
	}

	if eligible, gated := p.RenderGateStats(); eligible+gated > 0 {
		log.Printf("render gate: eligible=%d gated=%d (upcoming, deadline-less, within %d days, rotation 1/%d)",
			eligible, gated, pipeline.RenderHorizonDays, pipeline.RenderRotationBuckets)
	}
	log.Printf("ingest batch %s complete", rep.BatchID)
	if rep.Truncated {
		log.Printf("  WARNING: run truncated by deadline/cancellation (%s); "+
			"cut-short sources did not update their discovery baseline", cfg.IngestDeadline)
	}
	for _, sr := range rep.Sources {
		status := "ok"
		if sr.Aborted {
			status = "ABORTED(" + sr.AbortReason + ")"
		}
		log.Printf("  source=%s discovered=%d (raw=%d dropped_by_cap=%d) parsed=%d stored=%d skipped=%d %s",
			sr.Source, sr.Discovered, sr.DiscoveredRaw, sr.DroppedByCap, sr.Parsed, sr.Stored, sr.Skipped, status)
	}

	// Deadlines are ratchet facts (store carry-forward), so this count should
	// only ever rise between runs — a drop in the journal is the cheapest
	// possible regression alarm. Best-effort: never fails the ingest.
	if n, cerr := store.CountUpcomingWithDeadline(ctx, db, time.Now().UTC().Format("2006-01-02")); cerr == nil {
		log.Printf("deadline coverage: upcoming events with a deadline = %d", n)
	}

	// Invalidate the Cloudflare edge cache so it stops serving stale event JSON/
	// HTML now that fresh data is in the store. This is what makes the long edge
	// s-maxage safe (see internal/api/cache.go). It is best-effort: a purge failure
	// is logged but never fails the ingest — the data is already persisted; only the
	// edge is briefly stale until s-maxage expires. A fresh context is used so a
	// near-deadline ingest ctx cannot abort the purge (the client has its own
	// timeout). No-op when purge env is unset.
	if ingestChangedData(rep) {
		if err := cfpurge.PurgeEverything(context.Background(), cfg.CFPurgeZoneID, cfg.CFPurgeToken, nil); err != nil {
			log.Printf("ingest: cloudflare cache purge failed (non-fatal): %v", err)
		} else if cfg.CFPurgeToken != "" {
			log.Printf("ingest: cloudflare cache purged")
		}
	}
	return nil
}

// readOfficialPageText preserves the official fetcher as the only network
// boundary, then gives the Solar evidence collector the same selected HTML as
// deterministic extraction. Renderer failure deliberately falls back to static
// text so enrichment remains additive and non-fatal.
func readOfficialPageText(ctx context.Context, officialFetcher *fetch.Fetcher, selector render.TextSelector, pageURL string) (string, error) {
	res, err := officialFetcher.Fetch(ctx, pageURL, fetch.Conditional{})
	if err != nil || res.NotModified || res.StatusCode != http.StatusOK {
		return "", err
	}
	return string(render.StaticOrRendered(ctx, selector, res.URL, res.Body)), nil
}

type officialResourceFetcher struct {
	fetcher *fetch.Fetcher
}

func (f officialResourceFetcher) Fetch(ctx context.Context, rawURL string) (render.Resource, error) {
	res, err := f.fetcher.Fetch(ctx, rawURL, fetch.Conditional{})
	if err != nil {
		return render.Resource{}, err
	}
	if res.NotModified || res.StatusCode != http.StatusOK {
		return render.Resource{}, fmt.Errorf("renderer resource %q: HTTP %d", rawURL, res.StatusCode)
	}
	return render.Resource{Body: res.Body, ContentType: res.ContentType}, nil
}

// ingestChangedData reports whether the batch processed any events into the store
// (events handed to ApplyBatch). It gates the Cloudflare purge: there is no point
// purging when a run stored nothing (e.g. every source aborted). Because the
// dataset refreshes about once per day, a run that does store events is the right
// cadence to refresh the edge — over-purging on a no-op-diff day is negligible.
func ingestChangedData(rep pipeline.Report) bool {
	for _, sr := range rep.Sources {
		if sr.Stored > 0 {
			return true
		}
	}
	return false
}

// runServe starts the read-only HTTP API.
func runServe(cfg config.Config) error {
	db, err := store.OpenRead(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	// Build the full read API (routes + Task 3.5 middleware chain) from the
	// read-only handle. The zero-value MiddlewareConfig is valid and falls back to
	// the documented quota/concurrency defaults.
	handler, err := api.Router(db, api.MiddlewareConfig{})
	if err != nil {
		return err
	}
	log.Printf("eventsintel serve listening on %s", cfg.HTTPAddr)
	return http.ListenAndServe(cfg.HTTPAddr, handler)
}

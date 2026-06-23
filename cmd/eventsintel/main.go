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

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/cfpurge"
	"github.com/smpain/event-intelligence-api/internal/config"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/pipeline"
	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/sources/benchmark"
	"github.com/smpain/event-intelligence-api/internal/sources/coex"
	"github.com/smpain/event-intelligence-api/internal/sources/kintex"
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
		// Production allowlist (default already covers the two venues; passed
		// explicitly here so the wiring is auditable from the command).
		fetch.WithAllowedHosts(
			"www.coex.co.kr",
			"www.kintex.com",
			"www.ces.tech",
			"www.nvidia.com",
			"convention.bio.org",
			"websummit.com",
			"qatar.websummit.com",
			"slush.org",
			"techcrunch.com",
			"worldsummit.ai",
			"hlth.com",
			"themedtechconference.com",
			"www.medica-tradefair.com",
			"jcd-expo.jp",
			"www.medicaltaiwan.com.tw",
			"www.himssconference.com",
			"automatica-munich.com",
			"www.roboticssummit.com",
			"www.switchsg.org",
			"www.rsna.org",
			"www.computextaipei.com.tw",
			"vivatechnology.com",
			"www.worldhealthexpo.com",
			"informaconnect.com",
			"www.ai-expo.net",
			"2026.ieee-icra.org",
			"www.himss.org",
			"www.humanx.co",
			"www.ieee-ras.org",
			"www.gitexeurope.com",
			"www.worldaic.com.cn",
			"www.worldrobotconference.com",
		),
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

	// Register the venue adapters (single registration point).
	sources.Register(coex.New())
	sources.Register(kintex.New())
	sources.Register(benchmark.New())

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
		WithOfficialFetcher(officialFetcher)

	// WALL-CLOCK DEADLINE: cap the whole crawl so a hung Discover or a slow
	// cumulative detail fetch can never let one run exceed the cron interval.
	// Without this, the flock single-flight lock would mean the next scheduled
	// run is silently skipped indefinitely. The pipeline checks ctx between
	// items/sources and marks the run truncated (it will NOT poison the
	// discovery floor by recording a cut-short Discovered count as the baseline).
	ctx, cancel := context.WithTimeout(context.Background(), cfg.IngestDeadline)
	defer cancel()

	rep, err := p.Run(ctx, db, srcs, f)
	if err != nil {
		return err
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

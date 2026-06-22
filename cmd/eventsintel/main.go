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
	"github.com/smpain/event-intelligence-api/internal/config"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/pipeline"
	"github.com/smpain/event-intelligence-api/internal/sources"
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
		fetch.WithAllowedHosts("www.coex.co.kr", "www.kintex.com"),
	)
	if err != nil {
		return err
	}

	// Register the venue adapters (single registration point).
	sources.Register(coex.New())
	sources.Register(kintex.New())

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
		WithSourceConcurrency(cfg.SourceConcurrency)

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
	return nil
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

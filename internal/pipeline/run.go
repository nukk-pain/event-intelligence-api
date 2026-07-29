// Package pipeline is the ingest orchestrator (Task 4.1). For each registered
// source it runs Discover -> (per Ref) Fetch -> Parse -> classify+Normalize,
// collects the normalized events, and applies them through the Phase-1
// store.ApplyBatch change-detection path (events + change_log + raw_snapshot).
//
// Three safety properties are enforced here, not in the store:
//
//   - PER-ITEM recover(): each Ref is processed inside its own recover()-guarded
//     unit. A parse/normalize failure or panic on one item records an
//     ingest_error and SKIPS that item; it never half-upserts and never aborts
//     the source. A pre-existing good row for that event_id is left untouched.
//   - CIRCUIT BREAKER per source: if discovery yields 0/too-few (below a floor
//     computed from the last successful run) or the changed fraction crosses a
//     threshold, that source's batch is ABORTED with NO diffs/upserts and a
//     fetch_anomaly is recorded. Other sources still proceed.
//   - SINGLE-FLIGHT is handled by the caller via AcquireLock (lock.go).
//   - WALL-CLOCK DEADLINE: the caller passes a ctx with a timeout
//     (cfg.IngestDeadline). Run checks ctx between sources and between refs and
//     stops cleanly when it fires, marking Report.Truncated. A source cut short
//     this way does NOT update its discovery-floor baseline (a partial Discovered
//     count must not poison the next run's relative floor).
package pipeline

import (
	"sync/atomic"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/render"
)

// BreakerConfig tunes the per-source circuit breaker. The zero value is valid
// and uses Defaults() via New.
type BreakerConfig struct {
	// MinFloorFraction trips the breaker when this run's discovered count is
	// below MinFloorFraction * lastSuccessfulDiscovered (e.g. 0.5 = "fewer than
	// half of last time"). Applies only when a prior successful baseline exists.
	MinFloorFraction float64

	// AbsoluteFloor trips the breaker when discovered < AbsoluteFloor regardless
	// of history. A discovery of 0 always trips (covers the no-baseline case and
	// total-failure case).
	AbsoluteFloor int

	// MaxChangedFraction trips the breaker when the fraction of normalized events
	// that would change (vs. the persisted store) exceeds this threshold AND the
	// prior store was non-empty. Guards against a parser regression silently
	// rewriting every record. 0 disables the changed-fraction check.
	MaxChangedFraction float64
}

// DefaultBreaker is the breaker policy used when New is given the zero value.
func DefaultBreaker() BreakerConfig {
	return BreakerConfig{
		MinFloorFraction:   0.5,
		AbsoluteFloor:      1,
		MaxChangedFraction: 0, // disabled by default; opt-in per deployment
	}
}

// Pipeline runs a single ingest batch.
type Pipeline struct {
	batchID string
	breaker BreakerConfig
	// maxDiscover caps how many Refs per source are processed in one batch (0 =
	// unbounded). Source adapters are responsible for ordering refs by ingest
	// priority before this cap is applied. This bounds each run for politeness and
	// cost; the number dropped is reported, never silently truncated.
	maxDiscover int
	// sourceConcurrency caps how many independent sources can run at once.
	sourceConcurrency int
	detailWorkers     int
	officialFetcher   *fetch.Fetcher
	textSelector      render.TextSelector
	// Render-gate accounting: how many official-page reads were allowed to
	// reach the browser versus skipped as unable to benefit.
	renderEligibleCount atomic.Int64
	renderGated         atomic.Int64
	// enricher optionally resolves fields the deterministic parse could not.
	// Nil keeps ingest fully deterministic.
	enricher EventEnricher
	// actionEnricher optionally resolves action signals the deterministic
	// second-hop extractor could not find.
	actionEnricher ActionEnricher
	// now returns the batch verification timestamp (ISO8601). Overridable in
	// tests for determinism; defaults to time.Now in RFC3339.
	now func() string
}

// New returns a Pipeline tagged with batchID using the default breaker policy.
func New(batchID string) *Pipeline {
	return &Pipeline{
		batchID:           batchID,
		breaker:           DefaultBreaker(),
		sourceConcurrency: 1,
		detailWorkers:     1,
		now:               func() string { return time.Now().UTC().Format(time.RFC3339) },
	}
}

// WithBreaker overrides the circuit-breaker policy.
func (p *Pipeline) WithBreaker(b BreakerConfig) *Pipeline { p.breaker = b; return p }

// WithMaxDiscover caps Refs processed per source (keeps newest; 0 = unbounded).
func (p *Pipeline) WithMaxDiscover(n int) *Pipeline { p.maxDiscover = n; return p }

// WithClock overrides the batch timestamp source (tests).
func (p *Pipeline) WithClock(now func() string) *Pipeline { p.now = now; return p }

// WithDetailWorkers caps detail refs processed per source. Non-positive values
// keep the current setting.
func (p *Pipeline) WithDetailWorkers(n int) *Pipeline {
	if n > 0 {
		p.detailWorkers = n
	}
	return p
}

func (p *Pipeline) WithOfficialFetcher(f *fetch.Fetcher) *Pipeline {
	p.officialFetcher = f
	return p
}

// WithTextSelector attaches the shared, optional JS-shell fallback. It is
// called only after the existing official fetch succeeds, so it cannot create
// a second HTTP, robots, or SSRF path.
func (p *Pipeline) WithTextSelector(selector render.TextSelector) *Pipeline {
	p.textSelector = selector
	return p
}

// BatchID returns the batch identifier this pipeline stamps onto change_log /
// ingest_error / fetch_anomaly rows.
func (p *Pipeline) BatchID() string { return p.batchID }

// SourceReport is the per-source outcome of one batch.
type SourceReport struct {
	Source        string
	DiscoveredRaw int  // refs Discover returned before the maxDiscover cap
	DroppedByCap  int  // refs dropped by the maxDiscover cap (oldest)
	Discovered    int  // refs actually processed this batch (after cap)
	Parsed        int  // refs that fetched+parsed+normalized successfully
	Stored        int  // events handed to store.ApplyBatch (== Parsed unless aborted)
	Skipped       int  // refs dropped by per-item recover() (parse/normalize/panic)
	Aborted       bool // circuit breaker tripped -> nothing stored for this source
	AbortReason   string
	Cancelled     bool  // run was cut short by deadline/cancellation before finishing this source's refs
	Err           error // discovery-level error (non-fatal to other sources)
}

// Report is the whole-batch outcome.
type Report struct {
	BatchID string
	Sources []SourceReport
	// Truncated is true when the run hit its wall-clock deadline (or ctx was
	// cancelled) before processing every source/ref. A truncated batch is NOT
	// equivalent to a clean one: cut-short sources must not record their reduced
	// Discovered count as the new floor baseline (floor-poisoning guard).
	Truncated bool
}

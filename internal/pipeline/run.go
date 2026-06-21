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
	"context"
	"database/sql"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/normalize"
	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/store"
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
	// unbounded). Sources return Refs oldest-first, so the cap keeps the TAIL —
	// the newest events. This bounds each run for politeness and cost (e.g. COEX's
	// sitemap exposes ~8.7k historical exhibitions back to 2002; a freshness
	// service only needs the recent ones). The number dropped is reported, never
	// silently truncated.
	maxDiscover int
	// now returns the batch verification timestamp (ISO8601). Overridable in
	// tests for determinism; defaults to time.Now in RFC3339.
	now func() string
}

// New returns a Pipeline tagged with batchID using the default breaker policy.
func New(batchID string) *Pipeline {
	return &Pipeline{
		batchID: batchID,
		breaker: DefaultBreaker(),
		now:     func() string { return time.Now().UTC().Format(time.RFC3339) },
	}
}

// WithBreaker overrides the circuit-breaker policy.
func (p *Pipeline) WithBreaker(b BreakerConfig) *Pipeline { p.breaker = b; return p }

// WithMaxDiscover caps Refs processed per source (keeps newest; 0 = unbounded).
func (p *Pipeline) WithMaxDiscover(n int) *Pipeline { p.maxDiscover = n; return p }

// WithClock overrides the batch timestamp source (tests).
func (p *Pipeline) WithClock(now func() string) *Pipeline { p.now = now; return p }

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

// Run orchestrates one ingest batch over the given sources, using the supplied
// SSRF-guarded fetcher and writer handle. Each source is independent: a
// discovery error or tripped breaker on one source does not stop the others.
// Run itself returns an error only for an unrecoverable problem applying the
// (already safe) per-source results; per-source failures are surfaced in Report.
func (p *Pipeline) Run(ctx context.Context, db *sql.DB, srcs []sources.Source, f *fetch.Fetcher) (Report, error) {
	rep := Report{BatchID: p.batchID}

	for _, s := range srcs {
		// Wall-clock deadline / cancellation: stop starting NEW sources once the
		// run is out of time. Already-completed sources stay in the report; the
		// remaining ones are simply not attempted. Marking the batch truncated
		// keeps a partial run distinguishable from a clean one.
		if ctx.Err() != nil {
			rep.Truncated = true
			break
		}
		sr := p.runSource(ctx, db, s, f)
		if sr.Cancelled {
			rep.Truncated = true
		}
		rep.Sources = append(rep.Sources, sr)
	}
	return rep, nil
}

// runSource processes one source end-to-end with per-item isolation and the
// circuit breaker. It never panics out (the per-item recover guards the risky
// Parse/Normalize calls; discovery errors are captured into the report).
func (p *Pipeline) runSource(ctx context.Context, db *sql.DB, s sources.Source, f *fetch.Fetcher) SourceReport {
	sr := SourceReport{Source: s.ID()}

	refs, err := s.Discover(ctx, f)
	if err != nil {
		// A discovery failure is NOT a legitimately-empty page: do not let it
		// masquerade as a 0-discovery reap. Record it and leave prior rows intact.
		sr.Err = err
		sr.Aborted = true
		sr.AbortReason = fmt.Sprintf("discovery error: %v", err)
		prev, _, _ := store.LoadBatchStats(ctx, db, s.ID())
		_ = store.RecordFetchAnomaly(ctx, db, store.FetchAnomaly{
			Source: s.ID(), Reason: sr.AbortReason,
			Discovered: 0, Parsed: 0, PrevStored: prev.Stored, BatchID: p.batchID,
		})
		return sr
	}
	sr.DiscoveredRaw = len(refs)
	// Bound the batch: keep the newest maxDiscover refs (sources return oldest
	// first, so the tail is newest). Report the drop rather than hiding it.
	if p.maxDiscover > 0 && len(refs) > p.maxDiscover {
		sr.DroppedByCap = len(refs) - p.maxDiscover
		refs = refs[len(refs)-p.maxDiscover:]
	}
	sr.Discovered = len(refs)

	now := p.now()

	// Per-item processing with recover() isolation. Failures are recorded and
	// skipped; survivors are collected for the batch apply.
	events := make([]model.Event, 0, len(refs))
	for _, ref := range refs {
		// Cheap deadline/cancellation check between items: once the run is out of
		// time, stop iterating immediately instead of paying the synchronous
		// Fetch/Parse/Normalize cost for every remaining ref. The source is marked
		// cut-short so its (incomplete) Discovered count does NOT become the next
		// run's floor baseline (see SaveBatchStats gating below).
		if ctx.Err() != nil {
			sr.Cancelled = true
			sr.AbortReason = "deadline/cancelled mid-source; baseline not updated"
			break
		}
		ev, stage, perr := p.processRef(ctx, s, f, ref, now)
		if perr != nil {
			sr.Skipped++
			_ = store.RecordIngestError(ctx, db, store.IngestError{
				EventID: ref.EventID, Source: s.ID(), Stage: stage,
				Message: perr.Error(), BatchID: p.batchID,
			})
			continue
		}
		events = append(events, *ev)
	}
	sr.Parsed = len(events)

	// CIRCUIT BREAKER: decide BEFORE writing any diff/upsert for this source.
	if reason, tripped := p.shouldTrip(ctx, db, s.ID(), sr.Discovered, events); tripped {
		sr.Aborted = true
		sr.AbortReason = reason
		prev, _, _ := store.LoadBatchStats(ctx, db, s.ID())
		_ = store.RecordFetchAnomaly(ctx, db, store.FetchAnomaly{
			Source: s.ID(), Reason: reason,
			Discovered: sr.Discovered, Parsed: sr.Parsed, PrevStored: prev.Stored,
			BatchID: p.batchID,
		})
		return sr
	}

	// Apply via the Phase-1 change-detection path (events + change_log +
	// raw_snapshot, disappearance-safe, idempotent).
	if err := store.ApplyBatch(ctx, db, events, p.batchID); err != nil {
		// An apply failure is a real error for THIS source; record it as an
		// anomaly but keep other sources running. ApplyBatch is per-event
		// transactional, so already-applied events stay; we do not roll the
		// source back.
		sr.Err = err
		sr.AbortReason = fmt.Sprintf("apply error: %v", err)
		_ = store.RecordFetchAnomaly(ctx, db, store.FetchAnomaly{
			Source: s.ID(), Reason: sr.AbortReason,
			Discovered: sr.Discovered, Parsed: sr.Parsed, BatchID: p.batchID,
		})
		return sr
	}
	sr.Stored = len(events)

	// FLOOR-POISONING GUARD: only record the discovery baseline when this source
	// ran to completion. If the run was cut short by the wall-clock deadline, the
	// Discovered count reflects a partial crawl; persisting it would depress the
	// next run's relative floor and let a real catalog shrink ratchet down across
	// consecutive runs without ever tripping the 50% breaker. The collected
	// survivors are still applied above (work is preserved), but the breaker
	// baseline stays anchored to the last COMPLETE run.
	if !sr.Cancelled {
		_ = store.SaveBatchStats(ctx, db, store.BatchStats{
			Source: s.ID(), Discovered: sr.Discovered, Parsed: sr.Parsed,
			Stored: sr.Stored, BatchID: p.batchID,
		})
	}
	return sr
}

// processRef fetches, parses, classifies (inside Normalize) and normalizes ONE
// ref under a recover() guard. It returns (event, "", nil) on success, or
// (nil, stage, err) on failure/panic where stage is one of fetch|parse|normalize|panic.
func (p *Pipeline) processRef(ctx context.Context, s sources.Source, f *fetch.Fetcher, ref sources.Ref, now string) (ev *model.Event, stage string, err error) {
	defer func() {
		if r := recover(); r != nil {
			ev = nil
			stage = "panic"
			err = fmt.Errorf("panic processing %s: %v\n%s", ref.EventID, r, debug.Stack())
		}
	}()

	res, ferr := f.Fetch(ctx, ref.URL, fetch.Conditional{})
	if ferr != nil {
		return nil, "fetch", fmt.Errorf("fetch %s: %w", ref.URL, ferr)
	}
	// A 304 has no body to parse and no prior in-memory state here; treat as a
	// non-fatal skip (the existing row simply stays as-is). This is not an error.
	if res.NotModified {
		return nil, "fetch", fmt.Errorf("not modified (no body to parse) for %s", ref.EventID)
	}
	if res.StatusCode != 200 {
		return nil, "fetch", fmt.Errorf("detail status %d for %s", res.StatusCode, ref.EventID)
	}

	parsed, perr := s.Parse(ctx, res)
	if perr != nil {
		return nil, "parse", fmt.Errorf("parse %s: %w", ref.EventID, perr)
	}
	if parsed == nil {
		return nil, "parse", fmt.Errorf("parse %s: nil parsed event", ref.EventID)
	}
	// The durable id comes from discovery; ensure it survives parsing.
	if parsed.EventID == "" {
		parsed.EventID = ref.EventID
	}
	if parsed.URL == "" {
		parsed.URL = ref.URL
	}

	// Normalize runs classification internally (classify.Classify) and validates
	// schema rules; an invalid record returns an error and is NOT persisted.
	e, nerr := normalize.Normalize(parsed, now)
	if nerr != nil {
		return nil, "normalize", fmt.Errorf("normalize %s: %w", ref.EventID, nerr)
	}
	return e, "", nil
}

// shouldTrip evaluates the circuit-breaker policy for a source's run and returns
// (reason, true) if the source's batch must be ABORTED without writing.
//
// Floor logic:
//   - discovered == 0 always trips (a 0-discovery run must mutate nothing —
//     AC-10), unless there has never been a baseline AND AbsoluteFloor<=0.
//   - discovered < AbsoluteFloor trips.
//   - with a prior successful baseline: discovered < MinFloorFraction*prev trips.
//
// Changed-fraction logic (opt-in, MaxChangedFraction>0): compute how many of the
// normalized events differ from the persisted content_hash; if the prior store
// was non-empty and that fraction exceeds the threshold, trip (parser-regression
// guard).
func (p *Pipeline) shouldTrip(ctx context.Context, db *sql.DB, source string, discovered int, events []model.Event) (string, bool) {
	b := p.breaker

	// Absolute / zero floor.
	if discovered == 0 {
		return "discovery returned 0 refs (floor); preserving prior rows", true
	}
	if b.AbsoluteFloor > 0 && discovered < b.AbsoluteFloor {
		return fmt.Sprintf("discovered %d < absolute floor %d", discovered, b.AbsoluteFloor), true
	}

	// Relative floor vs. last successful run.
	prev, found, err := store.LoadBatchStats(ctx, db, source)
	if err == nil && found && prev.Discovered > 0 && b.MinFloorFraction > 0 {
		floor := b.MinFloorFraction * float64(prev.Discovered)
		if float64(discovered) < floor {
			return fmt.Sprintf("discovered %d < %.0f%% of last successful %d",
				discovered, b.MinFloorFraction*100, prev.Discovered), true
		}
	}

	// Changed-fraction guard (opt-in).
	if b.MaxChangedFraction > 0 && len(events) > 0 {
		changed, total, prevRows, herr := p.changedFraction(ctx, db, events)
		if herr == nil && prevRows > 0 && total > 0 {
			frac := float64(changed) / float64(total)
			if frac > b.MaxChangedFraction {
				return fmt.Sprintf("changed fraction %.2f > threshold %.2f (parser regression guard)",
					frac, b.MaxChangedFraction), true
			}
		}
	}

	return "", false
}

// changedFraction counts how many of the supplied events differ from their
// persisted content_hash. It returns (changed, total, prevExistingRows, err).
// prevExistingRows lets the caller skip the guard on a cold store (first ingest).
func (p *Pipeline) changedFraction(ctx context.Context, db *sql.DB, events []model.Event) (changed, total, prevRows int, err error) {
	for i := range events {
		e := events[i]
		var stored string
		row := db.QueryRowContext(ctx, `SELECT content_hash FROM events WHERE event_id=?`, e.EventID)
		serr := row.Scan(&stored)
		if serr == sql.ErrNoRows {
			// brand new -> "changed" relative to nothing, but does not count as a
			// pre-existing row; new events are expected on growth and should not
			// trip the regression guard.
			total++
			continue
		}
		if serr != nil {
			return 0, 0, 0, serr
		}
		prevRows++
		total++
		if store.ContentHash(e) != stored {
			changed++
		}
	}
	return changed, total, prevRows, nil
}

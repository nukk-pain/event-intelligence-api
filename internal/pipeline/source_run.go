package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/normalize"
	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/store"
)

type sourceRun struct {
	db      *sql.DB
	source  sources.Source
	fetcher *fetch.Fetcher
	writeMu *sync.Mutex
}

type refResult struct {
	event   *model.Event
	stage   string
	err     error
	started bool
}

type fallbackParser interface {
	ParseFallback(ctx context.Context, ref sources.Ref, cause error) (*sources.ParsedEvent, error)
}

func (p *Pipeline) runSource(ctx context.Context, run sourceRun) SourceReport {
	db := run.db
	s := run.source
	f := run.fetcher
	sr := SourceReport{Source: s.ID()}

	refs, err := s.Discover(ctx, f)
	if err != nil {
		sr.Err = err
		sr.Aborted = true
		sr.AbortReason = fmt.Sprintf("discovery error: %v", err)
		run.writeMu.Lock()
		defer run.writeMu.Unlock()
		prev, _, _ := store.LoadBatchStats(ctx, db, s.ID())
		_ = store.RecordFetchAnomaly(ctx, db, store.FetchAnomaly{
			Source: s.ID(), Reason: sr.AbortReason,
			Discovered: 0, Parsed: 0, PrevStored: prev.Stored, BatchID: p.batchID,
		})
		return sr
	}
	sr.DiscoveredRaw = len(refs)
	if p.maxDiscover > 0 && len(refs) > p.maxDiscover {
		sr.DroppedByCap = len(refs) - p.maxDiscover
		refs = refs[len(refs)-p.maxDiscover:]
	}
	sr.Discovered = len(refs)

	now := p.now()
	results, cancelled := p.processRefs(ctx, s, f, refs, now)
	events := make([]model.Event, 0, len(refs))
	ingestErrors := make([]store.IngestError, 0)
	for i := range results {
		result := results[i]
		if !result.started {
			continue
		}
		ref := refs[i]
		if result.err != nil {
			sr.Skipped++
			ingestErrors = append(ingestErrors, store.IngestError{
				EventID: ref.EventID, Source: s.ID(), Stage: result.stage,
				Message: result.err.Error(), BatchID: p.batchID,
			})
			continue
		}
		events = append(events, *result.event)
	}
	if cancelled {
		sr.Cancelled = true
		sr.AbortReason = "deadline/cancelled mid-source; baseline not updated"
	}
	sr.Parsed = len(events)

	run.writeMu.Lock()
	defer run.writeMu.Unlock()
	writeCtx := ctx
	if sr.Cancelled {
		writeCtx = context.WithoutCancel(ctx)
	}
	for _, ingestErr := range ingestErrors {
		_ = store.RecordIngestError(writeCtx, db, ingestErr)
	}
	if sr.Cancelled {
		return sr
	}

	if reason, tripped := p.shouldTrip(writeCtx, db, s.ID(), sr.Discovered, events); tripped {
		sr.Aborted = true
		sr.AbortReason = reason
		prev, _, _ := store.LoadBatchStats(writeCtx, db, s.ID())
		_ = store.RecordFetchAnomaly(writeCtx, db, store.FetchAnomaly{
			Source: s.ID(), Reason: reason,
			Discovered: sr.Discovered, Parsed: sr.Parsed, PrevStored: prev.Stored,
			BatchID: p.batchID,
		})
		return sr
	}

	if err := store.ApplyBatch(writeCtx, db, events, p.batchID); err != nil {
		sr.Err = err
		sr.AbortReason = fmt.Sprintf("apply error: %v", err)
		_ = store.RecordFetchAnomaly(writeCtx, db, store.FetchAnomaly{
			Source: s.ID(), Reason: sr.AbortReason,
			Discovered: sr.Discovered, Parsed: sr.Parsed, BatchID: p.batchID,
		})
		return sr
	}
	sr.Stored = len(events)

	_ = store.SaveBatchStats(writeCtx, db, store.BatchStats{
		Source: s.ID(), Discovered: sr.Discovered, Parsed: sr.Parsed,
		Stored: sr.Stored, BatchID: p.batchID,
	})
	return sr
}

func (p *Pipeline) processRefs(ctx context.Context, s sources.Source, f *fetch.Fetcher, refs []sources.Ref, now string) ([]refResult, bool) {
	results := make([]refResult, len(refs))
	if len(refs) == 0 {
		return results, ctx.Err() != nil
	}

	workers := p.detailWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > len(refs) {
		workers = len(refs)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				ev, stage, err := p.processRef(ctx, s, f, refs[idx], now)
				results[idx] = refResult{
					event:   ev,
					stage:   stage,
					err:     err,
					started: true,
				}
			}
		}()
	}

	cancelled := false
queueRefs:
	for i := range refs {
		if ctx.Err() != nil {
			cancelled = true
			break
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
			cancelled = true
			break queueRefs
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		cancelled = true
	}
	return results, cancelled
}

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
		if fallback, ok := s.(fallbackParser); ok {
			parsed, perr := fallback.ParseFallback(ctx, ref, ferr)
			if perr != nil {
				return nil, "fetch", fmt.Errorf("fetch %s: %w; fallback: %v", ref.URL, ferr, perr)
			}
			return p.normalizeParsed(ctx, f, ref, parsed, now)
		}
		return nil, "fetch", fmt.Errorf("fetch %s: %w", ref.URL, ferr)
	}
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
	return p.normalizeParsed(ctx, f, ref, parsed, now)
}

func (p *Pipeline) normalizeParsed(ctx context.Context, f *fetch.Fetcher, ref sources.Ref, parsed *sources.ParsedEvent, now string) (ev *model.Event, stage string, err error) {
	if parsed == nil {
		return nil, "parse", fmt.Errorf("parse %s: nil parsed event", ref.EventID)
	}
	if parsed.EventID == "" {
		parsed.EventID = ref.EventID
	}
	if parsed.URL == "" {
		parsed.URL = ref.URL
	}
	p.enrichActions(ctx, f, parsed, now)

	e, nerr := normalize.Normalize(parsed, now)
	if nerr != nil {
		return nil, "normalize", fmt.Errorf("normalize %s: %w", ref.EventID, nerr)
	}
	return e, "", nil
}

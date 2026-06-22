package pipeline

import (
	"context"
	"database/sql"
	"sync"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// WithSourceConcurrency caps independent sources run at once. Non-positive
// values keep the current setting.
func (p *Pipeline) WithSourceConcurrency(n int) *Pipeline {
	if n > 0 {
		p.sourceConcurrency = n
	}
	return p
}

// Run orchestrates one ingest batch over the given sources, using the supplied
// SSRF-guarded fetcher and writer handle. Each source is independent: a
// discovery error or tripped breaker on one source does not stop the others.
// Run itself returns an error only for an unrecoverable problem applying the
// (already safe) per-source results; per-source failures are surfaced in Report.
func (p *Pipeline) Run(ctx context.Context, db *sql.DB, srcs []sources.Source, f *fetch.Fetcher) (Report, error) {
	rep := Report{BatchID: p.batchID}
	reports := make([]SourceReport, len(srcs))
	started := make([]bool, len(srcs))
	sourceConcurrency := p.sourceConcurrency
	if sourceConcurrency < 1 {
		sourceConcurrency = 1
	}
	slots := make(chan struct{}, sourceConcurrency)
	var wg sync.WaitGroup
	var writeMu sync.Mutex

startSources:
	for i, s := range srcs {
		if ctx.Err() != nil {
			rep.Truncated = true
			break
		}

		select {
		case slots <- struct{}{}:
			started[i] = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				reports[i] = p.runSource(ctx, sourceRun{
					db:      db,
					source:  s,
					fetcher: f,
					writeMu: &writeMu,
				})
			}()
		case <-ctx.Done():
			rep.Truncated = true
			break startSources
		}
	}
	wg.Wait()

	if ctx.Err() != nil {
		rep.Truncated = true
	}
	for i := range reports {
		if !started[i] {
			continue
		}
		if reports[i].Cancelled {
			rep.Truncated = true
		}
		rep.Sources = append(rep.Sources, reports[i])
	}
	return rep, nil
}

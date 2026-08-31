# Pipeline Package Guide

## Scope

- Orchestrates one ingest batch from registered source discovery through fetch,
  parse, enrichment, normalization, and `store.ApplyBatch`.
- Owns failure isolation, concurrency, source circuit breakers, browser-render
  gating, and ingest reporting; it does not define source parsing or storage
  schema.

## Where To Look

- `run.go`, `run_sources.go`, `source_run.go` — configuration, source workers,
  detail workers, and ordered reports.
- `source_breaker.go`, `lock.go` — preservation gates and process single-flight.
- `source_enrich.go`, `actions.go`, `enricher.go` — deterministic and optional
  enrichment boundaries.
- `render_gate.go`, `source_render.go` — scarce headless-render eligibility and
  static-body fallback.
- `source_fallback.go` — explicit adapter fallback interface.

## Invariants

- Each ref runs behind `recover`; fetch/parse/normalize/panic failures append an
  `ingest_error`, skip only that ref, and leave an existing good row untouched.
- Discovery errors, zero/floor regressions, and enabled changed-fraction trips
  abort that source with zero event writes and a `fetch_anomaly`; other sources
  continue.
- A cancelled or deadline-truncated source never applies partial events and
  never updates its successful discovery baseline.
- Source work may run concurrently, but reports retain input order and writes
  remain serialized against the single SQLite writer.
- `maxDiscover` drops and reports the oldest refs according to adapter ordering;
  never truncate silently.
- Official-page and headless work must reuse the approved fetch path. Browser
  rendering is only for upcoming, missing-deadline, in-horizon rotation slots;
  render failure falls back to the already-fetched static body.
- Catalog truth wins over optional enrichment, and enrichment failure remains
  non-fatal and provenance-bounded.
- `AcquireLock` is the process-level nonblocking single-flight gate; callers
  treat `ErrLocked` as a clean skip.

## Verification

- Full package: `go test ./internal/pipeline -count=1`.
- Isolation/breakers: `go test ./internal/pipeline -run 'Isolation|Breaker|Cancelled|Fallback' -count=1`.
- Concurrency: `go test -race ./internal/pipeline -run 'Concurrent|Workers|SourceReports' -count=1`.
- Enrichment/rendering: `go test ./internal/pipeline -run 'Enrich|Action|Render' -count=1`.
- Store or source contract changes require their package tests in the same run.

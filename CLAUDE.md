# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`eventsintel` is a single CGo-free Go binary that crawls Korean + international AI/robotics/bio/medical-device industry events (COEX, KINTEX, and international "benchmark" conferences) into a SQLite store, and serves a free, read-only, cache-first HTTP/JSON API and human UI. Deployed at `events.nukk.net`. The binary has two subcommands: `ingest` (batch crawl) and `serve` (read API).

## Commands

```sh
make build          # go build -o bin/eventsintel ./cmd/eventsintel
make test           # go test ./...
make vet            # go vet ./...
make migrate        # builds, then runs `eventsintel ingest` to apply embedded schema to eventsintel.db

go test ./internal/pipeline/...            # one package
go test ./internal/store -run TestApplyBatch   # one test
go test -race ./internal/pipeline/...      # race detector (pipeline is concurrent — use this for pipeline/fetch/store changes)

./bin/eventsintel ingest    # one crawl batch (writes eventsintel.db)
./bin/eventsintel serve     # read API on :8080 (override via EVENTSINTEL_HTTP_ADDR)

# Linux deploy binary:
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o eventsintel-linux ./cmd/eventsintel
```

Runtime config is `config.Default()` overlaid with `EVENTSINTEL_*` env vars (`config.FromEnv`) — there is no config file. Production systemd units set `EVENTSINTEL_HTTP_ADDR`, `EVENTSINTEL_DB_PATH`, etc.

## Architecture

Two paths share one SQLite store. `cmd/eventsintel/main.go` is the only place that wires sources, fetchers, and config together.

**Ingest path** (`internal/pipeline`): for each registered source — `Discover → (per Ref) Fetch → Parse → classify + Normalize → store.ApplyBatch`. Four safety properties live in the orchestrator, *not* the store:
- **Single-flight**: the whole run holds a flock (`pipeline.AcquireLock`); an overlapping cron run that can't take the lock exits 0 cleanly.
- **Wall-clock deadline**: `main` passes a `ctx` with `cfg.IngestDeadline`; the pipeline checks it between sources/refs and marks `Report.Truncated`. A cut-short source does **not** update its discovery-floor baseline (avoids floor-poisoning the next run).
- **Per-item recover()**: each Ref is processed in its own recover-guarded unit. A parse/normalize failure or panic records an `ingest_error`, skips that item, and leaves a pre-existing good row untouched — it never half-upserts and never aborts the source.
- **Per-source circuit breaker** (`source_breaker.go`): if discovery falls below a floor (relative to last successful run) or the changed fraction crosses a threshold, that source's batch is **aborted with zero diffs/upserts** and a `fetch_anomaly` is recorded. Other sources still proceed.

**Read path** (`internal/api`): `api.Router` builds the chi route table and wraps it in a middleware chain. Outermost→innermost: concurrency cap (503 load-shed) → per-IP quota (60/min, 2000/day; emits `X-RateLimit-*`/429) → ETag conditional-GET (304) → response-size cap → handlers. Handlers use content negotiation (`Respond`): JSON by default, Markdown via `Accept: text/markdown` or `?format=md`, HTML UI at `/` for browsers. **No live LLM generation on reads** — everything is served from the store/embedded assets.

### Adding a source

A source is `internal/sources.Source` (`ID()`, `Discover()`, `Parse()`). `Discover` returns `Ref{EventID, URL}`; `Parse` turns one fetched page into a `*ParsedEvent` holding **raw, unparsed** fields (dates/names kept exactly as scraped — the `internal/normalize` stage owns all date parsing, taxonomy, and `missing_fields`). Adding a venue is: implement `Source` + `sources.Register(...)` in `main.go` + add its host(s) to the fetcher allowlist there + a `config.SourceConfig` row. The pipeline iterates `sources.All()` and never names a concrete adapter. Existing adapters: `internal/sources/{coex,kintex,benchmark}`.

### Store

`internal/store` uses the pure-Go `modernc.org/sqlite` driver (driver name `"sqlite"`) to stay CGo-free and ship as one static binary. Single-writer (`OpenWrite`, pool capped to 1 conn, WAL) + many readers (`OpenRead`, `mode=ro`). PRAGMAs are applied via the DSN so no connection escapes them. Schema is embedded SQL under `internal/store/migrations/` (`//go:embed`); `store.Migrate` applies every file in filename order on writer startup and each is idempotent (`IF NOT EXISTS`). `ApplyBatch` is the change-detection path (events + `change_log` + `raw_snapshot`).

### Fetch

`internal/fetch.Fetcher` is the only outbound HTTP path: SSRF-guarded with a **host allowlist** (defined in `main.go`), robots.txt enforcement, and per-host rate limiting. Policy is **HTTP-first, deterministic static-HTML parsing (goquery)**; a headless browser is a last-resort fallback only for genuinely JS-rendered sources and is prohibited as a default (see IDEA.md "Efficiency Strategy"). `main` builds two fetchers: the allowlisted venue fetcher and an `officialFetcher` (`WithAnyPublicHost`) for second-hop action enrichment.

### Published artifacts

`internal/static` embeds the product documents served verbatim: `openapi.yaml`, `llms.txt`, the HTML UI, and CSS/JS assets. They live in their own package because `//go:embed` can't reference paths outside its package directory. Treat `openapi.yaml` and `llms.txt` as the public API contract — keep them in sync with handler behavior.

## Project conventions

- **Soul documents** drive the work. At session start read `AGENTS.md`, then `IDEA.md`, `PLAN.md`, `PROGRESS.md`, `DECISIONS.md`. The authoritative executable plan is under `.tasks/<id>/PLAN.md`; root `PLAN.md` is strategy/shaping. Keep `PROGRESS.md` current (focus, evidence, blockers, next action) and record promotion/scope/deployment/API-policy decisions in `DECISIONS.md`. Preserve uncertainty in `IDEA.md` — don't turn open questions into facts.
- **Hard constraints**: public API stays read-only and cache-first; no live LLM on normal reads; never put clinic/patient/EMR/medical-platform private-connector/private-network data in this repo or deployment. Source provenance on every enriched claim; store short factual summaries, not wholesale copyrighted descriptions.
- Tests live beside code as `*_test.go`; deterministic fetch fixtures go under `internal/sources/<venue>/testdata/`. Use `-race` for pipeline/fetch/store changes.
- Deployment runbook + systemd/Caddy artifacts are in `deploy/` (see `deploy/README.md`).

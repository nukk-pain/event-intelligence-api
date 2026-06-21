# 0001. Stack: Go + modernc.org/sqlite + SQLite for COEX/KINTEX ingestion

- Status: accepted
- Date: 2026-06-21
- Deciders: smpain
- Related: `.tasks/260621-coex-kintex-ingestion/PLAN.md` (Task 0.1), `IDEA.md`, `prototype/manual-dataset-schema.md`
- Supersedes: none
- Superseded by: none

## Context

The Event Intelligence API ingests COEX/KINTEX exhibition data on a schedule,
normalizes it to the v0.1 JSON schema, tracks change state, and serves a
keyless, read-only, cache-first API at `events.nukk.net`.

Three properties of this workload drive the technology choice, and all three
flow from the cost-efficiency / zero-ops mandate in `IDEA.md`:

1. **Read-path COGS must be ~$0.** `IDEA.md` (Cost Structure, Efficiency
   Strategy) requires that normal reads hit a database/cache/static surface with
   no live LLM calls and near-zero per-request cost. The product is "a data
   product, not an AI-heavy product"; the moat is source coverage, identity
   resolution, update history, and API reliability — not model inference.
2. **Operations must be near-zero.** The MVP runs on the existing shared
   Developer DigitalOcean VPS (`developer-vps`, apps under `/srv/developer`)
   alongside other projects. It must not add a database server to operate,
   patch, back up, or contend for RAM, and it must not require a build toolchain
   on the VPS. `IDEA.md` budgets "incremental cost near $0 until resize/backups
   are needed."
3. **The write path is a single scheduled batch.** Ingest runs every ~6 hours
   (`PLAN.md` Phase 4) as one process. Concurrency is "one writer + many
   readers," not high-throughput OLTP. This is the access pattern SQLite is
   strongest at and where a client/server RDBMS would be pure overhead.

The runtime shape is therefore: a long-lived read API process and a periodic
ingest process, both touching one local data file, on a shared low-spec VPS,
with no human in the request loop and a hard zero-ops bias.

## Options Considered

### 1. Implementation language: Go vs Python vs Node

| Option | Pros | Cons |
|---|---|---|
| **Go** | Compiles to a single static binary — `scp` and run, no runtime/venv/`node_modules` on the VPS; low, predictable memory (good on a shared box); first-class stdlib `net/http` for both the fetcher and the read API; goroutines fit the discover/fetch fan-out; strong typing matches the strict, versioned schema; fast cold start for a cron-driven batch. | Slightly more parsing/normalization boilerplate than Python; smaller HTML-scraping ecosystem (mitigated — goquery is sufficient for the confirmed-static COEX/KINTEX HTML). |
| **Python** | Richest scraping/parsing ecosystem; fastest to prototype. | Deploy footprint: interpreter + venv/dependency management on the shared VPS; heavier idle memory for a resident API; GIL/async friction for concurrent fetches; weaker compile-time guarantees against schema drift. |
| **Node** | Good async I/O; one language if a JS UI is ever added. | `node_modules` + runtime to manage on the VPS; higher idle memory than Go; dynamic typing weakens the schema contract; no UI is in v1 scope, so the "one language" benefit is hypothetical. |

### 2. SQLite driver: modernc.org/sqlite (pure Go) vs mattn/go-sqlite3 (CGo)

| Option | Pros | Cons |
|---|---|---|
| **modernc.org/sqlite** | Pure Go — no CGo, so `CGO_ENABLED=0` yields a fully static binary with **zero C toolchain on the VPS**; trivial cross-compile from the Mac to the Linux VPS; embeds a current SQLite (3.5x); no shared-library/glibc coupling. Requires Go 1.25+ (local is 1.26.3). | Marginally slower than the C engine on heavy workloads — irrelevant here (batch writes + cache-first reads, low QPS). |
| **mattn/go-sqlite3** | Thin binding over the canonical C SQLite; marginally faster; battle-tested. | CGo: needs a C compiler to build, defeats the static-binary/zero-toolchain goal, complicates cross-compilation, and couples the artifact to the target's libc. Directly contradicts zero-ops. |

### 3. Data store: SQLite vs Postgres vs JSONL files

| Option | Pros | Cons |
|---|---|---|
| **SQLite (embedded)** | Zero server to run/patch/back up — the store is one file (back up = copy the file). Fits "single writer + concurrent readers" exactly via **WAL mode**. Real SQL (indexes, transactions, JSON columns) for upsert, per-event transactions, change_log diffing, and cursor pagination. No network hop, no auth surface, no extra RAM. Matches the zero-ops mandate. | Single-host (acceptable — one VPS, one app). Concurrency must be configured deliberately: WAL + `busy_timeout` + single-writer connection + a read-only handle (specified in `PLAN.md` Task 0.3). |
| **Postgres** | Strong concurrency, network access, mature tooling; natural if this grew into a multi-node service. | A server to install, secure, patch, tune, and back up on a shared low-spec VPS — pure operational and memory overhead for a one-writer/6h batch with no multi-host need. Violates zero-ops; defers cost the project explicitly wants near $0. |
| **JSONL / flat files** | Simplest possible store; matches the v0.1 prototype seed format; trivially diffable and append-friendly. | No indexes, transactions, or concurrency control. Change detection, per-event idempotent upsert, cursor pagination, and safe read-during-write would all be hand-rolled and race-prone. Good as the *human curation seed* (kept in `prototype/`), wrong as the *automated system of record*. |

### 4. Deploy shape: systemd single binary vs per-app Docker Compose (VPS runbook)

The VPS runbook convention is per-app **Docker Compose** under `/srv/developer`
behind a single shared `/etc/caddy/Caddyfile` (site block append → `caddy
validate` → `systemctl reload caddy`).

| Option | Pros | Cons |
|---|---|---|
| **systemd unit running one static Go binary** | No container layer, daemon, or image registry on the shared box — lowest possible idle memory and operational surface, which is the whole point of choosing pure-Go + SQLite. The SQLite file lives on a host bind path (e.g. `/srv/developer/events-intel/data/`), trivially backed up; the cron `ingest` and the API share it directly with no Docker volume/networking indirection. `systemctl`/journald give logs, restart-on-failure, and a single-flight-friendly oneshot for cron. | Diverges from the project's documented Docker Compose convention; needs an explicit exception (this ADR) and care that the cron `ingest` process and the resident API agree on the same on-disk WAL file. |
| **Per-app Docker Compose (runbook default)** | Consistent with the documented runbook; isolation; reproducible image. | A container runtime, image build/ship, and a Docker volume around a single local file — overhead that contradicts the zero-toolchain rationale that motivated pure-Go in the first place. Sharing one SQLite WAL file between the API container and a cron-launched ingest container adds avoidable volume/locking complexity. |

## Decision

Adopt **Go + `modernc.org/sqlite` + a single embedded SQLite database file**:

- **Go** for both the ingest batch and the read API — one static binary, low
  memory, stdlib HTTP, strong typing for the versioned schema.
- **`modernc.org/sqlite` (pure Go, CGo-free)** so `CGO_ENABLED=0` produces a
  fully static, cross-compiled-from-Mac binary that runs on the VPS with **no C
  toolchain and no shared libraries**. Build requires Go 1.25+ (local 1.26.3).
- **SQLite as the single system of record**, opened in **WAL mode** with
  `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`; the writer uses a
  single connection (`SetMaxOpenConns(1)`) and the API uses a separate read-only
  (`?mode=ro`) handle — supporting the one-writer / many-reader pattern (per
  `PLAN.md` Task 0.3). The `prototype/*.jsonl` seed stays as the human curation
  artifact, not the runtime store.

**Deploy shape:** deploy as a **systemd-managed single static binary** under
`/srv/developer/events-intel/`, taking an explicit, ADR-justified exception to
the per-app Docker Compose runbook convention. Rationale: containerizing a
single CGo-free static binary plus a one-file SQLite store would reintroduce
exactly the runtime/toolchain overhead the pure-Go choice exists to avoid, and
would complicate sharing one WAL file between the resident API and the
cron-launched `ingest`. Caddy integration still follows the runbook (append a
site block to the single `/etc/caddy/Caddyfile` → `caddy validate` →
`systemctl reload caddy`), so only the app-process supervisor differs from the
default. If a future service in this workspace needs multi-container topology or
host isolation, revisit per the conditions below.

## Consequences

### Positive

- **Read-path COGS ≈ $0 / zero-ops:** reads are served from a local SQLite file
  with cache headers and no LLM calls — satisfying the `IDEA.md` cost mandate.
  No database server, container runtime, or build toolchain to run on the shared
  VPS; backup is "copy one file."
- **One artifact:** `CGO_ENABLED=0 go build` yields a single static binary that
  cross-compiles from the Mac and runs on the Linux VPS unchanged; the same
  binary provides the API and the `ingest` subcommand.
- **Right-sized concurrency:** WAL + `busy_timeout` + single-writer + read-only
  handle directly support read-during-write (`PLAN.md` AC-9) without a
  client/server RDBMS.
- **Schema discipline:** Go's static typing and SQLite's real SQL/transactions
  back the strict v0.1 schema, idempotent per-event upserts, and the change_log
  diff feed.

### Negative / Trade-offs

- **Single-host store.** No built-in horizontal scaling or multi-writer support.
  Acceptable for an MVP on one VPS; revisit if the service must scale out.
- **Concurrency is configuration, not default.** Forgetting WAL / `busy_timeout`
  / RO-handle would surface as `SQLITE_BUSY` under read-during-write — mitigated
  by pinning these PRAGMAs on every connection DSN (Task 0.3) and the AC-9 smoke
  test.
- **Driver performance ceiling.** `modernc.org/sqlite` is slower than the C
  engine under heavy load; immaterial for a 6h batch + low-QPS cache-first API.
- **Runbook divergence.** The systemd-binary deploy departs from the Docker
  Compose convention and requires this documented exception and consistency
  between the cron `ingest` and the API over the shared WAL file.

### Follow-ups

- `PLAN.md` Task 0.3 implements the module layout, DDL, indexes, and the
  WAL/`busy_timeout`/RO-handle connection setup decided here.
- ADR 0002 (Task 0.2) records the broader automation-first supersession of the
  manual-dataset-first approach and updates `DECISIONS.md`.

## Reopen / Revisit Conditions

Revisit this decision if any of the following hold:

- The service must run on more than one host or needs multi-writer concurrency
  (consider Postgres).
- SQLite write contention or DB size becomes a measured bottleneck under real
  ingest volume.
- A future workspace service legitimately needs container isolation or a
  multi-container topology, making the Docker Compose runbook the better fit.
- The pure-Go driver's performance becomes a measured constraint (consider
  mattn/go-sqlite3, accepting CGo).

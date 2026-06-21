# 0002 - Supersede manual-dataset-first with automation-first ingestion

## Metadata

- Status: accepted
- Date: 2026-06-21
- Decision Maker: smpain
- Related Project: `event-intelligence-api`
- Supersedes: `DECISIONS.md` (Promote AI Industry Event Intelligence API into a project workspace, 2026-06-21) — specifically its "manual-dataset-first before implementation" consequence
- Authoritative Execution Plan: `.tasks/260621-coex-kintex-ingestion/PLAN.md`

## Context

The original promotion decision (`DECISIONS.md`, 2026-06-21) and the root shaping
`PLAN.md` agreed on a manual-dataset-first sequencing: build a 30-day manual
coverage dataset to validate source quality and field reliability **before**
writing any crawler, UI, or API code. That gate is captured in the original
consequence "Implementation should not start until the manual dataset/schema step
is done or explicitly superseded by a later decision." This ADR is that explicit
supersession.

Three things changed between promotion and 2026-06-21:

1. **The schema is already validated.** `prototype/manual-dataset-schema.md` (v0.1)
   fixed the first target user/workflow (founders/operators), the file format
   (JSONL), the durable-ID convention, missing-field tracking, summary copyright
   rule, date/null conventions, and the controlled taxonomy. The manual dataset's
   primary purpose — proving the data model before code — is satisfied at the
   schema level.

2. **Source feasibility was verified, not assumed.** A grounded preflight
   (`.tasks/260621-coex-kintex-ingestion/PREFLIGHT.md`, 2026-06-21) re-probed live
   sources: COEX and KINTEX detail pages are confirmed **static, field-rich HTML**,
   so an HTTP-first / CDP-fallback deterministic parser is feasible without a
   browser. Venue pages carry no registration/exhibitor deadlines, confirming the
   "action fields = null + missing_fields in v1" honesty posture. The remaining
   unknown (KINTEX `list.do` vs `clist.do` discovery endpoint) is converted into an
   explicit in-plan discovery spike (Task 2.3) rather than a blocking unknown.

3. **Hand-curation does not scale to freshness.** The real workload for this product
   is freshness, change tracking, and provenance (per `IDEA.md`). A manually
   maintained JSONL file cannot deliver `last_checked_at`, change feeds, or periodic
   re-checking at any useful cadence. Continuing to hand-fill records would burn the
   one scarce resource (curation time) on work that the deterministic pipeline must
   do anyway.

## Decision

Adopt an **automation-first** approach for COEX/KINTEX ingestion, superseding the
manual-dataset-first gate. Concretely:

- Keep the manual dataset schema (`prototype/manual-dataset-schema.md` v0.1) as the
  **data contract / SSOT for fields**, but stop treating "fill a 30-day manual
  dataset by hand" as a precondition for writing ingestion code.
- Build the ingestion backend now: deterministic HTTP fetch (with SSRF egress
  guard), source adapters + deterministic parsers, rule-based classification,
  normalization into the v0.1 schema, a SQLite canonical store with change_log and
  raw snapshots, a 6h batch + cron, and a cache-first read-only API.
- Honor the cost-efficiency mandate: **zero LLM on the read path and zero LLM in v1
  ingestion** (deterministic parsers + rule-based classification; cheap-model
  classification fallback is deferred to a follow-up plan). Read COGS stays ~0,
  served from SQLite + cache headers.
- The authoritative, executable specification for this work — exact DDL, PRAGMAs
  (`journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`,
  single writer + RO API handle), content_hash domain, disappearance policy,
  circuit breaker, API wire contracts, quota, and acceptance criteria — is
  `.tasks/260621-coex-kintex-ingestion/PLAN.md`. The root `PLAN.md` remains the
  shaping/strategy document and now points at the execution plan.

## Alternatives Considered

| Option | Pros | Cons |
|---|---|---|
| Keep manual-dataset-first (status quo) | Lowest upfront code; forces source inspection by hand | Curation time spent on throwaway data; cannot deliver freshness/change tracking; schema is already validated so the gate adds little |
| Automation-first (chosen) | Delivers freshness/change feed/provenance — the actual moat; reuses validated v0.1 schema as contract; deterministic parsers keep COGS ~0 | Requires building fetch/parse/store/API now; parser breakage risk (mitigated by golden fixtures + circuit breaker) |
| Hybrid: manual seed + parallel automation | Some hand-validated rows immediately | Two sources of truth for the same data; reconciliation overhead; no clear owner for conflicts |

## Consequences

- **Positive:** The product builds toward its real value (freshness, change
  tracking, provenance, durable IDs, schema versioning) instead of a static
  hand-maintained file. The validated v0.1 schema is reused as the normalization
  target, so no design work is discarded. Read path stays cost-efficient (no LLM,
  cache-first), satisfying the workspace cost-efficiency mandate.
- **Negative / risk:** Parser correctness now depends on source HTML stability.
  Mitigated by golden fixtures (`make refresh-fixtures`), a coverage/field-charge
  circuit breaker (no diff/upsert on 0/sudden-drop discovery, existing data
  preserved), a disappearance policy (events absent from a run are not
  deleted/changed, only `last_checked_at` goes stale), per-item `recover()`
  isolation, and idempotency tests.
- **Sequencing:** The original consequence "Implementation should not start until
  the manual dataset/schema step is done or explicitly superseded by a later
  decision" is now satisfied by this ADR (explicit supersession). Implementation
  proceeds via `.tasks/260621-coex-kintex-ingestion/PLAN.md`.
- **No history deleted.** `DECISIONS.md` retains the original promotion record and
  is amended to reference this ADR.

## Reopen Or Revisit Conditions

Revisit if deterministic parsing proves unreliable across source HTML changes
(circuit breaker firing repeatedly), if maintenance cost of the parsers exceeds
the cost of curation it replaced, or if a richer enrichment step genuinely
requires reintroducing a curated/human-in-the-loop dataset.

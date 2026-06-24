# 0003 - Cross-source content-key de-duplication and canonical selection

## Metadata

- Status: accepted
- Date: 2026-06-24
- Decision Maker: smpain
- Related Project: `event-intelligence-api`
- Authoritative Execution Plan: `.tasks/260623-showala-source/PLAN.md`

## Context

A new source adapter (SHOWALA, an exhibition portal) was added to catch
externally-organized KINTEX hall rentals — e.g. "2026 로보월드" / RoboWorld — that
KINTEX's own `list.do` calendar does not publish until close to the event. The
SHOWALA adapter is scoped to KINTEX events, so it overlaps with the existing
`kintex` adapter for any event that appears on BOTH the venue calendar and the
portal.

The store identifies events solely by `event_id` (TEXT PRIMARY KEY), and every
adapter mints its own source-prefixed id (`kintex-<seq>`, `showala-<idx>`). There
was no cross-source identity: the same real-world event discovered by two adapters
became two rows, and would surface twice in the public API. `series_id` (the
schema's general cross-source grouping field) is explicitly out of v1 scope and is
always null. So a targeted de-dup mechanism was needed for the venue-overlap case
without taking on general series detection.

## Decision

Introduce **ingest-time content-key de-duplication** in the store layer:

1. **content_key** — a conservative, source-independent fingerprint
   `normalize(name) | start_date(ISO) | venue_id`. `normalize(name)` lowercases and
   strips only the year, the edition marker (`제N회` / `Nth`), and non-alphanumerics.
   The key is NULL when start_date or venue_id is absent (such an event is never
   de-duped). For SHOWALA, `venue_id` is resolved from the scraped venue NAME
   (킨텍스/KINTEX → `kintex`) so its key aligns with the kintex adapter's.
2. **superseded** — a soft flag. Each `content_key` cluster keeps exactly one
   canonical row (`superseded=0`); the rest are `superseded=1` and excluded from
   every read path (list, detail, sources, category facet, change feed). Rows are
   never deleted; provenance / change_log / raw_snapshot are preserved.
3. **Canonical selection by source authority** — venue-native adapters
   (coex/kintex/benchmark) outrank the aggregator (showala); ties break on the
   lexicographically smallest `event_id`. An unknown source prefix defaults to HIGH
   authority so a classification gap can only ever show a possible duplicate, never
   hide a real event.
4. **Reconciliation runs after every applied event** (new/updated/unchanged) and
   re-elects the whole cluster, so the outcome is order-independent across separate
   `ApplyBatch` calls and idempotent on re-run. When an event's key changes, its
   OLD cluster is re-reconciled too, so a departing canonical never orphans its
   cluster with zero visible rows.

`content_key` and `superseded` are operational/derived columns, deliberately
EXCLUDED from `content_hash` (and thus from `CanonicalFields`/the change feed), so
adding them does not mark existing rows "updated" or perturb the
`(updated_at,event_id)` cursor.

## Alternatives considered

- **General `series_id` detection** — the schema's intended cross-source grouping.
  Rejected for now: it is a broader, fuzzier problem (multi-venue, multi-edition
  series) explicitly deferred past v1. content_key solves the concrete venue-overlap
  duplication without that scope.
- **Content-based primary key** (hash instead of `source-<id>`). Rejected: a
  breaking change to the immutable `event_id` PRIMARY KEY and all stored ids.
- **Post-ingest reconciliation job** (separate merge pass). Rejected: more moving
  parts and a window of visible duplicates; in-line reconciliation keeps the store
  always-consistent.
- **Hard delete of duplicates.** Rejected: loses provenance and is irreversible if
  the authority/normalization rule later changes; a soft flag is reversible.

## Consequences

- The public API shows one row per real KINTEX event even when both the venue and
  the aggregator carry it; the venue-sourced record wins. Aggregator-only events
  (RoboWorld) appear normally with an `aggregator`-type `sources[]` entry.
- De-dup is biased to **fail open**: conservative name normalization means a missed
  match shows a duplicate (visible, correctable) rather than a false merge (a hidden
  real event).
- A deep link to a superseded `event_id` resolves as not-found; the canonical row is
  the one the listing links to.
- Migration adds `content_key` + `superseded` columns (and indexes) in three
  separate idempotent files so a crash mid-migration cannot leave the schema
  permanently missing a column.

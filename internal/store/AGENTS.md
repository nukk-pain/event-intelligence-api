# Store Package Guide

## Scope

- Owns the canonical SQLite store, embedded migrations, semantic hashing and
  diffs, cross-source deduplication, read queries, and ingest audit records.
- Persistence semantics here are API-visible; treat migration, cursor, and
  canonicalization changes as contract changes.

## Where To Look

- `db.go`, `migrations/` — CGo-free SQLite handles, WAL pragmas, and ordered
  idempotent schema changes.
- `store.go`, `diff.go` — canonical field projection, transaction boundaries,
  hash/diff consistency, and `ApplyBatch`.
- `dedup.go` — conservative content keys and canonical-row election.
- `query.go`, `query_scan.go` — read reconstruction, filters, and keyset cursors.
- `ingest.go` — per-item errors, source anomalies, and last-successful baselines.

## Invariants

- Use `OpenWrite` for the single writer and `OpenRead` for read-only pools. WAL,
  busy timeout, and foreign keys are applied through every connection's DSN.
- Migrations are embedded and applied in filename order; make every migration
  repeat-safe and audit every affected INSERT/scan projection.
- `CanonicalFields` is the single semantic projection for both `ContentHash`
  and `Diff`. Never let freshness, identity, curation, or field ordering create
  false changes.
- One event upsert, its change rows, and optional raw snapshot are atomic.
  Unchanged events must not advance `updated_at`; absent batch members are never
  deleted or mutated.
- Deadlines ratchet only while plausible for the same event edition. Silence
  from enrichment must not erase a verified fact or revive an implausible one.
- Deduplication is soft and conservative: hide superseded rows, preserve their
  history, and prefer a visible duplicate over a false merge.
- Keep list pagination ordered and cursor-filtered on the same unique key.
  Public reads exclude superseded rows consistently across list/detail/change.

## Verification

- Full package: `go test ./internal/store -count=1`.
- Hash/diff/apply: `go test ./internal/store -run 'ContentHash|Diff|ApplyBatch' -count=1`.
- Migrations/dedup: `go test ./internal/store -run 'Migrate|Dedup|ContentKey' -count=1`.
- Query/cursor work also requires the matching `internal/api` tests.
- Run `go test -race ./internal/store -count=1` for connection, batch, or shared
  state changes.

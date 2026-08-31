# Agent Package Guide

## Scope

- Owns OpenAI-compatible model calls, event-query parsing, enrichment, and the
  profile-driven source-discovery action loop.
- Does not crawl arbitrary URLs itself. Search and page-open capabilities enter
  through injected `SearchTool` and `PageOpener` interfaces.

## Where To Look

- `discovery_types.go` — profiles, public request/result contracts, and hard
  ceilings.
- `discover.go` — request-local action loop and termination accounting.
- `discovery_candidates.go`, `discovery_url.go` — candidate normalization,
  profile admission, provenance, and URL validation.
- `discovery_action.go`, `discovery_model.go` — JSON action wire contract and
  model budget reservation.
- `discovery_open.go` — offered/pending URL gate and metadata-only page rescue.
- `discovery_yield.go` — count-only outcome and yield trace.
- `llm.go`, `search_tavily.go` — backend and search-provider adapters.

## Invariants

- Profiles and budgets are server-owned. Caller values are clamped to hard
  ceilings: 2 searches, 8 turns/calls, 3 opens, 4,000 completion tokens, 30
  candidates, 800 goal runes, and 60 seconds per model call.
- Redact contacts and bound every goal, query, title, snippet, metadata field,
  and reason before it can enter a model prompt or response.
- Only canonical, public HTTP(S) URLs offered by the tool may be accepted or
  opened. Reject userinfo, private/local targets, rewritten URLs, and
  profile-disallowed patterns.
- Treat crawled/search text as delimited untrusted data. Corrections are fixed
  server strings and never echo hostile model or page content.
- Preserve request isolation and provenance. `yield_trace` is additive,
  count-only, and must balance parsed, accepted, and dropped selections.
- A failed page open is non-fatal; it must not overwrite existing candidate
  fields or create a new fetch target.
- Keep `SearchTool` compatibility when extending richer discovery metadata.

## Verification

- Full package: `go test ./internal/agent -count=1`.
- Budgets and cancellation: `go test ./internal/agent -run 'Budget|Cancellation|Deadline' -count=1`.
- URL/provenance boundary: `go test ./internal/agent -run 'Candidate|Provenance|Open' -count=1`.
- Choreography and trace changes: `go test ./internal/agent -run 'Choreography|YieldTrace' -count=1`.
- Run `go test -race ./internal/agent -count=1` when changing request-local
  state or shared adapters.

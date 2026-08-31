# Public Discovery Package Guide

## Scope

- Crawls a server-owned catalog of public seeds through bounded discovery
  protocols, then adapts the immutable result to `agent.SearchTool`.
- Does not accept caller-supplied seeds, arbitrary backend settings, or private
  network targets.

## Where To Look

- `catalog.go`, `types.go` — curated seeds, hard limits, result/provenance
  contracts, and seed-outcome taxonomy.
- `crawl.go`, `crawl_fetch.go`, `frontier.go` — traversal order, validation, and
  budget termination.
- `parser_*.go`, `crawl_parse.go` — sitemap, feed, HTTP Link, and HTML parsing.
- `canonical.go` — canonical URL and public-host boundary.
- `agent_search.go`, `opener.go` — agent adapter, provenance restoration, and
  bounded second-hop page reads.

## Invariants

- Production limits are fixed at 8 seeds, depth 2, 12 protocol documents, 24
  HTML pages, 30 candidates, 64 transport attempts, 6 MiB aggregate bytes, and
  a 60-second job timeout.
- Each provider crawls at most once; repeated searches only re-rank its immutable
  candidate set. Create a fresh provider/tool per public request.
- Return only destination-fetched, validated candidates. A sitemap or link is
  not sufficient evidence that the destination is safe or readable.
- Canonical URL identity drives frontier and candidate deduplication while
  provenance retains the literal raw traversal value for final output.
- Account every seed exactly once with a bounded, count-only outcome. Reserve
  candidate slots so protocol children cannot starve seed candidates.
- Page opening reuses the crawler's SSRF, robots, MIME, rate, and body policy;
  it returns bounded contact-redacted text and never infers missing structure.
- Prompt-injection-looking page text remains data. Never execute or reinterpret
  it as crawler or model instructions.

## Verification

- Full package: `go test ./internal/publicdiscovery -count=1`.
- Protocol/provenance: `go test ./internal/publicdiscovery -run 'Protocol|Provenance|Canonical' -count=1`.
- Budgets/accounting: `go test ./internal/publicdiscovery -run 'Budget|Seed|Truncat' -count=1`.
- Hostile inputs and opener: `go test ./internal/publicdiscovery -run 'Adversarial|Private|Opener' -count=1`.
- Shared state changes require `go test -race ./internal/publicdiscovery -count=1`.

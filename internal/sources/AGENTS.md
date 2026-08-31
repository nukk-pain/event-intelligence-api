# Sources Package Guide

## Scope

- Defines the adapter contract and registry; child packages implement concrete
  venue, association, aggregator, and reviewed benchmark sources.
- Adapters discover durable detail refs and parse raw source fields. They do not
  normalize dates, taxonomy, confidence, or `missing_fields`.

## Structure

- `source.go` — `Source`, `Ref`, raw `ParsedEvent`, action signals, provenance,
  and registry.
- `coex/`, `kintex/` — venue-native schedule/detail adapters.
- `aiia/`, `akei/` — Korean association adapters.
- `showala/` — aggregator adapter with referer/pagination behavior.
- `benchmark/` — reviewed catalog plus deterministic page extraction/fallback.

## Invariants

- `Ref.EventID` is durable and source-prefixed; parsed identity must remain tied
  to the discovered ref and source URL.
- Preserve scraped names, dates, venue text, and nullable absence exactly in
  `ParsedEvent`. Parsing and classification belong downstream in `normalize`.
- `ClassifyText` is the source-owned title/organizer projection used by the
  shared classifier; do not embed source-specific classification rules there.
- Action fields require an official or reviewed source and matching
  `ParsedSource` provenance. Missing facts stay nil; never infer deadlines,
  cost, or capabilities from marketing language alone.
- Discovery and parse are deterministic and side-effect free apart from their
  injected `fetch.Fetcher` calls.
- A new adapter is incomplete until `cmd/eventsintel/main.go` registers it,
  `config.Default()` includes a source row, and required hosts are reviewed in
  `fetch.ProductionAllowedHosts`/caller options.
- Use static fixtures under the adapter's `testdata/`; `make refresh-fixtures`
  is currently not implemented.

## Verification

- Contract/registry: `go test ./internal/sources -count=1`.
- One adapter: `go test ./internal/sources/<adapter> -count=1`.
- All adapters: `go test ./internal/sources/... -count=1`.
- Verify both discovery and parse fixtures, plus exact main/config/fetch wiring,
  before claiming a source is integrated.

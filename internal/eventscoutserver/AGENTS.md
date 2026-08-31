# Event Scout Server Package Guide

## Scope

- Implements the isolated anonymous discovery HTTP service, separate from the
  cache-first `internal/api` router.
- Owns request parsing, quotas, concurrency/deadline gates, trusted-proxy client
  identity, sanitized responses/logging, and discovery-runner wiring.

## Where To Look

- `handler.go`, `types.go`, `response.go` — explicit routes and JSON contracts.
- `quota.go`, `proxy.go`, `middleware.go` — anonymous limits, identity, request
  IDs, panic recovery, and privacy-safe logs.
- `discovery.go` — fresh request-local public crawl/opener plus agent loop.
- `config.go`, `server.go` — startup validation and graceful lifecycle.
- `../../cmd/eventscout-server/README.md` — operator surface; code and tests win
  if historical limits there drift.

## Invariants

- Expose only `POST|OPTIONS /v1/discover`, `GET /healthz`, and `GET /readyz`.
  The normal read API must not mount these routes.
- The JSON body is exactly one `goal` field: 4 KiB body cap, 1–800 runes, valid
  UTF-8, no unknown/duplicate fields, and no trailing document.
- Server-owned limits are not request parameters: 2 requests/10 minutes,
  24/day, 2 active jobs without queueing, and at most a 60-second request.
- Trust `X-Forwarded-For` only when the direct peer matches an exact configured
  CIDR; otherwise quota by peer address.
- Build a fresh public search tool and opener per request. The runner allows at
  most two offered-candidate page opens and restores crawler provenance before
  serialization.
- Responses are `no-store`. Logs and errors never contain the goal, page/model
  content, upstream errors, panic values, API keys, or authorization material.
- Success traces are bounded, count-only, request-local, and serialize empty
  source/reason collections as arrays. Error envelopes remain trace-free.
- Startup currently validates the named runtime backend; changing provider
  selection requires contract tests and workspace model-registry review, not a
  silent fallback.

## Verification

- Full package: `go test ./internal/eventscoutserver -count=1`.
- HTTP contract: `go test ./internal/eventscoutserver -run 'Handler|Router|ManualHTTP' -count=1`.
- Limits/privacy: `go test ./internal/eventscoutserver -run 'Quota|Limit|Privacy|Proxy' -count=1`.
- Runner/config: `go test ./internal/eventscoutserver -run 'Discovery|RuntimeConfig' -count=1`.
- Shared quota or request-state changes require `go test -race ./internal/eventscoutserver -count=1`.

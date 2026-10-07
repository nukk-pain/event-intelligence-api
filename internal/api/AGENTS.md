# Read API Package Guide

## Scope

- Implements the normal public, read-only, cache-first API and embedded human
  UI route.
- This router must remain LLM-free and must never expose the isolated
  `eventscoutserver` discovery route.

## Where To Look

- `api.go`, `events.go`, `detail.go`, `meta.go` — route table and handlers.
- `middleware.go`, `cache.go` — load shedding, quota, trusted proxy identity,
  response caps, ETags, and cache headers.
- `cursor.go`, `envelope.go` — stable pagination and collection wire shapes.
- `render.go` — JSON/Markdown negotiation and injection-safe Markdown views.
- `../../static/openapi.yaml`, `../../static/llms.txt` — embedded public
  contracts that must track handler behavior.

## Invariants

- Routes are GET-only. JSON is the default; Markdown is a rendering of the same
  fields selected by `?format` or `Accept: text/markdown`.
- Preserve one error envelope, explicit status mapping, non-null collection
  arrays, effective limit echo, and keyset cursor semantics.
- Keep the middleware order: concurrency cap, per-IP quota, ETag conditional
  handling, response-size cap, then handlers.
- Trust one valid `X-Real-Client-IP` only from loopback socket peers. Legacy
  CF/XFF headers never establish identity. Normalize mapped IPv4 before /64.
- Every negotiated response varies on `Accept`; root HTML and root JSON must not
  poison each other's shared cache.
- Markdown free text is escaped and URLs are bounded autolinks. Do not allow
  stored text to inject rows, links, or HTML.
- Any route/schema/field change updates handler tests, `static/openapi.yaml`,
  `static/llms.txt`, and route/OpenAPI consistency tests together.

## Verification

- Full package: `go test ./internal/api -count=1`.
- Contract/routes: `go test ./internal/api -run 'Route|OpenAPI|Schema|Meta' -count=1`.
- Cache/middleware: `go test ./internal/api -run 'Cache|ETag|Quota|Concurrency|ResponseSize' -count=1`.
- Negotiation/injection: `go test ./internal/api -run 'Render|Markdown|Root' -count=1`.
- Before deployment, run the root gates and finish with `deploy/verify.sh` as
  required by the repository guide.

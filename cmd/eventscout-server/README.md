# eventscout-server — anonymous public discovery HTTP service

`eventscout-server` is a separate, isolated HTTP binary for one public
discovery operation. Callers do not create accounts, obtain API keys, choose an
LLM, submit seed URLs, or access private networks. The server owns the Solar
backend and the public-crawl catalog; callers submit only a short discovery
goal.

This binary is not the normal `eventsintel` read API. The existing internal API
at [`events.nukk.net`](https://events.nukk.net) remains read-only, cache-first,
and LLM-free on normal reads. Its `api.Router` deliberately does not expose
`/v1/discover`; run this binary separately and place it behind the deployment's
existing proxy/auth boundary if it is made reachable from the Internet.

## Operator startup

The Solar key is operator-only. It is required at startup, even when
`EVENTSINTEL_LOCAL_BASE_URL` is configured; a local model is not a server
fallback. The key is read from the process environment, never from an HTTP
request, and must not be committed or logged.

```sh
# Keep the real value in the shell/secret manager only.
export EVENTSINTEL_SOLAR_API_KEY='...'
export EVENTSINTEL_SOLAR_BASE_URL='https://api.upstage.ai/v1' # optional override
export EVENTSINTEL_SOLAR_MODEL='solar-open2'                  # optional override

# eventscout-server reads EVENTSCOUT_HTTP_ADDR; its default is 127.0.0.1:8081.
EVENTSCOUT_HTTP_ADDR='127.0.0.1:8081' \
  go run ./cmd/eventscout-server
```

If the key is missing, startup fails closed with
`eventscout server: Solar backend is required` and no listener is started. A
local backend being available does not change this requirement. `EVENTSCOUT_TRUSTED_PROXY_CIDRS`
is optional; set it only to the exact proxy CIDRs that are allowed to supply the
first valid `X-Forwarded-For` address for per-client quota accounting.

```sh
# Reproducible no-key startup check (expected non-zero exit and the error above):
env -u EVENTSINTEL_SOLAR_API_KEY \
  EVENTSINTEL_LOCAL_BASE_URL='http://127.0.0.1:18900/v1' \
  go run ./cmd/eventscout-server
```

For a no-third-party-key test smoke that does not intentionally start the
Solar-gated binary, see [`cmd/eventscout/README.md`](../eventscout/README.md).
For the non-gating operator Solar + curated-public smoke, run
`./scripts/smoke-solar-public-discovery.sh` from the repository root. It skips
cleanly when the operator key is unavailable and never prints the key,
authorization material, or goal; with a key it applies a bounded wall-clock
timeout and reports a sanitized pass/fail result.

## HTTP contract

The service exposes only these routes:

| Method and path | Success | Notes |
| --- | --- | --- |
| `POST /v1/discover` | `200` JSON | body must be exactly `{"goal":"..."}` |
| `OPTIONS /v1/discover` | `204` | credentialless CORS preflight |
| `GET /healthz` | `200` JSON | `{"status":"ok"}` |
| `GET /readyz` | `200` JSON | `{"status":"ready"}` |

`POST /v1/discover` returns `{ "sources": [...], "meta": {...} }`. The metadata
always reports `provider: "public"` and the server-selected `profile: "events"`.
`sources` contains validated public event-source URLs and provenance. The
response is `Cache-Control: no-store`; it is not part of the internal
cache-first read API. CORS allows `Origin: *`, `POST, OPTIONS`, and
`Content-Type`, with no credentialed CORS.

No arbitrary URL or backend is accepted. The JSON decoder rejects unknown
fields, arrays, trailing JSON, malformed JSON, wrong content types, and blank
goals. A URL placed inside the goal remains untrusted text; it cannot add a seed
to the server-owned catalog.

## Anonymous limits and errors

There is no caller signup or API key. Quota identity is the peer IP, or the
first valid forwarded IP only when the peer is in `EVENTSCOUT_TRUSTED_PROXY_CIDRS`.
Input validation runs before quota accounting, so malformed requests are
rejected as `400 invalid_request` without consuming a discovery slot.
The exact request limits are:

| Limit | Value | Observable behavior |
| --- | ---: | --- |
| Request body | 4 KiB | larger/invalid body → `400 invalid_request` |
| Goal | 1–800 Unicode runes | blank or over 800 → `400 invalid_request` |
| Per-client request window | 2 requests / 10 minutes | next request → `429 rate_limited` |
| Per-client daily window | 24 requests / 24 hours | next request → `429 rate_limited` |
| Active discovery jobs | 2 | third simultaneous job → `503 server_busy` (not queued) |
| Server discovery deadline | 60 seconds | exceeded/cancelled upstream → `504 deadline_exceeded` |
| `Retry-After` | 1–600 seconds | present on `429` as header and JSON seconds field |

Public crawling is bounded per request: 6 curated seeds, traversal depth 2, 12
protocol documents, 24 HTML pages, 30 candidates, 64 transport attempts, 6 MiB
aggregate response bytes, 512 KiB per response, at most two redirects, one
retry for retryable upstream failures, and 30 outbound requests per host per
minute. The agent loop is separately capped at 2 rounds, 4 model calls, 4,000
completion tokens, 30 total candidates (5 per source), an 800-rune goal, and a
20-second per-call model timeout (hard maximum 60 seconds). When a bound is
reached the response remains honest and includes `meta.truncation_reasons`;
the server never silently expands the budget.

Errors use a stable JSON envelope:

```json
{"error":{"code":"rate_limited","message":"request quota exceeded","retry_after_seconds":600}}
```

The relevant codes are `invalid_request` (400), `method_not_allowed` (405),
`not_found` (404), `rate_limited` (429), `server_busy` (503),
`deadline_exceeded` (504), and sanitized `internal_error` (500). `429` includes
`Retry-After`; other errors do not expose upstream response bodies, goals,
credentials, or panic values. Every response carries an `X-Request-ID`.

## Example request

```sh
curl -sS \
  -H 'Content-Type: application/json' \
  --data '{"goal":"한국 AI·로봇 산업의 공식 행사 일정 소스"}' \
  http://127.0.0.1:8081/v1/discover
```

The service does not accept `seed`, `profile`, `backend`, `budget`, or arbitrary
URL fields. For the existing cache-first event data, use the normal read API and
its [OpenAPI contract](../../static/openapi.yaml) or [agent summary](../../static/llms.txt).

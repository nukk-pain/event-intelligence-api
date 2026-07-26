# eventscout — autonomous source-discovery agent (loop ①)

Given a goal, the model proposes search queries, a search tool runs them, and the
model judges which results are real event-listing sources worth crawling. The
model chooses the next search action for each round; the caller cannot supply a
seed URL, backend, profile, or crawl budget through the search goal.

## Provider selection

`public` is the default search provider. It is keyless for the caller and uses a
server-owned catalog of six public event sites (COEX, KINTEX, CES, NVIDIA GTC,
BIO International, and MEDICA). It follows only public HTTP(S) links discovered
from those seeds, checks robots.txt, and rejects private/loopback/link-local/
metadata destinations and URL userinfo. A goal containing a URL is still only
text; it does not inject an arbitrary crawl seed.

The other providers are explicit, optional modes:

| `-search-provider` | Use | Credential |
| --- | --- | --- |
| `public` (default) | bounded public crawl from the curated catalog | none for search |
| `fixture` | deterministic offline keyword fixture at `cmd/eventscout/fixtures/search.json` | none |
| `tavily` | credential-backed Tavily web-index search | `EVENTSINTEL_TAVILY_API_KEY` |

Fixture is not the default. Use it when an offline, repeatable transcript is
needed; use Tavily only when a real third-party search is intentionally desired.
The public HTTP service described in [`cmd/eventscout-server/README.md`](../eventscout-server/README.md)
uses `public` internally and does not expose this provider flag to callers.

## Run the CLI

The local model backend is selected first by default (`EVENTSINTEL_LOCAL_BASE_URL`
defaults to `http://127.0.0.1:18900/v1`). Solar is an explicit operator choice;
set `EVENTSINTEL_SOLAR_API_KEY` and pass `-backend solar` when using it. The key
belongs in the process environment only, never in a goal, fixture, transcript,
or committed file.

```sh
# Curated public crawl (default provider; no Tavily key)
go run ./cmd/eventscout \
  -goal '2026년 이후 한국 AI 로봇 바이오 의료기기 공식 행사 소스 찾기'

# Deterministic offline fixture (explicit opt-in; no third-party network)
go run ./cmd/eventscout \
  -search-provider fixture \
  -backend local \
  -rounds 2 \
  -goal '한국 AI 로봇 행사 공식 소스 찾기'
```

The effective discovery guardrails are server-owned and hard-clamped even when
larger CLI flags are supplied: at most 2 rounds, 4 model calls, 4,000 total
completion tokens (1,000 reserved per call by default), 30 candidates total and
5 candidates per source, an 800-rune goal, and a 20-second per-model-call
default (never above 60 seconds). The public crawler adds 6 seeds, depth 2, 12
protocol documents, 24 HTML pages, 64 transport attempts, 30 candidates, 6 MiB
aggregate response bytes, 512 KiB per response, two redirects, and 30 requests
per host per minute. Truncation is reported in the public CLI provider's
`public_discovery.truncation_reasons` and in the server response's
`meta.truncation_reasons`; it is not a reason to accept an unvalidated URL.

## Yield diagnostics

For a completed successful `public` provider run, the CLI may add a
request-local, count-only `yield_trace` object with exactly these fields:
`outcome`, `terminal_reason`, `crawler_validated`, `offered`,
`prefilter_dropped`, `prefilter_reasons`, `proposal_calls`, `judge_calls`,
`judge_entries_parsed`, `judge_entries_dropped`, and `accepted`.

`prefilter_reasons` breaks the prefilter total into fixed count-only keys —
`invalid_url`, `url_pattern`, `missing_title`, `missing_location`,
`missing_date`, `past_date` — which always sum to `prefilter_dropped`. The
`public_discovery` block also reports `seed_candidates`, `skipped_documents`,
`malformed_documents`, and `seed_outcomes`. Seed pages are crawled before
sitemap children and are the only candidates guaranteed a title, so together
these say whether an empty offer set came from untitled crawl output, a profile
pattern, or seed pages that were fetched and rejected.

`seed_outcomes` accounts for every enqueued seed with exactly one of
`candidate`, `robots_disallowed`, `http_status`, `body_too_large`,
`unsupported_content`, `transport_error`, `duplicate`, `candidate_cap`, or
`not_attempted`; its `candidate` value always equals `seed_candidates`.

The trace classifies the run; it does not expose a goal, candidate, URL,
fetched content, model payload, or credential, and its counters are never
reused by another request. It is not a minimum-result assertion: a bounded
present-key smoke may validly report `accepted: 0` with its outcome and terminal
reason classification.

## Tavily mode (optional)

For a fully automated third-party search, provide the credential only through the
process environment or an ignored local `.env` file:

```sh
export EVENTSINTEL_SOLAR_API_KEY='...'
export EVENTSINTEL_TAVILY_API_KEY='...'

go run ./cmd/eventscout \
  -backend solar \
  -search-provider tavily \
  -rounds 2 \
  -goal '2026년 이후 한국 AI 로봇 바이오 의료기기 공식 행사 소스 찾기'
```

Without `EVENTSINTEL_TAVILY_API_KEY`, selecting `tavily` fails before a model or
search request is sent. As of 2026-07-18, Tavily documents 1,000 free API credits
per month without a credit card; a basic search costs one credit. See the
official [API credit documentation](https://docs.tavily.com/documentation/api-credits)
and [search endpoint](https://docs.tavily.com/documentation/api-reference/endpoint/search).

Only public event-discovery queries belong in this mode. Contact-like text is
redacted before a query leaves the process; result titles, snippets, and URLs are
sanitized, and malformed, private-network, localhost, credential-bearing, or
contact-bearing URLs are rejected. Tavily's [privacy policy](https://www.tavily.com/privacy)
says it collects search queries and may share parts with search-index providers,
so never put private, patient, clinical, account, or other personal data in a
goal or query.

## Zero-third-party-key smoke

This command exercises provider selection, the anonymous HTTP handler over a
local `httptest` server, quota/input/privacy tests, and the public-crawl safety
tests without an Upstage or Tavily credential and without external network
access:

```sh
env -u EVENTSINTEL_SOLAR_API_KEY -u EVENTSINTEL_TAVILY_API_KEY \
  EVENTSINTEL_LOCAL_BASE_URL=off \
  go test ./cmd/eventscout ./internal/eventscoutserver ./internal/publicdiscovery -count=1
```

The command is intentionally a test smoke, not a production-server startup:
`eventscout-server` requires the operator's Solar key at startup even when a
local backend is configured. See the server runbook for that explicit failure
and for the anonymous `POST /v1/discover` contract.

For the non-gating operator check that runs Solar against the curated `public`
provider, use [`scripts/smoke-solar-public-discovery.sh`](../../scripts/smoke-solar-public-discovery.sh)
from the repository root:

```sh
./scripts/smoke-solar-public-discovery.sh
```

It exits `0` with `SKIPPED_CREDENTIAL_UNAVAILABLE` when
`EVENTSINTEL_SOLAR_API_KEY` is absent; that path makes no model or network call.
With the operator key present, it runs one bounded Solar round, forces
`-search-provider public`, redacts captured output, and exits non-zero on a
timeout, unexpected provider, or missing fixed yield classification. Zero
accepted sources remain a valid classified observed result.

## Privacy and scope

The public service accepts only a goal string. Do not put secrets or personal
data in it. `eventscout-server` structured logs contain request ID, status,
duration, and fixed limit counters—not the goal, fetched page text, model
credentials, or provider error details. The interactive CLI prints its local
goal to its own terminal as part of its normal output, so treat that terminal
as sensitive and do not redirect it to shared logs.

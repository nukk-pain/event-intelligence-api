# Decision Record

## Metadata

- Decision: Promote AI Industry Event Intelligence API into a project workspace
- Status: accepted
- Date: 2026-06-21
- Decision Maker: smpain
- Related Project: `event-intelligence-api`
- Related Status: `shaping`

## Context

The idea began as a service that makes COEX, KINTEX, and domestic/international
AI, humanoid, bio, and medical-device event information readable for humans and
agent-friendly through an API.

During shaping, the following constraints were clarified:

- No monetization is planned at capture time.
- API access should be free, but quota-limited for operational protection.
- The target public domain is `events.nukk.net`.
- The existing Developer DigitalOcean VPS can host the MVP if it stays
  public-only, read-only, and cache-first.
- The next validation step is a 30-day manual coverage dataset.

## Options Considered

| Option | Pros | Cons |
|---|---|---|
| Keep as docs-only idea | Avoids premature project setup | Harder to run focused schema/prototype work |
| Promote to project workspace | Gives a clean place for plan, progress, decisions, references, and prototype artifacts | Requires maintaining local project docs |
| Start implementation immediately | Fast visible progress | Risks building UI/API before source quality and schema are validated |

## Evidence

- Source idea document exists with problem, user candidates, API concept,
  constraints, cost structure, efficiency strategy, unit economics, moat, and
  next action.
- Workspace rules require project promotion through a dry-run and starter docs.
- User approved the promotion dry-run on 2026-06-21.

## Rationale

Promote the item now because the direction is clear enough for independent
shaping work, but keep status at `shaping` because the data model and source
coverage still need validation before implementation.

## Consequences

- Project root is `/Users/smpain/Developer/event-intelligence-api/`.
- Local docs become the working surface for future sessions.
- Implementation should not start until the manual dataset/schema step is done
  or explicitly superseded by a later decision. **Superseded 2026-06-21 by
  `docs/decisions/0002-automation-first.md`**: the manual-dataset-first gate is
  explicitly lifted in favor of automation-first ingestion (the v0.1 schema is
  validated and source feasibility is verified); implementation now proceeds via
  `.tasks/260621-coex-kintex-ingestion/PLAN.md`.
- `docs/workspace/inventory/PROJECTS.md` should track this as an active shaping
  project.

## Follow-Up Actions

- [ ] Create the 30-day manual coverage dataset schema.
- [ ] Create a seed data file structure.
- [ ] Select the first target user/workflow.
- [ ] Draft the minimal API contract.

## Reopen Or Revisit Conditions

Revisit this decision if manual source coverage is too weak, maintenance cost is
too high, or a better project framing replaces the event intelligence API
concept.

## Supersedes

None. (This is the original promotion decision.)

## Superseded By

`docs/decisions/0002-automation-first.md` (2026-06-21) supersedes the
manual-dataset-first sequencing in this record. The promotion itself stands; only
the "complete a 30-day manual dataset before implementation" gate is replaced by
an automation-first approach. The validated v0.1 schema
(`prototype/manual-dataset-schema.md`) is retained as the data contract, and the
authoritative execution plan is `.tasks/260621-coex-kintex-ingestion/PLAN.md`.

---

## Decision: Show All COEX/KINTEX Venue Events In Date Browsing

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

The v0.1 implementation originally used `excluded=true` as both a taxonomy flag
and a default-list hiding rule. That made the browser and API miss COEX events
that users expected to see, including upcoming August 2026 events.

### Decision

Venue/date browsing must show all discovered COEX/KINTEX events. The
`excluded` field remains in the schema as a taxonomy-confidence flag, but it is
not a default visibility filter.

### Consequences

- `GET /api/v1/events` no longer excludes `excluded=true` rows by default.
- The public UI is a schedule browser first; category filters are optional.
- COEX discovery must include current schedule pages and pagination, not only
  WordPress sitemap shards.

---

## Decision: Default Homepage To Upcoming Categorized Events

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

The homepage is for quick human scanning. Showing past events and uncategorized
venue listings first made the useful events harder to see.

### Decision

The homepage defaults to upcoming events that have taxonomy categories. Users
can switch the event-scope dropdown to `모든 행사 보기` when they want the full
upcoming COEX/KINTEX venue schedule.

### Consequences

- Past events are not exposed in the homepage controls.
- The public API remains unchanged; this is a browser default and filtering
  policy.
- Developer/API links are moved out of the header. Only essential integration
  links remain in the footer.

---

## Decision: Default Venue API Reads To Current And Upcoming Events

- Status: accepted
- Date: 2026-06-25
- Decision Maker: smpain

### Context

The deployed API could look stale or partial when callers requested
`list=venue` without `since`: the raw keyset order could surface old stored
COEX/KINTEX rows before current schedules, even though the UI already supplied
an upcoming date floor.

### Decision

For `GET /api/v1/events?list=venue`, omitted `since` now defaults to today's
date in `Asia/Seoul`. Explicit `since` still wins, so clients can request older
venue rows intentionally.

### Consequences

- Domestic venue API reads are current/upcoming by default.
- Historical venue data remains available by passing an explicit older `since`.
- `list=all` and `list=benchmark` keep their existing behavior.

---

## Decision: Use One-Year KINTEX Listing Discovery

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

KINTEX `list.do` defaults to roughly one month of events and paginates the
result. Reading only the first default page caused the service to miss future
events that users expected to see, including August 2026 schedules.

### Decision

KINTEX discovery requests a rolling 365-day date range from the current Korean
date and follows every advertised listing page within that range.

### Consequences

- KINTEX event coverage increases from the short default page to the full
  upcoming public listing range.
- Discovery still uses the browser-free `list.do` HTML and durable `seq` detail
  IDs.
- The date range moves daily with the KST clock.

---

## Decision: Deploy Ingest Concurrency Runtime Defaults

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

The ingest concurrency speedup was locally verified with per-host request
limiting, bounded source concurrency, and bounded per-source detail workers.
The production ingest unit needed the new concurrency knobs while preserving
the existing polite request rate.

### Decision

Redeploy `eventsintel` from current `main` HEAD `b245fdc` and install the
updated ingest systemd unit with `EVENTSINTEL_SOURCE_CONCURRENCY=2`,
`EVENTSINTEL_DETAIL_WORKERS=4`, and unchanged `EVENTSINTEL_RATE_PER_MIN=30`.

### Consequences

- Scheduled refreshes can overlap independent COEX/KINTEX work without raising
  the per-host request rate.
- The API binary and ingest unit are now aligned with the committed concurrency
  implementation.
- No frontend, schema, taxonomy, or public API contract change is part of this
  deployment.

---

## Decision: Use Source-Derived Event Summaries Only

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

The previous `summary` implementation generated a deterministic line from
event name, dates, venue, and organizer. That made the field look filled, but it
did not describe what the exhibition or event was actually about.

### Decision

Do not render `summary` in list cards. Persist `summary` only when the venue
detail page exposes real descriptive content: COEX `행사 소개`-style fields, or
KINTEX `행사내용`/`행사목적`/`행사품목`. If no source description exists, keep
`summary` null and record it in `missing_fields`.

### Consequences

- Summary is useful in the detail/API surface without making list cards noisy.
- Rows without real event description content remain honest nulls.
- Existing generated template summaries are cleared and not regenerated.

---

## Decision: Use Daily Refresh And One-Year Venue Lookahead

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

The service originally refreshed on a 6-hour ingest timer, briefly moved to a
3-hour cadence while extending the venue lookahead, then the user clarified the
refresh should be longer, such as 24 hours or 3 days. KINTEX already used a
rolling 365-day listing range, but COEX current-schedule discovery used a
180-day range. The target is lower crawl pressure while keeping symmetric
one-year lookahead for COEX and KINTEX.

### Decision

Run scheduled ingest every 24 hours. COEX and KINTEX discovery should both cover
a rolling 365-day future window from the current Korean date. COEX current
schedule discovery is the primary path for the one-year public schedule; the
WordPress sitemap is only a fallback when current schedule discovery yields no
refs. This skips overlapping sitemap work and prevents historical sitemap rows
from crowding current one-year schedule refs out of the ingest cap.

### Consequences

- Public schedule freshness improves without increasing the polite per-host
  request rate.
- COEX schedule discovery can walk more advertised schedule pages for the
  one-year window.
- COEX raw discovery now reflects the current schedule window when the schedule
  page is available, with sitemap discovery retained as a fallback.
- True cross-run conditional skipping still requires persisting detail-page
  validators such as ETag or Last-Modified.

---

## Decision: Enrich Events From Public Organizer Pages

- Status: accepted
- Date: 2026-06-22
- Decision Maker: smpain

### Context

The original product idea emphasized that users care about actions such as
registering, exhibiting, sponsoring, booking meetings, and tracking programs,
not only event dates. Venue pages often expose the official event homepage but
do not reliably include those action fields.

### Decision

When a COEX/KINTEX venue detail includes a public homepage URL, the ingest
pipeline may perform one HTTP-only second-hop fetch to that official/organizer
page. The parser extracts source-backed action signals such as registration,
exhibit, sponsorship, matchmaking, startup-program, cost, and deadline hints.
The read API remains cache-first and read-only; normal API reads still do not
run live LLM generation.

### Consequences

- `actions`, `register_url`, `exhibit_url`, `registration_deadline`,
  `exhibitor_deadline`, and `cost_hint` can be populated from organizer pages.
- Organizer-page provenance is appended to `sources[]` so venue-backed and
  organizer-backed facts remain distinguishable.
- Unknown action fields remain false/null and are listed in `missing_fields[]`;
  callers must check missing fields before treating false booleans as confirmed
  absence.
- The official-page fetcher allows arbitrary public HTTP(S) hosts but keeps the
  existing SSRF public-IP guard, robots checks, rate limiting, and body limits.

## Decision: Edge-Cache The Read API At Cloudflare With Ingest-Time Purge

### Context

Every page open paid a full Cloudflare→origin round trip: live measurement showed
`/` and `/api/v1/events` returning `cf-cache-status: DYNAMIC` with TTFB 0.6–1.2s,
because the Go service only set `Cache-Control` on the landing HTML and static
assets — the data endpoints set none, and Cloudflare does not edge-cache HTML or
`/api/...` paths without an explicit Cache Rule. The dataset only changes when the
24h ingest batch runs, so these responses are highly cacheable.

### Decision

All read success responses now advertise
`public, max-age=120, s-maxage=3600, stale-while-revalidate=86400`
(`internal/api/cache.go`, applied via `writeJSON`, `Respond`'s Markdown branch,
the meta/openapi/llms handlers, and the root HTML/asset handlers). A Cloudflare
Cache Rule ("Cache Everything", respect origin TTL) is applied for `/`,
`/api/v1/*`, and `/llms.txt`, bypassing requests whose `Accept` negotiates
Markdown (Free-plan caches ignore `Vary: Accept`). After an ingest that stored
events, the binary issues a best-effort `purge_everything`
(`internal/cfpurge`, env-gated via `EVENTSINTEL_CF_PURGE_ZONE`/`_TOKEN`).

### Consequences

- Most page opens become edge `HIT`s; origin round trips drop to ≈1 per
  s-maxage window per PoP, backgrounded by stale-while-revalidate.
- Error responses (4xx/5xx/429) deliberately carry no `Cache-Control`, so the
  edge never caches an error.
- The long edge TTL is safe because ingest purges on data change; with purge
  unset, worst-case staleness is s-maxage (1h) on a once-daily dataset.
- Purge is best-effort and non-fatal: a purge failure logs but never fails
  ingest. Purge needs a token with Zone→Cache Purge; the Cache Rule needs
  Zone→Cache Rules (the existing DNS-only token is insufficient).
- Markdown-by-`Accept`-header is not edge-cached (collision guard); `?format=md`
  is a distinct cache key and unaffected.

---

## Decision: Isolate Anonymous Public Discovery From The Read API

- Status: accepted
- Date: 2026-07-20
- Decision Maker: smpain
- Scope: `cmd/eventscout`, `cmd/eventscout-server`, and their public-discovery boundary

### Context

The Solar-backed source-discovery loop now has two different operating surfaces.
The command-line tool is useful for local experiments, while a public HTTP
surface must be safe to call without an account or a caller-supplied provider.
The existing `eventsintel` API already has a separate read-only, cache-first
contract and must not acquire live LLM work as a side effect.

### Decision

- `eventscout` defaults to the keyless `public` search provider. It crawls only
  the six server-owned public seeds and follows bounded, robots-aware public
  HTTP(S) discovery. `fixture` is an explicit offline option; Tavily is an
  explicit credential-backed option. Fixture is not the default.
- `eventscout-server` is a separate binary exposing only the anonymous
  `POST /v1/discover` operation plus health/readiness routes. Callers submit
  exactly a goal string; there is no signup, API key, URL seed, backend,
  profile, or arbitrary-network request surface. Private, loopback, link-local,
  metadata, and credential-bearing URLs are rejected by the public crawl.
- Solar is an operator-only backend credential. `eventscout-server` must find a
  configured Solar backend and key at startup and fails closed when it is absent,
  even if a local backend is configured. The key is never accepted from callers,
  returned in responses, or written to structured logs.
- The anonymous service limits are fixed and documented: 4 KiB body, 1–800
  Unicode-rune goal, 2 requests per 10 minutes and 24 per day per client, 2
  active jobs, and a 60-second server deadline. Exceeded limits return stable
  `400`, `429` (with bounded `Retry-After`), `503`, or `504` error envelopes.
  The public crawl and model loop retain their independent hard caps and report
  truncation rather than expanding work.
- The service logs request metadata only (request ID, status, duration, and
  fixed limit counters). It does not log user goals, fetched page text, Solar or
  Tavily secrets, or upstream error bodies. The local interactive CLI may print
  its own goal to its terminal, so secrets and personal data remain forbidden in
  CLI goals as well.
- The normal `eventsintel` read API remains unchanged: read-only, cache-first,
  no live LLM generation, and no `/v1/discover` route in `api.Router`.

### Consequences

- A caller can use the public provider without Tavily or Solar credentials, but
  only the operator can run the public HTTP server because Solar startup is an
  explicit deployment requirement.
- Offline fixture tests remain reproducible without third-party network access;
  real Tavily search is opt-in and its query/privacy boundary is documented.
- Anonymous service quota numbers are intentionally different from the normal
  API's existing per-IP `60/min` and `2,000/day` read quotas. The two surfaces
  must not be conflated in runbooks or monitoring.
- The public HTTP service is isolated as its own listener/process. Deploying it
  is a separate operational decision and does not change normal API caching or
  data reads.

### Verification

- Source checks: `internal/eventscoutserver/config.go`, `handler.go`,
  `middleware.go`, `quota.go`, `internal/publicdiscovery/catalog.go`,
  `canonical.go`, `types.go`, and `internal/api` router tests.
- Documentation/config checks, the zero-third-party-key smoke, and the bounded
  operator script (`scripts/smoke-solar-public-discovery.sh`) are recorded in
  `.omo/evidence/solar-accountless-public-agent/task-6.txt`.

### Additive yield-diagnostic governance (2026-07-26)

- A completed successful public-discovery response may include the request-local,
  count-only `yield_trace` object. Its fixed fields are `outcome`,
  `terminal_reason`, `crawler_validated`, `offered`, `prefilter_dropped`,
  `prefilter_reasons`, `proposal_calls`, `judge_calls`, `judge_entries_parsed`,
  `judge_entries_dropped`, and `accepted`.
- `prefilter_reasons` is a nested, fixed-key, count-only breakdown of
  `prefilter_dropped` with exactly `invalid_url`, `url_pattern`,
  `missing_title`, `missing_location`, `missing_date`, and `past_date`. Exactly
  one reason is attributed per dropped result, so the six values always sum to
  `prefilter_dropped`. A bare total cannot tell a crawler that yields untitled
  candidates apart from a profile whose URL pattern excludes them; both appear
  as a zero-source run.
- The CLI's `public_discovery` block additionally reports `seed_candidates`,
  `skipped_documents`, and `malformed_documents`. Only the seed protocol
  guarantees a title (the seed name is its fallback), and seed pages are fetched
  before sitemap children, so a zero `seed_candidates` beside a nonzero
  skipped/malformed count means the seed pages were reached and rejected rather
  than never attempted. It also reports `seed_outcomes`, a fixed-key count-only
  tally with exactly one entry per enqueued seed — `candidate`,
  `robots_disallowed`, `robots_unavailable`, `http_status`, `body_too_large`, `unsupported_content`,
  `transport_error`, `duplicate`, `candidate_cap`, `not_attempted` — whose `candidate`
  value always equals `seed_candidates`. The operator smoke reports all of these
  alongside `truncated` and the fixed `truncation_reasons` enum. None of them
  carries a URL, host, or document content.
- `yield_trace` is a classification aid, not a source-data export: it contains
  no goal, candidate, URL, fetched content, model payload, or credential. Its
  counters must not be carried from one request to another.
- Existing error envelopes are unchanged and trace-free. The normal internal
  cache-first read API also remains unchanged; this diagnostic does not add
  live model work to normal reads.
- The Solar credential remains operator-only. No caller key is accepted by the
  CLI's public-provider contract or by the anonymous HTTP service.
- The present-key operator smoke is bounded. A classified result with
  `accepted: 0` is valid observed output and is not a minimum-live-result
  failure. Without the operator credential, the smoke reports
  `SKIPPED_CREDENTIAL_UNAVAILABLE` before making a model or network call.

### Reserve candidate slots for pending seeds (2026-07-27)

- Status: accepted
- Evidence: three bounded live operator smokes. The diagnostics above attributed
  every dropped candidate to `missing_title` and every seed to `candidate_cap`,
  with `robots_disallowed`, `http_status`, `body_too_large`,
  `unsupported_content`, and `transport_error` all zero.
- Cause: `crawl` drains the protocol (sitemap) queue before the HTML queue.
  Sitemap children fill the 30-candidate list with untitled entries, so the seed
  pages — fetched later and successfully — are rejected for want of a slot. Only
  the seed protocol guarantees a title, since the seed name is its fallback, so
  the model received no candidate it could judge and never made a judge call.
- Decision: `addCandidate` withholds one candidate slot per enqueued seed that
  has not yet reached an outcome. Seed candidates spend exactly those withheld
  slots.
- The total candidate cap is **not** raised, and no crawl, fetch, robots, SSRF,
  model, or token limit changes. The reservation is released as each seed is
  accounted for, so a crawl with no pending seed still fills the whole list.
- Result: the live smoke moved from `offered=0, judge_calls=0, accepted=0,
  outcome=budget_stopped` to `seed_candidates=5, offered=5, judge_calls=1,
  accepted=5, outcome=accepted`. Untitled sitemap children are still dropped by
  the prefilter; that remains intended, and no synthetic title is invented.
- This does not promise a minimum live result. It removes a structural
  condition that made zero unavoidable.

### Solar enrichment inside batch ingest (2026-07-27)

- Status: accepted
- Context: the deployed binary never called Solar. `cmd/eventsintel`,
  `internal/pipeline`, `internal/enrich`, and `internal/normalize` did not
  import `internal/agent`, so events.nukk.net ran purely on the deterministic
  crawler and the Solar work lived only in standalone commands.
- Decision: the pipeline gained an optional `EventEnricher` seam. It runs after
  normalization, inside batch ingest only, and the pipeline never names a
  concrete backend, exactly as it never names a concrete source adapter.
- The concrete `internal/solarenrich` implementation is deliberately narrow. It
  fills `start_date` and `end_date` and nothing else, because those are
  checkable against an ISO shape and are what Korean venue pages express most
  variably. A non-ISO answer is discarded rather than stored.
- It reads the raw scraped strings the normalizer failed to interpret, so no
  second fetch is made. Contact patterns are stripped before the text leaves
  the process.
- It never overwrites a source-derived value, only clears a field it actually
  filled, and appends an `eventsintel/solar-enrich` provenance entry with
  `date_confidence` lowered to `low`.
- Bounded and opt-in. It requires both `EVENTSINTEL_SOLAR_API_KEY` and
  `EVENTSINTEL_SOLAR_ENRICH=1`, caps model calls per run, applies a per-call
  timeout, and treats any error as non-fatal so the deterministic row stands.
- The read path stays LLM-free. `internal/api` is unchanged and is verified
  unchanged as a gate.

### Multi-hop action enrichment in ingest (2026-07-27)

- Status: accepted
- Evidence: a full scan of the 670 production events showed the date fields the
  first enricher targeted are 0% missing, while the action fields are 80-100%
  missing. `registration_deadline` 100%, `exhibitor_deadline` 99.9%,
  `actions.*` 96-99%, `register_url` 82%, `exhibit_url` 80%, `cost_hint` 80%.
  The deterministic extractor is already good at dates and poor at actions.
- Decision: the pipeline gained a second seam, `ActionEnricher`, invoked from
  the existing official-page second hop. The multi-hop agent loop that already
  existed as a CLI is now the ingest path for those fields.
- It reads the page the pipeline already fetched, so it costs no extra request
  of its own, and it follows links through the caller-supplied official fetcher
  so it inherits the same allowlist, robots policy, and rate limits.
- Only signals still nil are filled. A non-ISO deadline is discarded rather
  than stored, and a deterministic finding is never overwritten.
- Bounded by the same per-run ceiling and per-call timeout, and any error is
  non-fatal. One attempt is up to three model calls.
- Measured on a bounded live run of 40 events: 15 attempts, 4 events filled.
  The date enricher made 1 attempt and filled nothing, consistent with the
  scan. It is retained but is effectively inert on current sources.

### Korean deadline shapes in action enrichment (2026-07-27)

- Status: accepted
- Context: the enricher required an ISO date and discarded everything else, so
  `registration_deadline` moved from 216 missing to 211 across a 216-event
  control comparison. Korean venue pages write "2026년 9월 1일" far more often
  than an ISO date, and that gate rejected all of them.
- Decision: normalize the shapes carrying an explicit year, month, and day
  before the check. Text without all three, such as "선착순 마감", is still
  rejected rather than guessed at, and no component is ever invented.
- Measured against the same control on the same 216 events: registration
  deadline missing fell from 216 to 197, and events gaining at least one field
  rose from 24 to 40, which is 11.1% to 18.5%.
- The deterministic parser in `internal/normalize` is unchanged. This applies
  only to what the model returns.

### Remote MCP endpoint at events.nukk.net/mcp (2026-07-27)

- Status: accepted
- Context: the MCP server was the one place a real user could talk to the
  agent directly, and it required a Go toolchain. That made it developer-only.
- Decision: `eventmcp -http` serves the streamable HTTP transport as a separate
  daemon on 127.0.0.1:3008, and Caddy routes only the `/mcp` path to it. The
  normal read API keeps its own process and its LLM-free guarantee; the hard
  constraint is untouched because this is a distinct, explicitly-invoked tool
  endpoint, the same standing the anonymous eventscout demo already has.
- The server is stateless: no session id, GET answers 405, one POST is one
  JSON-RPC message. `ask_events` runs on the operator's Solar key, refuses to
  start without it, and is the only method that draws down the per-client
  quota (10 per ten minutes, 60 per day, keyed by the Caddy-forwarded client
  address and only trusted from a loopback peer). `search_events` stays
  LLM-free and unmetered.
- Live verification: initialize, tools/list, search_events, and a real
  ask_events ("다음 달 서울 AI 행사" → ai/서울/2026-08 filter, 7 events)
  through the public URL, with `deploy/verify.sh` still ALL CHECKS PASSED.

### Source promotion goes through code review, not runtime config (2026-07-28)

- Status: accepted
- Context: eventscout can discover and judge new public sources, but ingest
  only crawls the sources wired into `main.go` and the benchmark catalog. A
  path was needed from an accepted discovery to a crawled source.
- Decision: `eventscout -promote <dir>` emits review artifacts (seed-candidate
  JSONL, paste-ready catalog snippet, new-host allowlist diff); a human fills
  the missing fields from the official page and lands them as a normal commit.
  The crawl allowlist moved to `fetch.ProductionAllowedHosts` as the single
  audited list both ingest and the promotion diff read.
- Alternative rejected: runtime seed loading via env-pointed JSONL plus
  env-extended allowlist. It removes the rebuild step but moves the SSRF
  boundary out of reviewed code, so a bad judgment or edited host file could
  put a source into the public dataset without review. The asymmetry (approval
  is cheap, serving polluted data is not) decided it.
- Model-generated text (title, reason) enters the snippet only through `%q`
  escaping and the whole snippet must pass `go/format.Source`, so review
  content, not syntax, is the only thing a human can get wrong.

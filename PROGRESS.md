# Progress

## Metadata

- Current Status: `shaping`
- Current Stage: Shape
- Owner: smpain
- Started: 2026-06-21
- Last Updated: 2026-07-20

## Current Focus

Finish the Solar Agent Partner Stage 1 public surface without changing the
normal cache-first event API: document the anonymous public-discovery boundary,
operator-only Solar startup, and reproducible no-third-party-key smoke path.

## Completed

- [x] Captured source idea under `docs/ideas/ai/`.
- [x] Decided API should be free with protective quotas.
- [x] Decided MVP domain should be `events.nukk.net`.
- [x] Determined existing Developer DigitalOcean VPS is acceptable for a
  public-only, read-only, cache-first MVP.
- [x] Promoted the idea into a project workspace.

## Completed (2026-06-21 session)

- [x] Selected first target user/workflow: **startup founders / operators**.
- [x] Selected first dataset format: **JSONL**.
- [x] Created the 30-day manual coverage dataset schema
  (`prototype/manual-dataset-schema.md`, v0.1).
- [x] Created seed file structure + 3 illustrative records
  (`prototype/seed-events.jsonl`) and `prototype/taxonomy.md`.
- [x] Fixed frontend audit issues: hidden modal overlay no longer blocks the
  page, detail modal close button works after final render, root HTML has design
  system coverage (`DESIGN.md`), read-only SQLite serve works against a plain
  migrated DB, and the broken root-index tests compile/pass.
- [x] Deployed the frontend/runtime fixes to `events.nukk.net` by replacing the
  `developer-vps` linux/amd64 binary and restarting `eventsintel-api`.
- [x] Removed developer/AI-first frontend copy from the public UI. The homepage
  now leads with event discovery language, Korean controls/status text, and
  human-readable support links while keeping API/document URLs intact.
- [x] Changed event browsing policy so COEX/KINTEX venue/date lists show all
  discovered events; non-taxonomy events remain visible with `excluded=true`.
- [x] Added COEX current schedule discovery, now using a 365-day date range and
  schedule pagination so future pages are not missed.
- [x] Updated the public UI copy and client ordering to show COEX/KINTEX
  schedules as a date-first event browser instead of a narrow industry-only
  filter.
- [x] Changed the homepage default to upcoming categorized events, removed past
  event controls, added a `모든 행사 보기` scope option, and moved integration
  links from the header to the footer.
- [x] Removed the homepage subtitle copy under the title so the browsing
  controls become the first visible instruction.
- [x] Documented the gap between the original repo plan and current implemented
  product in `docs/original-plan-gap.md`.
- [x] Added the first action-oriented enrichment slice: optional official-page
  second-hop fetches can now populate registration/exhibit links, action
  booleans, registration/exhibitor deadlines, cost hints, and organizer
  provenance while preserving missing-field honesty when no signal is available.
- [x] Updated the event detail modal to show extracted cost hints and action
  signal chips such as 참가 가능, 부스 문의 가능, 후원 문의 가능, 비즈니스 상담,
  and 스타트업 프로그램 when those signals are source-backed.
- [x] Defined the founder/operator benchmark source set in
  `docs/founder-operator-event-radar.md`.
- [x] Added the first international benchmark source coverage slice, backed by
  a registered `benchmark` source adapter and
  `docs/benchmark-source-seed.jsonl`.
- [x] Expanded the registered `benchmark` source to 43 official international
  benchmark events, including the initial 10-source slice, all Oracle-recommended
  additions, China coverage, ICML 2026, and the expanded academic AI conference
  batch.
- [x] Changed `GET /api/v1/events?list=venue` so omitted `since` defaults to
  today's `Asia/Seoul` date, keeping domestic venue API reads current/upcoming
  by default while preserving explicit historical queries.
- [x] Deployed the venue API default change to `events.nukk.net`, restarted
  `eventsintel-api`, purged Cloudflare edge cache, and verified the public API:
  `list=venue` without `since` now has `min_start=2026-06-25`; explicit
  `since=1999-01-01` still returns historical rows.

## In Progress

- [x] COEX/KINTEX ingestion feasibility check (`prototype/coex-kintex-feasibility.md`)
  — both static-HTML, field-rich, HTTP-parseable; no headless browser needed.
- [ ] `/plan` the COEX/KINTEX ingestion backend + ADR superseding manual-first.
- [x] Choose 43 international benchmark sources.
- [x] Expand the registered `benchmark` source from 3 to 43 official
  international benchmark events and prove the new rows through a fresh local
  ingest and API response.

## Blockers

| Blocker | Impact | Owner | Resolution Needed |
|---|---|---|---|
| None | - | - | - |

## Evidence Collected

- Source idea document: `/Users/smpain/Developer/docs/ideas/ai/2026-06-21-ai-industry-event-intelligence-api.md`
- Deployment references confirm `nukk.net` is the Developer public domain.
- DNS check confirmed `nukk.net` resolves through Cloudflare nameservers.
- Frontend fix verification (2026-06-21): `go test ./...`, `go vet ./...`, and
  `go build -o /tmp/eventsintel-fixed ./cmd/eventsintel` pass; Chrome/Selenium
  verified 375px mobile card click, modal open/close, no hidden overlay blocking,
  no horizontal overflow, and screenshots at 375/768/1280px.
- Deployment verification (2026-06-21): deployed binary SHA256
  `985d62101c55a8a4d1d5bdc2695b0d620a403611a7ac508b86185661488e9ca4`;
  `eventsintel-api` restarted on `developer-vps` at `2026-06-21 14:31:07 UTC`;
  public smoke tests returned 200 for `/`, `/healthz`, `/llms.txt`, `/api/v1`,
  `/api/v1/schema`, `/api/v1/openapi.yaml`, and `/api/v1/events`; public mobile
  Selenium verified overlay hidden, card click, modal open/close, and no 375px
  horizontal overflow.
- Redeployment verification (2026-06-21): rebuild produced the same binary
  SHA256 `985d62101c55a8a4d1d5bdc2695b0d620a403611a7ac508b86185661488e9ca4`;
  `eventsintel-api` restarted again on `developer-vps` at
  `2026-06-21 14:37:00 UTC`; public smoke tests again returned 200 for `/`,
  `/healthz`, `/llms.txt`, `/api/v1`, `/api/v1/schema`,
  `/api/v1/openapi.yaml`, and `/api/v1/events`; public mobile Selenium again
  verified overlay hidden, card click, modal open/close, and no 375px horizontal
  overflow.
- Frontend copy cleanup verification (2026-06-21): added
  `TestRootIndexHTMLUsesHumanFriendlyCopy`; `go test ./...`, `go vet ./...`,
  and `go build ./cmd/eventsintel` pass. Local Chrome/Selenium against a seeded
  temp DB verified desktop/mobile copy, no old visible slop terms, no horizontal
  overflow, all-period cards, and modal open/close.
- Copy cleanup deployment verification (2026-06-21): deployed binary SHA256
  `0973a3224bc91bfdef23dbfd470eb30d056047a3ef7b303736893055c3e7a5c6`;
  `eventsintel-api` restarted on `developer-vps` at `2026-06-21 14:53:56 UTC`.
  Public smoke returned 200 for `/`, `/healthz`, `/llms.txt`, `/api/v1`,
  `/api/v1/schema`, `/api/v1/openapi.yaml`, and `/api/v1/events`; public
  Chrome/Selenium verified desktop/mobile copy, no old visible slop terms, no
  horizontal overflow, and modal open/close with percent-encoded event IDs.
- Event display policy verification (2026-06-22): `go test ./...`,
  `go vet ./...`, and `go build ./cmd/eventsintel` pass. Temp live ingest with
  `EVENTSINTEL_MAX_DISCOVER=160` discovered COEX raw=2090 and stored=160;
  local API returned 15 COEX events for `since=2026-08-01&before=2026-08-31`,
  including `AI서밋서울앤엑스포`, `2026 국제 병원의료산업 박람회`,
  `세계 제약ㆍ바이오ㆍ건강기능 산업 전시회 2026`, `2026 자율주행모빌리티산업전`,
  and `EV·피지컬 AI를 위한 배터리 안전성 및 충전 인프라 구축 방안 세미나`.
  Chrome/Selenium verified desktop search, 375px mobile layout, date-ordered
  cards, and no horizontal overflow.
- Event display deployment verification (2026-06-22): committed
  `5597ad8 Show all venue events`; deployed linux/amd64 binary SHA256
  `f213739bff953c24e942a5e2115d9dd87cde0823a32f783ec6d2ca27ca946a2c`;
  `eventsintel-api` restarted active on `developer-vps` at
  `2026-06-21 15:18:23 UTC`. Public smoke returned 200 for `/`, `/healthz`,
  `/llms.txt`, `/api/v1`, `/api/v1/schema`, `/api/v1/openapi.yaml`, and
  `/api/v1/events`. Production ingest completed at `2026-06-21 15:33:18 UTC`
  with COEX raw=2090, discovered=400, stored=400 and KINTEX stored=9. Public
  API returned 15 COEX events for `since=2026-08-01&before=2026-08-31`,
  including `AI서밋서울앤엑스포` and `2026 국제 병원의료산업 박람회`; public
  Chrome/Selenium verified the new root HTML, date-first event list, search for
  `AI서밋`, and no 375px horizontal overflow.
- Homepage scope verification (2026-06-22): `go test ./internal/api -run
  'TestRootIndex|TestRootIndexServesInteractiveHTML_whenBrowserAcceptsHTML|TestRootIndexHTMLUsesHumanFriendlyCopy'
  -count=1`, `go test ./...`, `go vet ./...`, and `go build ./cmd/eventsintel`
  pass. Local Chrome/Selenium with production DB copy verified default
  `31개 주요 행사`, no `기타` badge in default cards, `모든 행사 보기` shows
  `98개 행사`, header has 0 links, footer has `연동 안내` and `요약 문서`, search
  finds `AI서밋서울앤엑스포`, and 375/768px layouts have no horizontal overflow.
- Homepage scope deployment verification (2026-06-22): committed
  `a174c0d Refine homepage event scope`; deployed linux/amd64 binary SHA256
  `ef48bba078b0ff1cd3e0e7d4ddc0e382ee92759d95a30f1c95dadbd8f566b27e`;
  `eventsintel-api` restarted active on `developer-vps` at
  `2026-06-22 00:29:06 UTC`. Public smoke returned 200 for `/`, `/healthz`,
  `/llms.txt`, `/api/v1`, `/api/v1/openapi.yaml`, and `/api/v1/events`.
  Public Chrome/Selenium verified header links=0, footer links=`연동 안내` and
  `요약 문서`, default `31개 주요 행사` with no `기타` badge, `모든 행사 보기`
  shows `98개 행사` with `기타`, and 375px mobile has no horizontal overflow.
- Homepage subtitle removal verification (2026-06-22): requested copy
  `장소와 기간만 고르고, 예정된 행사를 날짜순으로 훑어보세요.` removed from
  `static/index.html`; `go test ./internal/api -run
  'TestRootIndex|TestRootIndexServesInteractiveHTML_whenBrowserAcceptsHTML|TestRootIndexHTMLUsesHumanFriendlyCopy'
  -count=1`, `go test ./...`, `go vet ./...`, and `go build ./cmd/eventsintel`
  pass. Local Chrome/Selenium verified subtitle element count=0, default
  `31개 주요 행사`, and no 375/1280px horizontal overflow.
- Homepage subtitle removal deployment verification (2026-06-22): committed
  `954fe0d Remove homepage subtitle`; deployed linux/amd64 binary SHA256
  `202fd3a4f20a2359b982f9ef0f909f03fb38a5e26071269e295c8b840456360b`;
  `eventsintel-api` restarted active on `developer-vps` at
  `2026-06-22 00:37:40 UTC`. Public smoke returned 200 for `/`, `/healthz`,
  and `/api/v1/events`; public HTML no longer contains the removed copy or
  `site-desc`; public Chrome/Selenium verified subtitle element count=0,
  default `31개 주요 행사`, and no 375/1280px horizontal overflow.
- KINTEX lookahead verification (2026-06-22): removed the homepage title accent
  dot and expanded KINTEX discovery to request `searchStartDt`, `searchEndDt`,
  and every advertised `pageIndex` through the next 365 days. `go test ./...`,
  `go vet ./...`, and `go build ./cmd/eventsintel` pass. Local live ingest with
  `EVENTSINTEL_MAX_DISCOVER=80` discovered KINTEX raw=35, stored=35, with future
  dates from `2026-06-25` through `2026-11-27`; the public-style API query for
  `venue=kintex&since=2026-06-22&before=2027-06-22&limit=100` returned 35 rows.
  Playwright screenshots at 1280x900 and 375x812 verified the title renders as
  `COEX·KINTEX 행사 모아보기` without a trailing dot and default cards remain
  readable with no horizontal overflow.
- KINTEX lookahead deployment verification (2026-06-22): committed
  `07eb976 Expand KINTEX future discovery`; deployed linux/amd64 binary SHA256
  `7e4f44fc6450918ca11ead825f66358232fe5a00ddf2d1ca8d7aaab53deafbed`;
  `eventsintel-api` restarted active on `developer-vps` at
  `2026-06-22 00:49:14 UTC`. Public smoke returned 200 for `/`, `/healthz`,
  `/llms.txt`, `/api/v1`, `/api/v1/schema`, `/api/v1/openapi.yaml`, and
  `/api/v1/events`; public HTML has `COEX·KINTEX 행사 모아보기` without
  `accent-dot`. Production ingest completed at `2026-06-22 01:04:54 UTC` with
  KINTEX raw=35/stored=35 and COEX raw=2090/stored=400. Public API returned 35
  KINTEX rows for `since=2026-06-22&before=2027-06-22`, spanning `2026-06-25`
  to `2026-11-27`; public Playwright verified loaded 1280x900 and 375x812
  screens.
- Ingest concurrency local verification (2026-06-22): after Todo 6, deterministic
  fake-latency proof in `.omo/evidence/task-6-deterministic-verbose.txt`
  reported sequential elapsed `990.443584ms`, concurrent elapsed `195.499917ms`,
  `80.3%` improvement, and equal stored counts (`stored_sequential=8`,
  `stored_concurrent=8`). Live temp ingest smoke/count evidence in
  `.omo/evidence/task-6-live-ingest.txt` used matching
  `EVENTSINTEL_MAX_DISCOVER=80`, `EVENTSINTEL_RATE_PER_MIN=240`, and
  `EVENTSINTEL_INGEST_DEADLINE=5m`; sequential mode (`SOURCE_CONCURRENCY=1`,
  `DETAIL_WORKERS=1`) stored COEX=80 and KINTEX=36 in `real 46.34`, while
  concurrent mode (`SOURCE_CONCURRENCY=2`, `DETAIL_WORKERS=4`) stored COEX=80
  and KINTEX=36 in `real 36.50`. Todo 7 did not deploy or touch production;
  deployment was not requested. Final local gates were captured in
  `.omo/evidence/task-7-green.txt`, `.omo/evidence/task-7-diffcheck.txt`,
  `.omo/evidence/task-7-lsp.txt`, `.omo/evidence/task-7-manual.txt`, and
  `.omo/evidence/task-7-code-review.md`.
- Ingest concurrency redeployment verification (2026-06-22): deployed current
  `main` HEAD `b245fdc` to `developer-vps` after confirming no tracked local
  changes remained to commit. Deployed linux/amd64 binary SHA256
  `712d2ceb2814d7c6f69f058c3f046c61477157159cbfe4efa7b2ac92dc24fca2`;
  remote `/srv/developer/events-intel/eventsintel` matched the same SHA.
  `eventsintel-api` restarted active at `2026-06-22 05:44:51 UTC`, and
  `eventsintel-ingest.timer` is active/waiting. The installed ingest unit now
  has `EVENTSINTEL_RATE_PER_MIN=30`, `EVENTSINTEL_SOURCE_CONCURRENCY=2`, and
  `EVENTSINTEL_DETAIL_WORKERS=4`. Public smoke returned 200 for `/`,
  `/healthz`, `/llms.txt`, `/api/v1`, `/api/v1/schema`,
  `/api/v1/openapi.yaml`, and `/api/v1/events`.
- Visitor-focused event detail redeployment verification (2026-06-22): deployed
  current `main` HEAD `fffb933` to `developer-vps`. Deployed linux/amd64 binary
  SHA256 `d467ed766e6b6de1cfc9ec5b17b30029d7474fd6e5fc0a3a4ef6d3b2910986a2`;
  remote `/srv/developer/events-intel/eventsintel` matched the same SHA.
  `eventsintel-api` restarted active at `2026-06-22 06:25:20 UTC`. Public smoke
  returned 200 for `/`, `/healthz`, `/llms.txt`, `/api/v1`, `/api/v1/schema`,
  `/api/v1/openapi.yaml`, `/api/v1/events`, and all split frontend assets under
  `/assets/`. Public HTML links `theme.css`, `index.css`, `list.css`,
  `detail.css`, `ui.js`, `detail.js`, and `app.js`; public `detail.js` exposes
  visitor-facing `행사 정보`, `참가 정보`, and `공식 페이지` copy without the
  removed developer-facing detail sections. Public `app.js` includes final dialog
  close focus and venue comma-spacing normalization.
- Load-more duplicate perception fix (2026-06-22): public API page-walk evidence
  showed no duplicate `event_id` values across raw pages, but the homepage
  fetched raw pages, filtered to categorized events client-side, and then
  re-sorted by event date. That made "더 보기" often leave the same visible cards
  at the top while adding little or no visible content. The frontend now merges
  pages by unique `event_id`, keeps sorting/filtering helpers in `events.js`, and
  the default categorized load-more path looks ahead through bounded additional
  pages until new visible events are added or the feed ends. Local gates passed:
  `go test ./internal/api -run 'TestRootIndex|TestListEvents_FullIterationNoGapsNoDupes' -count=1`,
  `go test ./...`, `go vet ./...`, `go build ./cmd/eventsintel`, `node --check`
  for `static/events.js` and `static/app.js`, JS helper smoke tests, and
  `git diff --check`. Local Chrome/CDP against a temp ingest DB verified default
  cards increased from 22 to 32 after clicking "더 보기", with 10 new IDs and
  duplicate IDs `[]`. Desktop 1440x900 and mobile 390x844 screenshots after the
  click show 32 cards, no horizontal overflow, and readable Korean wrapping.
- Load-more duplicate perception redeployment verification (2026-06-22):
  committed `79cb5db fix(frontend): make load more add new events`; deployed
  linux/amd64 binary SHA256
  `049d05233fe06a79077ec30e4313e828c2c6ca89be3d9ec1d206df1b8ae7cdef` to
  `developer-vps`. Remote `/srv/developer/events-intel/eventsintel` matched the
  same SHA, and `eventsintel-api` restarted active at `2026-06-22 06:40:05 UTC`.
  Public smoke returned 200 for `/`, `/healthz`, `/llms.txt`, `/api/v1`,
  `/api/v1/schema`, `/api/v1/openapi.yaml`, `/api/v1/events`,
  `/assets/events.js`, and `/assets/app.js`. Browser-style public HTML includes
  `/assets/events.js`, that asset exposes `EventIntelEvents.mergeUnique`, and
  production Chrome/CDP verified clicking "더 보기" changed visible cards from
  32 to 33 with one new ID and duplicate IDs `[]`.
- Exhausted load-more button fix (2026-06-22): production page walk showed
  public raw pages had 124 future rows but only 33 displayable major events
  (32 on the first raw page, 1 on the second, then no more). Because the visible
  list is date-sorted, the extra one-item load still looked like copied content
  to users. The default categorized frontend path now scans bounded remaining
  pages during initial load and hides "더 보기" when no additional displayable
  major events remain. Local gates passed: `go test ./internal/api -run
  'TestRootIndex|TestListEvents_FullIterationNoGapsNoDupes' -count=1`,
  `go test ./...`, `go vet ./...`, `go build ./cmd/eventsintel`,
  `node --check static/app.js`, `node --check static/events.js`, and
  `git diff --check`. Local Chrome/CDP with a temp live ingest DB verified
  desktop and mobile default views had 32 cards, 32 unique IDs, count
  `32개 주요 행사`, no horizontal overflow, and hidden "더 보기".
- Exhausted load-more redeployment verification (2026-06-22): committed
  `533934f fix(frontend): hide exhausted load more`, then found Cloudflare was
  still serving stale `/assets/app.js` from its four-hour asset cache. Committed
  `e5e00ae fix(frontend): bust cached app asset` so browser HTML requests
  `/assets/app.js?v=20260622-hide-more`. The final deployment used
  `go build -buildvcs=false` to avoid VCS-only checksum churn from this progress
  record; linux/amd64 binary SHA256
  `31e2b9cb0cdf2971a08a3029bb186c0b2fe98c0cb7d9fcb8c5722cd6e6f5414c` matched
  remote `/srv/developer/events-intel/eventsintel`, and `eventsintel-api`
  restarted active after the final deploy. Public smoke returned 200 for
  `/`, `/healthz`, `/llms.txt`, `/api/v1`, `/api/v1/schema`,
  `/api/v1/openapi.yaml`, `/api/v1/events`, `/assets/events.js`, and
  `/assets/app.js`. Public HTML references `/assets/app.js?v=20260622-hide-more`,
  and that versioned asset contains the bounded scan fix. Production Chrome/CDP
  verified desktop and mobile default views load 33 cards, 33 unique IDs, count
  `33개 주요 행사`, hidden "더 보기", and no horizontal overflow.
- Action-field enrichment local verification (2026-06-22): `go test ./...`,
  `go vet ./...`, `go build ./cmd/eventsintel`, `node --check static/detail.js`,
  `node --check static/app.js`, `node --check static/events.js`, and
  `git diff --check` pass. `internal/pipeline`
  `TestRun_EnrichesActionFieldsFromOfficialPage` drives a venue detail through
  optional official-page fetch, extraction, normalization, SQLite storage, and
  the `/api/v1/events/{event_id}` read API, proving `register_url`,
  `exhibit_url`, `registration_deadline`, `exhibitor_deadline`, `cost_hint`,
  action booleans, and organizer source provenance are observable through the
  public contract.
- Action detail UI local verification (2026-06-22): detail modal assets now
  render source-backed cost and action signal chips in the `참가 정보` section.
  `go test ./internal/api -run TestRootIndexServesSplitFrontendAssets -count=1`
  verifies the split CSS asset exposes `.detail-signals`; `node --check
  static/detail.js`, `node --check static/app.js`, and `node --check
  static/events.js` pass. A Node render smoke verified the modal contains cost,
  registration/exhibit deadlines, action chips, and official/register/exhibit
  links for an action-enriched event. Playwright captured a local screenshot at
  `/tmp/events-action-modal.png`, and visual inspection confirmed the new chips
  and links render without overlap at 1024px width. Full gates also pass:
  `go test ./...`, `go vet ./...`, `go build ./cmd/eventsintel`, and
  `git diff --check`. LSP diagnostics could not run because the transport
  closed.
- Action intelligence contract/live verification (2026-06-22): embedded
  `static/openapi.yaml` now documents the `Actions` object plus `register_url`,
  `exhibit_url`, `registration_deadline`, `exhibitor_deadline`, `homepage_url`,
  and `summary`; `static/llms.txt` tells agents to check `missing_fields[]`
  before treating false action booleans as source-backed absence. Added
  `TestOpenAPIEventSchemaDocumentsActionFields` to prevent contract drift.
  Bounded live ingest to `/tmp/events-action-live.db` with
  `EVENTSINTEL_MAX_DISCOVER=25`, `EVENTSINTEL_RATE_PER_MIN=240`,
  `EVENTSINTEL_SOURCE_CONCURRENCY=2`, `EVENTSINTEL_DETAIL_WORKERS=4`, and
  `EVENTSINTEL_INGEST_DEADLINE=4m` stored COEX 25/25 and KINTEX 25/25. The
  temp DB had 21 register links, 27 exhibit links, 22 non-unknown cost hints, 3
  sponsor signals, 2 matchmaking signals, and 1 startup-program signal; no
  deadline signal appeared in that bounded live sample, so deadline fields
  remained in `missing_fields[]`. Local API smoke against the temp DB proved
  `GET /api/v1/events/kintex-26042904` returns action booleans, register/exhibit
  links, `cost_hint=free`, two provenance sources including an organizer source,
  and missing-field honesty for absent deadlines. Playwright screenshots from
  served API data and served `detail.js`/`detail.css` were captured at
  `/tmp/events-action-modal-live-1280.png` and
  `/tmp/events-action-modal-live-375.png`; visual inspection confirmed action
  chips and links render without overlap on desktop and mobile. Final gates
  passed: `go test ./...`, `go vet ./...`, `go build ./cmd/eventsintel`,
  `node --check static/detail.js && node --check static/app.js && node --check
  static/events.js`, and `git diff --check`. LSP diagnostics could not run
  because the transport closed.
- Homepage title redeployment verification (2026-06-22): committed
  `5583f5c fix(frontend): simplify homepage title`; deployed with
  `go build -buildvcs=false` to `developer-vps`. Remote binary SHA256 matched
  `293fd32e0f6f8648494f6eebfc54d73a9bc1cda83d131c4ffdd4d3dd984ad576`, and
  `eventsintel-api` restarted active. Public smoke returned 200 for `/`,
  `/healthz`, `/llms.txt`, `/api/v1`, `/api/v1/schema`,
  `/api/v1/openapi.yaml`, `/api/v1/events`, and `/assets/app.js`. Public HTML
  and production Chrome/CDP verified both browser title and visible H1 are
  `행사 모아보기`, with no mobile horizontal overflow.
- Event summary persistence and backfill deployment verification (2026-06-22):
  committed `fe51f48 fix(store): persist event summaries`,
  `81e9878 fix(store): backfill summaries for unchanged events`, and
  `7f357f6 fix(store): backfill existing event summaries`. Deployed
  linux/amd64 binary SHA256
  `1c90e050738e7cdd6e83ff3281e30ff29d6f82f738f97e684685fc78414058a8` to
  `developer-vps`; remote `/srv/developer/events-intel/eventsintel` matched the
  same SHA and `eventsintel-api` restarted active at
  `2026-06-22 08:35:56 UTC`. Manual production ingest completed at
  `2026-06-22 08:50:04 UTC` with COEX discovered/stored `400/400` and KINTEX
  discovered/stored `38/38`. A copied production SQLite snapshot verified
  `531/531` rows have non-null `summary`; public
  `/api/v1/events?limit=20&since=2026-06-22` returned 20 rows and 20 non-null
  summaries. Public smoke returned 200 for `/`, `/healthz`, `/llms.txt`,
  `/api/v1`, `/api/v1/schema`, `/api/v1/openapi.yaml`, and `/api/v1/events`.
- Source-derived summary policy deployment verification (2026-06-22): committed
  `60769f4 fix(summary): use source-derived event descriptions`,
  `ab60b4a fix(store): mark missing cleared summaries`, and
  `c88f01b fix(frontend): bust source summary asset cache`. List cards no
  longer render or search hidden `summary` text. COEX summaries now come from
  venue detail content such as `행사 소개`; KINTEX summaries now come from
  `행사내용`, then `행사목적`, then `행사품목`. Normalize no longer fabricates
  summaries from name/date/venue/organizer; absent source description stays
  null and records `summary` in `missing_fields`. The previous template summary
  migration is disabled, existing template-shaped summaries are cleared, and
  null summaries are marked missing. Local gates passed: `go test ./...`,
  `go vet ./...`, `go build ./cmd/eventsintel`, `node --check static/app.js`,
  `node --check static/detail.js`, and `git diff --check`. A temporary live
  ingest DB stored 16/16 source-derived summaries and 0 template-shaped
  summaries; local Chrome/Selenium verified list cards render 0 `.card-summary`
  elements, search placeholder is `행사명 검색`, and the detail modal still shows
  `행사 설명` from source text. Deployed final linux/amd64 binary SHA256
  `abb3e8ef1d57ea10d545b8121e8d8a26b3317fdafab24f4ddf4fa27f443b2103` to
  `developer-vps`; remote `/srv/developer/events-intel/eventsintel` matched and
  `eventsintel-api` restarted active at `2026-06-22 09:41:11 UTC`. Production
  ingest completed at `2026-06-22 09:37:02 UTC` with COEX discovered/stored
  `400/400` and KINTEX discovered/stored `38/38`. A copied production SQLite
  snapshot verified `531` rows total, `409` non-null source-derived summaries,
  `0` template-shaped summaries, and `0` null-summary rows missing
  `missing_fields=["summary"]`. Public smoke returned 200 for `/`, `/healthz`,
  `/llms.txt`, `/api/v1`, `/api/v1/schema`, `/api/v1/openapi.yaml`,
  `/api/v1/events?limit=5`, and `/assets/app.js?v=20260622-source-summary`.
  Production Chrome/Selenium verified list cards render 0 `.card-summary`
  elements, cards do not contain the summary excerpt, and the detail modal for
  `kintex-26041303` renders `행사 설명` with the source summary excerpt. LSP
  diagnostics could not run because the LSP transport closed.
- Refresh/lookahead local verification (2026-06-22): set the ingest timer
  and default crawl interval to 24h, expanded COEX current schedule
  discovery from 180 to 365 days, increased COEX schedule page walking from 20
  to 80 pages, and changed COEX discovery to use the current schedule as the
  primary path with sitemap discovery retained only as fallback. Local gates
  passed: `go test ./...`, `go vet ./...`, and `go build ./cmd/eventsintel`.
  `gopls` diagnostics could not run because the server is not installed in this
  environment. A temporary live ingest with `EVENTSINTEL_MAX_DISCOVER=80`,
  `EVENTSINTEL_RATE_PER_MIN=240`, `EVENTSINTEL_SOURCE_CONCURRENCY=2`,
  `EVENTSINTEL_DETAIL_WORKERS=4`, and `EVENTSINTEL_INGEST_DEADLINE=5m`
  completed with COEX discovered/stored `94/94` from raw `94`, `dropped_by_cap=0`,
  and KINTEX discovered/stored `38/38`, with zero skipped items.
- Refresh/lookahead deployment verification (2026-06-22): deployed
  linux/amd64 binary SHA256
  `61a314e30dbdacb9a4d600edc7664f4a728106330985e0986492cae5ef2e4d17` to
  `developer-vps`; remote `/srv/developer/events-intel/eventsintel` matched the
  same SHA. `eventsintel-api` and `eventsintel-ingest.timer` are active. The
  first production run
  after switching COEX to schedule-primary discovery hit the expected breaker
  once because the previous COEX baseline was sitemap-capped at `400`; the
  verified COEX batch baseline was reset to `94/94/94`. The confirmation
  production ingest completed at `2026-06-22 11:13:00 UTC` with COEX
  discovered/stored `94/94` from raw `94`, `dropped_by_cap=0`, and KINTEX
  discovered/stored `38/38`, with zero skipped items. Public smoke returned 200
  for `/healthz` and `/api/v1/events`, and public API query
  `venue=coex&since=2026-06-22&before=2027-06-22&limit=100` returned 94 COEX
  rows with no next-page cursor.
- Daily refresh deployment verification (2026-06-22): updated the deployed
  binary and systemd timer after the user clarified the refresh interval should
  be longer. Deployed linux/amd64 binary SHA256
  `b39b85511151b87879b6b7dee37466101e64f1349f5b5bbf2a525c035aaeb293` to
  `developer-vps`; remote `/srv/developer/events-intel/eventsintel` matched the
  same SHA. `eventsintel-api` and `eventsintel-ingest.timer` are active, the
  installed timer now has `OnUnitActiveSec=24h`, and the next scheduled ingest
  is `2026-06-23 11:09:25 UTC`. Public smoke returned 200 for `/healthz` and
  `/api/v1/events`.
- Action enrichment deployment/backfill verification (2026-06-22): committed
  `a0c1bcb feat(ingest): enrich events with organizer actions`; deployed
  linux/amd64 binary SHA256
  `3f74c1a8f5ff4a29fe262d66dfc952853d1df286b06cf211168e1690b13c5a29` to
  `developer-vps`, backing up the previous binary first. Remote
  `/srv/developer/events-intel/eventsintel` matched the same SHA, and
  `eventsintel-api` restarted active. Manual production backfill ran through
  `eventsintel-ingest.service` and completed at `2026-06-22 12:57:56 UTC` with
  COEX discovered/stored `94/94` and KINTEX discovered/stored `38/38`. Public
  smoke returned 200 for `/`, `/healthz`, `/llms.txt`, `/api/v1`,
  `/api/v1/schema`, `/api/v1/openapi.yaml`, `/api/v1/events`,
  `/assets/detail.js`, and `/assets/detail.css`. Public OpenAPI exposes
  `Actions`, `register_url`, and `homepage_url`. Public API query
  `venue=kintex&since=2026-06-22&limit=100` returned 38 rows with 13 register
  links, 12 exhibit links, 10 non-unknown cost hints, 1 sponsor signal, and 1
  matchmaking signal. Sample event `kintex-26051208` returns register and
  exhibit URLs, two provenance sources including an organizer source, and
  missing-field honesty for unknown deadline/cost/sponsor/program fields.
- Benchmark-source planning (2026-06-23): `docs/founder-operator-event-radar.md`
  defines the founder/operator workflow, a 25-source benchmark set across AI,
  robotics, bio, digital health, and medtech, and source-quality/completeness
  criteria.
- International benchmark source implementation (2026-06-23): added the first
  registered `benchmark` adapter slice for official international event pages.
  Targeted red/green coverage:
  `go test ./internal/sources/benchmark ./internal/normalize ./internal/config
  -run 'TestParse_|TestNormalize_InternationalParsedEvent|TestDefault_IncludesBenchmarkSource'
  -count=1`.
- International benchmark local verification (2026-06-23): `go test ./...`,
  `go vet ./...`, `go build -o /tmp/eventsintel-benchmark ./cmd/eventsintel`,
  and `git diff --check` pass. Local ingest against `/tmp/eventsintel-benchmark.db`
  stored benchmark `3/3`, COEX `3/3`, and KINTEX `3/3` with `EVENTSINTEL_MAX_DISCOVER=3`.
  Local API on `127.0.0.1:8101` returned all three benchmark rows, and browser
  QA confirmed the homepage remained stable with no horizontal overflow at
  1280px or 390px widths.
- Benchmark source expansion slice (2026-06-23): expanded the registered
  `benchmark` adapter from 3 to exactly 10 official future/upcoming benchmark
  events: CES 2027, NVIDIA GTC 2027, BIO International Convention 2027,
  Web Summit Lisbon 2026, Slush 2026, TechCrunch Disrupt 2026,
  World Summit AI 2026, HLTH USA 2026, The MedTech Conference 2026, and
  MEDICA 2026. Added catalog fallback dates, summaries, action signals, and
  missing-field honesty so JS-heavy pages can still produce structured,
  source-declared records without inventing facts. Focused red/green and
  runtime evidence:
  `go test ./internal/sources/benchmark ./internal/pipeline ./internal/normalize
  ./cmd/eventsintel -count=1`, `go vet ./internal/sources/benchmark
  ./internal/pipeline ./internal/normalize ./cmd/eventsintel`, JSONL `jq`
  structure checks, and a temp `eventsintel ingest` + localhost
  `/api/v1/events?limit=100` smoke that stored and returned 10/10 benchmark rows
  with dates, place, homepage, and action-or-honesty fields.
- Benchmark action-honesty follow-up (2026-06-23): scoped generic second-hop
  action enrichment away from benchmark catalog records so broad organizer
  navigation cannot fill future-event register/exhibit/sponsor fields without
  source-backed event-year evidence. Added a pipeline regression for BIO
  International Convention 2027; fresh localhost API evidence on
  `127.0.0.1:8110` returns BIO with `has_matchmaking=true`, null
  register/exhibit URLs, and register/exhibit/deadline honesty in
  `missing_fields`.
- Benchmark source finalization (2026-06-23): renamed the benchmark planning
  artifact to `docs/founder-operator-event-radar.md`, updated the original gap
  note to reflect the completed 10-source slice, and tightened date
  normalization so RFC3339 timestamps are parsed strictly while invalid
  ISO-shaped prefixes are rejected instead of sliced.
- Benchmark event-family expansion (2026-06-23): changed the registered
  `benchmark` adapter from 10 to 31 official event-family records by including
  all Oracle-recommended additions: Web Summit Qatar, BioJapan, Medical Taiwan,
  HIMSS Global, automatica, Robotics Summit, SWITCH, RSNA, COMPUTEX / InnoVEX,
  VivaTech, World Health Expo Dubai / WHX Tech, BIO-Europe, AI & Big Data Expo
  Europe, HLTH Europe, ICRA, HIMSS AI in Healthcare Forum, HumanX Europe, IROS,
  GITEX AI Europe, plus China coverage for WAIC Shanghai and World Robot
  Conference Beijing. Added official URLs, dates, region/domain notes, and at
  least one action signal or missing-field honesty path for each source family.
  Local ingest against `/tmp/eventsintel-benchmark-all.db` stored benchmark
  `31/31`, COEX `31/31`, and KINTEX `31/31` with `EVENTSINTEL_MAX_DISCOVER=31`.
- Benchmark ordering policy (2026-06-23): UI sorting now groups benchmark rows
  as upcoming dated events first, TBA/date-missing records next, and completed
  events last. Completed events sort by most recently ended first inside the
  archive group.
- Benchmark list deployment (2026-06-23): committed `5dccb2c feat(events):
  expand benchmark event lists`, deployed linux/amd64 binary SHA256
  `34958505ee2daa4a40db901819ae19eb88085070144a9b6243415964f31f24ec` to
  `developer-vps`, and restarted `eventsintel-api` active at
  `2026-06-23 09:27:07 UTC`. Manual production ingest completed at
  `2026-06-23 09:31:12 UTC` with benchmark `31/31`, COEX `95/95`, and KINTEX
  `38/38` stored. Public smoke returned 200 for `/`, `/healthz`, `/llms.txt`,
  `/api/v1`, `/api/v1/schema`, `/api/v1/openapi.yaml`, and `/api/v1/events`;
  public HTML shows `AI·로봇·헬스케어 행사 모아보기`, `국내 행사 일정`, and
  `글로벌 주요 행사`. Public API `list=benchmark&limit=100` returned 31
  benchmark-only rows including WAIC Shanghai and World Robot Conference
  Beijing; `list=venue&since=2026-06-23&limit=100` returned no benchmark IDs.
- Benchmark ICML addition (2026-07-06): added ICML 2026 as the 32nd official
  benchmark source family after confirming the official ICML page exposes
  COEX Seoul dates, registration/pricing links, workshop registration status,
  and closed exhibitor applications. Added `icml.cc` to the ingest allowlist
  and updated benchmark seed/count docs.
- Benchmark ICML deployment verification (2026-07-06): deployed linux/amd64
  binary SHA256
  `188394d296a0f17fd1d472613576d378cf03fb940642c1783e818d93e7b4a748` to
  `developer-vps`, backing up the previous binary as
  `eventsintel.bak-20260706T111436Z`. `eventsintel-api` restarted active, manual
  production ingest completed at `2026-07-06 11:21:26 UTC` with benchmark
  `32/32`, COEX `83/83`, KINTEX `38/38`, and SHOWALA `65/65`, then Cloudflare
  cache purged. `deploy/verify.sh` printed `ALL CHECKS PASSED`, and public API
  `list=benchmark&limit=100` returned `benchmark-icml-2026` plus 32 benchmark
  rows.
- Academic benchmark expansion (2026-07-06): expanded the official benchmark
  source family list from 32 to 43 by adding ACL 2026, ACM KDD 2026,
  IJCAI-ECAI 2026, SIGGRAPH 2026, AIES 2026, COLM 2026, EMNLP 2026,
  NeurIPS 2026, AAAI-27, ICLR 2027, and CVPR 2027. ICLR 2027 uses `TBA`
  dates because the official future-meetings page confirms only the region
  so far; normalization should mark date fields missing honestly.
- Academic benchmark deployment verification (2026-07-06): committed
  `66f6f11 feat(events): expand academic benchmark sources`, deployed
  linux/amd64 binary SHA256
  `186338e9ac22793e836f72dce0be10d2d94a3db58a1ff525232ce9c3102088df` to
  `developer-vps`, and restarted `eventsintel-api`. Manual production ingest
  completed at `2026-07-06 11:56:11 UTC` with benchmark `43/43`, COEX `83/83`,
  KINTEX `38/38`, and SHOWALA `65/65`, then Cloudflare cache purged.
  `deploy/verify.sh` printed `ALL CHECKS PASSED`; public API
  `list=benchmark&limit=100` returned 43 benchmark rows including all 11 new
  academic additions, with ICLR 2027 date fields empty as expected.
- Solar Open 2 Stage 1 verification (2026-07-17): direct model access confirmed,
  and an omitted `reasoning_effort` failure was isolated with API toggle proof.
  The client now sends `minimal` for `solar-open2`, protected by an httptest
  regression. Final extraction scored 54/54 fields across 6 Korean event cases
  with 118 average output tokens and 1.9s average latency. Solar-backed
  `eventagent`, `eventscout`, and live-API `eventmcp` scenarios all completed.
  Reproducible evidence is in
  `.tasks/260711-solar-agent/abbench-solar-open2-20260717.md`.
- Solar live-search judgment evaluation (2026-07-17): ran the three Korean
  queries proposed by Solar through a real web-search tool, then passed 12
  returned hits to the unchanged Solar source judge. It accepted all 8 labeled
  official event/venue sources and rejected all 4 report/admissions negatives
  (12/12; 720 input + 808 output tokens; 12.42 seconds). Direct opens verified
  representative organizer and venue schedules. A Bing RSS retrieval attempt
  scored 0/5 relevance and was removed. Evidence and the manual-bridge caveat
  are in `.tasks/260711-solar-agent/eventscout-live-search-eval-20260717.md`.
- Automated Tavily source discovery (2026-07-18): added the credential-backed
  Tavily `SearchTool`, CLI provider selection, bounded HTTP handling, outbound
  contact redaction, result sanitization, and fixture-preserving regression
  coverage in `db70d64`. Targeted and full race tests plus missing-key fail-fast
  checks pass. A real Solar+Tavily transcript remains pending until the user
  completes the authorized GitHub/Tavily account flow; no credential has been
  stored or exposed.
- Anonymous discovery documentation/governance (2026-07-20): `eventscout` now
  documents the keyless `public` provider as the default, with `fixture` and
  Tavily as explicit optional modes. The separate `eventscout-server` runbook
  documents anonymous goal-only requests, no signup/arbitrary URLs/private
  networking, the exact 4 KiB/800-rune/2-per-10-minute/24-per-day/2-active/60s
  service limits and stable error behavior, plus the operator-only Solar key
  startup requirement. Structured server logs are documented as metadata-only;
  the normal cache-first read API remains unchanged. A zero-third-party-key
  local smoke invocation and source/config/link verification are recorded in
  `.omo/evidence/task-6-docs-governance.txt`.

## Validation Notes

No product validation has been run yet. Current evidence level is Low.

## Metrics Or Signals

| Signal | Current Evidence | Interpretation |
|---|---|---|
| Source coverage | Not measured | Need manual dataset |
| API usefulness | Not measured | Need sample contract and user/agent workflow review |
| Hosting fit | Existing VPS documented as suitable for small public services | Good enough for MVP if cache-first |

## Next Action

Finish the Solar Agent Partner Stage 1 public README/review, commit and push the
verified `feat/solar-agent` branch, then submit the repository link and review
before 2026-07-31. The previous benchmark-source enrichment choice remains the
next core-product action after the Solar submission.

## Decision Needed

- Decision: which benchmark sources need source-specific deadline, exhibitor,
  and program extraction after the 43-source baseline is verified.
- Evidence Needed: candidate venue/organizer source families
- Deadline: before seed dataset fill begins

## Change Log

| Date | Change | Reason |
|---|---|---|
| 2026-06-21 | Initial progress file | Project promoted from idea document |
| 2026-06-21 | Target user = founders/operators; format = JSONL; created schema + seed prototype | Resolved gating decisions and executed the documented Next Action |

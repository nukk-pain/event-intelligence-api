# Progress

## Metadata

- Current Status: `shaping`
- Current Stage: Shape
- Owner: smpain
- Started: 2026-06-21
- Last Updated: 2026-06-22

## Current Focus

Tighten the public event browsing experience while keeping the read-only API
stable and deployment-ready.

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
- [x] Added COEX current schedule discovery using a 180-day date range and
  schedule pagination so future pages such as August 2026 are not missed.
- [x] Updated the public UI copy and client ordering to show COEX/KINTEX
  schedules as a date-first event browser instead of a narrow industry-only
  filter.
- [x] Changed the homepage default to upcoming categorized events, removed past
  event controls, added a `모든 행사 보기` scope option, and moved integration
  links from the header to the footer.
- [x] Removed the homepage subtitle copy under the title so the browsing
  controls become the first visible instruction.

## In Progress

- [x] COEX/KINTEX ingestion feasibility check (`prototype/coex-kintex-feasibility.md`)
  — both static-HTML, field-rich, HTTP-parseable; no headless browser needed.
- [ ] `/plan` the COEX/KINTEX ingestion backend + ADR superseding manual-first.
- [ ] (Deferred) Choose 20-30 international benchmark sources.

## Blockers

| Blocker | Impact | Owner | Resolution Needed |
|---|---|---|---|
| International benchmark source list is undefined | Medium | smpain | Choose 20-30 benchmark events or source families |

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

## Validation Notes

No product validation has been run yet. Current evidence level is Low.

## Metrics Or Signals

| Signal | Current Evidence | Interpretation |
|---|---|---|
| Source coverage | Not measured | Need manual dataset |
| API usefulness | Not measured | Need sample contract and user/agent workflow review |
| Hosting fit | Existing VPS documented as suitable for small public services | Good enough for MVP if cache-first |

## Next Action

Continue source coverage and UI review with the homepage title-only header.

## Decision Needed

- Decision: international benchmark source list (which 20-30 events define
  "good enough" first-30-day coverage)
- Evidence Needed: candidate venue/organizer source families
- Deadline: before seed dataset fill begins

## Change Log

| Date | Change | Reason |
|---|---|---|
| 2026-06-21 | Initial progress file | Project promoted from idea document |
| 2026-06-21 | Target user = founders/operators; format = JSONL; created schema + seed prototype | Resolved gating decisions and executed the documented Next Action |

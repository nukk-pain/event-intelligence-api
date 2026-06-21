# Progress

## Metadata

- Current Status: `shaping`
- Current Stage: Shape
- Owner: smpain
- Started: 2026-06-21
- Last Updated: 2026-06-21

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

## Validation Notes

No product validation has been run yet. Current evidence level is Low.

## Metrics Or Signals

| Signal | Current Evidence | Interpretation |
|---|---|---|
| Source coverage | Not measured | Need manual dataset |
| API usefulness | Not measured | Need sample contract and user/agent workflow review |
| Hosting fit | Existing VPS documented as suitable for small public services | Good enough for MVP if cache-first |

## Next Action

Continue source-verified dataset expansion, starting with fresher COEX/KINTEX
event discovery.

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

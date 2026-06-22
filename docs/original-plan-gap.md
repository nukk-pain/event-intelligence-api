# Original Plan Gap

## Metadata

- Status: current gap note
- Date: 2026-06-23
- Scope: compare the original repo plan with the current implemented product
- Sources: `IDEA.md`, `PLAN.md`, `DECISIONS.md`, `PROGRESS.md`,
  `docs/decisions/0002-automation-first.md`,
  `.tasks/260621-coex-kintex-ingestion/PLAN.md`

## Summary

The original repo plan was to build a free, public event intelligence service
for COEX/KINTEX first, then domestic and international AI, humanoid robotics,
bio, digital-health, and medical-device events. It would expose both a readable
human surface and a stable, read-only, agent-friendly API.

The current product has already moved beyond the original shaping stage for the
domestic venue wedge: COEX/KINTEX ingestion, normalization, SQLite storage,
read-only API, `llms.txt`, public UI, and deployment to `events.nukk.net` exist.
Action-oriented organizer-page enrichment has also been implemented and
backfilled. The main remaining gaps are now international/source breadth, the
choice of one target workflow, alert/export use cases, API-key tiers, and
long-term data quality operations.

## Superseded Gap

The original `PLAN.md` said implementation should wait until a 30-day manual
coverage dataset was built and reviewed. That sequencing gap is no longer
active.

`docs/decisions/0002-automation-first.md` explicitly superseded the
manual-dataset-first gate because the v0.1 schema was validated, COEX/KINTEX
source feasibility was verified, and freshness/change tracking required an
automated pipeline. The retained artifact is the v0.1 schema contract in
`prototype/manual-dataset-schema.md`, not a hand-maintained source of truth.

## Current Implemented State

| Area | Current state |
|---|---|
| Domestic venue wedge | COEX and KINTEX are the implemented launch sources. |
| Ingestion | Deterministic HTTP-first ingestion exists, with scheduled refresh, source concurrency, and polite rate controls. |
| Store | SQLite is the canonical runtime store, with WAL-oriented API/ingest deployment. |
| API | `/api/v1`, `/api/v1/events`, detail, sources, changes, schema, OpenAPI, and `llms.txt` surfaces exist. |
| UI | Public event browser exists at `events.nukk.net`, with COEX/KINTEX date browsing and a default major-event view. |
| Deployment | Systemd-managed static Go binary is deployed behind Caddy/Cloudflare. |
| Cost posture | Normal reads are database/cache/static backed; no live LLM generation is part of the read path. |
| Data policy | Public-only, read-only, source-provenance-first posture is intact. |
| Action enrichment | Official/organizer page second-hop enrichment exists for public `homepage_url` records. Public API and modal UI expose register/exhibit links, cost hints, action booleans, and missing-field honesty. |

Production evidence as of the action enrichment backfill:

- Commit `a0c1bcb` deployed to `events.nukk.net` and backfilled through
  `eventsintel-ingest.service` on 2026-06-22.
- Production ingest completed with COEX `94/94` and KINTEX `38/38` stored.
- Public KINTEX query showed 13 register links, 12 exhibit links, 10 cost hints,
  1 sponsor signal, and 1 matchmaking signal.
- Direct production SQLite inspection on 2026-06-23 showed 535 total rows, 56
  rows with `register_url`, 61 with `exhibit_url`, and 49 with non-unknown
  `cost_hint`.

## Remaining Gaps Against Original Plan

| Original plan area | Gap now | Notes / next action |
|---|---|---|
| Domestic and international coverage | International benchmark events are still deferred. | Choose 20-30 benchmark events or source families before adding global coverage. |
| Korea-wide venue coverage | BEXCO, SETEC, and other Korean venues are not implemented. | Add only after COEX/KINTEX quality and maintenance cost are stable. |
| Industry intelligence taxonomy | The taxonomy exists, but the public surface is still mostly venue/date browsing. | Decide whether the first product should optimize for founders/operators, BD teams, analysts, or agent developers. |
| Action-oriented fields | First-pass action enrichment exists, but coverage is partial and heuristic. | Register/exhibit/cost signals are now populated where public organizer pages expose them. Deadline, sponsor, matchmaking, startup-program, and application-period extraction still needs source-specific rules and PDF/microsite handling. |
| Exhibitor/sponsor/program data | Basic booleans and links exist, but full structured programs are not reliable. | Requires organizer microsites, PDFs, exhibitor lists, sponsor package pages, or event-specific source adapters beyond generic link/text matching. |
| Alerts and saved filters | Not implemented. | Original human surface mentioned saved filters, alerts, and time-sensitive views. |
| CRM/export workflows | Not implemented. | Original concept included BD and sales workflows; current API is a base data surface only. |
| Agent polling at scale | Basic read-only API and change feed exist, but free API keys and partner keys are not implemented. | Launch policy documented keyless, free-key, and allowlisted tiers; current public surface is effectively keyless. |
| Quota operations | Keyless quota policy exists in API docs/code, but real usage-based tuning is not documented yet. | Revisit after traffic logs show request shape, cache hit rate, and crawler cost. |
| Data quality operations | Parser tests and deployment evidence exist, but no long-running correction workflow is documented. | Need correction process, source-breakage triage, and freshness SLA if this becomes more than a prototype. |
| Human editorial layer | "Why this event matters" and curated notes are not implemented. | Original plan allowed editorial notes only when evidence exists; not required for the current MVP. |
| Monetization | No paid product or billing exists. | This matches the original capture-time decision: free public service, no active monetization plan. |
| API write/enrichment endpoints | Not implemented. | This matches the original non-goal for public v1. |
| Live LLM answers | Not implemented. | This matches the original cost-efficiency rule and read-path non-goal. |

## Product Gap Framing

The current product proves the domestic venue ingestion and public read surface.
The action-enrichment backfill makes the COEX/KINTEX wedge more useful, but the
product still does not prove the broader "industry intelligence" claim. To prove
that claim, the next work should add either:

1. More source breadth: international benchmarks and more Korean venues.
2. More workflow depth: better deadlines/program/exhibitor data, alerts, and
   exports for one chosen user workflow.

Doing both at once risks turning the project into a generic event portal. The
original plan warned against that. The next scope decision should choose one
axis and keep the other constrained.

## Concrete Next Decisions

- First user/workflow: founders/operators, BD teams, investors/analysts,
  attendees, or agent/API developers.
- Next expansion axis: more sources vs deeper workflow fields.
- International benchmark set: which 20-30 events or source families define
  "global AI/humanoid/bio/medical-device coverage".
- API identity: stay keyless only, or add free API keys for stable agent usage.
- Data operations: minimum freshness SLA, correction process, and source-breakage
  response policy.

## Recommended Next Gap

The next gap to fill should be **workflow focus plus source set selection**, not
another generic field pass.

Reason: COEX/KINTEX now have the basic date, summary, action, provenance, API,
and UI surfaces. The remaining work can easily branch into a generic event
portal unless the product chooses one concrete workflow. The highest-leverage
choice is:

**Startup founders / operators tracking AI, robotics, bio, digital-health, and
medical-device opportunities.**

For that workflow, the next implementation should define a small benchmark set
and add source coverage around it:

- 20-30 international or regional benchmark events/source families.
- A source-quality scorecard: event date, official URL, registration/exhibit
  links, cost, deadlines, program/partnering signal, provenance, and freshness.
- A first "opportunity shortlist" view/API filter for founder/operator use:
  upcoming, source-backed, actionable events with missing fields visible.

This directly tests the original thesis that the product is an industry
intelligence layer, not only a COEX/KINTEX schedule browser.

Current planning artifact:

- `docs/founder-operator-opportunity-radar.md`

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

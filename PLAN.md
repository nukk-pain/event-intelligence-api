# Plan

## Metadata

- Status: `shaping` (this document) — active execution scope has moved to the
  task plan below
- Lifecycle Stage: Shape
- Owner: smpain
- Created: 2026-06-21
- Last Updated: 2026-06-21

> **Active execution scope:** This root `PLAN.md` is the shaping/strategy
> document. The authoritative, executable plan for the current build is
> `.tasks/260621-coex-kintex-ingestion/PLAN.md` (COEX/KINTEX ingestion backend +
> read-only API). Per `docs/decisions/0002-automation-first.md` (2026-06-21), the
> manual-dataset-first gate described below is superseded by an automation-first
> approach; the v0.1 schema in `prototype/manual-dataset-schema.md` is retained as
> the data contract. Single execution scope lives in the `.tasks` plan.

## Summary

Shape a free public event intelligence UI and read-only API for AI, humanoid,
bio, digital health, and medical-device events, starting with COEX and KINTEX
and deploying the MVP under `events.nukk.net`.

## Problem

High-signal industry events are scattered across venue pages, organizer sites,
PDFs, microsites, and press releases. Humans need a readable filtered view, and
agents need stable structured data with provenance, change tracking, and low
ambiguity.

## Goals

- Define the first useful event data schema and API contract.
- Build a 30-day manual coverage dataset to validate sources and fields before automation.
- Keep API reads free, quota-limited, read-only, cache-first, and agent-friendly.
- Prepare for MVP deployment on the existing Developer DigitalOcean VPS using `events.nukk.net`.

## Non-Goals

- No paid product or billing flow in the initial scope.
- No live LLM generation on normal API reads.
- No clinic, patient, EMR, medical-platform private connector, or private network data.
- No broad consumer event portal before the focused industry wedge is validated.
- No organizer self-serve submission workflow until data QA and abuse controls are clear.

## Proposed Approach

Start with a manual seed dataset and schema-first API design. Use the dataset to
validate what can be reliably captured from COEX, KINTEX, and selected
international benchmark events. Only after the data model is proven should the
project add crawler automation, UI, and API implementation.

## Scope

### Included

- Manual event dataset schema.
- Seed dataset covering COEX/KINTEX and 20-30 international benchmark events.
- API resource and field draft.
- Free quota policy and cache rules.
- Deployment assumptions for `events.nukk.net` on the existing VPS.
- Source provenance and update-state conventions.

### Excluded

- Payment, billing, premium access, or paid API tiers.
- User accounts unless needed for free API keys.
- Write/enrichment endpoints in the public API.
- Live LLM response generation for event lookups.
- Private medical or clinic data.

## Risks

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Source pages are incomplete or inconsistent | High | High | Start with manual dataset to identify reliable fields and failure modes |
| API abuse overloads the shared VPS | Medium | Medium | Use read-only endpoints, cache headers, pagination limits, and protective quotas |
| Event data becomes stale | High | Medium | Track `last_checked_at`, update state, source provenance, and changes feed |
| Copyright issues from copied descriptions | Medium | Medium | Store short factual summaries and links, not wholesale descriptions |
| Scope expands into generic event portal | Medium | Medium | Keep first taxonomy focused on AI, robotics, bio, digital health, and medical devices |
| LLM costs creep into read path | Medium | Low | Prohibit live LLM generation on normal API reads |

## Dependencies

- Source idea document in `docs/ideas/ai/`.
- Developer VPS and `nukk.net` Cloudflare/Caddy deployment setup.
- COEX/KINTEX source availability.
- Selected international benchmark event source list.
- Final choice of implementation stack before code starts.

## Acceptance Criteria

- [ ] A dataset schema exists for the 30-day manual coverage test.
- [ ] A seed dataset file exists with representative COEX/KINTEX and international records.
- [ ] Each record includes source URL, status, confidence, and missing-field tracking.
- [ ] A minimal API contract draft exists for event list, detail, changes, sources, and schema endpoints.
- [ ] Free API quota rules are documented and consistent with cache-first serving.
- [ ] Deployment notes identify the `events.nukk.net` DNS/Caddy path and VPS constraints.

## Verification Strategy

| Criterion | Verification Method | Evidence |
|---|---|---|
| Dataset schema exists | Inspect schema/data files | `prototype/` or later data path |
| Seed records exist | Validate record count and required fields | Manual dataset file |
| Provenance fields exist | Inspect sample records | Manual dataset file |
| API contract exists | Inspect OpenAPI or markdown contract | Later contract file |
| Quota rules documented | Inspect `IDEA.md` and future API docs | `IDEA.md`, API docs |
| Deployment notes documented | Inspect `IDEA.md` and deployment docs | `IDEA.md`, future deployment plan |

## Prototype Question

Can a small manually curated dataset produce a useful, reliable, agent-readable
event feed before crawler automation is built?

## Prototype Method

Create a 50-100 record manual dataset covering COEX/KINTEX and 20-30
international benchmark events. Fill the target fields, mark missing data and
ambiguity, then derive the minimal API contract from observed data reality.

## Decision State

- Current Recommendation: continue
- Evidence Level: low
- Decision Needed By: after the first manual dataset review

## Open Questions

- [ ] Which target user/workflow should the first prototype optimize for?
- [ ] Which international sources define the benchmark event set?
- [ ] Should the first dataset be CSV, JSONL, SQLite, or all three?
- [ ] Which implementation stack should be used after schema validation?
- [ ] Should free API keys exist at launch, or only after keyless traffic appears?

## Approval

- Reviewer: smpain
- Decision: approved for project promotion
- Date: 2026-06-21
- Notes: Promote from idea document into `/Users/smpain/Developer/event-intelligence-api/`.

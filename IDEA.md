# AI Industry Event Intelligence API

## Metadata

- Title: AI Industry Event Intelligence API
- Status: `shaping`
- Lifecycle Stage: Shape
- Date Captured: 2026-06-21
- Date Promoted: 2026-06-21
- Source: `/Users/smpain/Developer/docs/ideas/ai/2026-06-21-ai-industry-event-intelligence-api.md`
- Owner: smpain
- Evidence Level: Low
- Working Category: event intelligence / AI industry data / agent-friendly API / exhibition discovery
- Target Domain: `events.nukk.net`

## One-Line Thesis

COEX, KINTEX, and domestic/international AI, humanoid, bio, and medical-device
event information is fragmented across venue pages, organizer sites, PDFs,
press releases, and social posts; a curated event intelligence service can make
that information readable for humans and structured enough for agents to
consume through a clean API.

## Raw Notes

User-provided raw idea:

1. COEX, KINTEX 행사 정보.
2. 국내외 AI, humanoid, bio, 의료 기기 행사 정보를 가독성 좋게 표시해주고 API로 agent-friendly 하게 제공해주는 서비스.

Promotion-time decisions:

- API is free at capture time; no monetization plan is active.
- Free does not mean unlimited. Quotas protect the service and the shared VPS.
- MVP public domain is `events.nukk.net`.
- Existing Developer DigitalOcean VPS is acceptable for MVP if the data is
  public-only, read-only, and cache-first.
- The first concrete validation artifact is a 30-day manual coverage dataset.

## Problem

Technology and healthcare-adjacent event discovery is fragmented and
operationally annoying.

1. COEX, KINTEX, and similar venue calendars are useful but venue-centered, not
   industry-intelligence-centered.
2. Organizer pages often change structure, hide details in PDFs, or split
   information across Korean and English pages.
3. International event information is scattered across associations, venue
   pages, LinkedIn posts, conference microsites, sponsor pages, and press
   releases.
4. People who care about events usually care about actions, not just dates:
   register, exhibit, sponsor, book travel, meet companies, track competitors,
   or brief a sales team.
5. AI agents cannot reliably use many event pages because the pages are not
   normalized, dates are ambiguous, categories are inconsistent, and update
   state is unclear.

## Target User Or Customer

No paid customer is assumed yet. The first target user/workflow still needs to
be selected.

Candidate users:

- Startup founders and operators tracking AI, humanoid robotics, bio, digital
  health, and medical-device opportunities.
- Business-development and sales teams looking for events where buyers,
  distributors, hospitals, manufacturers, investors, or partners gather.
- Investors, analysts, and researchers tracking sector activity and company
  presence.
- Conference and exhibition attendees who want a readable filtered event view.
- AI agents that need reliable event data for planning, monitoring, outreach,
  lead generation, travel planning, or market research.

## Existing Alternatives

- Venue calendars: COEX, KINTEX, BEXCO, SETEC, and convention center websites.
- Organizer and association websites.
- Event discovery sites such as Eventbrite-style directories, LinkedIn Events,
  Meetup, Luma, Peatix, or local equivalents.
- Trade-media calendars and newsletters.
- Manual spreadsheet tracking by founders, sales teams, and agencies.
- General web search and LLM search.

## Product Concept

Build a curated event intelligence layer with two equal surfaces:

1. Human-readable event pages and dashboards.
2. Agent-friendly API and feed endpoints.

Initial taxonomy:

- AI infrastructure, AI applications, enterprise AI, generative AI.
- Humanoid robotics, industrial robots, automation, smart factory.
- Bio, biotech, pharma partnering, lab automation.
- Medical devices, digital health, hospital tech, rehabilitation tech,
  diagnostics.
- Venue focus: COEX and KINTEX first for Korea; then BEXCO, SETEC, major Asian
  venues, and major global conferences.

## Agent-Friendly API Direction

Candidate resources:

- `GET /api/v1/events`
- `GET /api/v1/events/{event_id}`
- `GET /api/v1/venues/{venue_id}/events`
- `GET /api/v1/categories/{category}/events`
- `GET /api/v1/events/changes`
- `GET /api/v1/events/{event_id}/sources`
- `GET /api/v1/schema`

Launch quota decision:

| Access Type | Limit | Use Case |
|---|---:|---|
| Keyless public API by IP | 60 requests/minute and 2,000 requests/day | Browsing, demos, light agent polling |
| Free API key | 300 requests/minute and 20,000 requests/day | Personal agents, small tools, integrations |
| Allowlisted research/partner key | Case-by-case, initially 100,000 requests/day | Bulk mirrors, research, public-interest reuse |

API principles:

- JSON first.
- Read-only public API.
- Stable schemas with explicit nullable fields.
- Cursor pagination and `limit <= 100`.
- `updated_since` and `changed_since` filters for agent polling.
- `ETag`, `Last-Modified`, and cache headers.
- Source provenance on every enriched claim.
- No live LLM generation on normal API reads.
- Publish OpenAPI and `llms.txt`.

### Serialization Policy (storage vs delivery)

Decision (2026-06-21): keep **one canonical structured store** and branch only
the *output representation* at the point of consumption. Do not maintain two
source-of-truth formats.

- **Canonical store + default API response: JSON / JSONL.** Stable keys, explicit
  nullable fields, provenance, and diffable change feeds. This is the correct wire
  format for agents that call the API as a tool (function-calling pipelines parse
  JSON; handing them Markdown forces a re-parse).
- **Markdown / `llms.txt` is a rendered view, not a second store.** When events are
  injected into an LLM prompt to be *read* (RAG, "summarize these events"), serve a
  Markdown table/section view. It is ~30-50% cheaper in tokens than JSON for bulk
  records (keys are not repeated per row) and degrades better under truncation.
- **Format is negotiated, data is identical.** Default `application/json`; offer a
  Markdown rendering via `Accept: text/markdown` or `?format=md`, plus a published
  `llms.txt`. Same fields, two encodings.
- **Rule of thumb:** machine *parses/produces* structure → JSON (and JSON-schema /
  structured output for any extraction step); machine *reads* structure into a
  prompt → Markdown. This keeps the read path token-cheap, which matches the
  cost-efficiency mandate.

## Why Now

- AI agents are becoming practical workflow participants, but they need
  structured data rather than brittle scraping.
- Korean and international tech/healthcare event information is fragmented
  across web pages, PDFs, venue calendars, and organizer posts.
- AI, humanoid robotics, biotech, and medical-device categories are all
  event-heavy domains where business timing matters.
- COEX and KINTEX are strong domestic wedges because they are recurring,
  high-signal, and operationally relevant for Korean founders and B2B teams.

## Constraints

- Event pages and PDFs have inconsistent structure.
- Date parsing must handle Korean, English, timezones, multi-day events,
  postponed events, and changed schedules.
- Crawling must respect robots.txt, rate limits, and source terms.
- The API needs durable IDs and schema versioning from the start.
- Avoid republishing copyrighted event descriptions wholesale.
- Store source provenance and show short factual summaries.
- Medical-device and bio events should not be framed as medical advice.
- Data freshness and correction handling are the real workload.
- Manual curation is necessary before automation quality is proven.

## Cost Structure

Initial assumption: the MVP should be a data product, not an AI-heavy product.
Owned GPU usage should be 0 at the start. Most requests should be served from a
database, cache, static pages, or search index.

| Metric | Initial Hypothesis |
|---|---|
| Monthly AI COGS per free human user | Near $0 if browsing cached event pages |
| Monthly AI COGS per free API user | Target near $0 for normal read-heavy usage; avoid live LLM calls |
| Monthly ARPU per user | 0 KRW at capture time; no monetization planned |
| Monthly hosting budget | Start on existing Developer VPS; incremental cost near $0 until resize/backups are needed |
| Operating cost driver | Manual curation time, crawler maintenance, backups, and async LLM enrichment if used |
| Sustainability target | Keep total incremental cash cost low enough to run as a public utility/prototype |

## Efficiency Strategy

1. Search/rules first: use venue pages, source metadata, structured parsers, and
   controlled taxonomies before LLM calls.
1b. **HTTP-first / CDP-fallback fetch policy** (verified for COEX/KINTEX
   2026-06-21): fetch sources with plain HTTP and parse static HTML
   deterministically. Use a headless browser / CDP session **only** as a fallback
   for sources that are genuinely JS-rendered and expose no usable HTML or data
   endpoint. Spinning up a browser per page is far more CPU/memory/latency than
   HTTP and is prohibited as a default. When a browser is unavoidable, attach to
   the existing CDP session (per global rule) rather than launching a new
   Playwright/Puppeteer instance. COEX and KINTEX both serve static, field-rich
   HTML, so neither needs a browser.
2. Model routing: deterministic parsers for known sources, cheap small model for
   classification if needed, stronger model only for hard extraction failures.
3. Response caching: cache common filters, event summaries, category pages, and
   agent summaries by event version.
4. Prompt optimization: keep prompts short and reuse a small schema instruction.
5. Output limits: event summaries should be short; API fields should be
   structured; no long generated prose by default.
6. Batch processing: crawl, normalize, diff, and summarize in scheduled batches
   instead of generating per user request.
7. Manual review loop: use human correction on important events to improve rules
   and taxonomy.

## Unit Economics

This is not a paid product at promotion time.

- Monthly ARPU: 0 KRW.
- Break-even subscriber count: not applicable while free.
- Cost target: incremental hosting cost near 0 while using the existing VPS.
- Main constraint: manual curation time and operational maintenance, not model
  inference cost.

Why it survives even if basic AI becomes free:

- The value is source coverage, event identity resolution, update tracking,
  category taxonomy, freshness, confidence, API reliability, and workflow
  integration.
- Cheap AI helps reduce enrichment cost, but does not replace the data asset.

## Moat

The moat cannot be "AI features."

Potential defensibility:

- Domain data across Korean venues, international organizers, PDFs, and niche
  industry sources.
- Update history for changes, postponements, cancellations, and source
  provenance.
- Practical taxonomy for AI, humanoid, bio, medical devices, digital health,
  robotics, automation, and buyer/sponsor relevance.
- Workflow fit for saved alerts, CRM/export workflows, agent polling, and
  business-development playbooks.
- Stable API IDs, schema versioning, confidence fields, and change feeds.
- Operational efficiency through cheap-first parsing and curation.

## Open Questions

- [ ] Who is the first target user/workflow: founders, BD teams, investors,
  agencies, event organizers, or agent/API developers?
- [ ] Is the first wedge COEX/KINTEX only, or Korea-wide AI/humanoid/bio/medical-device events?
- [ ] Which vertical is most urgent: AI, humanoid robotics, bio, medical devices,
  or digital health?
- [ ] Which source list should define good enough coverage for the first 30 days?
- [ ] What is the minimum useful API contract for agents: search only, changed
  feed, or full event detail with source provenance?
- [ ] How should the product handle uncertain, postponed, cancelled, or duplicate events?
- [ ] Can event organizers be persuaded to submit structured data directly?

## Next Action

Create the 30-day manual coverage dataset schema and seed file for COEX/KINTEX
plus international benchmark events.

## Move Forward When

- The first target user/workflow is named.
- COEX/KINTEX source coverage is manually tested.
- The category taxonomy is small and usable.
- The API schema has a minimal stable draft.
- At least one user or agent workflow shows that the event feed would change a
  real workflow.
- Cost structure is recalculated with measured ingestion volume and current
  model pricing.

# Founder / Operator Benchmark Sources

## Metadata

- Status: benchmark source planning
- Date: 2026-06-23
- Scope: define the next product gap after COEX/KINTEX schedule browsing and
  first-pass action enrichment
- Related: `IDEA.md`, `docs/original-plan-gap.md`, `PROGRESS.md`,
  `prototype/manual-dataset-schema.md`

## Goal

Define benchmark source families for startup founders and operators tracking
AI, robotics, bio, digital-health, and medical-device events.

This should not become a generic event portal. The product should answer:

> Which upcoming source-backed events are worth acting on now, and what can a
> startup operator do next?

## Target Workflow

Primary user:

- Startup founder or operator in Korea or Asia, building in AI, robotics,
  biotech, digital health, or medical devices.

Primary jobs:

- Find upcoming events worth attending, exhibiting at, or applying to.
- Avoid missing registration, booth, startup program, or partnering windows.
- Compare domestic COEX/KINTEX opportunities against a small global benchmark
  set.
- Export or hand off actionable event facts to an internal planning tool or
  agent.

Non-goals for this phase:

- Consumer leisure event discovery.
- Broad every-venue coverage.
- Paid CRM, lead database, attendee scraping, or private network data.
- Live LLM answers on normal reads.

## Benchmark Source Set

Use this set to prove the international/source-breadth side of the original
thesis. These are event/source families, not all guaranteed parser adapters for
the first implementation pass.

| Priority | Source family | Domain | Region | Why it belongs |
|---:|---|---|---|---|
| 1 | CES / CES Digital Health Summit | AI, digital health, robotics | US/global | Major cross-domain launch and partnership surface with digital-health and AI relevance. |
| 2 | NVIDIA GTC | AI infrastructure, physical AI, robotics | US/global | High-signal AI ecosystem event; useful for AI infrastructure and robotics startups. |
| 4 | VivaTech | AI, startups, enterprise innovation | France/Europe | Major startup/operator event with global investor and enterprise presence. |
| 5 | Web Summit Lisbon | startups, AI, investors | Portugal/global | Founder/investor workflow benchmark and startup program surface. |
| 6 | Web Summit Qatar | startups, AI, investors | Qatar/MENA | Regional founder/investor expansion benchmark. |
| 7 | Slush | startups, investors | Finland/Europe | Founder-focused event with meetings and investor density. |
| 8 | TechCrunch Disrupt | startups, investors | US/global | Startup Battlefield, founder visibility, and startup/VC timing signals. |
| 9 | World Summit AI | AI, startups, investors | Netherlands/Europe | AI-specific ecosystem with startup and investor programming. |
| 10 | AI & Big Data Expo Europe / TechEx | AI, data, robotics, enterprise tech | Netherlands/Europe | Exhibitor-heavy AI and robotics-adjacent source with B2B action signals. |
| 11 | GITEX AI Europe | AI, startups, investors | Germany/Europe | AI exhibition with startup, investor, and public-sector partnership surface. |
| 12 | HumanX Europe | AI, enterprise, investors | Netherlands/Europe | AI leadership event; useful if source quality exposes partner/startup signals. |
| 13 | Robotics Summit & Expo | robotics, automation | US | Commercial robotics operator benchmark with expo/action surfaces. |
| 14 | IEEE ICRA | robotics, automation | global/academic-industry | Premier robotics conference; strong technical signal, weaker exhibit/startup signal. |
| 15 | IEEE/RSJ IROS | robotics, intelligent systems | global/academic-industry | Robotics research benchmark for technical ecosystem tracking. |
| 16 | automatica | robotics, automation, manufacturing | Germany/Europe | Major robotics/automation trade fair with exhibitor action surface. |
| 17 | BIO International Convention | biotech, partnering | US/global | Biotech partnering and business-development benchmark. |
| 19 | HLTH USA | digital health, healthcare innovation | US | Healthcare innovation event with register/sponsor/action surfaces. |
| 20 | HIMSS Global Health Conference | digital health, health IT | US/global | Major health IT and digital-health benchmark. |
| 21 | HIMSS AI in Healthcare Forum | AI in healthcare | US | Focused AI-health event with clear registration signal. |
| 22 | MEDICA | medical devices, healthcare | Germany/global | Major medtech trade fair with strong exhibitor surface. |
| 23 | The MedTech Conference | medical devices, investment | US | Medtech business-development and investment benchmark. |
| 24 | RSNA Annual Meeting / AI Showcase | medical imaging, AI, medtech | US/global | Medical-imaging AI and exhibitor benchmark. |
| 25 | Medical Taiwan | medtech, digital health | Taiwan/Asia | Asia medtech/digital-health benchmark with B2B sourcing angle. |

Primary source URLs:

- CES / Digital Health Summit: `https://www.ces.tech/explore-ces/digital-health-summit/`
- NVIDIA GTC: `https://www.nvidia.com/gtc/`
- VivaTech: `https://vivatech.com/`
- Web Summit Lisbon startup program: `https://websummit.com/startups/`
- Web Summit Qatar: `https://qatar.websummit.com/`
- Slush: `https://slush.org/`
- TechCrunch Disrupt: `https://techcrunch.com/events/techcrunch-disrupt/`
- World Summit AI: `https://worldsummit.ai/`
- AI & Big Data Expo Europe: `https://www.ai-expo.net/europe/`
- GITEX AI Europe: `https://www.gitexeurope.com/`
- HumanX Europe: `https://www.humanx.co/europe/`
- Robotics Summit & Expo: `https://www.roboticssummit.com/`
- IEEE ICRA: `https://2026.ieee-icra.org/`
- IEEE/RSJ IROS: `https://www.ieee-ras.org/conferences-workshops/financially-co-sponsored/iros/`
- automatica: `https://automatica-munich.com/en/`
- BIO International Convention: `https://convention.bio.org/future-dates`
- HLTH USA: `https://hlth.com/events/usa/`
- HIMSS Global Health Conference: `https://www.himssconference.com/`
- HIMSS AI in Healthcare Forum: `https://www.himss.org/events-overview/ai-in-healthcare-forum-boston/`
- MEDICA: `https://www.medica-tradefair.com/`
- The MedTech Conference: `https://themedtechconference.com/`
- RSNA Annual Meeting: `https://www.rsna.org/annual-meeting`
- Medical Taiwan: `https://www.medicaltaiwan.com.tw/en/index.html`

Selection rules:

- Prefer official organizer pages over aggregators.
- Prefer event families with at least one public action surface: register,
  exhibit, sponsor, startup program, partnering, meeting tool, or application.
- Keep the first set at 20-30 families. Add more only after source quality and
  maintenance cost are measured.
- Exclude general tech events unless they expose a founder/operator action path
  relevant to AI, robotics, bio, digital health, or medical devices.

## Source Quality Criteria

Each event should receive a completeness profile. This is not a popularity rank;
it tells users and agents whether the record is actionable.

Required for a benchmark record:

- `event_id`
- `name`
- `start_date`
- `end_date`
- `venue` or online/hybrid format
- at least one official `sources[]` entry
- `homepage_url` or equivalent official event URL
- at least one taxonomy category
- `last_checked_at`
- `missing_fields[]`

Direct action signals that can make a record useful for operator follow-up:

- `register_url`
- `exhibit_url`
- `registration_deadline`
- `exhibitor_deadline`
- `actions.can_register`
- `actions.can_exhibit`
- `actions.can_sponsor`
- `actions.has_matchmaking`
- `actions.has_startup_program`

Supporting context that can make an already actionable record more useful:

- `cost_hint` not `unknown`
- source-derived `summary` as supporting context
- organizer/source provenance beyond the venue calendar

Do not hide low-completeness records by default in the API. The browser
should preserve honest missing fields for agents.

## First International Coverage Slice

Implemented benchmark source seed:

- `docs/benchmark-source-seed.jsonl`

Implemented registered source adapter:

- `benchmark`: current 10-source catalog, including CES 2027, NVIDIA GTC 2027,
  BIO International Convention 2027, Web Summit Lisbon 2026, Slush 2026,
  TechCrunch Disrupt 2026, World Summit AI 2026, HLTH USA 2026,
  The MedTech Conference 2026, and MEDICA 2026.

This slice intentionally covers official pages from different domains before
expanding the full benchmark set.

## Acceptance Criteria

- The founder/operator workflow is documented and narrow.
- The benchmark source set contains 20-30 official source families.
- Source-quality criteria are explicit and testable.
- UI/API changes are concrete enough to implement without another strategy
  discussion.
- The plan does not require private data, paid CRM, broad consumer discovery, or
  live LLM reads.

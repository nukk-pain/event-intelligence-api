# Manual Coverage Dataset Schema (v0.1)

## Purpose

Schema for the 30-day manual coverage dataset that validates source quality and
field reliability **before** any crawler, UI, or API code is written. This is the
prototype artifact named in `PLAN.md` (Prototype Method) and `PROGRESS.md`
(Next Action).

## Shaping Decisions (2026-06-21)

- **First target user/workflow**: startup **founders / operators**.
  - Implication: the schema prioritizes *action* fields (can I register / exhibit
    / sponsor, and by when) over deep analyst/benchmark fields. Founders ask
    "should my team go, what do we do there, and what is the deadline."
- **First file format**: **JSONL** (one event object per line).
  - Chosen for agent-friendliness, explicit nullable fields, append-friendly
    manual curation, and easy missing-field tracking. CSV/SQLite are deferred.

## Files

| File | Role |
|---|---|
| `prototype/manual-dataset-schema.md` | This schema (human-readable contract). |
| `prototype/seed-events.jsonl` | Seed records, one JSON object per line. |
| `prototype/taxonomy.md` | Controlled category/audience vocabularies (small, fixed). |

## Conventions

- Encoding: UTF-8. Dates: ISO 8601 (`YYYY-MM-DD`). Datetimes: ISO 8601 with offset.
- Every field is present on every record. Unknown values use `null`, **and** the
  field key is added to `missing_fields[]`. This makes coverage gaps measurable
  (Acceptance Criteria: "missing-field tracking").
- No wholesale copying of organizer descriptions. `summary` is a short factual
  line written by the curator (≤ 240 chars), not a paste.
- `event_id` is a durable human-readable slug and must never change once assigned.

## Record Schema

### Identity
| Field | Type | Notes |
|---|---|---|
| `schema_version` | string | `"0.1"`. |
| `event_id` | string | Durable slug, e.g. `coex-2026-ai-expo`. Stable forever. |
| `series_id` | string\|null | Recurring-series identity, e.g. `ai-expo-korea`. Links editions. |
| `name` | string | Primary display name. |
| `name_ko` | string\|null | Korean name if distinct. |
| `name_en` | string\|null | English name if distinct. |
| `edition` | string\|null | e.g. `"2026"`, `"제7회"`. |

### When / Where
| Field | Type | Notes |
|---|---|---|
| `start_date` | date\|null | First day. |
| `end_date` | date\|null | Last day (same as start if single-day). |
| `timezone` | string\|null | IANA tz, e.g. `Asia/Seoul`. |
| `date_confidence` | enum | `high` \| `medium` \| `low`. Date parsing ambiguity flag. |
| `status` | enum | `scheduled` \| `tentative` \| `postponed` \| `cancelled` \| `ended`. |
| `format` | enum | `onsite` \| `hybrid` \| `online`. |
| `venue` | object\|null | See Venue object. |
| `country` | string | ISO 3166-1 alpha-2, e.g. `KR`, `US`. |

#### Venue object
| Field | Type | Notes |
|---|---|---|
| `venue_id` | string\|null | Slug, e.g. `coex`, `kintex`. |
| `name` | string | Display name. |
| `hall` | string\|null | Hall/booth area if known. |
| `city` | string | City. |

### Classification (founder/operator relevance)
| Field | Type | Notes |
|---|---|---|
| `categories` | string[] | From `taxonomy.md`, e.g. `["ai", "medical-devices"]`. ≥1. |
| `audience` | string[] | e.g. `["b2b", "investors", "buyers"]`. May be empty. |
| `scale` | object\|null | `{ "visitors": int\|null, "exhibitors": int\|null }` if disclosed. |

### Founder / Operator action fields (target-specific differentiator)
| Field | Type | Notes |
|---|---|---|
| `actions` | object | Booleans, default `false` when unknown (and key listed in `missing_fields`). |
| `actions.can_register` | bool | Attendee registration is open/available. |
| `actions.can_exhibit` | bool | Exhibitor booths are sold. |
| `actions.can_sponsor` | bool | Sponsorship packages exist. |
| `actions.has_matchmaking` | bool | Buyer/partner matchmaking program. |
| `actions.has_startup_program` | bool | Startup pavilion / pitch / discount track. |
| `register_url` | string\|null | Direct attendee registration link. |
| `exhibit_url` | string\|null | Exhibitor application link. |
| `registration_deadline` | date\|null | Last day to register (early-bird ignored for v0.1). |
| `exhibitor_deadline` | date\|null | Booth application deadline. |
| `cost_hint` | enum | `free` \| `paid` \| `mixed` \| `unknown`. |

### Provenance & freshness (required by Acceptance Criteria)
| Field | Type | Notes |
|---|---|---|
| `sources` | object[] | ≥1. Each: `{ "url", "type", "publisher", "retrieved_at" }`. |
| `sources[].type` | enum | `venue` \| `organizer` \| `association` \| `press` \| `social` \| `aggregator`. |
| `homepage_url` | string\|null | Canonical official event page. |
| `last_checked_at` | date | When a human last verified this record. |
| `update_state` | enum | `new` \| `unchanged` \| `updated` \| `conflicting`. |
| `confidence` | enum | `high` \| `medium` \| `low`. Overall record trust. |
| `missing_fields` | string[] | Dotted paths of fields left `null`/unknown, e.g. `["registration_deadline","venue.hall"]`. |
| `ambiguity_notes` | string\|null | Free text for conflicts, postponements, duplicate risk. |

### Curation meta
| Field | Type | Notes |
|---|---|---|
| `curated_by` | string | e.g. `smpain`. |
| `created_at` | date | Record creation. |
| `updated_at` | date | Last edit. |

## Validation Rules (manual, for v0.1)

1. `event_id`, `schema_version`, `name`, `country`, `categories`, `sources`,
   `last_checked_at`, `update_state`, `confidence`, `missing_fields` are required
   and non-null.
2. Every key whose value is `null` (or an `actions.*` boolean that is unknown)
   MUST appear in `missing_fields`.
3. `end_date >= start_date` when both present.
4. `categories` values must exist in `taxonomy.md`.
5. At least one `sources[]` entry with a resolvable `url`.

## Open Items For Review After Seeding

- Are the founder/operator action fields actually fillable from real source pages,
  or mostly `null`? (This is the core prototype question.)
- Is `confidence` better as enum or 0–1 numeric once we have ~50 records?
- Do we need `language` per source, or is `country` + name pairs enough?

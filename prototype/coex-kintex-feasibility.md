# COEX / KINTEX Ingestion Feasibility (2026-06-21)

Goal: before building the ingestion backend, confirm whether COEX and KINTEX
event data is (a) legally fetchable, (b) statically parseable, and (c) carries the
v0.1 schema fields. Checked with read-only `curl`, low volume.

## Verdict: ✅ Both feasible with deterministic HTTP + static-HTML parsing. No headless browser required.

## COEX (`www.coex.co.kr`)

| Aspect | Finding |
|---|---|
| robots.txt | Permissive. Only `/wp-admin/`, `/wp-content/uploads/` disallowed. Sitemap published. |
| Platform | WordPress. Custom post type **`exhibitions`** (5 sitemap shards). |
| Discovery | `wp-sitemap-posts-exhibitions-1..5.xml` lists every exhibition URL. |
| REST API | **Locked** — `/wp-json/wp/v2/exhibitions` → 401/404 (`show_in_rest` off). Cannot use clean JSON API; must parse HTML pages. |
| Detail page | Static HTML (~62 KB). Contains labels **기간 / 장소 / 주최 / 주관 / 홈페이지 / 문의**. Dates as `2024.08.24`. `og:` tags present, no JSON-LD. No JS shell. |
| Caveat | The `exhibitions` type is the **whole venue calendar** — includes non-industry events (study-abroad fairs, seminars, alumni events). Requires a classification/filter step to keep only AI/robotics/bio/medical-device events. |

**COEX path**: sitemap (discover) → fetch `/exhibitions/<slug>/` (static) → parse
field labels → classify against taxonomy → normalize to v0.1 JSON.

## KINTEX (`www.kintex.com`)

| Aspect | Finding |
|---|---|
| robots.txt | **Explicitly allows** `ClaudeBot`, `GPTBot`, `anthropic-ai`, `PerplexityBot`, `Bingbot`, `Google-Extended`. Only `/admin/`, `/login/` disallowed. AI-crawler friendly. |
| Discovery | Listing under `/web/ko/event/clist.do?searchType=…` (11/12/23/D filters) and homepage. `list.do` itself returned 0 inline links → list rows likely loaded via an AJAX/`clist.do` data call (hit that endpoint directly with HTTP; no browser needed). |
| Detail page | `/web/ko/event/view.do?seq=<id>` — static HTML (~45 KB). Contains **기간 / 장소 / 홀 / 주최 / 주관 / 홈페이지**. Dates as `2026-06-18`. |
| IDs | **Durable numeric `seq`**, date-encoded (e.g. `26060201` ≈ 2026-06 event). Maps cleanly to a stable `event_id` / dedup key. |
| Sitemap | `/sitemap.xml` returned 400 to our request — non-blocking; discovery works via `clist.do`/homepage. |

**KINTEX path**: `clist.do` (discover seq list) → fetch `view.do?seq=` (static) →
parse fields → classify → normalize to v0.1 JSON.

## Implications for the backend

- **HTTP-first, deterministic parser per venue.** Both serve static HTML; a
  headless browser / CDP is unnecessary overhead and would violate the
  cost-efficiency mandate. Reserve a browser only as a fallback for any future
  source that is genuinely JS-rendered (neither of these two is).
- **Action fields are the open risk.** `can_register` / `registration_deadline` /
  `exhibitor_deadline` largely live on the *organizer* site (linked via 홈페이지),
  not the venue page. Plan for an optional second hop to the organizer URL, or
  accept these as `null` + `missing_fields` in v1.
- **Classification is mandatory for COEX** (mixed calendar); lighter for KINTEX.
- **No LLM on the read path**; classification can be rule/keyword-first, small
  model only for ambiguous titles.

## Next

Take this into `/plan` for the ingestion backend (stack, scheduler, parser
modules, change-diff, classification, read API) + an ADR superseding the
manual-dataset-first decision.

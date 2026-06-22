# Preflight: COEX/KINTEX Ingestion Backend

> 2026-06-21 · grounded multi-agent preflight (impact / AC audit / live source re-probe / constraints / deps). All items verified against live DNS, live source HTTP, pkg.go.dev, and the repo. Findings folded into PLAN.md v2.

## Impact / Scope

Greenfield Go project — no `go.mod`/`cmd/`/`internal/` exist, zero risk to existing code. New-file map is coherent. The risk surface is entirely in Phase 4 deploy:

| # | Finding | Sev | Action (in PLAN) |
|---|---------|-----|------------------|
| I1 | `events.nukk.net` DNS record **does not exist** (dig empty). `nukk.net` → Cloudflare proxied. | high | Task 4.3 reworded "verify"→**create** (DNS-only first), pre-req before HTTPS-200 AC |
| I2 | Deploy shape (systemd binary + Caddyfile.snippet) **contradicts VPS runbook** (per-app Docker Compose under `/srv/developer`, single `/etc/caddy/Caddyfile` + `systemctl reload caddy`) | med | Task 4.3 aligned to runbook (or ADR 0.1 justifies single static binary); Caddy = append site block + validate + reload |
| I3 | Cloudflare proxy ON → first-time Caddy TLS issuance ordering omitted | med | Task 4.3 ordered substeps: DNS-only → origin HTTPS 200 → enable proxy → Full(Strict) |
| I4 | KINTEX `clist.do` discovery is an **unverified inference** (feasibility saw list.do return 0 links; clist.do never fetched/parsed) | med | Task 2.3 spike: curl real list.do/clist.do, commit fixture, gate 2.4; flagged in Risks |
| I5 | robots.txt assumed, not enforced at fetch time | low | Task 1.1 robots fetch+cache gate (or documented static allowlist) |
| I6 | Scheduled ingest needs no-overlap/single-flight lock | low | Task 4.1/4.2 flock single-flight |
| I7 | `docs/decisions/` absent; DECISIONS.md `Supersedes: None` needs update; two PLAN.md files coexist | info | Task 0.2 creates ADRs, updates Supersedes, repoints root PLAN |

## AC Audit

All 8 original ACs were Given/When/Then-shaped; several referenced unpinned fields or bundled claims. Rewrites applied:

- **AC-1**: pinned the exact v0.1 required-field set + a validator enforcing rules 1–5 (was: only rule 1 implied).
- **AC-2**: scoped invariance precisely — content_hash byte-identical + 0 new change_log + update_state unchanged; `last_checked_at` expected to advance and excluded (resolves the conflict with the "always update last_checked_at" rule).
- **AC-3**: named the `change_log` columns asserted on.
- **AC-5**: replaced "same data" with a concrete field-set equality + Link-header parity.
- **AC-6**: `excluded` field added to the v0.1 schema (was undefined); split into classify + default-list-exclusion halves.
- **AC-7**: split into 7a (zero-LLM via import-scan AND runtime no-egress), 7b (cache headers/304), 7c (60/min AND 2000/day) — the daily cap was previously built but untested.
- **AC-8**: "resolvable url" → "syntactically valid https url (no live network)" to avoid violating the zero-network read-path principle; +type/publisher.
- New: AC-9 (read-during-write), AC-10 (anomaly preservation), AC-11 (actions.* honesty), AC-12 (SSRF guard).

## Live Source Re-probe

- COEX/KINTEX detail pages **confirmed static** (re-verified). **No registration/exhibitor deadline on the venue page** → the plan's "action fields = null + missing_fields in v1" assumption holds.
- COEX returns `Cache-Control: no-store, no-cache` and **no ETag/Last-Modified** → conditional-GET 304 fast-path is **best-effort only**; freshness must rely on `content_hash` (Task 1.1 corrected).
- KINTEX discovery endpoint (`list.do` vs `clist.do`) remains **unverified** — confirm via spike before building the parser (Task 2.3).

## Constraints / Cost-efficiency (IDEA.md mandate)

Plan is **strongly compliant**: read-path COGS ~0 (zero LLM on reads + AC-7a guard), cache-first (ETag/304 + conditional GET), scheduled batch-only ingest, single Go binary + SQLite, keyless quota exactly matches IDEA (60/min + 2000/day), HTTP-first/CDP-fallback respected, international out of scope. Tightened:

- Daily 2000/day cap now asserted in AC-7c (was built, untested).
- Markdown token-cheapness (single header row, no per-row keys) noted in Task 3.4 + AC-5.
- Copyright: `summary` ≤240-char factual template enforced in Task 2.6 (was risk-table only).
- Classification cheap-model fallback explicitly deferred to a follow-up PLAN.

## Dependencies (verified on pkg.go.dev + live)

All five libraries sound, maintained, version-current (June 2026):

- **modernc.org/sqlite v1.52.x** (pure-Go, embeds SQLite 3.53.2, Go 1.25+; local go1.26.3 OK) — **chosen** over mattn/go-sqlite3 (CGo) to keep a single static binary + zero VPS toolchain. Requires explicit WAL + busy_timeout + single-writer (database/sql pools connections).
- **goquery v1.12.x** — right parser; UTF-8 input required (both venues UTF-8, confirmed) → fetcher UTF-8 guard added.
- **chi/v5 v5.3.x** — zero external deps; ideal for the read API.
- **x/time/rate** — covers 60/min burst; 2000/day needs a separate day-bucket.

## Verdict

✅ Ready for `/execute-plan`. No blocking unknowns remain; the one unverified runtime assumption (KINTEX discovery endpoint) is converted into an explicit in-plan spike (Task 2.3) that gates its dependent task.

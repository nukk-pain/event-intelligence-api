# Event Intelligence API Agent Guide

Scope: this file governs `/Users/smpain/Developer/event-intelligence-api` and
its children.

## Project Source

- Source idea document: `/Users/smpain/Developer/docs/ideas/ai/2026-06-21-ai-industry-event-intelligence-api.md`
- Workspace registry: `/Users/smpain/Developer/docs/workspace/inventory/PROJECTS.md`
- Workspace pipeline: `/Users/smpain/Developer/docs/workspace/planning/idea-pipeline.md`
- AI idea intake protocol: `/Users/smpain/Developer/docs/workspace/planning/ai-idea-intake.md`
- VPS deployment reference: `/Users/smpain/Developer/docs/workspace/deployment/developer-vps-runbook.md`

## Session Start

At the start of a new session in this project:

1. Read this `AGENTS.md`.
2. Read `IDEA.md`, `PLAN.md`, `PROGRESS.md`, and `DECISIONS.md`.
3. Check current files before editing.
4. Run commands from this project root unless a deeper guide says otherwise.

## Project Rules

- Preserve uncertainty in `IDEA.md`; do not turn open questions into facts.
- Keep `PLAN.md` as the current execution scope.
- Keep `PROGRESS.md` updated with current focus, evidence, blockers, and next action.
- Record major promotion, scope, pause, archive, supersede, deployment, or API policy decisions in `DECISIONS.md`.
- Public API reads should be cache-first and read-only unless a later approved plan changes that.
- Do not put clinic, patient, EMR, medical-platform private connector, or private network data in this project or deployment.
- Use `events.nukk.net` as the default public MVP domain unless superseded by a decision record.
- Follow `/Users/smpain/Developer/AGENTS.md`, `/Users/smpain/Developer/CLAUDE.md`, and global guardrails.

## Verification

- Narrow package check: `go test ./internal/<package> -count=1`.
- Full repository gates: `make test`, `make vet`, then `make build`.
- Concurrency-sensitive changes under `internal/pipeline`, `internal/fetch`, or
  `internal/store`: also run `go test -race ./internal/<package> -count=1`.
- Enrichment audit changes: run `make eval-report`; this invokes
  `python3 eval/audit.py --report` against the current audit inputs.
- `make refresh-fixtures` is a placeholder that exits successfully without
  refreshing anything. Do not cite it as fixture-validation evidence.

## Deployment Routing

- Before any VPS, Caddy, Cloudflare, systemd, or public-URL work, read
  `deploy/README.md` and `../docs/workspace/deployment/AGENTS.md`.
- Build the production artifact with the exact CGo-free linux/amd64 command in
  `deploy/README.md`; preserve the SQLite data directory and migration order.
- Every deploy ends with `deploy/verify.sh`. For an origin-first check, run
  `deploy/verify.sh http://127.0.0.1:3005` on the VPS before the public-edge
  check. A service being active is not release evidence.
- If a deployment changes the public inventory, run
  `python3 ../docs/workspace/deployment/update-public-deployments.py` and inspect
  the generated inventory diff.
- Package-specific rules live under `internal/*/AGENTS.md`; the normal read API
  and the isolated discovery service have separate HTTP and deployment bounds.

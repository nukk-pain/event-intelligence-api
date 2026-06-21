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

For documentation-only work, validate structure against the relevant template
and check for trailing whitespace.

Before implementation starts, add project-specific commands for:

- Schema/data validation for the manual event dataset.
- API contract checks, ideally from an OpenAPI spec.
- Build/test/manual QA commands for the chosen web/API stack.

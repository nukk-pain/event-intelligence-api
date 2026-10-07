# C3a security implementation

Scope: API/MCP identity and bounded quota maps, persistent anonymous MCP provider budget, execution limits, cancellation, HTTP timeouts, Go security updates, and build provenance. Unit hardening, Linux permissions and deployment belong to C3b. No deployment, push, merge, source-checkout edits (except the authorized `.env` mode change), or live paid requests.

Execution: `execute-plan` and `evidence-gated-work`, under the dispatched implementation request and coordinator security decisions.

## Evidence gate

- Completion: forged/ambiguous forwarding headers cannot change identity; live quota cannot be evicted; every HTTP tool has finite execution/request budgets; UTC provider reservations survive restart and fail closed; deadline/disconnect reach fake provider/read API; both servers enforce socket timeouts; source and final linux binaries pass vulnerability checks.
- Unchanged: ordinary API remains read-only, cache-first and LLM-free; event schema/data, ingest/provider/model settings, ports and stateless MCP compatibility.
- Failure modes: quota eviction/restart replenishes a live budget; timeout returns before work releases its slot; direct client spoofing reaches a trusted bucket. Verification uses fake clocks, concurrent callers, temporary state and local test listeners.
- Stop: no authorized production mutation; reconcile repeated failure rather than reinterpret exit codes. Separate final diff/adversarial review before commit.

## Baseline

`go test ./cmd/eventmcp ./internal/api ./internal/agent -count=1`: MCP and agent pass; API fails only `TestGetEvent_MarkdownNegotiation` because its September 2026 fixture is now ended. This pre-existing date-dependent failure is not a security RED test.

Coordinator approved all-tool 10/minute and 120/day/client; global/client concurrency 8/2; existing ask 10/10min and 60/day; global LLM concurrency 2; shared ask+search 20/minute and 800/day with at most two GETs/call. Notifications cancellation remains unsupported without safe ownership; only HTTP context deadline and disconnect cancel work.

Quota configuration: `EVENTMCP_LLM_DAILY_LIMIT=200` (0 blocks; max 10000), `EVENTMCP_MAX_CONCURRENT=8` (1..64), `EVENTMCP_CLIENT_CONCURRENT=2` (1..8), `EVENTMCP_LLM_CONCURRENT=2` (1..16), and mandatory separate `EVENTMCP_QUOTA_DIR`. Invalid config fails startup. C3b will wire its StateDirectory.

## Implementation self-check

- The normal API and MCP now share `internal/clientidentity`: loopback-only canonical identity, duplicate/malformed fallback, mapped IPv4 and IPv6 /64. The Caddy template's three reverse proxies overwrite the canonical header; local Caddy adaptation passes.
- Both quota maps cap 10000 identities and preserve all live windows. All HTTP tool calls have client/global quotas and bounded execution with no waiting queue.
- `reservations.jsonl` plus a lifetime `quota.lock` is the only new durable state, in the operator-selected private MCP quota directory. UTC provider reservations are appended and fsynced before I/O, survive restart, and never refund failures/cancellation. Redirects cannot escape the two-GET/provider request accounting.
- HTTP context passes through handle, query, ParseQuery and Backend.Chat. Fake provider/read API observed deadline and disconnect; request slots clear after actual completion. Same request IDs remain independent. Unsafe stateless notifications/cancelled remains deliberately unsupported.
- Both daemons use ReadHeader=5s, Read=10s, Write=45s, Idle=60s. Scaled real TCP tests cover slow headers/body, idle close, write timeout and normal responses.
- Go is pinned to 1.26.8, x/text to 0.39.0. Before source audit found 9 callable standard-library vulnerabilities, and prior API/MCP binaries found 9/8. Source and both clean Linux binary audits now report none. Go 1.26 omits VCS metadata in linked worktrees, so release builds use a clean ordinary local clone; revision, modified=false, CGO0 and linux/amd64 are verified. Final hashes and commit evidence are retained in the coordinator implementation report.
- Security-focused tests, affected non-API race tests, API quota/cache/route/schema race tests, vet and both local daemon builds pass. Full-suite API baseline has the documented date-dependent fixture failure; a coordinator decision to repair only that test is pending.
- Authorized source-checkout `.env` mode changed 0644 to 0600 using mode-only stat/chmod/stat. No content read. Tracked `eventmcp-linux` removed from the index, build outputs ignored, history preserved.

## Sequential adversarial review

Counterexamples exercised: 250 parallel reservations with exactly 200 admissions, 201st provider request rejected, same-day restart, UTC rollover/backwards clock, corrupt/incomplete state, private-file mode, symlink journal, conflicting process owner, failed persistence, provider failures/cancel without refund, 10000-map saturation and two-hour idle retention, later ask window retention, global/client/model N+1 rejection, shared 800-call read budget, empty-page cursors, redirect accounting, upstream 429 isError, duplicate IDs and cross-client stateless notifications.

No new app security headers, ports, event schema/data, ingest changes, model/token changes, production actions, pushes or merges. Follow-up ownership: C3b owns unit/Linux permissions/WAL/operational deployment and rollback; the coordinator owns external API catalog and new-state inventory/backup registration. Implementation submission is distinct from independent verification and operational release.

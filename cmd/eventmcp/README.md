# eventmcp — MCP server exposing events as agent tools (loop ③)

An MCP (Model Context Protocol) server so other agents (Hermes, Claude Code,
Cursor) can query the event-intelligence data as a tool. JSON-RPC 2.0 over
stdio, newline-delimited, no external dependencies.

## Tools

- `search_events` — structured filter (category, region, keyword, from_date,
  to_date). No LLM; pure data lookup.
- `ask_events` — natural-language question (e.g. `다음 달 서울 로봇 행사`). The
  model parses it into a filter (Solar once configured, local otherwise), then
  the same LLM-free lookup runs. This keeps data lookup model-free while making
  the model the natural-language front door.

By default the tools query the **live read API** (events.nukk.net) — real
deployed data. Category/date bounds are pushed to the server (with a category
alias, e.g. robotics -> humanoid-robotics); no date bound defaults to upcoming
events. Use `-source <file>` for an offline fixture, `-api-base <url>` /
`EVENTSINTEL_API_BASE` to point elsewhere.

## Try it (search_events needs no LLM)

```sh
# live API (default)
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_events","arguments":{"category":"robotics"}}}' \
  | go run ./cmd/eventmcp

# offline fixture
go run ./cmd/eventmcp -source cmd/eventmcp/fixtures/events.json < requests.jsonl
```

`ask_events` uses the LLM backend (local qwen36-dwq by default; Solar once
`EVENTSINTEL_SOLAR_API_KEY` is set) to parse the question into a filter, then
queries the same data source. Register with an MCP client by pointing it at the
built `eventmcp` binary over stdio.

## 원격 사용 (설치 없이)

Go 없이 URL 등록만으로 씁니다. 배포된 엔드포인트는
`https://events.nukk.net/mcp` 입니다.

```sh
# Claude Code
claude mcp add --transport http events https://events.nukk.net/mcp
```

등록 후 자기 AI에게 그대로 물으면 됩니다. "다음 달 서울 AI 행사 뭐 있어?"
`search_events`는 모델 없이 돕니다. `ask_events`는 서버 운영자의 Solar 키로
질문을 필터로 바꾸며 클라이언트당 10분에 10회, 하루 60회까지 쓸 수 있습니다.
두 도구 모두 클라이언트당 분당 10회, 하루 120회까지 쓸 수 있습니다.
검색 예산은 전체 사용자가 공유하며 분당 20회, 하루 800회입니다.
한도를 넘으면 429와 Retry-After를 돌려줍니다.

모델 호출은 UTC 기준 전체 하루 200회가 기본 한도이며 재시작해도 사용량을
유지합니다. 실패하거나 취소된 호출도 사용량에 포함됩니다. 검색은 모델 예산을
쓰지 않습니다.

세션은 발급하지 않고 GET 스트림은 405로 답합니다. 요청 하나가 JSON-RPC 메시지
하나입니다. 요청은 30초 후 취소하며 연결 종료도 하위 작업에 전달합니다.
세션이 없어 요청의 소유자를 확인할 수 없으므로 별도 notifications/cancelled
알림은 무시합니다.

## Operator configuration

HTTP mode requires a private, pre-created directory selected by
`EVENTMCP_QUOTA_DIR`. It must be separate from the event database directory,
mode 0700, and owned by the MCP account. `reservations.jsonl` and `quota.lock`
are mode 0600. One process holds the lock for its lifetime. Every model call
reserves one unit and fsyncs the journal before provider I/O. Corrupt/unwritable
state or another process holding the lock prevents paid calls. Never delete,
truncate, or restore older quota state during a code rollback.

| Variable | Default | Valid range |
| --- | --- | --- |
| `EVENTMCP_LLM_DAILY_LIMIT` | 200 | 0..10000; 0 disables model calls |
| `EVENTMCP_MAX_CONCURRENT` | 8 | 1..64 |
| `EVENTMCP_CLIENT_CONCURRENT` | 2 | 1..8 |
| `EVENTMCP_LLM_CONCURRENT` | 2 | 1..16 |
| `EVENTMCP_QUOTA_DIR` | required | absolute private directory |

Invalid values fail startup. No execution waiting queue is created. Slots remain
occupied until underlying work returns, including cancellation. HTTP `-max`
must be 1..200 and each lookup makes at most two non-redirecting API GETs,
including empty pages. Shared read budgets apply to ask and search together and
leave headroom under the ordinary loopback API's 60/minute and 2000/day budget.
They are process-local, so other local API callers or MCP restarts can still
exhaust the shared API bucket. An upstream 429 returns an MCP `isError` result.

Client identity trusts only one valid `X-Real-Client-IP` from a loopback socket
peer. Legacy forwarding headers and malformed/duplicate canonical values are
ignored. Mapped IPv4 is unwrapped and other IPv6 is grouped by /64. Both client
quota maps cap identities at 10000 and reject new identities at capacity without
evicting live budgets. Per-client quotas remain process-local.

The durable daily limit covers only anonymous HTTP `ask_events` provider
requests. Stdio and ingest do not use this journal. Provider redirects are
disabled for this HTTP path to preserve one request per reservation. Existing
models, token ceilings and ingest budgets remain unchanged.

Production builds must use a clean, ordinary Git checkout of the release
commit. Go 1.26 can omit VCS metadata in linked worktrees even with
`-buildvcs=true` ([Go issue 58218](https://github.com/golang/go/issues/58218)).
Build both daemons with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` and
`-buildvcs=true`, then use `go version -m` to require the intended
`vcs.revision`, `vcs.modified=false`, and those platform settings. Missing
metadata fails the release check. Keep the binary SHA256 alongside the commit
and audit both files with `govulncheck -mode=binary` before deployment.

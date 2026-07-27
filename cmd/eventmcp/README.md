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
`search_events`는 모델 없이 동작하고, `ask_events`는 서버 운영자의 Solar 키로
질문을 필터로 바꿉니다. `ask_events`에는 클라이언트당 10회, 10분 및 60회, 1일
한도가 있고 넘으면 429와 Retry-After를 돌려줍니다.

서버는 상태가 없습니다. 세션을 발급하지 않고 GET 스트림은 405로 답하며, 요청
하나가 JSON-RPC 메시지 하나입니다. 질문 문장 외에 어떤 것도 보내지 마세요.

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

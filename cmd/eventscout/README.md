# eventscout — autonomous source-discovery agent (loop ①)

Given a goal, the model proposes search queries, a search tool runs them, and the
model judges which results are real event-listing sources worth crawling —
deciding the next action itself each round. This is the most agentic loop:
the model, not a person, decides what to search and what to crawl.

## Run

```sh
go run ./cmd/eventscout                       # local backend, fixture search
go run ./cmd/eventscout -backend solar        # after 2026-07-17
go run ./cmd/eventscout -rounds 2 -goal "..." # tune
```

Search is pluggable (`agent.SearchTool`). This CLI ships a fixture-backed search
(`fixtures/search.json`: keyword groups → canned results) so the loop runs
offline. The fixture deliberately mixes real event sources (venue calendars,
organizer pages) with noise (a news article, a personal blog, a shopping page)
so the model's judgment is exercised.

For a fully automated live-search run, select the Tavily adapter and provide the
credential only through the process environment or an ignored local `.env`
file:

```sh
export EVENTSINTEL_SOLAR_API_KEY='...'
export EVENTSINTEL_TAVILY_API_KEY='...'

go run ./cmd/eventscout \
  -backend solar \
  -search-provider tavily \
  -rounds 2 \
  -goal '2026년 이후 한국 AI 로봇 바이오 의료기기 공식 행사 소스 찾기'
```

As of 2026-07-18, Tavily documents 1,000 free API credits per month without a
credit card; a basic search costs one credit. See the official
[API credit documentation](https://docs.tavily.com/documentation/api-credits)
and [search endpoint](https://docs.tavily.com/documentation/api-reference/endpoint/search).

Only public event-discovery queries belong in this mode. Before a query leaves
the process, contact-like text is redacted. Result titles and snippets are also
redacted, and malformed, private-network, localhost, credential-bearing, or
contact-bearing URLs are rejected. Tavily's privacy policy says it collects
search queries and may share parts with search-index providers, so never place
private, patient, clinical, account, or other personal data in a goal or query.
See [Tavily's privacy policy](https://www.tavily.com/privacy).

Fixture remains the default. Choosing `-search-provider tavily` without
`EVENTSINTEL_TAVILY_API_KEY` fails before any model or search request is sent.

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
offline. Swap in a real web-search tool later without changing the agent logic.
The fixture deliberately mixes real event sources (venue calendars, organizer
pages) with noise (a news article, a personal blog, a shopping page) so the
model's judgment is exercised.

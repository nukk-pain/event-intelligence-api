# eventagent — Solar-backed event-intelligence agent

Runs the agent loop over one event: extract core facts from the main page,
decide which linked pages are worth reading, enrich with founder-actionable
facts (registration deadline, booth/exhibitor, startup program), and print a
brief. Backend-agnostic (`internal/agent`); runs on local qwen36-dwq today and
Solar once `EVENTSINTEL_SOLAR_API_KEY` is set.

## Run

```sh
# real crawl — fetch a live event page, then fetch only the links the agent picks
go run ./cmd/eventagent -url 'https://www.coex.co.kr/exhibitions/<...>/'

# fixture case (offline)
go run ./cmd/eventagent -case cmd/eventagent/fixtures/ai-conf

# after 2026-07-17
go run ./cmd/eventagent -url '<...>' -backend solar
```

### Real-crawl mode (`-url`)

Fetches the page through `internal/fetch` (SSRF IP guard, robots.txt, per-host
rate limiting), pulls `<title>`/og-meta + readable body text and candidate links
(with anchor text), and lets the agent choose which links to read — fetching
only the chosen pages.

**Where it works:** text-bearing pages — press releases, PDFs (as text), blog
announcements, server-rendered event pages. This is the agent's real target: the
readable long-tail that has no hand-written adapter.

**Known limitation:** most Korean venue/organizer sites (coex.co.kr, kintex.com,
many `*.kr` organizer pages) are JS-rendered SPAs whose static HTML is only site
chrome, so extraction returns little. Those need either a site-specific adapter
(the production pipeline has COEX/KINTEX) or the headless CDP fallback
(`fetch.CDPFetcher` interface exists but is intentionally unwired). Do not judge
the agent on JS venue pages; feed it pages that actually contain text.

### Fixture mode (`-case`)

```
<case>/main.txt        event page text
<case>/links/*.txt      one candidate linked page per file;
                        FIRST line = URL, rest = page text
```

Contact info (phone/email) is stripped before any text is sent to a model.

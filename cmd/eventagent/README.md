# eventagent — Solar-backed event-intelligence agent

Runs the agent loop over one event case: extract core facts from the main page,
decide which linked pages are worth reading, enrich with founder-actionable
facts (registration deadline, booth/exhibitor, startup program), and print a
brief. Backend-agnostic (`internal/agent`); runs on local qwen36-dwq today and
Solar once `EVENTSINTEL_SOLAR_API_KEY` is set.

## Run

```sh
go run ./cmd/eventagent -case cmd/eventagent/fixtures/ai-conf
go run ./cmd/eventagent -case <dir> -backend solar   # after 2026-07-17
```

## Case layout

```
<case>/main.txt        event page text
<case>/links/*.txt      one candidate linked page per file;
                        FIRST line = URL, rest = page text
```

The agent decides which links to read (an LLM step), so include real candidate
links (register / booth / location, etc.) to exercise the multi-hop path.
Contact info (phone/email) is stripped before any text is sent to a model.

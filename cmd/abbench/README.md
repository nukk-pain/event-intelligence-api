# abbench — LLM extraction A/B harness

Measures how well different LLM backends extract structured event facts from
raw, unstructured Korean event text, and what each extraction costs in tokens.
Standalone (no internal imports); runs against any OpenAI-compatible endpoint.

## Run

```sh
go run ./cmd/abbench -fixtures cmd/abbench/fixtures
```

- **local** backend is on by default (`http://127.0.0.1:18900/v1`, `qwen36-dwq`).
  Override with `EVENTSINTEL_LOCAL_BASE_URL` / `EVENTSINTEL_LOCAL_MODEL`, or set
  `EVENTSINTEL_LOCAL_BASE_URL=off` to skip.
- **solar** backend appears once `EVENTSINTEL_SOLAR_API_KEY` is set (Open 2 Early
  Access, from 2026-07-17). See `.env.example`.

## Add a case

Drop two files in the fixtures dir:

- `<name>.input.txt`  — raw event announcement text
- `<name>.gold.json`  — expected fields (null for absent). Keys: name, name_en,
  start_raw, end_raw, venue_name, city, organizer (주최), host (주관), homepage_url.

Dates score by digit sequence; URLs case-insensitively; other fields by
normalized text. Contact info (phone/email) is stripped before send (compliance).

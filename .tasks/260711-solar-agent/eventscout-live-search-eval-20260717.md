# EventScout real-web-search evaluation — 2026-07-17

## Goal

Evaluate Solar Open 2 as a source-discovery agent with real web-search results,
instead of the deterministic `cmd/eventscout` fixture.

## Method

1. Ran the normal Solar `Discover` loop for three rounds to obtain the model's
   own Korean search queries.
2. Submitted those exact queries to a real web-search tool.
3. Passed 12 returned results, without changing their source class, through the
   existing `judgeResults` prompt on `solar-open2` with
   `reasoning_effort=minimal`.
4. Manually opened representative accepted sources to verify that they expose
   an event schedule or venue calendar.

This was a manually bridged live evaluation. The repository does not yet have
credentials for a production search API, so it is not an automated end-to-end
`SearchTool` run.

## Solar-proposed queries

1. `한국 AI 로봇 바이오 의료기기 전시회 일정 공식 홈페이지 주최기관 캘린더`
2. `한국 AI 로봇 바이오 의료기기 산업 전시회 공식 캘린더 주최기관 일정 2026`
3. `한국 전시컨벤션센터 AI 로봇 바이오 의료기기 행사 일정 캘린더 2026`

## Judgment set

Expected event sources (8):

- Korea RoboCup Association
- Korea Artificial Intelligence Conference
- Industrial AI EXPO
- Busan Robot Competition
- AIoT Korea Exhibition
- RoboCup 2026 Symposium
- Korea AI Association summer conference
- Songdo Convensia event calendar

Expected non-sources (4):

- securities-company market calendar
- university admissions guide
- cosmetics industry weekly report
- securities-company robotics industry report

## Result

| Metric | Result |
|---|---:|
| True positives | 8/8 |
| True negatives | 4/4 |
| Precision | 100% |
| Recall | 100% |
| Classification accuracy | 12/12 (100%) |
| Solar usage | 720 input + 808 output tokens |
| Solar latency | 12.42 s |

Representative direct verification also passed:

- `robocupkorea.org` identifies the organizing association, describes the
  competition as annual, and lists 2026 dates and venues.
- `industrialaiexpo.or.kr` exposes the official 2026 full program and KINTEX
  exhibition dates.
- Songdo Convensia exposes a first-party monthly venue event calendar.

## Failed retrieval experiment

Bing's public RSS response returned nearly identical generic Korea links for all
three queries. Solar accepted five of those weak results, but manual relevance
was 0/5. The experimental adapter was removed rather than shipped.

## Conclusion

With relevant real search results, Solar's source judgment was perfect on this
small test. The current bottleneck is retrieval quality and production search
integration, not the Solar classification prompt. The next implementation step
is a documented search API adapter with explicit credentials, bounded requests,
and a small labeled live-search regression set.

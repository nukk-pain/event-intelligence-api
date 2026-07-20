# Event Intelligence API

흩어진 행사 페이지를 수집해 사람이 읽기 좋은 일정과 에이전트가 호출할 수 있는
read-only API로 제공합니다. 운영 중인 공개 서비스는
[events.nukk.net](https://events.nukk.net)입니다.

이 저장소에는 결정적 Go 크롤러와 함께 Upstage Solar Open 2를 사용하는 세 가지
에이전트 루프가 들어 있습니다.

1. `eventscout` — 목표를 받아 검색어와 다음 조사 대상을 스스로 결정합니다.
2. `eventagent` — 행사 페이지와 링크를 따라 등록 마감, 부스 신청, 스타트업
   프로그램을 찾아 실행 가능한 브리핑으로 조립합니다.
3. `eventmcp` — 다른 에이전트가 자연어로 라이브 행사 API를 조회할 수 있는 MCP
   서버입니다.

## Architecture

```text
public event pages
        |
        v
deterministic Go ingest ----> SQLite ----> read-only API / browser
        |
        +----> Solar Open 2 agent loops
               source discovery / extraction / enrichment
```

일반 API 조회에서는 LLM을 호출하지 않습니다. Solar는 배치 수집과 자연어 질의
해석에만 사용하며, 연락처는 모델 전송 전에 제거합니다.

## Solar Open 2 quick start

Go 1.26 이상과 Upstage API 키가 필요합니다. 키는 파일에 커밋하지 말고 현재 셸에만
주입합니다.

```sh
export EVENTSINTEL_SOLAR_API_KEY='...'
export EVENTSINTEL_SOLAR_MODEL='solar-open2'

# 한 행사를 추출하고 관련 링크를 골라 액션 브리핑 생성
go run ./cmd/eventagent \
  -case cmd/eventagent/fixtures/ai-conf \
  -backend solar

# 자율 소스 발견 루프 (keyless public provider가 기본값; Tavily 키 불필요)
go run ./cmd/eventscout -backend solar -rounds 2

# 재현 가능한 fixture 검색 (명시적 offline 옵션)
go run ./cmd/eventscout -backend solar -search-provider fixture -rounds 2

# 정식 Tavily 검색 API를 쓰는 완전 자동 소스 발견 루프
export EVENTSINTEL_TAVILY_API_KEY='...'
go run ./cmd/eventscout \
  -backend solar \
  -search-provider tavily \
  -rounds 2 \
  -goal '2026년 이후 한국 AI 로봇 바이오 의료기기 공식 행사 소스 찾기'

# Solar Open 2 추출 벤치마크
EVENTSINTEL_LOCAL_BASE_URL=off \
  go run ./cmd/abbench -v -fixtures cmd/abbench/fixtures
```

`eventscout`는 서버가 관리하는 공개 seed를 순회하는 `public` 검색 provider를
기본으로 사용합니다. 재현 가능한 fixture 검색은 `-search-provider fixture`를
지정할 때만 사용하며, 정식 Tavily 검색 API도 명시적으로 선택할 수 있습니다.
Tavily 키는 `EVENTSINTEL_TAVILY_API_KEY` 환경변수에만 주입하며, 없으면 외부
요청 전에 실패합니다. 2026-07-18 기준 공식 문서상 무료
한도는 월 1,000 credits이고 기본 검색은 1 credit입니다. 검색 질의는 외부 공급자에
전송되므로 공개 행사 조사 문구만 사용해야 합니다. 연락처 형태는 전송 전에 제거하고,
결과의 제목·요약과 URL도 같은 개인정보·사설망 경계를 통과한 것만 모델에 전달합니다.
상세 실행법과 공식 한도·개인정보 링크는 `cmd/eventscout/README.md`에 있습니다.

라이브 행사 데이터를 MCP로 조회할 수도 있습니다.

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask_events","arguments":{"question":"다음 달 서울 AI 행사"}}}' \
  | go run ./cmd/eventmcp
```

각 명령의 상세 사용법은 `cmd/eventagent`, `cmd/eventscout`, `cmd/eventmcp`,
`cmd/abbench`의 README에 있습니다.

## Benchmark

2026-07-17, 같은 한국어 행사 공지 6건에서 9개 필드씩 총 54개 필드를 평가했습니다.

| Backend | Field accuracy | Avg output tokens | Avg latency |
|---|---:|---:|---:|
| Local Qwen 3.6 DWQ | 100% | 2,494 | 22.2s |
| Solar Open 2 | 100% | 118 | 1.9s |

Solar Open 2는 정확도를 유지하면서 로컬 대조군보다 출력 토큰을 약 21배 줄였고,
약 12배 빨랐습니다. Private Beta 가격이 공개되지 않아 토큰 수를 비용 프록시로
사용했습니다. 재현 가능한 전체 결과는
[`abbench-solar-open2-20260717.md`](.tasks/260711-solar-agent/abbench-solar-open2-20260717.md)에
기록했습니다.

`solar-open2`는 `reasoning_effort`를 생략하면 짧은 구조화 요청에도 hidden
reasoning을 많이 사용할 수 있었습니다. 이 저장소는 추출 요청에 `minimal`을
명시해 출력 예산을 실제 JSON에 집중시킵니다.

## Solar Agent Partner 후기

공개 행사 API를 운영하면서, 흩어진 한국어 행사 정보를 스스로 찾고 파고드는
자율 에이전트의 뇌로 Solar Open 2를 붙여 봤습니다. 단순 추출 데모에 그치지 않고
소스 발견, 행사별 멀티홉 조사, 다른 에이전트가 호출하는 MCP 질의까지 세 경로를
Go로 연결했습니다. 고정 fixture에서는 로컬 Qwen과 Solar 모두 54개 필드를 전부
맞혔지만, Solar는 평균 출력이 2,494토큰에서 118토큰으로 줄고 지연도 22.2초에서
1.9초로 짧아졌습니다. 특히 `reasoning_effort=minimal`을 명시하자 불필요한 hidden
reasoning 없이 짧은 JSON을 안정적으로 반환했습니다. 한국어 사실 추출처럼 답의
형태가 분명한 작업에서는 더 오래 생각하게 하는 것보다 모델의 추론 예산을 정확히
통제하는 편이 품질과 비용을 함께 잡는다는 점이 가장 실용적인 배움이었습니다.

## Core API

```sh
make test
make build

EVENTSINTEL_DB_PATH=eventsintel.db ./bin/eventsintel ingest
EVENTSINTEL_DB_PATH=eventsintel.db ./bin/eventsintel serve
```

수집기는 공개 HTTP(S) 소스만 허용하며 SSRF 방어, robots.txt, 호스트별 속도 제한,
본문 크기 제한을 적용합니다. API는 cache-first, read-only로 동작합니다.

## Data policy

- 공개 행사 사실만 처리합니다.
- 전화번호와 이메일은 모델 호출 전에 제거합니다.
- 환자, 임상, EMR 또는 사설망 데이터는 이 프로젝트에 넣지 않습니다.
- 저작권 있는 원문 전체 대신 짧은 사실 요약과 출처 URL만 저장합니다.

## License

[MIT](LICENSE)

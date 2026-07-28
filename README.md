# Event Intelligence API

흩어진 행사 페이지를 수집해 사람이 읽기 좋은 일정과 에이전트가 호출할 수 있는
read-only API로 제공합니다. 운영 중인 공개 서비스는
[events.nukk.net](https://events.nukk.net)입니다.

**결정적 크롤러가 못 읽는 공식 행사 페이지를 Solar Open 2
에이전트가 링크를 따라가며 복구하고, 추가된 61개 값을 전수 감사해 근거 없는
값은 저장하지 않게 만들었습니다.** 감사 전체는 [`eval/`](eval/)에서
`make eval-report`로 재생성됩니다.

이 저장소에는 결정적 Go 크롤러와 함께 Solar Open 2를 사용하는 세 가지
에이전트 루프가 들어 있습니다.

1. `eventscout`는 목표 하나를 받아, 매 턴 검색과 페이지 열람과 채택과 종료 중
   다음 행동을 Solar가 직접 고르는 발견 루프입니다. 코드는 순서를 정하지 않고
   예산과 경계만 지킵니다.
2. `eventagent`는 행사 페이지와 링크를 따라 등록 마감, 부스 신청, 스타트업
   프로그램을 찾아 바로 쓸 수 있는 브리핑으로 묶습니다.
3. `eventmcp`는 MCP 서버입니다. 다른 에이전트가 자연어로 라이브 행사 API를
   조회합니다.

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
전송되므로 공개 행사 조사 문구만 사용해야 합니다. 같은 연락처 제거 규칙이 질의에도 걸리고,
결과의 제목과 요약, URL도 같은 개인정보와 사설망 경계를 통과한 것만 모델에
전달합니다. 상세 실행법과 공식 한도, 개인정보 링크는 `cmd/eventscout/README.md`에 있습니다.

## 내 AI에 행사 도구 연결하기

MCP를 지원하는 AI 도구라면 설치 없이 URL 등록만으로 라이브 행사 데이터를
씁니다.

```sh
# Claude Code 기준
claude mcp add --transport http events https://events.nukk.net/mcp
```

등록한 뒤에는 자기 AI에게 그대로 물으면 됩니다. "다음 달 서울 AI 행사 뭐
있어?" 서버가 질문을 필터로 바꿔 실데이터를 돌려줍니다. 위 질문은 실제로
`ai` 카테고리, 서울, 2026년 8월 필터로 변환되어 7건을 반환했습니다.

- `search_events`는 구조화 검색입니다. 모델 없이 동작하고 한도가 없습니다.
- `ask_events`에는 자연어로 물어봅니다. 서버 운영자의 Solar 키로 돌며
  클라이언트당 10분에 10회, 하루 60회까지입니다. 넘으면 429와 함께
  Retry-After를 돌려줍니다.
- 서버는 세션을 만들지 않습니다. 질문 문장 외에는 아무것도 보내지 마세요.

로컬 stdio로 직접 돌릴 수도 있습니다.

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask_events","arguments":{"question":"다음 달 서울 AI 행사"}}}' \
  | go run ./cmd/eventmcp
```

각 명령의 상세 사용법은 `cmd/eventagent`, `cmd/eventscout`, `cmd/eventmcp`,
`cmd/abbench`의 README에 있습니다.

## 추출 동작 확인 실험

소규모 동작 확인입니다. 엄밀한 비교 벤치마크로 쓰기에는 표본이 작습니다.
2026-07-17, 같은 한국어
행사 공지 6건에서 9개 필드씩 총 54개 필드를 평가했습니다.

| Backend | Field accuracy | Avg output tokens | Avg latency |
|---|---:|---:|---:|
| Local Qwen 3.6 DWQ | 100% | 2,494 | 22.2s |
| Solar Open 2 | 100% | 118 | 1.9s |

두 행은 같은 fixture와 같은 프롬프트를 쓰지만 한 번의 실행에서 나란히 잰 값이
아닙니다. 로컬 수치는 프롬프트 튜닝 직후 두 백엔드를 함께 돌린 실행에서, Solar
Open 2 수치는 같은 날 Solar만 따로 돌린 실행에서 가져왔습니다.

토큰 격차의 상당 부분은 추론 예산 차이입니다. Solar는 `reasoning_effort=minimal`로
눌러놓았고 로컬 대조군은 추론을 켠 채로 돌았습니다. 조건을 맞춰 재면 격차는
줄어듭니다. 지연 시간 역시 맥북에서 돌린 로컬 서빙과 클라우드 API를 비교한 값이라
모델 자체의 속도 차이로 읽으면 안 됩니다. 정확도는 양쪽 다 만점이어서 이 표로는
우열을 가릴 수 없고, 6건은 변별력을 보기에 적은 표본입니다.

Private Beta 가격이 공개되지 않아 토큰 수를 비용 프록시로 사용했습니다. 재현
가능한 전체 결과는
[`abbench-solar-open2-20260717.md`](.tasks/260711-solar-agent/abbench-solar-open2-20260717.md)에
기록했습니다.

`solar-open2`는 `reasoning_effort`를 생략하면 짧은 구조화 요청에도 hidden
reasoning을 길게 썼습니다. 이 저장소는 추출 요청에 `minimal`을
명시해 출력 예산을 실제 JSON에 집중시킵니다.

## 라이브 소스 후보 판정

지금까지 증명된 것은 카탈로그 후보의 판정이지 카탈로그 밖 신규 소스의
발견이 아닙니다. 그 한계 안에서의 기록입니다.

`eventscout`의 공개 탐색은 한동안 소스를 한 건도 반환하지 못했습니다. 어디서
비는지 알 수 없어서 계기판을 먼저 만들었습니다. 후보가 어느 단계에서 사라지는지를
개수로만 기록하고, 목표 문장이나 후보 URL, 가져온 본문, 모델 응답은 남기지
않습니다.

계측을 붙이고 나서 원인이 보였습니다. 사이트맵에서 나온 후보가 30개 상한을 먼저 채우는
바람에, 뒤늦게 정상적으로 받아온 seed 페이지가 자리를 얻지 못했습니다. 제목이
보장되는 경로는 seed뿐이라 모델에는 판정할 거리가 아예 가지 않았습니다.

교정은 상한을 올리는 대신 아직 처리되지 않은 seed 수만큼 자리를 비워두게
고쳤습니다. 총 후보 상한 30은 그대로이고 크롤과 robots, SSRF, 토큰 한도도 건드리지
않았습니다.

| 항목 | 교정 전 | 교정 후 |
|---|---:|---:|
| 모델에 제안된 후보 | 0 | 5 |
| 판정 호출 | 0 | 1 |
| 채택된 공개 소스 | 0 | 5 |

운영자용 스모크 스크립트
[`scripts/smoke-solar-public-discovery.sh`](scripts/smoke-solar-public-discovery.sh)로
재현할 수 있습니다. 키가 없으면 모델도 네트워크도 건드리지 않고 건너뜁니다.
채택 0건도 분류된 정상 결과입니다. 이번 교정은 0건이 구조적으로
불가피하던 상태를 없앤 것이지 매 실행의 최소 건수를 약속하지 않습니다.

### 안무를 모델에게 넘긴 뒤

위 교정까지는 검색과 판정의 순서를 코드가 고정하고 있었습니다. 이후 루프를
다시 짜서, 매 턴 Solar가 검색과 후보 페이지 열람과 채택과 종료 중 하나를 JSON
액션으로 고르고 루프는 그것을 실행만 하게 바꿨습니다. 코드에 남긴 것은
예산(턴 8회, 검색 2회, 열람 3회)과 경계(SSRF, robots, 후보 목록 밖 URL 열람
거부)입니다. 바꾼 날 운영 서버의 첫 실행 기록입니다.

| 항목 | 고정 안무 | 액션 루프 |
|---|---:|---:|
| 모델 턴 | 4 | 8 |
| 자발적 페이지 열람 | 없음 | 3 |
| 채택된 공개 소스 | 5 | 10 |

출력 토큰은 여덟 턴을 합쳐 590개였습니다. 열람으로 읽은 본문은 연락처를
지우고 길이를 자른 뒤에만 프롬프트로 돌아오며, 크롤된 제목이 프롬프트 구획을
위조하지 못하게 개행과 꺾쇠를 제거합니다.

## 수집 파이프라인의 Solar 보강

배포되는 `eventsintel ingest`가 Solar를 호출합니다. 결정적 크롤러가 공식 행사
페이지에서 뽑지 못한 참가 정보를 멀티홉 에이전트가 한 단계 더 들어가 채웁니다.
읽는 페이지는 파이프라인이 이미 가져온 것이라 요청을 새로 만들지 않고, 링크
추적도 같은 fetcher를 통해 allowlist와 robots 정책, 요청 속도 제한이 같이
걸립니다. 이 경로는 수집 때만 돌고 읽기 API에는 붙지 않습니다.

기여분은 같은 216건을 두고 보강을 끈 대조 실행과 비교해 측정했습니다.

| 필드 | 대조 결측 | 보강 후 결측 |
|---|---:|---:|
| `registration_deadline` | 216 | 197 |
| `register_url` | 110 | 92 |
| `actions.can_register` | 105 | 87 |
| `actions.has_startup_program` | 197 | 188 |

**216건 중 40건에서 비어 있던 필드가 하나 이상 채워졌습니다. 18.5%입니다.**

채운 값이 맞는지도 쟀습니다. 추가된 61개 값을 하나씩 공식 페이지와 대조해 30개를
근거로 검증했고, 영문명은 19개 중 17개가 확인된 반면 마감일은 26개 중 3개만
확인됐습니다. 반대 유형 마감일을 가져온 사례도 1건 있었습니다. 그래서 지금은
에이전트가 실제로 읽은 페이지에 그 날짜가 문자 그대로 있을 때만 마감일을
저장하고, 검증 도입 전에 저장된 마감일은 마이그레이션으로 전부 회수해 다음
수집이 근거 있는 값만 다시 채우게 했습니다. 상세는 [`eval/`](eval/)에
있습니다.

한 번의 수집은 142건을 시도하고 9.3분이 걸립니다. 20분 데드라인 안이고 속도
제한에 걸린 요청은 없었습니다. 동시 모델 작업은 6으로 묶어 분당 토큰 상한
아래에 머물게 했습니다.

마감일은 `2026년 9월 1일` 같은 한국어 표기를 ISO로 바꾼 뒤 저장합니다. 연월일이
모두 명시된 것만 받고 `선착순 마감`처럼 날짜가 없는 문구는 버립니다.

## Solar Agent Partner 후기

`eventscout`는 매 턴 Solar가 검색할지, 후보 페이지를 열어볼지, 채택할지,
그만할지 고른다. 코드는 예산이랑 SSRF 경계만 지킨다. 운영 서버에서 여덟 턴을
혼자 끌고 가며 애매한 페이지 세 곳을 열어 확인하고 공개 소스 10건을 채택했다.
출력 토큰 590개. 검색 결과 12건을 주면 공식 소스 8건은 다 받고 보고서·입시
안내 같은 오답 4건은 다 거른다.

채택된 후보는 사람이 검토해서 커밋으로 크롤 대상에 넣는다. 이 경로로
인공지능산업협회 게시판과 전국 전시회 디렉토리가 들어와 행사 215건이 늘었고,
지방 전시장이 처음 잡히기 시작했다.

수집 쪽에서는 결정적 파서가 못 채운 참가 정보를 216건 중 40건에서 채웠다.
채운 값 61개는 공식 페이지랑 일일이 대조했고, 근거가 약한 마감일은 저장
조건을 조이고 이전 값을 회수했다. MCP 서버는 자연어 질문을 필터로 바꿔
라이브 API를 조회한다.

`reasoning_effort`는 꼭 주자. 안 주면 짧은 JSON 하나 받는 요청에도 숨은
추론을 길게 쓰고, 예산이 작으면 추론만 하다가 본문 없이 끝난다. `minimal`을
박으니 추론 토큰이 726에서 0이 됐고, 호출당 예약을 500토큰까지 줄여 같은
비용으로 턴을 두 배 돌린다.

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

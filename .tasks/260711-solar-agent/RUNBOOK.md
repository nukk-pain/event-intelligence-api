# RUNBOOK — Solar Stage 1 마무리 절차

제출물은 **7/31 마감**이다. 핵심 에이전트는 로컬(qwen36-dwq)로 먼저 완성했고,
2026-07-17에 Solar Open 2 연결·측정·튜닝과 세 루프 실행 검증까지 완료했다.
남은 필수 작업은 공개 README/후기, 커밋·푸시, 제출이다.

## 0. 사전 상태 (여행 전 완료됨)

- `feat/solar-agent` 브랜치에 전부 커밋됨.
- `internal/agent` — LLM 클라이언트 + 추출 + 멀티홉 보강(Solar-agnostic).
- `cmd/eventagent` — 에이전트 CLI (main.txt + links/ 케이스 실행).
- `cmd/abbench` — A/B 벤치마크. 로컬 baseline 저장됨(`baseline-local-*.md`).
- 준수사항(연락처 사전 제거·공개데이터·저작권) 코드에 반영됨.

## 1. Solar 키 발급 (7/17 이후, 5분)

1. console.upstage.ai에 승인된 사용자 계정으로 로그인.
2. Open 2 Early Access API 키 발급. 확인된 모델 ID는 `solar-open2`.
3. 로컬에서만 export (커밋 금지):

   ```sh
   export EVENTSINTEL_SOLAR_API_KEY=...            # secret
   export EVENTSINTEL_SOLAR_MODEL=<open2-model-id> # 콘솔에서 확인한 값
   # base url 기본값 https://api.upstage.ai/v1 (다르면 EVENTSINTEL_SOLAR_BASE_URL)
   ```

## 2. A/B 측정 (자동, 명령 하나)

```sh
go run ./cmd/abbench -v -fixtures cmd/abbench/fixtures
```

- 이제 `local`과 `solar` 두 행이 함께 나온다. `baseline-local-*.md`(88.9%)와 비교.
- `-v`로 어느 필드에서 Solar가 이기고 지는지 확인 → 프롬프트 튜닝 타깃.

## 3. 에이전트 Solar로 실행

```sh
# 루프 ② 멀티홉 보강 (한 행사 깊이 파기)
go run ./cmd/eventagent -case cmd/eventagent/fixtures/ai-conf -backend solar

# 루프 ① 자율 소스 발견 (fixture 검색으로 로직 확인)
go run ./cmd/eventscout -backend solar -rounds 2
```

- eventagent: 3콜(추출→링크선택→보강)이 Solar로 돌며 founder brief JSON 출력.
- eventscout: 라운드마다 2콜(질의 제안→소스 판별). fixture 검색이라 오프라인 동작.
  **실검색 연결**: `agent.SearchTool`을 구현한 실제 웹검색 어댑터를 만들어
  `cmd/eventscout`의 `fixtureSearch` 대신 주입. robots·공개데이터 준수 유지.
- 2026-07-17 실제 웹검색 수동 브리지 평가에서는 공식 소스 8/8 채택, 비소스
  4/4 제외(12/12). 상세 결과는 `eventscout-live-search-eval-20260717.md`.
  Bing 공개 RSS는 세 질의 모두 일반적인 한국 링크를 반복해 0/5였으므로 사용하지
  않는다. 자동화할 때는 문서화된 정식 검색 API와 자격증명을 사용한다.

### Tavily 정식 검색 API 자동 실행 (2026-07-18 구현)

Tavily 공식 문서 기준 무료 한도는 월 1,000 credits이며 basic search는 1 credit이다.
신용카드는 필요하지 않다. 계정과 키는 https://app.tavily.com 에서 만들되, 실제 키는
반드시 무시되는 로컬 `.env` 또는 현재 프로세스 환경에만 저장한다. 문서·터미널 캡처·
로그·커밋에는 키를 넣지 않는다.

```sh
export EVENTSINTEL_SOLAR_API_KEY=...   # secret
export EVENTSINTEL_TAVILY_API_KEY=...  # secret

go run ./cmd/eventscout \
  -backend solar \
  -search-provider tavily \
  -rounds 2 \
  -goal '2026년 이후 한국 AI 로봇 바이오 의료기기 공식 행사 소스 찾기'
```

질의는 Tavily에 전송되고 검색 인덱스 제공자와 일부 공유될 수 있으므로 공개 행사 조사
문구만 넣는다. 전화·이메일·환자·임상·계정·사설 데이터는 목표에 넣지 않는다. 구현은
연락처를 외부 검색 전에 제거하고 검색 결과도 다시 정제하지만, 이 경계를 민감정보
전송 허가로 해석해서는 안 된다.

- Credits: https://docs.tavily.com/documentation/api-credits
- Search API: https://docs.tavily.com/documentation/api-reference/endpoint/search
- Privacy: https://www.tavily.com/privacy

```sh
# 루프 ③ MCP 서버 — ask_events가 Solar로 자연어 파싱
export EVENTSINTEL_SOLAR_API_KEY=...   # (2번에서 export했으면 생략)
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask_events","arguments":{"question":"다음 달 서울 AI 행사"}}}' \
  | go run ./cmd/eventmcp
```

- eventmcp: `search_events`(LLM 없음)·`ask_events`(모델이 자연어→필터) MCP 툴.
  **프로덕션 연결(선택)**: `cmd/eventmcp`의 fixture 이벤트 로드를 store/read API
  조회로 교체. **MCP 클라이언트 등록**: 빌드한 `eventmcp` 바이너리를 stdio로 지정.

## 4. 튜닝 (반나절)

- Solar가 틀리는 필드에 맞춰 `internal/agent/extract.go`의 `ExtractPrompt`,
  `enrich.go`의 프롬프트를 조정. 로컬용에 맞춘 문구가 Solar엔 최적이 아닐 수 있음.
- **`response_format`(JSON schema 구조화 출력)은 공식 API Reference에
  "solar-pro-2 모델에서만 호환"이라고 명시되어 있음 — `solar-open2`는 지원 문서가
  없어 안 될 확률이 높다(2026-07-16 확인, console.upstage.ai/api/chat).
  구조화 출력 경로로 강화를 시도하지 말고, 현재처럼 프롬프트 유도 + 직접 파싱
  방식을 유지할 것. function-calling(`tools`/`tool_choice`)은 일반적으로 지원
  문서화되어 있으니 그쪽은 시도 가능. read 경로 LLM-free 원칙은 유지.
- `solar-open2`에서는 `reasoning_effort`를 생략하면 실제 API가 hidden reasoning을
  사용했다. 짧은 요청은 completion 예산을 전부 reasoning에 써 `content=null`로
  끝나기도 했다. 추출·질의처럼 짧은 구조화 응답이 목적이면 반드시
  `reasoning_effort=minimal`을 명시한다. 2026-07-17 직접 비교에서 생략 시 간단한
  JSON 요청에 reasoning 726토큰, `minimal` 명시 시 reasoning 0토큰이었다.
- fixture 몇 개 더 추가해 통계 신뢰도 보강(선택).

## 5. 후기 작성 (200자+)

- 초안: 워크스페이스 문서 `2026-07-11-...application.md` §4-1.
- `baseline-local-*.md` vs Solar 결과의 실제 수치(정확도·건당 토큰)로 마지막 문단
  채우기. 정직하게 — Solar가 이긴 구간/진 구간 둘 다.

## 6. 제출 (7/31 전)

- `feat/solar-agent`를 `main`에 머지(또는 브랜치 링크 그대로 제출).
- 후기를 README 또는 블로그에 게시.
- 결과물 링크를 Upstage 제출 채널에 제출(신청 시 안내받은 경로).

## 스트레치 (시간 남으면 / Stage 2)

- 루프 ① 자율 소스 발견(검색→신규 소스 판단→크롤 대상 결정).
- 루프 ③ MCP 도구 노출(Hermes/Claude Code가 행사 질의).

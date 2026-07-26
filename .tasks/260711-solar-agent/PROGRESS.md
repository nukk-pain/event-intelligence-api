# PROGRESS — Solar-backed autonomous event-intelligence agent

## Current focus

**(2026-07-26 기준)** Stage 1 코드 작업은 끝났다. 3겹 에이전트 루프 + Solar Open 2
연결 + 계정 없는 공개 탐색 + 수확량 진단까지 구현·검증했고, `feat/solar-agent`를
`main`에 머지해 origin에 푸시했다(`2bb0082`). 남은 것은 **7/31 전 Upstage 채널에
저장소 링크와 후기를 제출**하는 일뿐이다. 후기는 `README.md`의
"Solar Agent Partner 후기" 절에 실측 수치와 함께 들어 있다.

과거 진행 배경: 사용자 7/17~10일 여행이 Stage 1 기간을 거의 덮어, 여행 전에 핵심
에이전트를 로컬(qwen36-dwq)로 완성해 두고 복귀 후 "Solar 붙이기→측정→튜닝→후기"만
남기는 순서로 갔다. 그래서 `internal/agent`는 Solar-agnostic(OpenAI 호환)이고 키만
바꾸면 백엔드가 전환된다. 복귀 후 절차는 `RUNBOOK.md` 참조.

## Status

- [x] 워크스페이스 지원 문서 작성(전략·요건·후기 초안).
- [x] 지원 repo(event-intelligence-api) 공개 + MIT.
- [x] 작업 브랜치 `feat/solar-agent` + `.tasks/260711-solar-agent/` PLAN 스캐폴드.
- [x] `.env.example`에 Solar env 템플릿.
- [x] 신청서 제출 완료(2026-07-11).
- [x] 핵심 에이전트 구현(로컬 검증): `internal/agent`(추출+멀티홉 보강),
      `cmd/eventagent`(CLI), `cmd/abbench`(A/B). 준수사항 코드 반영.
- [x] **루프 ① 자율 소스 발견 구현**: `internal/agent/discover.go`(질의 제안 →
      검색 툴 → 소스 판별 루프, 다음 행동을 모델이 결정), `cmd/eventscout`(CLI +
      fixture 검색). 검색은 `SearchTool` 인터페이스로 추상화 → 7/17에 실검색 교체.
- [x] **루프 ③ MCP 노출 구현**: `internal/agent/eventquery.go`(자연어→필터는 모델,
      데이터 조회는 LLM-free), `cmd/eventmcp`(JSON-RPC 2.0 stdio MCP 서버, 무의존).
      툴 `search_events`(구조화)·`ask_events`(자연어). 프로토콜 핸드셰이크 검증됨.
      → **야심 버전 3겹 루프 전부 로컬 구현·검증 완료.**
- [x] **실데이터 연결(루프 ③)**: eventmcp가 fixture 대신 라이브 events.nukk.net
      read API 조회(카테고리 taxonomy 매핑·서버측 필터·upcoming 기본). search_events
      13건·ask_events 6건 실데이터 반환 검증. → 토이 아님, 라이브 시스템 위 에이전트.
- [x] 로컬 baseline 측정·저장(qwen36-dwq).
- [x] 복귀 후 절차 `RUNBOOK.md` 작성.
- [x] (7/17) Solar Open 2 키 연결 → A/B 측정 → 프롬프트 튜닝 완료.
      `reasoning_effort=minimal` 명시 후 6케이스 54/54(100%), 평균 출력 118토큰,
      평균 지연 1.9초. 결과: `abbench-solar-open2-20260717.md`.
- [x] Solar Open 2로 세 루프 실행 검증: eventagent 멀티홉 브리핑, eventscout
      소스 발견, eventmcp 자연어→필터→라이브 events.nukk.net 6건 조회.
- [x] (7/17) Solar가 제안한 3개 질의를 실제 웹검색에 실행하고 결과 12건을 기존
      판정 프롬프트에 투입: 공식 소스 8/8 채택, 비소스 4/4 제외(12/12, 100%).
      대표 공식 페이지 직접 열람 검증. Bing RSS는 검색 품질 0/5라 폐기.
      결과: `eventscout-live-search-eval-20260717.md`.
- [x] (7/18) 루프 ①에 자격증명 기반 Tavily 정식 검색 API 어댑터를 추가하고
      `-search-provider fixture|tavily`로 연결. fixture는 기본값으로 보존했다.
      연락처는 검색 전·결과 후 제거하고, 사설망·localhost·userinfo·연락처 포함 URL을
      차단한다. race 테스트, 전체 회귀, missing-key 조기 실패를 검증했다(`db70d64`).
- **(대체됨)** ~~신규 사용자 계정으로 Tavily 키를 발급해 Solar+Tavily 2-round 실제
      E2E를 실행한다.~~ → **7/19~20에 폐기**. 계정 발급이 사용자 인증 대기로 막혀 있어,
      제3자 키가 아예 필요 없는 `public` 프로바이더로 방향을 바꿨다. Tavily 어댑터
      자체는 선택 모드(`-search-provider tavily`)로 코드에 남아 있으나 실 키 E2E는
      끝내 실행하지 않았다.
- [x] (7/19~20) **계정 없는 공개 탐색**: 서버 소유 seed 카탈로그 기반 `public`
      프로바이더를 기본값으로 만들고, 엄격한 공개 크롤 경계(robots·SSRF·MIME·본문
      한도)를 붙였다. 익명 HTTP 데모 `cmd/eventscout-server`는 목표 문장만 받고
      가입·임의 URL·사설망을 허용하지 않으며 4 KiB/800자·2회/10분·24회/일·동시 2건·
      60초 한도로 묶여 있다. Solar 키는 운영자 전용이라 호출자가 넣을 수 없다.
- [x] (7/25~26) **수확량 진단(`yield_trace`)**: 라이브 탐색이 소스 0건을 반환할 때
      어느 단계에서 유실됐는지 알 수 없던 문제를 해결했다. 성공 응답에만 실리는
      요청-국소·카운트 전용 필드로 크롤러 검증 → 모델 제안 → 판정 → 채택 경계를
      드러낸다. 목표문·후보·URL·모델 페이로드·자격증명은 담지 않는다.
- [x] (7/26~27) **라이브 0건 원인 규명 및 교정**. 유실 사유를 카운트 전용으로
      분해해(`prefilter_reasons`, `seed_outcomes`) 라이브에서 원인을 확정했다:
      프로토콜(사이트맵) 큐가 HTML 큐보다 먼저 전부 처리돼 30개 후보 상한을 제목 없는
      사이트맵 자식으로 채우고, 뒤늦게 정상 fetch된 seed 페이지 5건이 전부
      `candidate_cap`으로 거부됐다. seed는 이름 폴백 덕에 제목이 보장되는 유일한
      경로라, 결과적으로 모델에 판정할 후보가 하나도 가지 않았다. 교정은 미처리 seed
      수만큼 후보 슬롯을 유보하는 것이다. 총 상한은 올리지 않았다 — seed가 바로 그
      유보분을 쓴다. 교정 후 라이브: `accepted=5`.
- [x] (7/26) `feat/solar-agent`를 `main`에 fast-forward 머지하고 origin에 푸시
      (`2bb0082`). 푸시 전 `go build`/`go vet`/`go test -race ./...` 전부 통과,
      추적 파일 시크릿 스캔 0건, `.env.example`은 전 항목 주석 처리 확인.
- [ ] (~7/31) **제출**: Upstage 채널에 저장소 링크 + 후기 제출. ← 유일한 잔여 작업

## Evidence

- events.nukk.net 운영 중(COEX/KINTEX/benchmark 인제스트 + read-only API).
- Solar Open 2 추출 A/B: `abbench-solar-open2-20260717.md` (6케이스 54/54, 평균
  출력 118토큰, 평균 지연 1.9초 — 로컬 대조군 대비 출력 토큰 약 21배 감소).
- 실검색 판정 평가: `eventscout-live-search-eval-20260717.md` (12/12).
- 계정 없는 공개 탐색 검증: `.omo/evidence/solar-accountless-public-agent/task-6.txt`
  (gitignore이므로 저장소에는 없고 로컬에만 있다).
- 수확량 진단 검증: `.omo/evidence/solar-live-yield-improvement/` — Todo 1~7 및
  최종 검증 F1~F4 증거. 마찬가지로 로컬 전용이며 worktree
  `event-intelligence-api-wt-solar-yield`에 있다.

## Known limitations

- **(해결됨 2026-07-27)** 라이브 공개 탐색이 0건을 반환하던 문제는 원인을 계측으로
  확정하고 교정했다. 아래 Status의 7/26~27 항목 참조. 교정 후 라이브 스모크는
  `seed_candidates=5 → offered=5 → judge_calls=1 → accepted=5`,
  `outcome=accepted`를 기록한다.
- 사이트맵 자식 후보 14건은 여전히 제목이 없어 prefilter에서 탈락한다
  (`missing_title=14`). 이는 의도된 동작이다. 제목 없는 URL은 판정 근거가 약하고,
  제목이 보장된 seed 후보가 이제 모델에 도달하므로 합성 제목을 붙일 이유가 없다.
- **seed 계정 불일치**: 카탈로그 seed는 6개인데 `seed_outcomes` 합계는 5다. 한 건이
  enqueue 단계에서 빠진다(정규화·중복·허용 검사 중 하나). 미해결.
- `accepted: 0`은 여전히 분류된 정상 관측값이며 스모크 실패가 아니다(`DECISIONS.md`의
  "Additive yield-diagnostic governance" 참조). 교정은 0건이 *구조적으로 불가피한*
  상태를 없앤 것이지, 매 실행의 최소 건수를 보장하지 않는다.
- Tavily 실 키 E2E는 실행되지 않았다. 위 Status의 대체 항목 참조.

## Blockers

- 없음. Tavily 계정 발급 대기는 `public` 프로바이더 채택으로 해소됐다.

## Next action

7/31 전에 Upstage 채널에 저장소 링크(`https://github.com/nukk-pain/event-intelligence-api`,
기본 브랜치 `main`)와 200자+ 후기를 제출한다. 후기 본문은 `README.md`의
"Solar Agent Partner 후기" 절에 있다.

이후(Stage 2 후보): 위 Known limitations의 prefilter 유실 경계를 안전하게 구분하는
진단을 추가하고, 그 근거가 나오면 후보 렌더링 또는 카탈로그를 교정한다.

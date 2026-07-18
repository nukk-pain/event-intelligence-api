# PROGRESS — Solar-backed autonomous event-intelligence agent

## Current focus

**여행 제약**: 사용자 7/17부터 10일 여행(~7/27 복귀) → Stage 1(7/17~7/31)을 거의
덮음. 제출물은 7/31 마감(매일 활동 불필요). 그래서 **여행 전에 핵심 에이전트를
로컬(qwen36-dwq)로 완성·검증**해 두고, 복귀 후 4일은 "Solar 붙이기→측정→튜닝→후기"만.
복귀 후 절차는 `RUNBOOK.md` 참조.

핵심 에이전트(루프 ② 추출+멀티홉 보강)는 여행 전 구현 완료. Solar는 7/17에만
붙일 수 있으므로 Solar-agnostic(OpenAI 호환)으로 구현 — 키만 넣으면 전환됨.

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
- [ ] 신규 사용자 계정으로 Tavily 키를 발급해 Solar+Tavily 2-round 실제 E2E를
      실행하고, 키와 개인정보가 없는 redacted transcript를 증거로 남긴다.
- [ ] (~7/31) 후기 수치 채우기 → 제출.

## Evidence

- events.nukk.net 운영 중(COEX/KINTEX/benchmark 인제스트 + read-only API).

## Blockers

- 자동 실검색 구현에는 차단점이 없다. 실제 E2E는 GitHub 신규 계정 생성 과정의
  사용자 이메일·비밀번호·CAPTCHA/인증 입력과 Tavily API 키 발급을 기다린다.

## Next action

사용자 인증 후 Tavily 키를 로컬 환경에만 저장하고 Solar+Tavily 실제 E2E를 실행한다.
그 뒤 전체 검증과 개인정보 스캔을 다시 수행해 `feat/solar-agent`를 마무리한다.

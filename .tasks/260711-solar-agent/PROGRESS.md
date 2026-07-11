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
- [ ] (7/17~) Solar 키 발급 → A/B 측정 → 프롬프트 튜닝 → 실검색 툴 연결(루프 ①).
- [ ] (~7/31) 후기 수치 채우기 → 제출.
- [ ] 프로덕션 연결(선택): eventmcp 데이터소스를 fixture → store/read API로 교체.

## Evidence

- events.nukk.net 운영 중(COEX/KINTEX/benchmark 인제스트 + read-only API).

## Blockers

- Solar Open 2 Early Access 미개시(7/17 시작). 그전까지 로컬 Qwen3.6로 A/B 하네스
  선구현 가능.
- 선정 여부 미확정.

## Next action

신청서 제출(7/15). 선정되면 PLAN의 Stage 1 체크리스트 순서대로 착수.

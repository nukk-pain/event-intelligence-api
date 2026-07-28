# PROGRESS: solar-agency

## 상태: 구현 중 (Phase 1 — 액션 루프)

## 완료
- 방향 결정: AskUserQuestion (2026-07-28) — "둘 다 (루프 먼저)": 도구 선택형
  액션 루프 우선, 시간 남으면 발견→PR 자동 연결
- 코드베이스 조사: 루프 본체(discover.go:75), JSON-in-text 프로토콜, opener가
  재사용할 bounded fetch·SSRF 가드(publicdiscovery) 확인
- PLAN.md 작성

## 완료 (체이닝)
- 구조적 검증 APPROVED (plan-document-reviewer)
- 기술 리뷰: 높음 0건, 중간 4건 반영 (배포 표면 정정, malformed 재프롬프트,
  프롬프트 상한, -rounds flag 호환), 낮음 2건 로그
- preflight: PREFLIGHT.md 생성 — 공개 /v1/discover 부재 발견, AC-7 정정

## 자동 결정 기록 (/start autonomous)
- HARD GATE 자동 통과 (autonomous 모드, 방향 결정은 사전 AskUserQuestion 완료)
- 마감 위험 "높음"은 사용자가 옵션 선택 시 리스크 고지문을 보고 선택했으므로
  재질문 없이 진행. 단 Task 3.3(프로덕션 배포)과 Task 4.1(VPS 시크릿)은
  착수 직전 AskUserQuestion 예정

## 구현 로그
- [x] Task 1.1 완료 — parseActionResponse (15 테스트). Quality 리뷰 반영:
  Query/Reason에 boundedRedactedText 적용
- [x] Task 1.2 완료 — actionModelRequest (7+1 테스트). Quality 리뷰 HIGH 반영:
  후보 title 개행/꺾쇠 주입으로 untrusted 블록 탈출 가능 → promptSafeLine
  새니타이저 + 회귀 테스트 추가
- 컨트롤러 결정: Task 1.3과 1.4는 같은 파일의 연쇄 수정이라 중간 상태가
  깨진 채 남는 것을 피하기 위해 단일 dispatch로 통합 실행

- [x] Task 1.3+1.4 완료 (단일 dispatch) — run() 액션 루프 재작성, MaxSearches/
  MaxTurns(8)/MaxOpens 도입, `malformed_action` terminal 신규, 안무 코드 grep
  0건, 109개 테스트 GREEN. 컨트롤러 후속: 후보 snippet/date/location 렌더링과
  profile 힌트(QueryTemplates/Locale/Language)를 프롬프트에 복원 (구현자
  Concern #2 해소)
- [x] Task 1.5 [DOUBT] 완료 — 아래 Doubt 결과 참조. Phase 1 게이트:
  `go test ./...` 전체 GREEN + `-race` (agent/publicdiscovery/eventscoutserver)
  GREEN + internal/api 무변경

## Doubt 결과 (Task 1.5, 2026-07-28)
- CLAIM: 액션 루프는 어떤 모델 행동에도 종료·예산·yield_trace 계약·open 금지·
  프롬프트 격리 불변식을 지킨다.
- 종료(1)·예산(2)·open 금지(5): reviewer 반박 실패 — HOLDS.
- [Valid+actionable, 수정 완료] goal·tried 쿼리가 promptSafeLine 없이 렌더링되어
  공개 endpoint의 goal로 untrusted 블록 위조 가능 → 렌더링 새니타이즈 + 회귀
  테스트 (`TestActionModelRequest_GoalAndTriedQueriesCannotForgeDelimiterBlocks`)
- [Valid+actionable, 수정 완료] blank-url accept 항목이 trace에서 증발 →
  `DroppedSelections` 도입, parsed=보낸 전부/dropped에 합산 + 회귀 테스트
- [Valid trade-off, 이월] eventscoutserver가 비nil 오류 시 채택분 포함 result를
  폐기 (예: accept 후 search 오류). **재작성 전 루프와 동일한 기존 semantics**이고
  익명 서버 오류 envelope은 governed 계약이라 이번 범위에서 변경하지 않음.
  Task 2.4 DECISIONS 기록에 알려진 동작으로 명시.
- Noise: 0건

- [x] Task 2.1 완료 — open 액션 + pending 구제 (메타데이터 부족 탈락분을 모델이
  열어 백필·승격). 2-Stage 리뷰: spec 리뷰의 3건은 Task 1.3 rename 여파 오인
  (Noise), quality APPROVED
- [x] Task 2.2+2.3 완료 (단일 dispatch) — publicdiscovery AgentPageOpener
  (같은 crawl fetcher 재사용: SSRF·robots·512KiB·15s), CLI `-opens`(기본 3),
  서버 MaxOpens=2 고정, 모델 호출 상한 4→8. 컨트롤러 후속: 토큰 예약이 실효
  턴을 4로 묶던 문제 → MaxTokensPerCall 1000→500, CLI `-max-tokens` 기본
  3000→500 (비용 상한 불변, 8턴 실현). 서버 테스트 기대값 5콜로 갱신
- [x] Task 2.4 완료 — DECISIONS.md 안무 이양 결정 기록, cmd README 2종 동기화

## 다음 단계
- Phase 3: Task 3.1 (로컬 전체 게이트) → 3.2 (라이브 스모크) → 3.3 (배포,
  AskUserQuestion 후) → 3.4 (README 후기)

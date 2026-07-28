# PLAN: 마감 커버리지 개선 (event-strategy-agent Stage 1 지원)

> 작성: 2026-07-29. 발주: event-strategy-agent 세션 — "Stage 2로 미루지 말고 Stage 1에서 만들어".
> 전제 실측: 다가오는 행사 100건 중 마감 보유 1건. 마감 없는 35건의 공식 페이지 표본 조사
> 결과 키워드 근처 날짜는 6건뿐이고 대부분 공지·행사일(가짜 신호), 진짜 마감은 1~2건
> (대표: kofurn register_url 페이지의 "사전등록 가능 기간 ~2026.08.26" — 파이프라인이
> homepage만 보고 register_url 페이지는 안 봐서 놓침).

## 원칙

- "틀린 마감 > 없는 마감" 사고(migration 0010) 재발 금지: 결정론 추출은 "마감"류
  라벨을 강제, 범위형("~날짜")은 등록·접수 문맥의 페이지(register/exhibit URL)에서만.
- 크롤링 경계: 새 fetch는 이미 알고 있는 register/exhibit URL 단일 페이지의 bounded
  fetch뿐 (재귀·신규 발견 없음). 기존 official fetcher(robots·5MB 상한) 재사용.

## Tasks

#### Task 1: 결정론 추출기 확장 `[TDD]`
- test: `internal/enrich/actions_test.go` (확장)
- impl: `internal/enrich/actions.go`
- [x] dateRe에 년월일형("2026년 9월 1일")과 `/` 구분자 추가
- [x] 마감 라벨 변형 추가 (사전등록/신청/접수 마감, 붙여쓰기 변형)
- [x] ~~deadlineNear가 라벨 앞 40자도 탐색~~ → 구현 중 폐기: goquery가 텍스트 노드를
      공백 없이 이어붙여 직전 항목의 날짜가 라벨에 거리 0으로 붙음("…07.31부스 신청
      마감…"). 라벨 뒤 탐색만 유지하고, 윈도우를 다음 마감 라벨에서 절단해 인접 항목
      누수도 차단 (테스트로 못 박음)

#### Task 2: register/exhibit 페이지 결정론 2차 패스 `[TDD]`
- test: `internal/enrich/actions_test.go`, `internal/pipeline/source_enrich_test.go`(있으면)
- impl: `internal/enrich/actions.go`, `internal/pipeline/source_enrich.go`
- [x] `DeadlineOnActionPage(body)`: 마감 라벨 우선, 없으면 등록·접수·신청 "기간" 라벨 뒤
      범위 끝 날짜(~/까지) — 등록 문맥 페이지 한정이라 행사기간 오탐 위험 통제
- [x] pipeline: 해당 deadline nil && URL 존재 && 행사 시작일이 오늘 이후일 때만 1회
      bounded fetch → nil-only 채움 + provenance(ExtraSources) 기록

#### Task 3: 배포 + 실측 `[Manual]`
- [x] `go test ./...` → linux 크로스컴파일 → runbook대로 백업·교체·재시작
- [x] `systemctl start eventsintel-ingest.service` 수동 1회 → 카운터·커버리지 전후 비교
- [x] `deploy/verify.sh` ALL CHECKS PASSED → event-strategy-agent 라이브 보고서로 최종 확인

## 결과 (2026-07-29)

- run 1 (배포 후): 마감 변경 22건 = 채움 14 + **삭제 8** → churn 근본 원인 발견:
  upsert가 enrichment 미도달 run의 nil로 저장값을 덮어씀 (7/27부터 매 run 상쇄).
- carry-forward 래칫(store.ApplyBatch) 추가 → run 2: 채움 8, 삭제 0. 커버리지 21→23.
- 다가오는 행사 마감 보유: ~1건 수준(첫 페이지 기준) → **API 전체 319건 중 21→23건**.
- event-strategy-agent 보고서에 KES(참가 7-31/부스 8-14)·AIoT(9-18) 마감 표기 확인 — 종단 증명.
- deploy/verify.sh ALL CHECKS PASSED ×2. 커밋: f650ba7(추출기+action page 패스), e91da47(래칫).

## Next

- `make eval-report` 정확성 감사 — 래칫이 값을 보존하므로 잘못된 값의 유일한 제거
  경로가 감사가 됐다. 의심 사례 1건 기록: 서울카페쇼 registration 2026-02-28
  (홈페이지 정적 텍스트에서 미확인, solar 증거 게이트 통과분).

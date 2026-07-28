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
- [ ] dateRe에 년월일형("2026년 9월 1일")과 `/` 구분자 추가
- [ ] 마감 라벨 변형 추가 (사전등록/신청/접수 마감, 붙여쓰기 변형)
- [ ] deadlineNear가 라벨 앞 40자도 탐색 (날짜가 라벨 앞에 오는 문장)

#### Task 2: register/exhibit 페이지 결정론 2차 패스 `[TDD]`
- test: `internal/enrich/actions_test.go`, `internal/pipeline/source_enrich_test.go`(있으면)
- impl: `internal/enrich/actions.go`, `internal/pipeline/source_enrich.go`
- [ ] `DeadlineOnActionPage(body)`: 마감 라벨 우선, 없으면 등록·접수·신청 "기간" 라벨 뒤
      범위 끝 날짜(~/까지) — 등록 문맥 페이지 한정이라 행사기간 오탐 위험 통제
- [ ] pipeline: 해당 deadline nil && URL 존재 && 행사 시작일이 오늘 이후일 때만 1회
      bounded fetch → nil-only 채움 + provenance(ExtraSources) 기록

#### Task 3: 배포 + 실측 `[Manual]`
- [ ] `go test ./...` → linux 크로스컴파일 → runbook대로 백업·교체·재시작
- [ ] `systemctl start eventsintel-ingest.service` 수동 1회 → 카운터·커버리지 전후 비교
- [ ] `deploy/verify.sh` ALL CHECKS PASSED → event-strategy-agent 라이브 보고서로 최종 확인

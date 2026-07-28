# PLAN: akei-source — AKEI 국내전시회 일정 ingest 소스 (승격 2호)

- worktree: false
- 승인: 자동 승인 (/start autonomous) + 승격 지시 명시("AKEI 승격해줘")
- 시작일: 2026-07-28

## 요구사항 재진술
1. `internal/sources/akei` 어댑터: 월별 일정 목록(`bo_table=schedule` +
   `searchYear/searchMonth/page`)에서 wr_id를 발견, 상세 페이지의 구조화 표
   (한/영 행사명, 주최, ISO 기간, 장소, 전시분야, 홈페이지)를 raw 파싱.
2. 발견 창은 이번 달 + 다음 2개월. 페이지는 신규 wr_id가 안 나오면 조기 중단
   (페이지 상한 6). 전 산업 수록 후 분류는 classifier가 담당 (COEX/KINTEX
   "전부 저장 + excluded 플래그" 정책 준용). MaxDiscoverPerSource=400이 상한.
3. 전화번호·이메일 필드는 추출하지 않음 (데이터 정책: 연락처 미저장).
4. 등록 3종: allowlist `www.akei.or.kr`, SourceConfig, Register. TLS는 표준
   (레거시 옵션 불필요 — curl 정상 협상 확인).
5. 배포는 AskUserQuestion 게이트.

## 위험
| 위험 | 대응 |
|---|---|
| page 파라미터가 실제로는 같은 내용 반복 | 신규 wr_id 0이면 중단 (dedupe 기반) |
| 전국 전시 대량 유입으로 ingest 시간 증가 | 3개월 창 + 400 상한 + 조건부 GET. 서킷브레이커 기존 동작 |
| COEX/KINTEX 행과 중복 | 기존 cross-source dedup이 venue-native 행으로 접음 (showala 선례) |

## 구현 계획
### Task 1: Discover `[TDD]`
- test: `internal/sources/akei/discover_test.go`
- impl: `internal/sources/akei/{source.go,discover.go}`
- [x] RED: list.html fixture로 wr_id 전건 Ref(akei-<wr_id>) 생성, 중복 제거,
      절대 URL 검증 → 실패 확인
- [x] GREEN: 월 반복(WithClock 주입) + 페이지 조기중단 구현

### Task 2: Parse `[TDD]`
- test: `internal/sources/akei/parse_test.go`
- impl: `internal/sources/akei/parse.go`
- [x] RED: detail.html에서 NameKo/NameEn/Organizer/StartRaw/EndRaw/VenueName/
      ClassifyText(전시분야 포함)/HomepageURL 검증 + 전화번호·이메일 미포함 검증
- [x] GREEN: .bbs_schedule_view 표 th/td 파싱

### Task 3: 등록 + 검증 `[TDD]`+`[Manual]`
- test: `internal/fetch/production_hosts_test.go`
- impl: `internal/fetch/production_hosts.go`, `internal/config/config.go`,
  `cmd/eventsintel/main.go`
- [x] RED→GREEN: allowlist·config·Register
- [x] akei 단독 라이브 ingest (스크래치 DB) — 저장 행·오류 0 확인
- [x] go build/vet/test + race 전체 GREEN

### Task 4: 배포 `[Manual]` — ⛔ AskUserQuestion 게이트
- [ ] 승인 → linux 빌드 → VPS 교체 → 수동 ingest → verify.sh + 라이브 확인

## AC
- [ ] AC-1: fixture Discover가 목록의 고유 wr_id 수만큼 Ref 반환
- [ ] AC-2: fixture Parse가 표 필드를 raw로 채우고 연락처는 미포함
- [ ] AC-3: 라이브 단독 ingest에서 akei 행 ≥10, ingest_error 0
- [ ] AC-4: go test ./... 전체 + race GREEN
- [ ] AC-5: 배포 후 verify.sh PASS + 라이브 API에 akei 행사 존재

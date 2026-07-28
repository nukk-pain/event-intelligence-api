# PLAN: aiia-source — AIIA(k-ai.or.kr) 공지 게시판 ingest 소스 추가

- worktree: false
- 승인: 자동 승인 (/start autonomous) + 승격 결정 AskUserQuestion 완료 (AIIA만)
- 시작일: 2026-07-28

## 요구사항 재진술

### 핵심 요구사항
1. `internal/sources/aiia` 어댑터: `k-ai.or.kr/bbs/board.php?tbl=bbs41` 목록에서
   행사성 게시물을 발견(Discover)하고 상세 페이지를 raw 필드로 파싱(Parse).
2. 등록 3종 세트: `fetch.ProductionAllowedHosts`에 `k-ai.or.kr` 추가,
   `config.Default()`에 SourceConfig 행, `main.go`에 `sources.Register(aiia.New())`.
3. 게시판에는 행사·모집·일반 공지가 섞여 있으므로 결정적 키워드 프리필터로
   행사성 게시물만 Ref로 승격 (행사|세미나|컨퍼런스|설명회|교류회|Tech-?Day|
   포럼|박람회|데모데이 등 — 대소문자 무시). 모집/과정 공고는 제외.
4. missing-field honesty: 상세 본문에서 보수적 패턴으로 raw 날짜 텍스트를 찾되
   없으면 nil (일정 미정으로 표시됨). 지어내지 않는다.
5. 실서비스 배포는 별도 게이트 (프로덕션 ingest 동작 변경이므로 AskUserQuestion).

### 범위 (Scope)
**포함**: aiia 어댑터 + 테스트 + testdata fixture, 등록 3종, 문서(README 어댑터
목록·candidates REVIEW-NOTES 상태 갱신).

**제외**: KIRIA(보류 결정), 페이지네이션 전체 크롤(1페이지 최근 게시물만 — 크론
반복으로 커버), 상세 본문 요약 저장(제목·URL·raw 날짜만), Solar 보강 연동 변경.

### 가정사항
- 게시판은 정적 PHP HTML (JS 렌더 아님, 2026-07-28 curl로 확인).
- EventID는 게시물 번호 기반 `aiia-<num>` (durable).
- 서버가 간헐적으로 느림(12초+) — fetcher 기존 타임아웃 정책 사용, 어댑터에서
  별도 재시도 안 함 (파이프라인 per-item recover가 처리).

## 위험 분석

| 위험 | 영향도 | 발생 가능성 | 대응 방안 |
|------|--------|-------------|----------|
| 행사 아닌 게시물이 이벤트로 저장 | 중간 | 중간 | 키워드 프리필터 + 제외 키워드(모집·채용·안내문 성격), fixture 테스트로 경계 고정 |
| 게시판 구조 변경 시 조용한 0건 | 중간 | 낮음 | 기존 소스별 서킷 브레이커(discovery floor)가 감지 — 어댑터 추가 작업 불필요 |
| 느린 서버가 ingest 데드라인 잠식 | 낮음 | 중간 | 1페이지만 크롤(게시물 ~15건), 데드라인·per-host rate limit 기존 정책 |
| 날짜 오추출 | 중간 | 중간 | 보수적 패턴(연월일 명시형만) raw 전달, 해석은 normalize가 담당. 확신 없으면 nil |

## 의존성 분석

### 내부 의존성
| 모듈 | 의존성 | 변경 필요 |
|------|--------|----------|
| internal/sources/aiia (신규) | goquery, fetch, sources | 예 |
| internal/fetch/production_hosts.go | 없음 | 예 (+k-ai.or.kr) |
| internal/config/config.go | 없음 | 예 (SourceConfig 행) |
| cmd/eventsintel/main.go | aiia | 예 (Register 1줄) |

### 외부 의존성
새 패키지 없음 (goquery 기설치).

### 영향 범위
- **직접**: internal/sources/aiia/{source.go,discover.go,parse.go,*_test.go,
  testdata/}, internal/fetch/production_hosts.go, internal/config/config.go,
  cmd/eventsintel/main.go
- **간접**: internal/fetch/production_hosts_test.go(호스트 추가 반영),
  .tasks/260728-scout-promote/candidates-fixture/REVIEW-NOTES.md(상태 갱신)

## 에이전트 구성
- orchestrator 단독 (레이어 얕고 순차 의존)

## 구현 계획

### Phase 0: fixture 확보 (예상: XS)

#### Task 0.1: 실페이지 fixture 다운로드 `[PoC]`
- **작업 내용**:
  - [x] 목록 페이지와 행사성 상세 1건·비행사성 상세 1건을 내려받아
        `internal/sources/aiia/testdata/`에 저장 (list.html, detail-event.html,
        detail-notice.html)
  - [x] 목록에서 게시물 링크·번호가 goquery 셀렉터로 안정적으로 잡히는지 확인
- **검증 방법**: fixture 3파일 존재 + 셀렉터 매칭 수 > 0

### Phase 1: 어댑터 구현 (예상: M)

#### Task 1.1: Discover — 목록 → 행사성 Ref `[TDD]`
- test: `internal/sources/aiia/discover_test.go`
- impl: `internal/sources/aiia/discover.go`, `internal/sources/aiia/source.go`
- **작업 내용**:
  - [x] RED: list.html fixture로 행사성 게시물만 Ref{aiia-<num>, 상세URL}로
        반환하고 비행사성(오픈채팅방 안내 등)은 제외되는 테스트 → 실패 확인
  - [x] GREEN: goquery로 `mode=VIEW` 링크 추출, 제목 키워드 프리필터,
        `WithBaseURL` 옵션(coex 패턴, httptest용)
- **검증 방법**: `go test ./internal/sources/aiia/ -run TestDiscover` GREEN

#### Task 1.2: Parse — 상세 → ParsedEvent `[TDD]`
- test: `internal/sources/aiia/parse_test.go`
- impl: `internal/sources/aiia/parse.go`
- **작업 내용**:
  - [x] RED: detail-event.html에서 Name(제목), URL, SourceID/EventID,
        ClassifyText, RetrievedAt가 채워지고, 본문에 연월일 명시 날짜가 있으면
        StartRaw로, 없으면 nil인 테스트 → 실패 확인
  - [x] GREEN: 구현. Organizer는 제목의 `[...]` 접두가 있으면 raw로 전달,
        본문 날짜는 `2026년 8월 12일`·`2026-08-12`·`2026.08.12` 명시형만
  - [x] 날짜 스캔은 게시물 본문 컨테이너로 한정 — 헤더의 작성일/등록일을
        행사 날짜로 오인하지 않음 (fixture에 작성일이 있는 케이스로 검증)
- **검증 방법**: `go test ./internal/sources/aiia/ -run TestParse` GREEN

#### Task 1.3: 등록 3종 + race 회귀 `[TDD]`
- test: `internal/fetch/production_hosts_test.go` (k-ai.or.kr 포함 기대 추가)
- impl: `internal/fetch/production_hosts.go`, `internal/config/config.go`,
  `cmd/eventsintel/main.go`
- **작업 내용**:
  - [x] RED: production_hosts_test에 `k-ai.or.kr` 기대 추가 → 실패 확인
  - [x] GREEN: allowlist 추가 + SourceConfig{ID:"aiia"} + Register
  - [x] `go test -race ./internal/pipeline/... ./internal/sources/...` 통과
- **검증 방법**: 전체 빌드 + race 테스트 GREEN

### Phase 2: 통합 검증·문서 (예상: S)

#### Task 2.1: 로컬 실수집 검증 `[Manual]`
- **작업 내용**:
  - [x] 임시 DB로 `eventsintel ingest` 실행(라이브 k-ai.or.kr 크롤) 후 aiia
        행이 저장되고 행사성 게시물만 들어갔는지 SQL로 확인
  - [x] `ingest_errors`·서킷브레이커 기록 0건 확인
- **검증 방법**: DB 조회 결과 스크린 로그

#### Task 2.2: 회귀 + 문서 `[Manual]`
- **작업 내용**:
  - [x] `go build`/`go vet`/`go test ./...` 전체 통과
  - [x] CLAUDE.md 어댑터 목록(coex,kintex,benchmark → +aiia 언급) 확인·갱신,
        REVIEW-NOTES.md에 승격 완료 상태 기록
- **검증 방법**: 전체 GREEN + 문서 diff

### Phase 3: 배포 (예상: S) — ⛔ AskUserQuestion 게이트

#### Task 3.1: 배포 승인 + 실행 `[Manual]`
- **작업 내용**:
  - [ ] AskUserQuestion으로 배포 확인 (프로덕션 ingest 변경)
  - [ ] linux 빌드 → VPS 교체 → 다음 ingest 크론 또는 수동 1회 실행 →
        `deploy/verify.sh` ALL CHECKS PASSED + aiia 행사 라이브 확인
- **검증 방법**: verify.sh 통과 + 라이브 API에서 aiia 소스 행사 조회

## 노력도 추정

| Phase | 규모 | 태스크 수 |
|-------|------|----------|
| Phase 0 | XS | 1 |
| Phase 1 | M | 3 |
| Phase 2 | S | 2 |
| Phase 3 | S | 1 |
| **합계** | **M** | **7** |

## 테스트 전략

### 단위 테스트
- [x] Discover: 행사성 필터 포함/제외 경계, EventID 형식, 상대→절대 URL
- [x] Parse: 날짜 있음/없음, organizer 접두, 특수문자 제목

### 통합 테스트
- [x] `go test -race ./internal/pipeline/... ./internal/sources/...`

### E2E 테스트
- [x] 로컬 ingest 라이브 1회 (Task 2.1)

### 수동 테스트
- [x] 저장 행의 excluded/classify 결과 확인 (ai 카테고리 매칭 기대)

## Acceptance Criteria (인수 기준)

- [ ] AC-1: Given list.html fixture, When Discover, Then 행사성 게시물만 Ref로
      반환되고 비행사성 게시물은 0건 포함된다
- [ ] AC-2: Given detail-event.html, When Parse, Then Name·EventID·URL·
      ClassifyText가 채워지고 본문에 명시 날짜가 없으면 StartRaw는 nil이다
- [ ] AC-3: Given 로컬 ingest 라이브 1회, When 완료, Then aiia 행 ≥1 저장,
      ingest_errors의 aiia 항목 0건이다
- [ ] AC-4: Given 이번 변경 전체, When `go test ./...`와 `-race` 부분 실행,
      Then 전부 통과한다
- [ ] AC-5: Given 배포 후, When deploy/verify.sh + 라이브 API 조회, Then
      ALL CHECKS PASSED이고 aiia 소스 행사가 응답에 나타난다

## Verification Strategy (검증 전략)

| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1 | 단위 테스트 | 포함 N>0, 제외 0 |
| AC-2 | 단위 테스트 | 필드 값·nil 검증 |
| AC-3 | 로컬 ingest + SQL | aiia 행 ≥1, 오류 0 |
| AC-4 | go test 전체 + race | exit 0 |
| AC-5 | verify.sh + curl | ALL CHECKS PASSED + aiia 행사 존재 |

## 병렬 실행 그룹
- 그룹 A: Task 0.1
- 그룹 B (A 후): Task 1.1 → 1.2 → 1.3 (순차)
- 그룹 C (B 후): Task 2.1 → 2.2
- 그룹 D (C 후, 사용자 게이트): Task 3.1


---

## 리뷰 로그

> 리뷰 일시: 2026-07-28

### 총평
showala 선례가 있는 게시판형 어댑터라 구조 위험은 낮고, 유일한 실질 위험은
행사성 판별과 날짜 오추출 — 둘 다 fixture 경계 테스트로 잠근다.

### 반영 완료
- [x] [중간][정확성] 작성일을 StartRaw로 오인 방지 — 본문 한정 스캔 → Task 1.2

### 미반영 (참고용)
- [낮음] 게시판 2페이지 이상 크롤 — 크론 반복이 커버, YAGNI
- [낮음] robots 404를 명시 기록 — fetcher 기존 정책, 어댑터 밖

## PREFLIGHT (요약)
- AC 검증: AC-1~5 측정 가능. AC-3은 라이브 게시판 상태 의존이나 현재 행사성
  게시물 존재 확인됨(Tech-Day). ✅
- 영향: 직접 4개 영역(신규 어댑터, allowlist, config, main), cross-service 0.
  read API·MCP·Solar 보강 무변경. 프로덕션 반영은 Phase 3 게이트.
- 데이터 감사: 스키마 변경 없음. 신규 source_id "aiia" 행 추가는 ApplyBatch
  기존 경로.
- 비즈니스 규칙: missing honesty(날짜 nil 허용), allowlist 코드 경유(어제 결정
  준수 — 이 작업 자체가 그 절차의 첫 실사용), 결정적 파싱(LLM 무관). ✅
- 판정: ✅ 준비 완료

## 실행 중 발견 작업 (2026-07-28)

### Task 1.4: 레거시 TLS 호스트 옵션 `[TDD]` (신규 — Phase 1에 추가)
- test: `internal/fetch/legacy_tls_test.go`
- impl: `internal/fetch/options.go`, `internal/fetch/transport.go`,
  `cmd/eventsintel/main.go`
- 배경: k-ai.or.kr 서버가 ECDHE 없이 RSA 키교환 스위트만 지원해 Go 기본
  TLS 설정으로는 handshake 실패 (Go 1.22+는 RSA-KEX를 기본에서 제외).
  평문 http 다운그레이드 대신, 명시된 호스트에만 RSA-KEX를 허용하는
  `WithLegacyTLSHosts` 옵션을 추가한다 (PFS 없는 암호화 > 평문).
- **작업 내용**:
  - [x] tlsConfigForHost 단위 테스트: 레거시 호스트만 RSA-KEX 포함, 그 외는
        기본 스위트
  - [x] DialTLSContext로 호스트별 적용, SSRF guardedDial 유지
  - [x] ingest fetcher에만 `WithLegacyTLSHosts("k-ai.or.kr")` 배선
  - [x] 라이브 검증은 Task 2.1 재실행이 담당

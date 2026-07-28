# PLAN: scout-promote — eventscout 채택 소스의 코드 경유 승격 경로

- worktree: false
- 승인: 자동 승인 (/start autonomous) + 아키텍처 AskUserQuestion 확인 완료
- 시작일: 2026-07-28

## 요구사항 재진술

### 핵심 요구사항
1. `eventscout -promote <dir>` 지정 시 채택 소스로부터 검토용 산출물 3개를 생성:
   `seed-candidates.jsonl`(benchmark seed 스키마 + scout 판정 근거),
   `catalog-snippet.go.txt`(붙여넣기용 `catalogEvent` 리터럴),
   `allowlist-hosts.txt`(기존 프로덕션 allowlist에 없는 신규 호스트만).
2. 승인은 사람이 스니펫을 코드에 반영·커밋하는 기존 git 리뷰 플로우. 런타임
   자동 등록은 하지 않는다 (AskUserQuestion에서 확정: allowlist 감사 표면 유지).
3. 값이 없는 필드는 비워두고 지어내지 않는다 (missing-field honesty).
4. `-promote` 미지정 시 기존 동작(출력 포함) 완전 불변.

### 범위 (Scope)
**포함**: cmd/eventscout promote 출력 모듈 + 플래그, 프로덕션 allowlist의 공유
변수 추출(단일 SSOT), cmd/eventscout/README.md 사용법, DECISIONS.md 결정 기록.

**제외**: 런타임 seed 로딩, allowlist env 확장, ingest·read API·MCP 변경,
카탈로그에 실제 소스 추가(그건 이 경로의 *사용*이지 구현이 아님), 배포.

### 가정사항
- `agent.DiscoveredSource`는 URL/Title/Reason/Date/Location/Provenance만 제공 —
  catalogEvent의 Country/Timezone/Organizer 등은 사람이 검토 단계에서 채운다.
- catalogEvent.StartRaw는 raw 문자열 필드이므로 scout의 Date 원문을 그대로 담아도
  normalize 단계 계약과 충돌하지 않는다.
- API-First 예외: 공개 API read-only 하드 제약이 우선. 승격은 운영자 전용
  코드 리뷰 워크플로우라 공개 endpoint 대상이 아니다.

## 위험 분석

| 위험 | 영향도 | 발생 가능성 | 대응 방안 |
|------|--------|-------------|----------|
| 생성된 Go 스니펫이 문법 오류로 빌드 실패 | 중간 | 중간 | 생성 시 `go/format.Source`로 유효성 강제 + 라운드트립 빌드 검증(Task 2.1) |
| allowlist SSOT 추출 리팩토링이 기존 ingest 동작 변경 | 중간 | 낮음 | 목록 내용 diff 0 검증 테스트 + 전체 회귀 |
| 스니펫의 문자열 이스케이프 누락(따옴표·백슬래시 포함 제목) | 중간 | 중간 | `%q` 포맷 사용 + 특수문자 fixture 테스트 |
| 승격 후보에 이미 카탈로그에 있는 소스 중복 | 낮음 | 중간 | EventID 슬러그·호스트 기준 기존 카탈로그 대조, 중복은 후보 파일에 표시 |

## 의존성 분석

### 내부 의존성
| 모듈 | 의존성 | 변경 필요 |
|------|--------|----------|
| cmd/eventscout (main.go + promote.go 신규) | internal/agent, internal/fetch | 예 |
| internal/fetch (production_hosts.go 신규) | 없음 | 예 (allowlist 변수 추출) |
| cmd/eventsintel/main.go | internal/fetch | 예 (변수 참조로 교체) |
| internal/sources/benchmark | 참조만 (스니펫 형식 근거) | 아니오 |

### 외부 의존성
새 패키지 없음 (`go/format`은 표준 라이브러리).

### 영향 범위
- **직접**: cmd/eventscout/main.go, cmd/eventscout/promote.go(신규),
  cmd/eventscout/promote_test.go(신규), internal/fetch/production_hosts.go(신규),
  internal/fetch/production_hosts_test.go(신규), cmd/eventsintel/main.go
- **간접**: cmd/eventscout/README.md, DECISIONS.md

## 에이전트 구성
- orchestrator 단독 (단일 작업 흐름, 순차 의존)

## 구현 계획

### Phase 1: 구현 (예상: M)

#### Task 1.1: 프로덕션 allowlist SSOT 추출 `[TDD]`
- test: `internal/fetch/production_hosts_test.go`
- impl: `internal/fetch/production_hosts.go`, `cmd/eventsintel/main.go`
- **작업 내용**:
  - [x] RED: `fetch.ProductionAllowedHosts`에 coex·kintex·ces.tech 등 대표
        호스트가 포함되고 중복이 없음을 검증하는 테스트 작성 → 실패 확인
  - [x] GREEN: main.go의 하드코딩 목록을 `internal/fetch/production_hosts.go`의
        exported 변수로 이동, main.go는 `fetch.WithAllowedHosts(fetch.ProductionAllowedHosts...)`
        로 교체 (목록 내용은 1:1 동일, 감사 가능성 유지 주석 이전)
- **검증 방법**: 새 테스트 GREEN + `go build ./...` + 기존 fetch/pipeline 테스트 통과

#### Task 1.2: promote 산출물 생성기 `[TDD]`
- test: `cmd/eventscout/promote_test.go`
- impl: `cmd/eventscout/promote.go`
- **작업 내용**:
  - [x] RED: fixture `DiscoveredSource` 목록(특수문자 제목 포함)으로
        3파일 생성·내용을 검증하는 테스트 작성 → 실패 확인
  - [x] GREEN: `writePromotionFiles(dir, sources, existingHosts)` 구현 —
        seed-candidates.jsonl(있는 값만, scout reason 포함),
        catalog-snippet.go.txt(`%q` 이스케이프, `go/format.Source` 통과 강제,
        빈 필드는 빈 문자열로 명시), allowlist-hosts.txt(기존 호스트 제외 신규만,
        정렬·중복 제거)
  - [x] 기존 카탈로그 호스트와 겹치는 후보는 jsonl 행에 `already_allowlisted`
        표시 (지우지 않고 사람 판단에 맡김)
  - [x] EventID 슬러그 규칙: 소문자·비영숫자→대시·`benchmark-` 접두. 기존
        카탈로그 EventID 또는 같은 실행 내 슬러그와 충돌하면 host를 덧붙이고
        jsonl에 `slug_collision` 표시
  - [x] 호스트 정규화: 소문자화 + 포트 제거 후 대조. URL 파싱 실패 소스는
        jsonl에 `invalid_url` 표시로 남기되 스니펫·allowlist에서는 제외
  - [x] 채택 소스 0건이면 파일을 만들지 않고 stderr 안내 한 줄
- **검증 방법**: `go test ./cmd/eventscout/ -run TestPromote` GREEN

#### Task 1.3: -promote 플래그 연결 `[TDD]`
- test: `cmd/eventscout/promote_test.go` (플래그 경로는 함수 단위로 검증)
- impl: `cmd/eventscout/main.go`
- **작업 내용**:
  - [x] `-promote <dir>` 플래그 추가, 실행 종료 직전 채택 소스로
        `writePromotionFiles` 호출. 미지정 시 호출 자체가 없음(기존 출력 불변)
  - [x] 디렉토리 생성 실패·쓰기 실패는 명시적 에러로 종료 (조용한 무시 금지)
- **검증 방법**: fixture provider 실행 비교 — `-promote` 유무에 따른 stdout diff 0

### Phase 2: 검증·문서 (예상: S)

#### Task 2.1: 스니펫 라운드트립 검증 `[Manual]`
- **작업 내용**:
  - [x] `-search-provider fixture`로 실행해 3파일 생성 확인
  - [x] 생성된 스니펫을 `internal/sources/benchmark/catalog_promoted.go`에 실제로
        붙여넣어 `go build ./...` 통과 확인 후 파일 삭제(라운드트립)
- **검증 방법**: 빌드 통과 스크린 로그 + 삭제 후 원상 복구 확인 (`git status` clean)

#### Task 2.2: 회귀 + 문서 `[Manual]`
- **작업 내용**:
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` 전체 통과
  - [x] `cmd/eventscout/README.md`에 promote 사용법·검토 절차 추가
  - [x] `DECISIONS.md`에 "코드 경유 승격" 결정(대안·근거 포함) 기록
- **검증 방법**: 전체 테스트 GREEN + 문서 diff 확인

## 노력도 추정

| Phase | 규모 | 태스크 수 |
|-------|------|----------|
| Phase 1 | M | 3 |
| Phase 2 | S | 2 |
| **합계** | **M** | **5** |

## 테스트 전략

### 단위 테스트
- [ ] production_hosts: 대표 호스트 포함·중복 없음
- [ ] promote: 3파일 내용, `%q` 이스케이프, `go/format.Source` 유효성,
      신규 호스트 필터, 0건 처리

### 통합 테스트
- [ ] `go test ./...` 전체 회귀 (allowlist 리팩토링 영향 확인)

### E2E 테스트
- [ ] fixture provider 실행 → 3파일 생성 → 스니펫 라운드트립 빌드 (수동)

### 수동 테스트
- [ ] `-promote` 미지정 실행의 stdout이 기존과 동일한지 diff

## Acceptance Criteria (인수 기준)

- [ ] AC-1: Given fixture 검색으로 채택 소스 N>0건, When `-promote out/` 실행,
      Then out/에 3파일이 생성되고 jsonl 행 수 = N이다
- [ ] AC-2: Given 생성된 catalog-snippet.go.txt, When 카탈로그 파일로 붙여넣어
      빌드, Then `go build ./...`가 통과한다
- [ ] AC-3: Given 채택 소스에 기존 allowlist 호스트 포함, When promote 실행,
      Then allowlist-hosts.txt에는 신규 호스트만 있다
- [ ] AC-4: Given 같은 빌드에서 동일 fixture 실행, When `-promote` 유무만 바꿈,
      Then stdout diff 0 이고 미지정 경로에는 파일 쓰기 호출이 없다
- [ ] AC-5: Given 이번 변경 전체, When `go test ./...`, Then 전부 통과하고
      프로덕션 allowlist 목록 내용은 변경 전과 1:1 동일하다

## Verification Strategy (검증 전략)

| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1 | 단위 테스트 + fixture 실행 | 3파일 존재, 행 수 일치 |
| AC-2 | 수동 라운드트립 (Task 2.1) | go build exit 0 |
| AC-3 | 단위 테스트 | 기존 호스트 0건 포함 |
| AC-4 | 수동 diff (Task 2.2) | stdout diff 0 |
| AC-5 | go test + 목록 diff 테스트 | 전체 GREEN, 목록 동일 |

## 병렬 실행 그룹
- 그룹 A: Task 1.1
- 그룹 B (A 후): Task 1.2 → 1.3 (promote가 fetch.ProductionAllowedHosts 참조)
- 그룹 C (B 후): Task 2.1 → 2.2

---

## 리뷰 로그

> 리뷰 일시: 2026-07-28

### 총평
운영자 전용 로컬 CLI 산출물이라 위험 표면이 작고, 유일한 공유 상태 변경
(allowlist SSOT 추출)은 목록 내용 1:1 검증으로 잠근다.

### 반영 완료
- [x] [중간][정합] EventID 슬러그 충돌 규칙과 표시 → Task 1.2
- [x] [중간][에러] URL 파싱 실패 소스 처리 + 호스트 정규화(소문자·포트) → Task 1.2
- [x] [중간][AC] AC-4를 "변경 전 바이너리 대조"에서 같은 빌드 내 플래그 유무
      diff로 재정의 (측정 가능성)

### 미반영 (참고용)
- [낮음] punycode 호스트 정규화 — 현 카탈로그에 IDN 없음, 필요 시 후속
- [낮음] promote 출력에 기존 카탈로그 이벤트 수 요약 — YAGNI

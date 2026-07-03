---
worktree: false
task: edge-cache
created: 2026-06-23
---

# PLAN: edge-cache — 프로덕션 페이지 딜레이 개선

## 요구사항 재진술

### 핵심 요구사항
1. 읽기 API 데이터 엔드포인트(events list/detail/sources/changes/meta/openapi/llms)에
   `Cache-Control` 응답 헤더를 부여한다 — 현재 루트 HTML·정적 에셋에만 있음(api.go:99,118).
2. Cloudflare 엣지가 HTML·API 응답을 실제로 캐싱하도록 만든다(현재 `cf-cache-status: DYNAMIC`).
3. ingest로 데이터가 변경되면 Cloudflare 캐시를 purge해 신선도를 보장한다(env-gated, 옵션).

### 범위 (Scope)
**포함**:
- `internal/api`: 성공 응답에 `Cache-Control` 추가 (단일 const + 최소 call site)
- `internal/config`: Cloudflare purge용 env 설정 필드 추가
- 신규 `internal/cfpurge`(또는 동등): Cloudflare purge API 클라이언트(env-gated, 비치명적)
- `cmd/eventsintel/main.go`: ingest 성공 + 변경 발생 시 purge 호출
- `deploy/`: Cloudflare Cache Rule 설정 스크립트/문서(dry-run 우선)
- `static/openapi.yaml` / `deploy/README.md`: 캐시 정책 문서화

**제외**:
- 서버사이드 초기 목록 인라인(워터폴 2차 왕복 제거)은 이번 범위 밖(후속 후보)
- Cloudflare 유료 기능(Argo, Custom Cache Key 등) 사용 안 함 — Free 플랜 호환만
- rate-limit/quota/동시성 미들웨어 동작 변경 없음

### 가정사항
- ingest는 24h 주기(`deploy/eventsintel-ingest.timer`) → 응답은 매우 캐시 친화적
- 데이터는 read-only·cache-first 불변식 유지 (CLAUDE.md hard constraint)
- ETag 미들웨어는 304 직전 버퍼 헤더를 실제 writer에 복사(middleware.go:128-133)하므로
  핸들러가 세팅한 `Cache-Control`/`Vary`는 200·304 양쪽에 보존됨 (Phase 1에서 테스트로 고정)
- Cloudflare Free 플랜은 `Vary: Accept`를 무시 → 같은 URL에서 JSON/MD가 섞일 수 있음
  → Cache Rule에서 `Accept: text/markdown` 요청은 bypass 처리로 회피

## 위험 분석

| 위험 | 영향도 | 발생 가능성 | 대응 방안 |
|------|--------|-------------|----------|
| 엣지가 stale 데이터를 길게 서빙 | 중간 | 중간 | s-maxage=3600 상한 + ingest 후 purge로 즉시 무효화. purge 미설정 시 worst-case 1h(일 1회 갱신 데이터엔 허용) |
| Accept 협상 충돌(MD 클라가 캐시된 JSON 수신) | 중간 | 낮음 | Cache Rule에 `Accept: text/markdown` bypass 조건. UI는 항상 JSON, MD는 저빈도 |
| Cloudflare Cache Rule 오설정으로 잘못된 캐싱(예: 에러/429 캐시) | 높음 | 낮음 | 200만 Cache-Control 부여(에러는 writeError 경로, 캐시 헤더 없음). Rule는 적용 직전 AskUserQuestion 게이트 + dry-run |
| purge 호출 실패가 ingest를 깨뜨림 | 중간 | 낮음 | purge는 비치명적(로그만), ingest 종료코드에 영향 없음 |
| CF 토큰 권한 부족(현재 토큰은 zone DNS edit) | 중간 | 중간 | Phase 3에서 권한 확인. purge엔 Zone.Cache Purge, Rule엔 Zone.Cache Rules 권한 필요 — 부족 시 사용자에 표면화 |
| 토큰/시크릿이 repo·로그에 노출 | 높음 | 낮음 | env로만 주입, 코드/문서/로그에 토큰 평문 금지. secure-commit 시크릿 스캔 |

## 위험 패턴 자동 감지

### [DOUBT] 부여 (Stage 2 — 키워드 "production"/"public API" + 5조건)
| Task | 매칭 키워드 | 만족 조건 |
|------|-----------|----------|
| Task 1.3 | "public API", "production", cache correctness | 3(타입系 미검증 속성: stale/오타입 미서빙), 5(공개 API·프로덕션 blast radius) |

## 의존성 분석

### 내부 의존성
| 모듈 | 의존성 | 변경 필요 |
|------|--------|----------|
| internal/api/render.go | Respond 성공 경로 | 예 (Cache-Control) |
| internal/api/events.go | writeJSON | 예 (Cache-Control choke point) |
| internal/api/meta.go | handleOpenAPI/LLMsTxt 직접 writer | 예 |
| internal/api/api.go | handleRoot HTML | 예 (const 통일) |
| internal/config/config.go | FromEnv 패턴 | 예 (purge 설정) |
| cmd/eventsintel/main.go | runIngest rep 반환 | 예 (purge 호출) |

### 외부 의존성
| 패키지 | 버전 | 용도 | 새로 추가 |
|--------|------|------|----------|
| net/http (stdlib) | - | purge POST | 아니오 |

신규 서드파티 의존성 없음(표준 라이브러리 net/http로 Cloudflare REST 호출).

### 영향 범위
- **직접**: internal/api/{render.go, events.go, detail.go, meta.go, api.go}, internal/config/config.go, cmd/eventsintel/main.go, 신규 internal/cfpurge/, deploy/
- **간접**: static/openapi.yaml, deploy/README.md (문서)

## 에이전트 구성
- orchestrator만 사용 (단일 작업 흐름, 파일 6~8개, 강한 순서 의존). 병렬 에이전트 불필요.

## 구현 계획

### Phase 1: Cache-Control 헤더 (예상: S)
> 목표: 모든 읽기 성공 응답에 일관된 Cache-Control 부여, 304/협상 동작 보존

#### Task 1.1: 공유 캐시 정책 const + helper `[TDD]`
- test: `internal/api/cache_test.go`
- impl: `internal/api/render.go` (또는 신규 `internal/api/cache.go`)
- **작업 내용**:
  - [ ] `const publicCacheControl = "public, max-age=120, s-maxage=3600, stale-while-revalidate=86400"` 정의
  - [ ] `func setEdgeCache(w http.ResponseWriter)` 헬퍼 (헤더 1줄 세팅)
  - [ ] `writeJSON`에 `setEdgeCache` 호출 추가 → Respond(JSON)·sources·schema·meta 커버 (주석으로 "writeJSON은 200 성공 전용; 비-200 재사용 시 캐시 정책 재검토" 경고 명시)
  - [ ] `Respond` 마크다운 분기에 `setEdgeCache` 추가
  - [ ] `handleOpenAPI`, `handleLLMsTxt`에 `setEdgeCache` 추가
  - [ ] `handleRoot`(HTML) / `staticAsset`의 max-age=300을 공유 정책으로 통일(에셋은 ?v= 버전드라 무방)
- **검증 방법**: `go test ./internal/api/...` — 각 엔드포인트 응답에 `Cache-Control` 존재 단언

#### Task 1.2: 에러 응답에 Cache-Control 미부여 회귀 테스트 `[TDD]`
- test: `internal/api/cache_test.go`
- impl: (코드 변경 없음 — 보장 고정)
- **작업 내용**:
  - [ ] 404/400/429 응답에 `Cache-Control`이 **없음**을 단언(writeError 경로는 캐시 헤더 미부여)
  - [ ] Cloudflare가 에러를 캐싱하지 않도록 origin이 캐시 신호를 주지 않음을 보장
- **검증 방법**: `go test ./internal/api/...`

#### Task 1.3: 캐시 정합성 검증 `[DOUBT][TDD]`
- claim: "Cache-Control 부여 후에도 (a) 304 응답이 Cache-Control/Vary를 보존하고 (b) Accept: text/markdown 클라이언트가 캐시된 JSON을 받는 오염이 origin 레벨에서 발생하지 않으며 (c) stale 데이터가 신선도 정책을 위반하지 않는다"
- artifact: `internal/api/render.go`, `internal/api/middleware.go`, `internal/api/cache_test.go`
- contract: 200·304 모두 Cache-Control 동일; Respond는 `Vary: Accept` 유지; 에러 무캐시; 엣지 협상 충돌은 Cache Rule bypass로 차단(Phase 3)
- **작업 내용**:
  - [ ] `/doubt` 로 위 claim 반증 시도(fresh context)
  - [ ] 304 경로 테스트: If-None-Match 매치 시 304 + Cache-Control 헤더 존재
  - [ ] `Vary: Accept` 보존 테스트
- **검증 방법**: doubt 통과 + 테스트 green

### Phase 2: Cloudflare purge 훅 (예상: S~M)
> 목표: ingest로 데이터 변경 시 엣지 캐시를 즉시 무효화 (env-gated, 비치명적)

#### Task 2.1: purge 설정 필드 추가 `[TDD]`
- test: `internal/config/config_test.go`
- impl: `internal/config/config.go`
- **작업 내용**:
  - [ ] `Config`에 `CFPurgeZoneID string`, `CFPurgeToken string`, (옵션) `CFPurgeURLs []string` 추가
  - [ ] `FromEnv`에 `EVENTSINTEL_CF_PURGE_ZONE`, `EVENTSINTEL_CF_PURGE_TOKEN` 오버레이 (미설정 시 빈 값 = 비활성)
  - [ ] 토큰은 env로만, 기본값/Default()에 평문 금지
- **검증 방법**: env 설정/미설정 시 필드값 단언

#### Task 2.2: Cloudflare purge 클라이언트 `[TDD]`
- test: `internal/cfpurge/cfpurge_test.go` (httptest 서버로 요청 형태 검증)
- impl: `internal/cfpurge/cfpurge.go`
- **작업 내용**:
  - [ ] `func PurgeAll(ctx, zoneID, token string, httpClient) error` — `POST https://api.cloudflare.com/client/v4/zones/{zone}/purge_cache` body `{"purge_everything":true}`, Bearer 토큰 (타깃 URL 목록 대비 단순·안전, 일 1회 빈도라 과purge 비용 무시 가능)
  - [ ] zoneID/token 빈 값이면 no-op(nil) 반환 = 비활성
  - [ ] non-2xx → error 반환(호출측이 로그만 하고 삼킴)
  - [ ] 타임아웃 설정(예: 10s), 토큰을 에러 메시지에 넣지 않음
- **검증 방법**: httptest로 Authorization 헤더·바디·no-op 경로 단언. (`-race` 권장)

#### Task 2.3: runIngest에 purge 배선 `[TDD]`
- test: `cmd/eventsintel/` 또는 purge 결정 로직을 함수로 분리해 `internal/...`에서 테스트
- impl: `cmd/eventsintel/main.go`
- **작업 내용**:
  - [ ] `rep` 반환 후(main.go:172 부근) `changed := sum(sr.Stored) > 0 && !allAborted` 계산
  - [ ] `changed && cfg.CFPurgeToken != ""` 이면 `cfpurge.PurgeAll(...)` 호출, 실패 시 `log.Printf` 경고만(ingest 성공 유지)
  - [ ] 변경 없음/비활성 시 호출 생략 + 로그
- **검증 방법**: 변경 판정 함수 단위 테스트(stored>0 / 0 / 전부 aborted 케이스)

### Phase 3: Cloudflare 엣지 설정 + 배포 검증 (예상: M)
> 목표: 실제 엣지 캐싱 활성화 및 프로덕션 검증. **비가역 단계는 적용 직전 게이트.**

#### Task 3.1: Cache Rule dry-run 스크립트/문서 `[Manual]`
- impl: `deploy/cloudflare-cache-rule.md` (+ 선택 `deploy/apply-cache-rule.sh`)
- **작업 내용**:
  - [ ] Cache Rule 표현식 문서화: `(http.host eq "events.nukk.net" and not http.request.headers["accept"][*] contains "text/markdown" and (http.request.uri.path eq "/" or starts_with(http.request.uri.path, "/api/v1/") or http.request.uri.path eq "/llms.txt"))` → action: Cache Everything + Edge TTL "respect origin"
  - [ ] 토큰 권한 요구사항 명시(Zone.Cache Rules, Zone.Cache Purge)
  - [ ] dry-run: 적용 전 zone id·rule JSON·영향 경로를 출력만
- **검증 방법**: 문서 리뷰 + dry-run 출력 확인 (실제 API 호출 없음)

#### Task 3.2: Cache Rule 프로덕션 적용 `[Manual]` ⚠️ 게이트
- **작업 내용**:
  - [ ] **AskUserQuestion으로 적용 승인** (비가역 프로덕션 변경)
  - [ ] 토큰 권한 확인(부족 시 사용자에 표면화하고 중단)
  - [ ] Cloudflare Rulesets API로 cache rule 생성
- **검증 방법**: `curl -sD- https://events.nukk.net/api/v1/events` 2회 → 2번째 `cf-cache-status: HIT`

#### Task 3.3: 코드 변경 배포 + purge env 설정 `[Manual]` ⚠️ 게이트
- **작업 내용**:
  - [ ] **AskUserQuestion으로 배포 승인**
  - [ ] `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` → scp → systemd 재시작(deploy/README 절차)
  - [ ] ingest systemd unit에 `EVENTSINTEL_CF_PURGE_ZONE`/`_TOKEN` 주입(Environment= 또는 EnvironmentFile)
  - [ ] 데이터 엔드포인트에 `Cache-Control` 노출 확인
  - [ ] 배포 직후(HTML/에셋 버전 변경 포함) `purge_everything` 1회 수동 실행 — 엣지의 구버전 HTML 제거
- **검증 방법**: origin(127.0.0.1:3005 또는 https) 응답 헤더에 Cache-Control, 수동 ingest 후 purge 로그 + 엣지 재캐시 확인

#### Task 3.4: 문서 동기화 `[Manual]`
- impl: `static/openapi.yaml`, `deploy/README.md`, `PROGRESS.md`, `DECISIONS.md`
- **작업 내용**:
  - [ ] openapi.yaml에 캐시 정책 명시(선택), deploy/README에 CF cache rule + purge env 운영 절차
  - [ ] DECISIONS.md에 엣지 캐싱·purge 정책 결정 기록
- **검증 방법**: 문서 일관성 확인, `make test`/`make vet` green

## 노력도 추정
| Phase | 규모 | 태스크 수 |
|-------|------|----------|
| Phase 1 | S | 3 |
| Phase 2 | S~M | 3 |
| Phase 3 | M | 4 |
| **합계** | **M** | **10** |

## 테스트 전략
### 단위 테스트
- [ ] 각 읽기 엔드포인트 응답에 Cache-Control 존재 (cache_test.go)
- [ ] 에러 응답에 Cache-Control 부재
- [ ] 304 경로 Cache-Control/Vary 보존
- [ ] config FromEnv purge 필드 오버레이
- [ ] cfpurge 클라이언트: Authorization/바디/no-op/non-2xx (httptest)
- [ ] ingest 변경 판정 로직(stored>0 / 0 / aborted)
### 통합/수동
- [ ] 프로덕션 `cf-cache-status: HIT` (2회 요청)
- [ ] ingest 후 purge → 엣지 재캐시 + 신선도
- [ ] `make test` / `make vet` / `go test -race ./...` green

## Acceptance Criteria
- [ ] AC-1: Given `/api/v1/events` 요청, When 200 응답, Then `Cache-Control: public, max-age=120, s-maxage=3600, stale-while-revalidate=86400` 헤더가 존재한다
- [ ] AC-2: Given detail/sources/changes/schema/meta/openapi.yaml/llms.txt 각 200 응답, When 조회, Then 동일 Cache-Control 헤더가 존재한다
- [ ] AC-3: Given 404/400/429 에러 응답, When 조회, Then Cache-Control 헤더가 **없다**
- [ ] AC-4: Given If-None-Match가 현재 ETag와 일치, When 조회, Then 304 + Cache-Control + Vary: Accept 헤더가 보존된다
- [ ] AC-5: Given purge env 미설정, When ingest 실행, Then purge 호출 없이 정상 종료(exit 0)한다
- [ ] AC-6: Given purge env 설정 + 변경 발생(stored>0), When ingest 실행, Then Cloudflare purge_cache가 1회 호출되고 실패해도 ingest는 exit 0이다
- [ ] AC-7: Given Cache Rule 적용 후 프로덕션, When `/api/v1/events`를 연속 2회 요청, Then 2번째 응답이 `cf-cache-status: HIT`이다
- [ ] AC-8: Given 코드 변경, When `make test && make vet`, Then 전부 green이다

## Verification Strategy
| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1,2 | 단위 테스트 | 응답 헤더 문자열 일치 |
| AC-3 | 단위 테스트 | 헤더 부재 단언 |
| AC-4 | 단위 테스트 | 304 status + 헤더 존재 |
| AC-5,6 | 단위 테스트 + 수동 ingest | 호출 횟수/종료코드 |
| AC-7 | 수동 curl 2회 | cf-cache-status=HIT |
| AC-8 | CI/로컬 | make test/vet 종료코드 0 |

## 병렬 실행 그룹
- 그룹 A (동시): Task 1.1, Task 2.1 (서로 독립 — api vs config)
- 그룹 B (A 후): Task 1.2, 1.3, 2.2
- 그룹 C (B 후): Task 2.3
- 그룹 D (C 후, 순차·게이트): Task 3.1 → 3.2 → 3.3 → 3.4

## 게이트 요약 (/start autonomous)
- 코드 변경(Phase 1·2)과 모든 테스트는 무정지 자율 진행
- **Task 3.2(Cache Rule 적용), Task 3.3(프로덕션 배포)**: 비가역 프로덕션 변경 → 실행 직전 AskUserQuestion 승인 필수

---

## 리뷰 로그

> 리뷰 일시: 2026-06-23 (구조 검증 APPROVED + 9-렌즈 기술 리뷰)

### 총평
choke point(writeJSON)·purge 비치명·게이트 분리가 적절. read-only·cache-first 불변식을 강화하는 방향. 중간/낮음 3건 반영.

### 반영 완료
- [x] [중간][기술] `writeJSON`에 캐시 헤더를 묶는 것은 "writeJSON = 200 성공 응답만"이라는 현재 불변식에 의존 → Task 1.1에 주석/문서로 그 가정을 명시하고, Task 1.2에서 에러 경로 무캐시를 회귀 테스트로 고정(이미 반영). 미래에 writeJSON을 비-200에 재사용하면 캐시 정책 재검토 필요 — 코드 주석으로 경고.
- [x] [중간][운영] purge 트리거를 ingest뿐 아니라 **배포 시점**에도 적용 → Task 3.3/3.4에 "HTML/에셋 버전 변경 배포 후 purge_everything 1회" 운영 절차 추가.
- [x] [낮음][기술] purge 방식은 `purge_everything` 채택(타깃 URL 목록 대비 단순·안전, 일 1회 빈도라 과purge 비용 없음) → Task 2.2에 명시.

### 미반영 (참고용)
- [낮음] 서버사이드 초기 목록 인라인으로 워터폴 2차 왕복 제거 — 별도 후속 작업(Scope 제외). 엣지 캐싱만으로 체감 딜레이 대부분 해소되므로 우선순위 낮음.
- [낮음] `stale-while-revalidate`는 주로 브라우저에 효과(CF Free의 SWR 지원은 제한적) — 무해하므로 유지.

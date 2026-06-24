---
task: showala-source
created: 2026-06-23
worktree: false
mode: /start autonomous
---

# PLAN: SHOWALA 전시포털 소스 어댑터 + cross-source 콘텐츠키 dedup

## 요구사항 재진술

### 핵심 요구사항
1. SHOWALA 전시포털(showala.com)을 크롤하는 새 source 어댑터(`internal/sources/showala/`)를 추가한다. 목록(`ex_list.php`) → 상세(`ex_detail.php?idx=N`) **2-hop 정적 HTML 파싱**.
2. SHOWALA가 발견하는 행사를 **KINTEX-venue 행사로 스코프**한다 (KINTEX/킨텍스 행사만 Ref/이벤트로 통과).
3. KINTEX `list.do`에는 아직 없지만 SHOWALA엔 있는 외부 주최 임대 전시(예: 2026 로보월드)를 포착한다.
4. 같은 실제 행사가 `kintex` 소스와 `showala` 소스 양쪽에서 발견돼도 **API에는 1개 행만 노출**한다. dedup 키 = (정규화 이름 + 시작일(ISO) + venue_id). **venue 권위 소스(kintex) 우선.**

### 범위 (Scope)
**포함**:
- 새 `showala` 소스 어댑터 (Source 인터페이스 구현 + 테스트 fixture)
- SHOWALA KINTEX-스코프 discovery (PoC로 실제 엔드포인트 확정)
- normalize에서 SHOWALA의 KINTEX 행사 venue_id가 `"kintex"`로 떨어지게 처리
- cross-source dedup: 콘텐츠키 + canonical 결정 + API 노출에서 superseded 제외
- main.go 배선 (register + allowlist + SourceConfig), openapi/llms.txt 동기화(영향 시)

**제외**:
- 일반 series_id 구현 (소스 간 행사 grouping의 일반해 — v1 범위 밖 유지). 이번 dedup은 콘텐츠키 기반 canonical 선택에 국한.
- SHOWALA의 KINTEX 외 venue(COEX 등) 확장 — 이번엔 KINTEX 스코프만. (확장 시 venue 매핑 일반화 필요 — 본문에 명시)
- headless 브라우저 (금지). JS 렌더링 의존 시 작업 중단하고 보고.

### 가정사항
- SHOWALA 목록/상세는 정적 HTML이며 goquery로 파싱 가능 (조사에서 확인, PoC로 재확인).
- SHOWALA에 KINTEX-venue로 사전 필터되는 listing 경로가 있다 (`convention_search.php` 등 — **PoC에서 확정**). 없으면 대안(상세 페이지 venue 확인 후 필터)으로 폴백하되 fetch 비용을 평가.
- 소스 실행 순서는 알파벳순(benchmark < coex < kintex < showala)이라 같은 run 내에서 kintex 행이 showala보다 먼저 적용된다. dedup은 **순서 비의존**으로 설계(과거 run에서 showala가 먼저 들어온 경우도 정합).

## 위험 분석

| 위험 | 영향도 | 발생 가능성 | 대응 방안 |
|------|--------|-------------|----------|
| SHOWALA에 KINTEX-스코프 listing이 없어 전체 목록(수백 건) 상세를 다 fetch해야 함 | 높음 | 중간 | Phase 0 [PoC]에서 KINTEX 필터 엔드포인트 확정. 없으면 fetch 예산·단계적 폴백을 PLAN에 반영하고 escalate |
| 콘텐츠키 우연 충돌(다른 행사가 같은 이름+날짜+venue) → 정상 행을 잘못 superseded | 높음 | 낮음 | 이름 정규화 규칙을 보수적으로(과도 제거 금지). 같은 키라도 source가 같으면 dedup 대상 아님. [DOUBT]로 검증 |
| 기존 단일 소스(coex/kintex/benchmark) 동작 회귀 | 높음 | 낮음 | dedup은 superseded 플래그(soft)로만 동작, 기존 행 삭제 금지. 기존 테스트 전부 green 유지 |
| SHOWALA가 JS로 목록을 렌더 | 중간 | 낮음 | 조사상 정적. PoC에서 raw HTML 재확인, JS 의존 시 중단·보고 |
| API 노출 변경(superseded 제외)이 cache/ETag와 어긋남 | 중간 | 낮음 | superseded는 read 쿼리 WHERE 절에서 제외, content_hash/ETag 계산에 자연 반영 확인 |
| SHOWALA 약관/robots 크롤 제한 | 중간 | 중간 | fetcher의 robots.txt 준수 경로 그대로 사용, 짧은 사실 요약만 저장(저작권 제약 준수) |

### 주요 위험 상세
#### 위험 1: KINTEX-스코프 discovery 부재
- **설명**: 목록 페이지가 venue 필터를 지원하지 않으면 전체 전시 상세를 fetch해 venue를 확인해야 함 → 효율 전략 위반.
- **완화**: Phase 0 [PoC]에서 `convention_search.php`/쿼리파라미터로 KINTEX 사전 필터 가능 여부 실측. 가능하면 그 경로를 Discover URL로 고정.
- **대응**: 불가 시 — (a) 날짜창으로 목록 축소, (b) 상세 venue 확인 후 비-KINTEX skip + per-host rate-limit 준수. fetch 건수가 예산 초과면 escalate.

## 위험 패턴 자동 감지

### [DOUBT] 부여 (Stage 2 — 키워드 + 5조건)
| Task | 매칭 키워드 | 만족 조건 |
|------|-----------|----------|
| Task 3.4 | cross-source dedup / schema change / idempotent / ordering / invariant | 1(branching), 2(pipeline·store·api 경계 횡단), 3(idempotent/ordering/canonical invariant), 5(schema migration·production) |

### ADR 작성 Task 추가
- Task 3.0: ADR 작성 (`/adr "cross-source 콘텐츠키 dedup 및 canonical 선택 전략"`) — event 식별 모델에 콘텐츠키/superseded를 도입하는 스키마·식별 결정의 *왜*를 영구 보존.

## 의존성 분석

### 내부 의존성
| 모듈 | 의존성 | 변경 필요 |
|------|--------|----------|
| `internal/sources/showala` | sources.Source, fetch.Fetcher, goquery | 신규 |
| `internal/sources` | Register/All | 변경 없음 (registry 그대로) |
| `internal/normalize` | venueIDForSource / venue 매핑 | 예 (showala→kintex venue_id) |
| `internal/store` | migrations, diff.ApplyBatch, 읽기 쿼리 | 예 (content_key/superseded 컬럼 + canonical 결정 + read 필터) |
| `internal/pipeline` | source_run, orchestrator | 가능 (dedup 위치가 store vs pipeline — PoC/리뷰에서 확정) |
| `internal/api` | 목록/상세 핸들러 쿼리 | 예 (superseded 제외) |
| `cmd/eventsintel/main.go` | register, allowlist, SourceConfig | 예 |
| `internal/static` | openapi.yaml, llms.txt | 조건부 (소스 목록/필드 노출 변경 시) |

### 외부 의존성
| 패키지 | 버전 | 용도 | 새로 추가 |
|--------|------|------|----------|
| github.com/PuerkitoBio/goquery | 기존 | HTML 파싱 | 아니오 (기존 사용) |

### 영향 범위
- **직접 영향**: `internal/sources/showala/*`(신규), `cmd/eventsintel/main.go`, `internal/normalize/normalize.go`, `internal/store/migrations/*`(신규 SQL), `internal/store/diff.go`(또는 dedup 모듈), `internal/api/*`(read 쿼리)
- **간접 영향**: `internal/static/openapi.yaml`, `internal/static/llms.txt`, 기존 store/api/normalize 테스트

## 구현 계획

### Phase 0: 사전 검증 (예상: S)
> 목표: SHOWALA 실제 HTML 구조와 KINTEX-스코프 discovery 경로를 확정하고 fixture를 확보한다.

#### Task 0.1: SHOWALA discovery 경로 확정 `[PoC]`
- **작업 내용**:
  - [ ] `https://showala.com/ex/ex_list.php`와 venue 검색 후보(`/page/convention_search.php` 등) raw HTML을 fetch해 정적 여부 확인
  - [ ] KINTEX로 사전 필터되는 listing URL/파라미터가 있는지 실측 (있으면 그것을 Discover URL로 채택)
  - [ ] 목록 앵커 패턴(`/ex/ex_detail.php?idx=N`)과 페이지네이션 구조 확인
  - [ ] 로보월드 상세(`ex_detail.php?idx=3219`)에서 이름/시작·종료일/venue/홈페이지/주최 셀렉터 확정
- **검증 방법**: PREFLIGHT.md에 채택 경로 + 셀렉터 맵 기록. 정적 파싱 불가/JS 의존이면 즉시 escalate.

#### Task 0.2: 테스트 fixture 캡처 `[Manual]`
- **작업 내용**:
  - [ ] `internal/sources/showala/testdata/`에 KINTEX-스코프 목록 HTML + 로보월드 상세 HTML 저장 (저작권: 파싱 테스트용 raw fixture, 산출물에 통째 저장 안 함)
- **검증 방법**: 파일 존재 + 골든 테스트에서 로드 성공

### Phase 1: SHOWALA 어댑터 (예상: M)
> 목표: Source 인터페이스를 KINTEX 어댑터 패턴으로 구현한다.

#### Task 1.0: Fetcher에 커스텀 Referer 헤더 지원 추가 `[TDD]`
- test: `internal/fetch/fetch_test.go`
- impl: `internal/fetch/fetch.go`
- **작업 내용**:
  - [ ] `Conditional`에 `Referer string` 필드 추가(또는 별도 `Headers`), `do()`의 헤더 블록에서 비어있지 않을 때만 `req.Header.Set("Referer", …)`
  - [ ] **additive 보장**: 빈 값이면 기존 동작 완전 불변. 모든 소스 공유 경로이므로 기존 fetch 테스트 전부 green 유지(회귀)
- **검증 방법**: Referer 설정 시 요청 헤더에 반영되는 테스트 + 미설정 시 헤더 부재 테스트. `go test ./internal/fetch/...`

#### Task 1.1: Source 골격 + Options `[TDD]`
- test: `internal/sources/showala/source_test.go`
- impl: `internal/sources/showala/source.go`
- **작업 내용**:
  - [ ] `Source` 구조체, `ID() == "showala"`, `Option`/`WithListURL`/`WithClock`, `New(opts...)`
- **검증 방법**: `go test ./internal/sources/showala -run TestNew`

#### Task 1.2: Discover (목록단계 KINTEX 필터 + 조기종료 → Ref) `[TDD]`
- test: `internal/sources/showala/discover_test.go`
- impl: `internal/sources/showala/discover.go`
- **작업 내용**:
  - [ ] 1페이지: `f.Fetch`로 `ex_list.php?place[]=1` fetch, `res.NotModified`/`res.StatusCode!=200` 명시 처리(4xx를 빈 목록으로 위장 금지)
  - [ ] 2페이지+: `ex_proc.php?action=exPagingNew&page=N&qstr=place%5B%5D%3D1`를 **Referer 헤더(Task 1.0)** 와 함께 fetch. fragment 끝 `:::next:::total` 토큰으로 페이징 제어
  - [ ] 목록 행(`li.ex_item`) 파싱: 개최장소(`div.ex_place`)에 `킨텍스`/`KINTEX` 포함 + 전시기간(`div.ex_date`) 시작일이 **미래(>= now)** 인 행만 채택
  - [ ] **조기 종료**: 행 단위로 시작일을 보고 첫 과거 날짜 행을 만나면 페이징 중단(목록은 다가오는 행사 오름차순). junk/온라인 더미 행 제외
  - [ ] `idx` 추출(`a.ex_tit_a` href) → `EventID = "showala-"+idx`, `URL = ex_detail.php?idx=N`. 내부 dedup + first-occurrence 순서
- **검증 방법**: fixture(1페이지+fragment)에서 로보월드 idx 포함 + 비-KINTEX/과거 행 제외를 골든 검증

#### Task 1.3: Parse (상세 → ParsedEvent, raw) `[TDD]`
- test: `internal/sources/showala/parse_test.go`
- impl: `internal/sources/showala/parse.go`
- **작업 내용**:
  - [ ] `fetch.ParseHTML`(goquery). `SourceID`/`EventID`(URL의 idx에서 결정적 도출)/`URL`/`RetrievedAt` 설정
  - [ ] 셀렉터(PREFLIGHT §2): `li.kor_tit`/`li.eng_tit`/`li.date p.des`(전시기간)/`li.where p.des`(개최장소·세부장소 2개, p.tit 라벨로 구분)/`li.homp a[href]`/`li.opener p.des`(주최)/`li.opener2 p.des`(주관)
  - [ ] `Name`, `StartRaw`/`EndRaw`(` ~ ` split, raw 유지), `VenueName`(개최장소 원문)/`Hall`(세부장소)/`City`, `Organizer`/`Host`, `HomepageURL`, `SummaryText`(짧은 사실 요약), `ClassifyText`
  - [ ] 방어적 venue 확인(개최장소에 킨텍스/KINTEX 없으면 스킵) — 스코프 1차 보증은 1.2
- **검증 방법**: 로보월드 상세 fixture → 기대 ParsedEvent 골든 비교(날짜 raw `2026-11-04`/`2026-11-07`, venue=킨텍스(KINTEX), homepage=robotworld.or.kr)

### Phase 2: normalize venue 매핑 (예상: S)
> 목표: SHOWALA KINTEX 행사의 venue_id가 `"kintex"`로 떨어져 dedup 키가 list.do와 일치.

#### Task 2.1: venue_id 결정에 venue-name 매핑 추가 `[TDD]`
- test: `internal/normalize/normalize_test.go`
- impl: `internal/normalize/normalize.go`
- **작업 내용**:
  - [ ] **venue-name 기반 매핑 채택**(PREFLIGHT §7): 개최장소 원문에 `킨텍스`/`KINTEX` 포함 시 `venue.venue_id == "kintex"`. (source 기반 `venueIDForSource["showala"]`는 SHOWALA가 향후 타 venue 담으면 깨지므로 불채택)
  - [ ] City/Hall 정규화가 깨지지 않음 확인, 기존 coex/kintex venue_id 회귀 없음
- **검증 방법**: SHOWALA ParsedEvent → normalize 결과 venue_id=="kintex" 단위 테스트

### Phase 3: cross-source 콘텐츠키 dedup (예상: L)
> 목표: 같은 행사를 1개 canonical 행으로 노출, venue 권위 소스 우선. 순서 비의존·idempotent.

#### Task 3.0: ADR 작성 `[Manual]`
- **작업 내용**:
  - [ ] `/adr "cross-source 콘텐츠키 dedup 및 canonical 선택 전략"` — 콘텐츠키 정의, superseded 모델, 권위 랭크, 대안(series_id/하드삭제) 기각 이유
- **검증 방법**: `docs/decisions/`에 ADR 파일 생성

#### Task 3.1: 콘텐츠키 + superseded 스키마 `[Manual]`
- impl: `internal/store/migrations/0002_dedup.sql` (신규, idempotent `IF NOT EXISTS`)
- **작업 내용**:
  - [ ] `events`에 `content_key TEXT`(정규화 이름+startISO+venue_id), `superseded INTEGER NOT NULL DEFAULT 0` 추가
  - [ ] `content_key` 인덱스 추가
- **검증 방법**: `make migrate` 후 PRAGMA로 컬럼/인덱스 확인, 재실행 idempotent

#### Task 3.2: content_key 계산 + 이름 정규화 `[TDD]`
- test: `internal/store/dedup_test.go` (또는 normalize 측)
- impl: dedup 키 계산 함수
- **작업 내용**:
  - [ ] 이름 정규화 규칙(공백 축약, 양끝 연도/회차 토큰 제거, 대소문자/전각 정리 — **보수적**), 키 = `norm(name)|startISO|venue_id`
  - [ ] start 미파싱(빈 ISO)·venue_id 부재 시 dedup 비대상(키 생성 안 함)
- **검증 방법**: "2026 로보월드"(kintex) vs SHOWALA 표기가 같은 키로, 무관 행사는 다른 키로 떨어지는 단위 테스트

#### Task 3.3: canonical 결정 (권위 랭크 + superseded 갱신) `[TDD]`
- test: `internal/store/dedup_test.go`
- impl: `internal/store/diff.go` 또는 신규 `internal/store/dedup.go`
- **작업 내용**:
  - [ ] 권위 랭크: 자체 venue 소스(coex/kintex) > aggregator(showala). map으로 정의
  - [ ] 이벤트 적용 시 같은 content_key 클러스터를 조회해 최고 권위 1건을 canonical(superseded=0), 나머지 superseded=1로 갱신. 동률이면 안정적 tiebreak(예: event_id 사전순)
  - [ ] 순서 비의존: 어느 소스가 먼저 들어와도 최종 canonical 동일. row 삭제 금지(soft)
- **검증 방법**: kintex→showala 순서/ showala→kintex 순서 모두 canonical=kintex로 수렴하는 테스트

#### Task 3.4: dedup을 ApplyBatch에 통합 `[DOUBT][TDD]`
- claim: "콘텐츠키 dedup은 소스 적용 순서와 무관하게 content_key당 정확히 1개의 canonical 행을 보장하고, 기존 단일 소스 행을 회귀시키지 않으며, 재실행에 idempotent하다."
- artifact: `internal/store/diff.go`, `internal/store/dedup.go`, `internal/store/migrations/0002_dedup.sql`
- contract: (1) content_key당 superseded=0 행이 정확히 1개 (2) 권위 높은 소스가 canonical (3) 단일 소스 행은 항상 canonical 유지 (4) 같은 batch 재적용 시 상태 불변 (5) 기존 change_log/raw_snapshot/ETag 동작 보존
- test: `internal/store/dedup_test.go` (+ `-race`)
- impl: `internal/store/diff.go`
- **작업 내용**:
  - [ ] ApplyBatch의 new/unchanged/changed 분기에 content_key 계산·canonical 재결정 통합
  - [ ] `/doubt` 호출로 위 contract를 fresh-context로 검증
- **검증 방법**: `go test -race ./internal/store/...`, doubt 통과

#### Task 3.5: API read에서 superseded 제외 `[TDD]`
- test: `internal/api/*_test.go`
- impl: `internal/api` 목록/상세 핸들러 쿼리
- **작업 내용**:
  - [ ] 목록/상세 조회에 `WHERE superseded = 0` 적용. 직접 event_id 조회 시 superseded 행 정책(404 vs canonical로 리다이렉트 — 리뷰에서 확정, 기본 404 제외)
  - [ ] ETag/캐시 일관성 확인
- **검증 방법**: 중복 행사 1건만 노출, 로보월드(SHOWALA 단독)는 노출되는 핸들러 테스트

### Phase 4: 배선 (예상: S)
> 목표: 소스 등록 + SSRF allowlist + SourceConfig.

#### Task 4.1: main.go 배선 `[Manual]`
- impl: `cmd/eventsintel/main.go`
- **작업 내용**:
  - [ ] `sources.Register(showala.New())`
  - [ ] allowlist에 `showala.com` 추가 (로보월드 등 외부 홈페이지는 officialFetcher 경로)
  - [ ] `config.SourceConfig` row(있다면) 추가
- **검증 방법**: `make build` 성공, `./bin/eventsintel ingest` 드라이 동작(네트워크 허용 시) 또는 단위/통합 테스트

### Phase 5: 통합·문서·검증 (예상: S)
> 목표: 전체 파이프라인 통합 검증 + 공개 계약 동기화.

#### Task 5.1: 파이프라인 통합 테스트 `[TDD]`
- test: `internal/pipeline/*_test.go`
- **작업 내용**:
  - [ ] kintex+showala가 같은 행사를 발견하는 fixture로 ApplyBatch까지 돌려 canonical 1건 검증
  - [ ] 로보월드(SHOWALA 단독)는 그대로 노출 검증
- **검증 방법**: `go test -race ./internal/pipeline/...`

#### Task 5.2: 공개 계약 동기화 `[Manual]`
- impl: `internal/static/openapi.yaml`, `internal/static/llms.txt`
- **작업 내용**:
  - [ ] 소스 목록에 showala 반영, dedup/노출 정책이 응답에 영향 있으면 문서화(스키마 필드 변경 없으면 최소)
- **검증 방법**: 관련 정적자산 테스트 green

## 노력도 추정
| Phase | 규모 | 태스크 수 |
|-------|------|----------|
| Phase 0 | S | 2 |
| Phase 1 | M | 4 |
| Phase 2 | S | 1 |
| Phase 3 | L | 6 |
| Phase 4 | S | 1 |
| Phase 5 | S | 2 |
| **합계** | **L** | **16** |

## 테스트 전략
### 단위 테스트
- [ ] showala discover/parse 골든(fixture 기반)
- [ ] normalize venue_id=="kintex" for showala KINTEX 행사
- [ ] content_key 계산/이름 정규화 경계 케이스
- [ ] canonical 결정 순서 비의존
### 통합 테스트
- [ ] kintex+showala 동일 행사 → canonical 1건 (`-race`)
- [ ] SHOWALA 단독(로보월드) → 노출 유지
- [ ] API read superseded 제외
### 수동 테스트
- [ ] `make migrate` idempotent
- [ ] `make build` + ingest 드라이(가능 시)

## Acceptance Criteria
- [ ] AC-1: Given SHOWALA 목록/상세 fixture, When `showala.Discover`+`Parse` 실행, Then 로보월드(idx=3219)가 `showala-3219` EventID로 발견되고 start=2026-11-04/end=2026-11-07/venue=KINTEX/homepage=robotworld.or.kr가 raw로 파싱된다.
- [ ] AC-2: Given SHOWALA 목록에 KINTEX 외 venue(수원메쎄 등) 또는 과거 행사가 섞여 있을 때, When Discover, Then 개최장소가 킨텍스/KINTEX이고 시작일이 미래인 행만 Ref로 통과한다(목록 단계 필터·조기 종료).
- [ ] AC-3: Given list.do와 SHOWALA가 같은 행사(같은 content_key)를 발견, When ingest 후 API 목록 조회, Then 해당 행사는 정확히 1건(canonical=kintex)만 반환된다.
- [ ] AC-4: Given list.do에 없고 SHOWALA에만 있는 로보월드, When API 목록 조회, Then 로보월드가 1건 노출된다.
- [ ] AC-5: Given dedup 도입 전 존재하던 단일 소스(coex/kintex) 행, When ingest 재실행, Then 그 행은 superseded되지 않고 그대로 노출되며 기존 테스트가 모두 green이다.
- [ ] AC-6: Given 동일 batch 재적용, When ApplyBatch 두 번 실행, Then content_key당 superseded=0 행 수가 1로 불변(idempotent)이다.
- [ ] AC-7: 정상 read 경로에 live LLM 호출이 없고, 어댑터는 headless 없이 정적 HTML만 사용한다.

## Verification Strategy
| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1 | 단위(golden) | fixture에서 기대 ParsedEvent 정확 일치 |
| AC-2 | 단위 | 비-KINTEX 입력이 0 Ref/skip |
| AC-3 | 통합(`-race`) | 동일 content_key, 목록 응답 count==1, source=kintex |
| AC-4 | 통합 | 로보월드 응답 count==1 |
| AC-5 | 회귀 | 기존 store/api/normalize 테스트 green, superseded=0 유지 |
| AC-6 | 단위/통합 | 2회 적용 후 canonical count 불변 |
| AC-7 | 코드 리뷰 + grep | read 경로 LLM 호출 없음, headless 미사용 |

## 병렬 실행 그룹
- 그룹 A (Phase 0 완료 후 동시): Task 1.0(Fetcher), Task 1.1, Task 3.1(스키마), Task 3.0(ADR)
- 그룹 B (A 후): Task 1.2(1.0·1.1 의존), 1.3, 2.1, 3.2
- 그룹 C (B 후): Task 3.3 → 3.4[DOUBT] (직렬), 3.5
- 그룹 D (C 후): Task 4.1 → 5.1 → 5.2

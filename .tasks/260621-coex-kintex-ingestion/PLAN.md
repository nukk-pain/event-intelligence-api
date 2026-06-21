# Plan: COEX/KINTEX Ingestion Backend + Read-only API

## Metadata

- Status: `done` ✅ — implemented + deployed live @ https://events.nukk.net (2026-06-21)
- worktree: false
- Owner: smpain
- Created: 2026-06-21 · Reviewed: 2026-06-21
- Stack: **Go (1.25+)** + **SQLite via `modernc.org/sqlite`** (pure-Go, no CGo)
- Supersedes: manual-dataset-first approach (DECISIONS.md) → automation-first (ADR Task 0.2)

## 요구사항 재진술

### 핵심 요구사항

1. COEX/KINTEX 행사 정보를 **주기적으로 수집**하고 **신선도(freshness)를 유지**하는 ingestion 백엔드.
2. 수집 데이터를 **v0.1 JSON 스키마**(`prototype/manual-dataset-schema.md`)로 정규화.
3. **변경 추적**(change feed): 신규/변경/취소/연기 감지 + `last_checked_at`/`update_state`.
4. **cache-first read-only API**: JSON 기본 + Markdown 렌더(`llms.txt`), 쿼터 제한.
5. **HTTP-first / CDP-fallback** 원칙: 정적 HTML을 HTTP로 받아 결정적 파싱(브라우저 미사용, feasibility 검증 완료).

### 범위 (Scope)

**포함**: COEX/KINTEX 어댑터, 결정적 파서, 규칙 기반 분류, 정규화, SQLite 정본 + change_log + raw snapshot, 배치 ingest + cron, read-only API(events/detail/changes/sources/schema/meta), 콘텐츠 협상(JSON/Markdown), 캐시 헤더, IP 쿼터, `events.nukk.net` 배포, ADR 2건.

**제외**: 국제 벤치마크 행사(deferred), organizer 2차 hop을 통한 action 필드 자동 수집(v1은 `null`+`missing_fields`), LLM 호출 일체(분류 cheap-model fallback 포함 → 후속 PLAN), UI/결제/계정/write 엔드포인트, API 키 발급(v1은 keyless IP 쿼터만), `update_state='conflicting'`(v1 미사용, enum만 보존).

### 가정사항 (검증·정정 반영 2026-06-21)

- ingest 배치와 read API는 **서로 다른 프로세스**가 하나의 SQLite 파일을 **WAL 모드로 공유**한다(단일 writer + 동시 reader).
- 소스는 **UTF-8**(COEX/KINTEX 2026-06-21 확인). goquery는 UTF-8 입력을 요구 → fetcher가 비-UTF-8 감지 시 변환.
- **COEX는 `Cache-Control: no-store, no-cache` + ETag/Last-Modified 미제공** → conditional-GET 304 fast-path는 **best-effort**이고, 신선도 판정은 `content_hash` 비교에 의존한다.
- **`events.nukk.net` 서브도메인은 아직 없음**(2026-06-21 dig empty). Task 4.3은 "확인"이 아니라 **생성** 단계다. `nukk.net`은 Cloudflare orange-cloud(proxied).
- **KINTEX 발견 엔드포인트(`list.do` vs `clist.do`)는 미검증 추론**이다. Task 2.3은 파서 작성 전에 **discovery 스파이크**로 실제 응답을 확정한다.

## 위험 분석

| 위험 | 영향도 | 가능성 | 대응 |
|------|--------|--------|------|
| 소스 HTML 구조 변경으로 파서 깨짐 | 높음 | 중간 | Golden fixture + **커버리지/필드충전율 circuit breaker**(Task 4.1): 발견 0/급감 시 diff·upsert 없이 중단·알람, 기존 데이터 보존 |
| 부분/빈 fetch를 대량 취소·삭제로 오해 → 정본 손상 | 높음 | 중간 | **disappearance policy**(Task 1.3): DB에 있고 이번 run에 없는 event는 변경 금지(상태/삭제 X), `last_checked_at`만 stale |
| SQLite reader/writer 경합 → 304/5xx | 높음 | 중간 | WAL + `busy_timeout` + per-event 짧은 트랜잭션 + writer 단일 커넥션 + API RO 핸들(Task 0.3) |
| SSRF: 발견 URL을 그대로 fetch | 높음 | 낮음 | egress allowlist(Task 1.1): http(s)만, public-IP만(loopback/169.254.0.0/16/RFC1918 거부), 호스트 allowlist, redirect 후 IP 재검증 |
| 쿼터 우회: Cloudflare 뒤 RemoteAddr/위조 XFF | 중간 | 중간 | trusted-proxy CIDR 검증 후 `CF-Connecting-IP`만 신뢰, IPv6 /64 버킷(Task 3.5) |
| 비산업 행사 오분류 | 중간 | 높음 | 규칙 분류 + `excluded` 플래그(삭제 금지) + 모호 시 low confidence |
| action 필드 venue 페이지 부재 | 중간 | 높음 | v1은 `null` + `missing_fields`로 정직 표기, organizer hop 후속 |
| 배치 재실행 비멱등/change_log 오염 | 높음 | 중간 | content_hash 도메인 정의 + per-event 트랜잭션 + 멱등성 테스트(`[DOUBT]`) |
| cron 중첩 실행 | 중간 | 중간 | flock single-flight(Task 4.1/4.2) |
| 저작권(설명 전문 복제) | 중간 | 중간 | `summary`는 구조화 필드에서 **템플릿 생성**(≤240자), 원문 본문 복사 금지(Task 2.6) |

## 의존성 분석

### 외부 의존성 (Go modules — preflight 검증 2026-06-21)
| 패키지 | 버전 | 용도 | 비고 |
|--------|------|------|------|
| `modernc.org/sqlite` | v1.52.x | SQLite 드라이버(**확정**) | pure-Go, CGo 없음 → 단일 정적 바이너리, VPS 툴체인 0. Go 1.25+ 필요 |
| `github.com/PuerkitoBio/goquery` | v1.12.x | HTML 파싱 | x/net/html 기반, 한국어 malformed HTML 관대. **UTF-8 입력 필요** |
| `github.com/go-chi/chi/v5` | v5.3.x | read API 라우팅 | 외부 의존 0, stdlib net/http |
| `golang.org/x/time/rate` | — | 60/min 버스트 | 2000/day는 별도 day-bucket 필요 |
| stdlib `net/http`, `encoding/xml` | — | fetch, sitemap | 별도 XML 의존 불필요(COEX sitemap 표준) |

### 외부 touch-points (배포 — preflight 검증)
- **DNS**: `events.nukk.net` A 레코드 **생성 필요**(현재 없음). `nukk.net` Cloudflare proxied.
- **VPS 런북 정합**: 앱은 `/srv/developer` 하위 **per-app Docker Compose**, Caddy는 단일 `/etc/caddy/Caddyfile`에 site block append + `caddy validate` + `systemctl reload caddy`. (Task 4.3에서 systemd-binary 대신 런북 방식 채택, 또는 ADR 0.1에서 단일 Go 바이너리 예외 정당화.)
- **TLS 순서**: DNS-only 생성 → 서비스/Caddy 배포 → origin HTTPS 200 확인 → Cloudflare proxy 활성 → Full(Strict) 확인.

## 에이전트 구성

- orchestrator + Phase 2 소스 어댑터(COEX ∥ KINTEX)만 병렬. 그 외 단일 흐름.

## 구현 계획

### Phase 0: 결정·뼈대 (예상: M)

#### Task 0.1: ADR 스택·저장소 `[Manual]`
- [ ] `/adr "Stack: Go + modernc.org/sqlite for COEX/KINTEX ingestion"` — 대안(Python/Node, Postgres/JSONL, mattn-CGo) 비교 + 비용효율·zero-ops 근거 + (systemd-binary vs Docker Compose 배포 방식 결정 포함)
- 검증 방법: `docs/decisions/`에 ADR 존재(대안/결정/결과영향 섹션)

#### Task 0.2: ADR 접근법 교체 + 문서 정합 `[Manual]`
- [ ] `/adr "Supersede manual-dataset-first with automation-first"`
- [ ] `DECISIONS.md`의 `Supersedes: None` → 본 결정 참조로 갱신
- [ ] 루트 `PLAN.md` 상태/포인터를 본 task PLAN으로 지정(AGENTS.md "single execution scope" 준수)
- 검증 방법: ADR 존재 + DECISIONS.md/루트 PLAN 갱신 확인

#### Task 0.3: Go 모듈·DDL·DB 커넥션 구성 `[Manual]`
- impl: `cmd/eventsintel/main.go`, `internal/store/migrations/0001_init.sql`, `internal/store/db.go`, `Makefile`, `config.go`
- [ ] 레이아웃: `internal/{fetch,sources,classify,normalize,store,api,pipeline}`
- [ ] DDL: `events`(+ `schema_version`, `content_hash`, `excluded` bool default false 컬럼), `sources`, `change_log`, `raw_snapshot`. 배열/객체는 JSON 컬럼
- [ ] 인덱스: `events(updated_at)`, `events(venue_id)`, `events(excluded)`, `change_log(changed_at)`, `change_log(event_id)`
- [ ] **DB 오픈 PRAGMA(모든 커넥션 DSN에 적용)**: `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`. writer는 `SetMaxOpenConns(1)`, API는 `?mode=ro` 별도 RO 핸들
- [ ] fixtures 컨벤션: `internal/sources/<venue>/testdata/` + `.meta`(URL/캡처일/해시), `make refresh-fixtures`
- 검증 방법: `go build ./...` 성공, 마이그레이션 시 4테이블+인덱스 생성, RO 핸들로 쓰기 시도 실패 확인

### Phase 1: Fetch + Store 코어 (예상: M)

#### Task 1.1: HTTP fetcher `[TDD]`
- test: `internal/fetch/fetch_test.go` · impl: `internal/fetch/fetch.go`
- [ ] `http.Client{Timeout}`, `golang.org/x/time/rate` rate limit, 재시도(backoff)
- [ ] **SSRF egress 가드**: http(s)만, custom `DialContext`로 목적지 IP가 public인지 검증(loopback/link-local 169.254.0.0/16 incl. 169.254.169.254/RFC1918/ULA/0.0.0.0 거부), **호스트 allowlist**(www.coex.co.kr, www.kintex.com), redirect 캡(≤3) + **redirect마다 IP 재검증**
- [ ] body `io.LimitReader`(예: 5MB) + Content-Length 초과 거부
- [ ] **best-effort conditional GET**: 서버가 validator 제공 시에만 If-None-Match/If-Modified-Since, 아니면 fetch + content_hash 비교(COEX no-cache 대응)
- [ ] **robots**: 각 호스트 robots.txt fetch+cache(TTL 24h), UA 기준 allow-check + Crawl-delay 반영. (또는 2026-06-21 feasibility 기반 정적 allowlist로 범위 한정하고 그 사실을 명시)
- [ ] UTF-8 아니면 `golang.org/x/text` 변환. CDP-fallback 훅 인터페이스만 정의(미구현)
- 검증 방법: httptest로 200/304/429/timeout/over-size/redirect-to-loopback/disallowed-path 케이스 통과

#### Task 1.2: SQLite store + upsert + content_hash `[TDD]`
- test: `internal/store/store_test.go` · impl: `internal/store/store.go`
- [ ] `UpsertEvent`: event_id 기준 upsert
- [ ] **content_hash 도메인 정의(명문화)**: 소스 파생 semantic 필드만의 canonical 직렬화(키 정렬, 배열 정렬, 날짜/공백 정규화). `last_checked_at`/`retrieved_at`/`created_at`/`updated_at`/`batch_id`는 **제외**
- [ ] **per-event 트랜잭션**: {event upsert, change_log insert, raw_snapshot upsert, last_checked_at 갱신} 하나로 commit, 오류 시 rollback
- 검증 방법: 동일 fixture를 retrieved_at만 바꿔 두 번 → content_hash 동일; 트랜잭션 중간 오류 주입 시 prior good row 보존

#### Task 1.3: 변경 감지 + 멱등 배치 `[DOUBT][TDD]`
- claim: "동일 소스 상태 2회 실행 시 change_log 0행·content_hash 불변·update_state 불변(멱등). 필드 변경 시 정확히 1개 change_log 행(event_id, field_path, old_value, new_value, changed_at, batch_id)."
- artifact: `internal/store/diff.go`, `internal/pipeline/run.go` · contract: 멱등성 + 정확 1행 + disappearance policy
- test: `internal/store/diff_test.go` · impl: `internal/store/diff.go`
- [ ] content_hash 비교 → `update_state` ∈ {new,unchanged,updated}; `last_checked_at` 항상 갱신
- [ ] 필드 단위 diff → `change_log` 행
- [ ] **disappearance policy**: 이번 run에 없는 기존 event는 변경/삭제 금지(`last_checked_at`만 stale)
- [ ] status scheduled→cancelled/postponed 전이: event 보존 + update_state='updated' + change_log 1행
- 검증 방법: `/doubt` 통과 후 RED→GREEN; 2회 무변화 / 변경 1행 / 다중필드 N행 / 상태전이 / disappearance 테스트

### Phase 2: 소스 어댑터 (예상: L, 병렬)

#### Task 2.0: Source 어댑터 계약 `[TDD]`
- test: `internal/sources/registry_test.go` · impl: `internal/sources/source.go`
- [ ] `type Source interface { ID() string; Discover(ctx) ([]Ref, error); Parse(ctx, raw) (*ParsedEvent, error) }` + registry(`map[string]Source` 단일 등록 지점)
- 검증 방법: registry에 coex/kintex 등록, 신규 소스 추가 = 인터페이스 구현 + registry 1줄 + config 1행(파이프라인 무수정) 단위 테스트

#### Task 2.1: COEX discovery `[TDD]`
- test: `internal/sources/coex/discover_test.go` · impl: `internal/sources/coex/discover.go`
- [ ] **동적 샤드 발견**: `wp-sitemap.xml` 인덱스 → `exhibitions` 샤드 `<loc>` 추적(고정 1..5 금지). 다음 샤드 404는 보조 fallback
- [ ] event_id = `coex-<wp-slug>`(durable)
- 검증 방법: N!=5 샤드 인덱스 fixture로 URL 추출 검증

#### Task 2.2: COEX detail parser `[TDD][Golden]`
- test: `internal/sources/coex/parse_test.go` · impl: `internal/sources/coex/parse.go`
- [ ] goquery: 기간/장소/주최/주관/홈페이지/문의; 날짜 `2024.08.24`
- [ ] 날짜 invariant: 파싱불가/end<start → null+`missing_fields`+`date_confidence=low`(+ambiguity_notes), 위조 날짜 금지
- [ ] Golden: 저장 HTML fixture → 기대 struct JSON
- 검증 방법: Golden 비교 + 불가날짜/역순날짜 케이스

#### Task 2.3: KINTEX discovery `[TDD]`  ⚠️ 미검증 — 스파이크 선행
- test: `internal/sources/kintex/discover_test.go` · impl: `internal/sources/kintex/discover.go`
- [ ] **스파이크(선행)**: 실제 `list.do`/`clist.do`(11/12/23/D 필터) curl → seq 목록 파싱 확인 → 응답을 discovery fixture로 커밋. 확정 전 Task 2.4 게이트
- [ ] fallback 경로: 홈페이지/카테고리에서 seq 링크 파싱, 또는 view.do seq 범위 페이징
- [ ] event_id = `kintex-<seq>`(durable 숫자)
- 검증 방법: 커밋된 fixture로 seq 추출 + empty/404/malformed 음성 케이스

#### Task 2.4: KINTEX detail parser `[TDD][Golden]`
- test: `internal/sources/kintex/parse_test.go` · impl: `internal/sources/kintex/parse.go`
- [ ] `view.do?seq=`: 기간/장소/홀/주최/주관/홈페이지; 날짜 `2026-06-18`; 날짜 invariant(2.2와 동일)
- 검증 방법: Golden 비교

#### Task 2.5: 분류(taxonomy) `[TDD]`
- test: `internal/classify/classify_test.go` · impl: `internal/classify/classify.go`, `internal/classify/taxonomy.go`(taxonomy.md에서 생성/embed)
- [ ] **taxonomy SSOT 코드화**: classify·normalize 공통 import. 목록 밖 카테고리는 테스트 실패
- [ ] 키워드 규칙 → categories; 비산업 → `excluded=true`(보관)
- [ ] 모호 제목 → low confidence(cheap-model fallback은 후속 PLAN으로 명시 deferred)
- 검증 방법: 라벨 케이스셋 정/오분류 + out-of-taxonomy 음성 테스트

#### Task 2.6: 정규화 → v0.1 스키마 `[TDD]`
- test: `internal/normalize/normalize_test.go` · impl: `internal/normalize/normalize.go`
- [ ] null 필드 **및 미확인 `actions.*`**(미확인=false로 두되 키 등록)를 `missing_fields[]`에 자동 등록
- [ ] **summary 템플릿 생성**: 추출된 사실 필드만으로(`{name} — {start}~{end} @ {venue}, 주최 {organizer}`) ≤240자, 원문 본문 복사 금지. 불가 시 null+missing_fields(LLM fallback 금지)
- [ ] URL 검증: http(s)만, `javascript:`/`data:`/`file:` 거부
- [ ] 필수 필드 검증(rule 1) + sources[] 각 항목 url/type(enum)/publisher/retrieved_at + end>=start(rule 3) + category∈taxonomy(rule 4) + sources≥1(rule 5)
- [ ] 검증 실패 레코드는 **거부(로그)·미저장**, 기존 good row 보존
- 검증 방법: 누락 필드/미확인 actions가 missing_fields에 반영 + 5개 validation rule 단위 테스트

### Phase 3: Read API (예상: L)

#### Task 3.1: events 목록 `[TDD]`
- test: `internal/api/events_test.go` · impl: `internal/api/events.go`
- [ ] **응답 envelope**: `{"data":[...],"page":{"next_cursor":str|null,"has_more":bool,"limit":int}}`
- [ ] cursor: `(updated_at,event_id)` 기반 opaque base64, `?cursor=`로 전달, insert에 안정
- [ ] 필터: `updated_since`/`changed_since`(RFC3339), `category`(taxonomy 값), `venue`(venue_id)
- [ ] `limit>100` → 100 clamp + 응답에 echo. **기본 목록은 `excluded=true` 제외**
- 검증 방법: >limit 데이터 전수 순회 시 중복/누락 0; 필터·clamp 테스트

#### Task 3.2: detail / sources / changes `[TDD]`
- test: `internal/api/detail_test.go` · impl: `internal/api/detail.go`
- [ ] 라우트 **/api/v1 prefix**: `GET /api/v1/events/{event_id}`, `/api/v1/events/{event_id}/sources`, `/api/v1/events/changes`
- [ ] `/events/changes` 피드: `{"data":[{event_id,field,old,new,update_state,changed_at}],"page":{...}}` 동일 cursor envelope, `since` 필터
- [ ] detail 응답에도 provenance(sources) 포함
- 검증 방법: 각 엔드포인트 응답·404(error envelope) 테스트

#### Task 3.3: schema / OpenAPI / llms.txt / meta `[TDD]`
- test: `internal/api/meta_test.go` · impl: `internal/api/meta.go`, `static/openapi.yaml`, `static/llms.txt`
- [ ] `GET /api/v1/schema`(v0.1 + `supported_versions`), OpenAPI, `/llms.txt`
- [ ] `GET /api/v1`(meta index): version/schema_version/endpoints/quota/vocabularies(taxonomy SSOT)
- [ ] route↔OpenAPI 경로 일치 테스트
- 검증 방법: 스키마 버전·필드 일치 + 라우트/OpenAPI 일치 테스트

#### Task 3.4: 콘텐츠 협상 JSON/Markdown `[TDD]`
- test: `internal/api/render_test.go` · impl: `internal/api/render.go`
- [ ] 기본 `application/json`; `?format=md`가 `Accept`보다 우선; Accept: text/markdown 지원
- [ ] 컬렉션 md는 **단일 헤더 행 테이블**(IDEA Serialization Policy: 키 비반복 token 절약), 페이지네이션은 `Link: <...?cursor=>; rel="next"` 헤더(json/md 공통)
- [ ] **md injection 가드**: free-text의 md 제어문자 escape, URL angle-wrap, raw HTML 금지
- 검증 방법: json/md 동일 필드셋 + 기본 json + Link 헤더 parity + `](http://evil)`·백틱 escape 테스트

#### Task 3.5: 캐시 헤더 + 쿼터 미들웨어 `[TDD]`
- test: `internal/api/middleware_test.go` · impl: `internal/api/middleware.go`
- [ ] `ETag`(write 시 precompute)/`Last-Modified` + If-None-Match 304
- [ ] **error envelope**: `{"error":{"code":"not_found|rate_limited|bad_request|invalid_cursor","message":str,"retry_after_s":int|null}}`
- [ ] 쿼터 **두 카운터**: 60/min(x/time/rate) AND 2000/day(day-bucket). 초과 시 429 + `Retry-After`
- [ ] **IP 도출**: RemoteAddr가 trusted-proxy CIDR(Cloudflare+localhost)일 때만 `CF-Connecting-IP` 신뢰, IPv6 /64 버킷
- [ ] 글로벌 concurrency limiter + 응답 size 캡, `X-RateLimit-Remaining/Reset`
- 검증 방법: 304 분기, 60/min·2000/day 임계 429, 위조 XFF 무시, error envelope 테스트

### Phase 4: 배치 오케스트레이션 + 배포 (예상: L)

#### Task 4.1: `ingest` 서브커맨드 `[Manual]`
- impl: `cmd/eventsintel/ingest.go`
- [ ] fetch→discover→parse→classify→normalize→store→diff 연결, `batch_id` 로깅
- [ ] **per-item recover() 격리**: 파싱/정규화 실패 시 `ingest_error`(event_id,stage,msg,batch_id) 기록 후 skip, 기존 row 보존
- [ ] **circuit breaker**: venue 발견 수 floor(예: 직전 성공의 <50% 또는 0) / 필드충전율·변경비율 임계 초과 시 그 소스 diff·upsert 없이 중단 + 알람
- [ ] **flock single-flight** + 전체 wall-clock deadline + 발견 concurrency bound
- 검증 방법: 로컬 1회 실행 단계별 카운트; 1개 불량 페이지 주입 시 나머지 정상 저장

#### Task 4.2: cron 스케줄 `[Manual]`
- impl: `deploy/crontab`(또는 systemd timer)
- [ ] 6h 주기 `flock -n eventsintel ingest`(중첩 skip)
- 검증 방법: dry-run 트리거 + 중첩 호출 skip 확인

#### Task 4.3: 배포 (DNS 생성 + Caddy + events.nukk.net) `[Manual]`
- impl: `deploy/`(런북 방식: Docker Compose under `/srv/developer` 또는 ADR 정당화된 systemd 바이너리), Caddy site block
- [ ] **events.nukk.net A 레코드 생성**(현재 없음, DNS-only 우선)
- [ ] Caddy: `/etc/caddy/Caddyfile`에 site block append → `caddy validate` → `systemctl reload caddy`
- [ ] TLS 순서: DNS-only → origin HTTPS 200 → Cloudflare proxy 활성 → Full(Strict)
- 검증 방법: `https://events.nukk.net/api/v1/schema` 200, proxy 활성 후 재확인

#### Task 4.4: 실데이터 E2E ingest `[Manual]`
- [ ] 실제 COEX/KINTEX 1회 ingest → venue별 레코드 수 + change feed 확인
- [ ] **read-during-write 스모크**: ingest 쓰기 중 API 200 유지(WAL 검증)
- 검증 방법: venue별 ≥1 정규화 레코드 + sources/changes 동작 + 동시성 200

### Phase 5: 검증 마감 (예상: S)

#### Task 5.1: AC 일괄 검증 `[Manual]`
- [ ] 아래 AC 전 항목 통과 확인 + PROGRESS.md AC 달성률 기록
- 검증 방법: 각 AC의 Verification Strategy 행을 실행해 pass 캡처(테스트 출력/curl 응답 첨부)

## 노력도 추정

| Phase | 규모 | 태스크 |
|-------|------|-------|
| 0 | M | 3 |
| 1 | M | 3 |
| 2 | L | 7 |
| 3 | L | 5 |
| 4 | L | 4 |
| 5 | S | 1 |
| **합계** | **XL** | **23** |

## 테스트 전략

- **단위**: fetch(SSRF/over-size/304/429), store(content_hash 도메인/트랜잭션), diff(멱등/disappearance/상태전이), 파서, 분류(taxonomy SSOT), 정규화(5 rule/actions honesty/summary), API(envelope/cursor/error/negotiation/quota).
- **Golden**: COEX/KINTEX 파서 fixture(+`.meta` provenance, `make refresh-fixtures`).
- **통합/E2E**: 로컬 ingest → SQLite → API; 실소스 1회(Task 4.4) + read-during-write.
- **수동**: 배포(DNS 생성/Caddy/TLS 순서), cron flock, AC 마감.

## Acceptance Criteria (인수 기준)

- [ ] AC-1: Given 1회 ingest, When DB 조회, Then v0.1 validator(스키마 rule 1–5)를 통과하는 레코드가 COEX ≥1, KINTEX ≥1 존재한다(required set: event_id, schema_version, name, country, categories≥1, sources≥1, last_checked_at, update_state, confidence, missing_fields).
- [ ] AC-2: Given 소스 무변경, When 연속 2회 ingest, Then 2회차 새 change_log 0행 AND 각 event `content_hash` byte-identical AND `update_state` 불변(`last_checked_at`은 advance 허용·비교 제외).
- [ ] AC-3: Given 소스 단일 필드 변경, When ingest, Then 정확히 1개 change_log 행(event_id, field_path, old_value, new_value, changed_at, batch_id) 기록 + `update_state='updated'`.
- [ ] AC-4: Given `GET /api/v1/events`, When 호출, Then envelope+cursor로 전수 순회(중복/누락 0), `updated_since`/`changed_since`(ISO8601)·`category`(taxonomy 값)·`venue`(venue_id) 필터 동작, `limit=101`→ ≤100 + next cursor.
- [ ] AC-5: Given `?format=md` 또는 `Accept: text/markdown`, When 호출, Then 동일 query의 md가 JSON과 같은 event_id 집합 + 각 event의 {name,start_date,end_date,venue.venue_id,categories,status} 동일, 기본은 application/json, 다중 페이지 시 Link next 헤더 parity.
- [ ] AC-6: Given classify 테스트셋의 비산업 COEX fixture(유학설명회 등), When 분류·저장, Then `excluded=true`로 보존되고 기본 `GET /api/v1/events`에서 제외된다.
- [ ] AC-7a: Given 임의 read, Then read 경로 LLM import 0 AND 핸들러 통합 테스트에서 LLM 호스트로의 egress 0.
- [ ] AC-7b: Given 임의 read, Then 응답에 `ETag`/`Last-Modified` 포함, If-None-Match → 304.
- [ ] AC-7c: Given keyless 호출, Then 60/min 초과 → 429 AND 2000/day 초과 → 429(둘 다 Retry-After).
- [ ] AC-8: Given event 상세, When `GET /api/v1/events/{id}/sources`, Then ≥1 source(문법상 유효한 https url[라이브 fetch 안 함] + ISO retrieved_at + type∈enum + publisher) 반환.
- [ ] AC-9: Given ingest 쓰기 진행 중, When 동시 API read, Then 200 유지(WAL + busy_timeout).
- [ ] AC-10: Given 발견 결과가 0/급감(circuit breaker 임계), When ingest, Then 기존 event는 삭제/상태변경되지 않고 배치는 실패로 표시·알람(silent 손상 없음).
- [ ] AC-11: Given venue 페이지에 등록정보 없음, When 정규화, Then 각 `actions.*` 키가 `missing_fields`에 존재하고 대응 `*_url`/`*_deadline`은 null+missing_fields.
- [ ] AC-12: Given 비-allowlist/loopback 호스트 URL, When fetcher 호출, Then 거부(SSRF 가드)되며 stored URL은 http(s)만.

## Verification Strategy

| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1 | 통합 ingest + validator | venue별 validator pass ≥1 |
| AC-2 | diff_test + 통합 2회 | change_log Δ=0, hash 동일 |
| AC-3 | diff_test(변경 fixture) | change_log 정확 1행, 컬럼 일치 |
| AC-4 | events_test | 전수순회 무중복/누락, 필터, clamp |
| AC-5 | render_test | 필드셋 동일, 기본 json, Link parity |
| AC-6 | classify_test + events_test | excluded 저장 + 기본목록 제외 |
| AC-7a | go list -deps ./internal/api grep + no-egress 테스트 | LLM import 0, egress 0 |
| AC-7b | middleware_test | ETag/Last-Modified, 304 |
| AC-7c | middleware_test | 60/min·2000/day 429 |
| AC-8 | detail_test | sources 4필드 + url 문법 |
| AC-9 | Task 4.4 스모크 | write 중 read 200 |
| AC-10 | pipeline_test(빈/급감 fixture) | 기존 보존 + 실패표시 |
| AC-11 | normalize_test | actions.* in missing_fields |
| AC-12 | fetch_test | loopback/비allowlist 거부 |

## 병렬 실행 그룹

- A(동시): 0.1, 0.2
- B(0.3 후): 1.1, 1.2 → 1.3
- C(1.x 후): 2.0 → {2.1+2.2 COEX} ∥ {2.3 스파이크→2.4 KINTEX} → 2.5 → 2.6
- D(2.x 후): 3.1~3.5
- E(3.x 후): 4.1 → 4.2/4.3 → 4.4 → 5.1

---

## 리뷰 로그

> 리뷰 일시: 2026-06-21 · 방식: 멀티에이전트 워크플로(9렌즈 + adversarial verify + grounded preflight, 99 agents)

### 총평
아키텍처(Go+SQLite, HTTP-first, 비용효율)는 건전. 결함은 "구현 계약 미정의"에 집중 — 동시성(WAL), content_hash 도메인, 데이터 보존 정책, SSRF, API wire contract, AC 정밀도. 모두 반영.

### 반영 완료 (높음/중간)
- [x] [기술] WAL/busy_timeout/RO핸들/단일writer → Task 0.3 + 가정 + AC-9
- [x] [기술] content_hash 도메인 명문화 → Task 1.2; per-event 트랜잭션 → 1.2/1.3
- [x] [에러] disappearance policy + circuit breaker + per-item 격리 → 1.3/4.1 + AC-10
- [x] [보안] SSRF egress allowlist + URL 검증 + md injection 가드 → 1.1/2.6/3.4 + AC-12
- [x] [보안] 쿼터 trusted-proxy IP + 60/min·2000/day → 3.5 + AC-7c
- [x] [UX] cursor/error/changes envelope, /api/v1 prefix → 3.1/3.2
- [x] [유지보수] Source 인터페이스/registry(Task 2.0), taxonomy SSOT(2.5), 동적 sitemap 샤드(2.1), schema_version 마이그레이션(0.3/3.3)
- [x] [도메인] actions.* missing_fields honesty(2.6/AC-11), summary 템플릿 copyright(2.6), robots 런타임 게이트(1.1)
- [x] [AC] AC-1·5·6·7 정밀화/분할, AC-9~12 신설
- [x] [preflight] events.nukk.net **생성**(없음 확인) + VPS 런북(Docker/Caddy) 정합 + TLS 순서 → 4.3
- [x] [preflight] KINTEX discovery 미검증 → Task 2.3 스파이크 선행 + Risks
- [x] [preflight] COEX no-cache → conditional GET best-effort(1.1)
- [x] [preflight] 드라이버 modernc.org/sqlite 확정, UTF-8 가정 명시
- [x] [구조] Task 5.1 검증 방법 라인 추가

### 미반영 (참고용, 낮음)
- change_log retention/compaction(인덱스만 반영, 보존정책은 운영 중 결정)
- fixtures `.meta` provenance(컨벤션만 0.3 반영)
- 분류 cheap-model fallback(후속 PLAN으로 명시 deferred)

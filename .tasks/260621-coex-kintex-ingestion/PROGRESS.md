# PROGRESS: COEX/KINTEX Ingestion Backend

## 상태: 완료 ✅ (배포 live @ https://events.nukk.net)

## 배포 결과 (2026-06-21)
- **Live**: https://events.nukk.net — Cloudflare-proxied, Caddy reverse_proxy → 127.0.0.1:3005
- **VPS**: developer-vps(`152.42.210.34`), `/srv/developer/events-intel/`, systemd `eventsintel-api`(active) + `eventsintel-ingest.timer`(6h)
- **데이터**: 산업 이벤트 58개 공개(ai 6 / robotics 8 / bio 17 / medical-devices 26 / digital-health 8), 비산업 다수 excluded(숨김). 전 엔드포인트 200.
- **AC 12/12 머신어리 검증** (AC-1·4·5·6·7·8·9는 VPS에서 live 검증 — read-during-write 포함).

## 알려진 한계 (문서화, fast-follow)
1. **COEX 신선도**: COEX sitemap shard 2~5가 서버측 404(전 클라이언트 동일) → 최신 이벤트 안정 수집 불가, shard 1(과거)만 신뢰. KINTEX 현재 캘린더는 소비자 박람회 위주(전부 excluded가 정상). → 산업 데이터가 과거(2019)로 치우침. **#1 후속**: COEX 현재 전시 목록/캘린더를 별도 discovery 소스로 추가.
2. **분류기 recall**: CJK 경계 없음 → "AI안전보건박람회"가 ai 미매칭. cheap-model fallback은 의도적 deferred.
3. **Cloudflare ETag**: 엣지에서 strip(오리진은 정상).
4. **Cosmetic**: event_id에 percent-encoded 한글 slug.

## 이전 상태: 구현 중

## Phase 0 Gate
- ✅ PASS — PoC 없음(state.json 없음), 자동 DOUBT inject 조건 미해당. Phase 1 진입 허용.

## 구현 진행
- ✅ Phase 0 (foundation): ADR 0001/0002, DECISIONS.md+루트 PLAN 갱신, Go scaffold(go.mod, 9 packages, DDL 4테이블+5인덱스, db.go WAL/RO핸들/embed migration, Makefile). `go build/vet/test ./...` exit 0 (독립 재검증 완료).
- ✅ Phase 1 (fetch/store/diff): TDD + 2-stage review + fixes. `go build/vet/test -count=1 ./...` exit 0.
  - 1.1 fetch: SSRF 2-layer guard(host allowlist + dialer IP 차단 + redirect 재검증 + DNS-rebind 핀), robots 인라인 매처, best-effort conditional GET, UTF-8 transcode, 5MB cap. 18 케이스(+race).
  - 1.2 store+model: ContentHash(타임스탬프/배치 제외 canonical), per-event 트랜잭션 upsert.
  - 1.3 diff: 멱등/단일행/다중행/상태전이/disappearance. [DOUBT] 통과.
- ✅ Phase 2 (sources/classify/normalize): TDD + Golden + 2-stage review. `go build/vet/test -count=1 ./...` exit 0 (7 test 패키지 green).
  - 2.0 Source interface + registry; 2.1 COEX 동적 sitemap 샤드 발견(실 fixture); 2.2 COEX 파서(Golden).
  - 2.3 KINTEX discovery — **스파이크로 `list.do` 확정**(clist.do는 0 seq → AJAX). `fnView('./view.do',<seq>)` 파싱, 브라우저 불필요. **PREFLIGHT I4 해소**.
  - 2.4 KINTEX 파서(Golden, view.do?seq=26060201); 2.5 분류(taxonomy SSOT, excluded, deferred cheap-model); 2.6 정규화(날짜 invariant, actions→missing_fields, summary 템플릿 ≤240, URL 검증, 5 rule).
  - 리뷰 medium 수정: KINTEX status/NotModified 체크, classify 회귀 테스트.
  - 잔여 low(Phase 4 hardening으로 이월): cost_hint='unknown'이 missing_fields에 잘못 포함, COEX 전체 샤드 실패 시 empty-success(→ 4.1 circuit breaker가 커버).
- ✅ Phase 3 (read API): TDD + 2-stage review. `go build/vet/test -count=1 ./...` exit 0 (8 패키지 green).
  - 3.1 events list(envelope/opaque cursor/필터/clamp) + store/query.go; 3.2 detail/sources/changes(+/api/v1 prefix); 3.3 schema/meta/openapi.yaml/llms.txt(route↔OpenAPI 일치 테스트); 3.4 JSON/MD 협상(단일헤더 테이블, Link 헤더, injection guard); 3.5 ETag/304 + error envelope + 60/min·2000/day 쿼터 + trusted-proxy IP + concurrency cap.
  - 리뷰 high 수정: changes 피드 keyset 페이지네이션 행 누락(3.2). medium 수정: route↔OpenAPI 불일치(3.4), changes Link 헤더 누락 + Vary:Accept(wire).
- ✅ Task 4.1 (ingest 오케스트레이션): Pipeline.Run(discover→fetch→parse→normalize→ApplyBatch), per-item recover 격리, per-source circuit breaker(floor+anomaly), flock single-flight, wall-clock deadline, migration 0002(ingest_error/fetch_anomaly/batch_stats). 리뷰 medium 3건(deadline/ctx cancel/floor-poisoning) 수정. config FromEnv 추가(systemd용 env override).
- ⏳ Phase 4 배포 (사용자 전체 승인) — 진행 중.

## 라이브 ingest가 잡아낸 실제 버그 3건 (유닛 테스트 전부 통과했으나 실데이터에서 발견)

1. **classify excluded 기본값 버그**: 산업 카테고리 무매칭 이벤트(학회/학술대회 등)를 `excluded=false`로 반환 → normalize rule-1(categories≥1)에서 **거부·드롭**(93 errors, 0 stored). 수정: 무매칭 → `excluded=true`(wedge 밖, 저장+숨김, 드롭 금지). `internal/classify/classify.go`.
2. **normalize rule-1 예외 누락**: excluded 이벤트는 카테고리가 없는 게 정상인데 rule-1이 거부. 수정: `!excluded`일 때만 categories≥1 요구. `internal/normalize/normalize.go`. (AC-6와 정합: excluded는 저장+숨김.)
3. **스케일/예의 문제**: COEX sitemap이 **8,748개 과거 전시(2002년~)** 노출 → 매 ingest마다 전량 크롤은 70분+ & 무례. 수정: `MaxDiscoverPerSource`(기본 400, env override) 캡으로 최신 N개만 처리, 드롭 수 로깅(silent truncation 금지). `internal/pipeline`, `internal/config`.

> 교훈: 유닛 테스트 8패키지 전부 green이었지만, 실제 COEX 데이터의 (a) 비산업 이벤트 비율, (b) 카탈로그 규모는 라이브 ingest로만 드러남. 그래서 배포 전 로컬 라이브 검증을 한 것.

## AC 검증 (Phase 5 / Task 5.1, 부분 — 로컬 테스트 기반)

> 전체 스위트 `go test -count=1 ./...` exit 0. AC-1(완전)/9/10은 ingest 오케스트레이션(Task 4.1) + 라이브 스모크(4.4) 필요 → 배포 단계에서 마감.

| AC | 상태 | 증거 |
|----|------|------|
| AC-1 스키마 준수 레코드 | 🟡 부분 | normalize ValidPasses/GoldenCOEX + COEX/KINTEX Golden 파서 통과(스키마 준수 증명). "실 ingest로 venue별 ≥1"는 Task 4.1 필요 |
| AC-2 멱등 2회 | ✅ | ApplyBatchIdempotentNoOp, UpdateThenRerunIdentical, ContentHashStableUnderFreshnessChange |
| AC-3 단일필드→1행 | ✅ | ApplyBatchSingleFieldUpdate, DiffSingleFieldOneRow, UpsertEventWritesChangeLog |
| AC-4 events 목록 | ✅ | FullIterationNoGapsNoDupes, Category/Venue/UpdatedSince/ChangedSince Filter, LimitClampAndEcho, ExcludedHidden, CursorRoundTrip |
| AC-5 JSON/MD 협상 | ✅ | Render_SameFieldSet, DefaultIsJSON, FormatQueryPrecedence, LinkHeaderParity, VaryAccept |
| AC-6 비산업 제외 | ✅ | Classify_ExcludedReturnsNoCategories, ListEvents_ExcludedHidden |
| AC-7a read LLM 0 | ✅ | `go list -deps ./internal/api` → LLM/model 의존 0 |
| AC-7b ETag/304 | ✅ | ETag_304OnMatch |
| AC-7c 60/min·2000/day | ✅ | Quota_PerMinuteBreach, Quota_PerDayBreach |
| AC-8 sources ≥1 유효 | ✅ | GetEventSources_OK, Normalize_ValidPasses(source 검증) |
| AC-9 read-during-write | 🔴 대기 | WAL/RO핸들 설계 완료, 명시적 동시성 스모크는 Task 4.4 |
| AC-10 circuit breaker 보존 | 🔴 대기 | Task 4.1 circuit breaker 미구현 |
| AC-11 actions→missing_fields | ✅ | Normalize_ActionsAllMissing |
| AC-12 SSRF/URL | ✅ | FetchRedirectToLoopbackRejected, DirectLinkLocalRejected, HostAllowlist, RejectsNonHTTPScheme, Normalize_RejectNonHTTPURL |

**달성: 10/12 ✅, 1 부분(AC-1), 2 대기(AC-9, AC-10) — 모두 Task 4.1/4.4(배포 단계)에서 마감.**

## Doubt 결과
- Task 1.3 [DOUBT] (멱등 변경감지): fresh-context adversarial 8개 risk 제기 → 구현에 반영(array-ordering 결정성, partial-batch 재실행 수렴, field_path 모호성, change_log 순서/중복카운트 등). RECONCILE: valid+actionable → 구현 시 반영. Contract misread 없음(PLAN 수정 불요).
- 리뷰 후속 medium 수정: content_hash에서 schema_version 제외, changed_at를 last_checked_at에서 분리, robots redirect 재검증, 공유 limiter Crawl-delay 격리.

## 이전: 리뷰+사전조사 완료

## 완료
- PLAN.md v2 작성 (Go + modernc.org/sqlite, 23 Task, 6 Phase)
- 구조적 검증 통과 (APPROVED, minor 1건 반영)
- 기술 리뷰 완료 — 멀티에이전트 9렌즈 + adversarial verify (99 agents)
  - raw 42 high/med findings → 검증 후 ~22개 클러스터로 PLAN.md 반영
- 사전 조사 완료 (PREFLIGHT.md): live DNS/소스 재탐침 + AC 감사 + 의존성 검증
- 사전 feasibility (`prototype/coex-kintex-feasibility.md`), v0.1 스키마 확정

## 핵심 반영 사항
- 동시성(WAL/busy_timeout/RO핸들), content_hash 도메인 정의, disappearance policy + circuit breaker
- SSRF egress allowlist, 쿼터 trusted-proxy IP + 60/min·2000/day, cursor/error envelope
- Source 인터페이스/registry, taxonomy SSOT, 동적 sitemap 샤드
- actions.* missing_fields honesty, summary 템플릿(copyright), AC 1·5·6·7 정밀화 + AC-9~12 신설
- preflight 정정: events.nukk.net **DNS 생성 필요**, VPS 런북(Docker/Caddy) 정합, KINTEX discovery 스파이크 선행, COEX no-cache → conditional GET best-effort

## 다음 단계
- `/execute-plan .tasks/260621-coex-kintex-ingestion/PLAN.md`

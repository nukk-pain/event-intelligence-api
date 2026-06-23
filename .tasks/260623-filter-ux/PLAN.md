# Plan: filter-ux (confs.tech 스타일 필터 개선 3종)

## Metadata

- Status: `in-progress`
- worktree: false
- Owner: smpain
- Created: 2026-06-23
- Stack: Go (read API/store) + vanilla JS/CSS (static UI). 신규 의존성 0.
- Entry: `/start` Fast mode (autonomous) → 본 PLAN → 구현 → 검증

## 요구사항 재진술

confs.tech 레퍼런스에서 가져올 3가지를 현재 `events.nukk.net` UI에 적용한다.

1. **분야 칩 건수 배지**: 분야 `<select>`를 클릭형 칩 그룹으로 교체하고, 각 칩에 해당 분야 행사 수를 배지로 표시(`AI 37` 형태).
2. **날짜 범위 필터**: 시작일/종료일 입력으로 행사 `start_date`를 범위로 좁힌다.
3. **필터 URL 반영**: 모든 필터 상태를 `location.search`에 직렬화 → 복붙·북마크·뒤로가기로 동일 상태 복원.

### 성공 기준 (AC)

- [ ] AC1: events 목록 응답에 `category_counts`(정규 순서 5종, category 필터 제외 집계)가 포함되고 단위테스트가 건수를 검증.
- [ ] AC2: `before` 파라미터가 OpenAPI/llms.txt에 문서화되고, since+before 조합이 테스트로 검증됨.
- [ ] AC3: UI에 분야 칩 그룹이 렌더되고, 각 칩에 정확한 건수 배지가 보이며, 칩 클릭으로 필터링/토글된다.
- [ ] AC4: UI에 날짜 범위 입력이 있고, 값 변경 시 `since`/`before`로 서버 필터링된다.
- [ ] AC5: 필터 변경 시 URL 쿼리가 갱신되고, 그 URL을 새로 열면 동일 필터 상태로 복원된다(뒤로/앞으로 포함).
- [ ] AC6: `go build/vet/test` 그린, 기존 테스트 무회귀, 브라우저 수동 검증 통과.

## 핵심 발견 (조사 결과, file:line)

- **날짜 범위는 백엔드에 이미 존재**: `handleListEvents`가 `since`→`MinStartDate`, `before`→`MaxStartDate` 파싱(`internal/api/events.go:68-69`); `store.ListEvents`가 `start_date >=/<=` 적용(`internal/store/query.go:116-123`). → 기능 추가 불필요, 문서화 + 프론트 연결만.
- **분야 건수는 어디에도 없음**: 신규. `events.categories`는 JSON 배열 TEXT 컬럼, 기존 필터가 `json_each`로 멤버십 매칭(`query.go:106-111`). 카테고리 5종 SSOT: `internal/classify/taxonomy.go:25-31`.
- **데이터 규모**: `list=venue&since=today` 응답이 100건+ `has_more=true`(라이브 확인). → 클라이언트 집계는 부정확 → **서버 facet으로 결정**.
- **scope(categorized/all)·search는 클라이언트 필터**(`static/events.js:41-44`, `static/app.js:64-70`). 칩 건수는 categorized(=`excluded=0`, 카테고리 보유) 행사 기준이라 scope와 무관하게 일관.
- **응답 봉투**: `Envelope{Data, Page}` 공용(`internal/api/envelope.go`); events 목록은 `eventListView`로 JSON/Markdown 협상(`internal/api/render.go:176-195`). changes 피드도 같은 Envelope 사용 → 신규 필드는 `omitempty` 필수.
- **프론트 필터 상태**: `static/app.js:7-17` state 객체. `eventsURL()`(app.js:112-119)가 단일 쿼리 빌드 지점. 카테고리/장소 select는 `/api/v1` vocabularies로 채움(app.js:196-208, 266-268). URL 직렬화 전무.

## 설계 결정

| 결정 | 선택 | 근거 |
|---|---|---|
| 분야 건수 집계 위치 | **서버 facet** | venue 목록이 한 페이지(100) 초과 → 클라 집계 부정확. GROUP BY 1회로 저렴(작은 테이블), confs.tech "AI N" 의미(전체 기준)와 일치 |
| facet 필터 범위 | category 제외, 나머지(list·venue·date) 포함 | 표준 faceting: 선택한 분야 외 다른 분야 건수도 보여 전환 가능 |
| 응답 위치 | `Envelope.category_counts` (`omitempty`, 정규 순서 슬라이스) | 추가 전용·비파괴. 슬라이스라 순서 결정적(ETag/test 안정) |
| 날짜 파라미터 | 기존 `since`/`before` 재사용 | 이미 구현·문서 컨벤션(YYYY-MM-DD). 신규 파라미터 도입 안 함(표면 최소화) |
| URL 반영 범위 | `list, scope, category, venue, from, to, q` | scope·search는 클라 필터지만 공유 완전성 위해 반영 |
| 필터 클럭 중복 제거 | `EventFilter.commonWhere()` 추출 | List/Counts가 같은 WHERE를 공유 → facet이 목록과 절대 어긋나지 않음(드리프트 버그 차단) |

## 위험 분석

| 위험 | 영향 | 가능성 | 대응 |
|---|---|---|---|
| facet WHERE가 목록 WHERE와 드리프트 → 건수 불일치 | 높음 | 중간 | `commonWhere()` 단일 빌더 공유, 동일 필터로 List/Counts 테스트 |
| 신규 응답 필드가 기존 클라이언트/테스트 깨뜨림 | 높음 | 낮음 | `omitempty` + 추가 전용. changes 피드 등 nil → 미출력. 기존 events_test 골든 갱신 |
| `json_each`가 NULL/잘못된 categories에서 에러 | 중간 | 낮음 | 기존 `query.go:109`가 이미 동일 패턴 사용(검증됨). store는 항상 유효 JSON 기록 |
| 칩 select 교체로 접근성 후퇴 | 중간 | 중간 | 칩은 `<button aria-pressed>` + 키보드 포커스, DESIGN.md 토큰 준수 |
| URL 복원이 vocab fetch보다 먼저 실행 → venue 옵션 부재 | 중간 | 중간 | 정적 컨트롤(list/scope/date)은 즉시 복원, venue는 populateFilters 후 복원. state는 먼저 채워 first load에 반영 |
| 커서 정렬(updated_at)이 날짜순 아님 | 낮음 | — | 기존 동작(클라가 start_date로 재정렬). 범위 외 — 변경 안 함 |
| 긴 URL | 낮음 | 낮음 | 빈 필터는 직렬화 생략(기본값 미포함) |

## 구현 계획

### Phase 1: 백엔드 facet + 문서 (store → api → contract)

#### Task 1.1: `store.CategoryCounts` + `commonWhere` 추출 `[Code]`
- impl: `internal/store/query.go`
- [ ] `EventFilter.commonWhere() ([]string, []any)` 추출 — list/venue/updated_since/changed_since/min·maxStartDate 공통 절. ListEvents가 이를 사용하고 category+cursor만 추가.
- [ ] `type CategoryCount struct{Category string; Count int}` (또는 map 반환).
- [ ] `CategoryCounts(ctx, db, filter) (map[string]int, error)`: `SELECT je.value, COUNT(*) FROM events e, json_each(e.categories) je WHERE <commonWhere> AND e.excluded = 0 GROUP BY je.value`.
- 검증: `internal/store/store_test.go`에 날짜/list/venue 필터별 건수 테스트. ListEvents 무회귀.

#### Task 1.2: API 응답에 `category_counts` 부착 `[Code]`
- impl: `internal/api/envelope.go`, `internal/api/events.go`, `internal/api/render.go`
- [ ] `Envelope`에 `CategoryCounts []CategoryCount \`json:"category_counts,omitempty"\``.
- [ ] `handleListEvents`: filter 복사 → Category/Cursor/Limit 비우고 `store.CategoryCounts` 호출 → `classify.Categories` 순서로 5종 슬라이스(0 포함) 구성 → env에 부착. 에러는 비치명(카운트 생략).
- [ ] `eventListView.Markdown()`: 표 앞에 `category_counts: ai N, ...` 한 줄(동일 필드셋 유지).
- 검증: `internal/api/events_test.go`에 category_counts 존재·순서·건수 + before 파라미터 테스트.

#### Task 1.3: 계약 문서 동기화 `[Code]`
- impl: `static/openapi.yaml`, `static/llms.txt`
- [ ] `before` 파라미터(YYYY-MM-DD, start_date <=) 추가, `category_counts` 응답 스키마 추가.
- [ ] llms.txt 필터 라인에 `before` + category_counts 설명.
- 검증: `caddy`/YAML 파싱 무오류(go test가 embed 로드).

### Phase 2: 프론트엔드 (chips → date → URL)

#### Task 2.1: 분야 칩 그룹 + 건수 배지 `[Code]`
- impl: `static/index.html`, `static/app.js`, `static/ui.js`, `static/list.css`, `DESIGN.md`
- [ ] index.html: 분야 `<select>` → 칩 컨테이너(`#f-category-chips`)로 교체.
- [ ] ui.js: `categoryChip(slug, count, selected)` → `<button class="chip-toggle cat-{slug}" aria-pressed>label <span class="chip-count">n</span></button>`.
- [ ] app.js: `applyPage`에서 첫 페이지의 `d.category_counts`를 `state.categoryCounts`에 저장 → `renderCategoryChips()`로 렌더, 클릭 시 `state.category` 토글 후 reload.
- [ ] list.css: `.chip-toggle`, `.chip-count`, 선택 상태, 칩 행 컨테이너. DESIGN.md 토큰 준수.
- [ ] DESIGN.md: Filter Bar 갱신 + Category Chip 컴포넌트/`chip-count` 토큰 추가.

#### Task 2.2: 날짜 범위 입력 `[Code]`
- impl: `static/index.html`, `static/app.js`, `static/list.css`
- [ ] index.html: `<input type="date" id="f-date-from">`, `#f-date-to` + 라벨(필터 바).
- [ ] app.js: state `dateFrom`/`dateTo`; `eventsURL`에 `since`(=dateFrom, venue는 미설정 시 today)/`before`(=dateTo) 반영; change 핸들러로 reload.
- [ ] list.css: 날짜 입력 스타일(기존 control 토큰).

#### Task 2.3: 필터 URL 직렬화/복원 `[Code]`
- impl: `static/app.js`
- [ ] `serializeFilters()`: state→URLSearchParams(`list,scope,category,venue,from,to,q`, 기본값 생략)→`history.replaceState`.
- [ ] `deserializeFilters()`: `location.search`→state + 정적 DOM 컨트롤(list/scope/date) 즉시 설정; venue는 populateFilters 후 설정.
- [ ] 모든 필터 change 핸들러 끝에서 `serializeFilters()` 호출; `popstate`로 복원+reload.
- [ ] init 순서: deserialize → syncListControls → Promise.all(load+vocab) → populateFilters → venue 복원.

### Phase 3: 검증 + 보고
- [ ] `go build ./... && go vet ./... && go test ./...` 그린.
- [ ] 로컬 serve(프로덕션 DB 사본) + CDP 브라우저로 칩 건수·날짜 범위·URL 복원 수동 확인(before/after 캡처).
- [ ] (선택) `[DOUBT]` facet 정확성 — 칩 합계가 categorized 목록 총수와 일치하는지.
- [ ] PROGRESS.md/보고 갱신. 배포는 별도 승인 후.

## Verification Strategy

| AC | 방법 | 증거 |
|---|---|---|
| AC1 facet | store/api 단위테스트 | `go test ./internal/store ./internal/api` |
| AC2 before | api 테스트 + 라이브 smoke | test 출력 |
| AC3 칩 | 브라우저 수동 + 캡처 | scratchpad PNG |
| AC4 날짜 | 브라우저 수동(범위 적용 전후 건수) | 캡처 |
| AC5 URL | 브라우저: 필터→URL 확인→reload 복원→back | 캡처/콘솔 |
| AC6 무회귀 | 전체 test/vet | 출력 |

## 비고

- 배포(`events.nukk.net`)는 outward-facing이라 **명시 승인 후** 진행. 본 작업 범위는 구현+로컬 검증까지.
- 커서 정렬을 start_date로 바꾸는 것은 범위 외(별도 결정 필요).

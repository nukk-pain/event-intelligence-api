# Progress: filter-ux

## 현재 초점

confs.tech 스타일 필터 3종 구현: 분야 칩 건수 배지 · 날짜 범위 필터 · 필터 URL 반영.

## 상태

- [x] 조사 (5개 서브시스템 병렬 read) — API/store/classify/frontend/contract 통합 완료
- [x] PLAN.md 작성 (서버 facet 결정, 계약 확정)
- [x] Phase 1: 백엔드 facet + 문서
  - [x] Task 1.1 store.CategoryCounts + commonWhere (+ 8 subtest)
  - [x] Task 1.2 API category_counts 부착 (+ facet/before/omitempty 테스트)
  - [x] Task 1.3 openapi/llms 동기화 (before 파라미터, category_counts 스키마)
- [x] Phase 2: 프론트 (칩 → 날짜 → URL) — ui.categoryChip, 날짜 입력, serialize/deserialize/popstate
- [x] Phase 3 검증: go test/vet/race 그린, gofmt clean, CDP 브라우저 E2E 통과
- [x] 적대적 다차원 리뷰 (workflow ws57qus6f, 22 agents) → 11 확정 발견 triage·수정 완료

## 리뷰 후속 수정 (workflow ws57qus6f)

수정함:
- (high) 룩어헤드 페이지네이션 중 필터 변경 레이스 → `loadSeq` 토큰으로 stale 체인 차단 (app.js).
- (critical/high) facet docstring 과장 주장 + 계약 모호성 → query.go docstring/주석 정밀화 + openapi category_counts 설명 보강(동작 변경 없음, excluded=0는 categorized 카운트로 의도된 동작).
- (low) categoryChip count 미이스케이프 → `escapeHtml(String(count))`.
- (medium) `.chip-count` opacity 0.8 다크모드 대비 → opacity 제거.
- (low) 날짜 입력 패딩 불일치 → 다른 컨트롤과 일치(6px 10px/13.5px).
- (low) 초기 빈 배지 깜빡임 → 초기 중복 renderCategoryChips 제거(로드 완료 시 카운트와 함께 렌더).

수정 안 함(근거):
- #4 facet stale: **무효** — facet은 category 차원을 무시(`category=""`)하므로 카테고리 선택 시 카운트 불변.
- #8 statusChip/costChip XSS: **선행 코드**(내 기능 아님), 서버 통제 enum → surgical scope 밖. 사용자에게 별도 보고.
- #5 restoreVenueControl 방어성: 현재 정상 동작, 미래 리팩터 가정 → 보류.

## 검증 증거 (로컬 serve + CDP, prod DB 사본)

- API: `category_counts=[ai:20, robotics:10, bio:7, med:9, health:8]`, Markdown 포맷도 동일 facet 라인 포함.
- 칩 클릭(바이오): URL→`?category=bio`, pressed=바이오, 카드 7 = 배지 7 (facet 정확).
- URL 복원: `?category=ai&from=2026-08-01&to=2026-08-31&scope=all` → AI pressed(건수 5로 갱신), 날짜 입력 채움, 카드 5 전부 8월.
- `go test ./...` + `-race` (api/store) 그린.

## 결정/방식

- URL 동기화는 `history.replaceState`(깔끔한 히스토리·공유 우선) 채택. 페이지 내 필터 간 back/forward는 미지원(조사 권고: 히스토리 오염 회피). 공유 URL 새로 열기 복원은 완전 동작.
- 칩 카테고리 SSOT는 ui.js `CATEGORY_ORDER`(classify.Categories와 동일 순서)에서 직접 → vocab fetch 레이스 제거.

## 자동 결정 기록 (/start autonomous)

- **HARD GATE 자동 통과**: /start 경유, 위험분석에 '높음' 진행불가 위험 없음.
- **분야 건수 = 서버 facet**: 라이브 데이터로 venue 목록 100건+ 확인 → 클라 집계 부정확 → 서버 GROUP BY 선택(질문 대신 데이터로 해소).
- **날짜 파라미터 = 기존 since/before 재사용**: 신규 파라미터 미도입(표면 최소화).
- **배포 보류**: outward-facing → 구현+로컬검증까지만, 배포는 승인 후.

## 증거

- 라이브 확인: `list=venue&since=2026-06-23&limit=100` → returned 100, has_more true.
- 조사 산출물: workflow wf_1398d23c-d89 (5 agents, file:line 매핑).

## 배포 (events.nukk.net, 2026-06-23)

- 커밋 3건: `f10aace` feat(filter-ux), `18dac5f` docs(CLAUDE.md), `8fcb4c7` fix(asset cache-bust).
- VPS 바이너리 교체(백업 `eventsintel.bak`) + `systemctl restart eventsintel-api`. DB 마이그레이션 없음(facet은 read-time).
- 공개 스모크: 모든 엔트리포인트 200, `category_counts` 라이브(ai:21…), `before` 동작, llms/openapi 갱신 확인.
- **배포 인시던트(해결)**: 1차 배포에서 app.js만 cache-bust → CDN이 stale ui.js 페어링 → `undefined.map` 에러. 전 asset version-bust + 재배포로 해결. CF 토큰은 purge 권한 없음(HTML은 DYNAMIC이라 무관). 라이브 재검증: 칩·날짜·URL 복원 정상.

## 블로커

- 없음. 작업 완료.

## 미해결(선택)

- 선행 statusChip/costChip XSS는 이번에 함께 하드닝 완료(ui.js).
- 페이지 내 필터 back/forward(현재 replaceState) — 필요 시 pushState 전환 가능.

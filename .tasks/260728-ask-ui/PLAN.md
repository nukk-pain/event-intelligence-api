# PLAN: ask-ui — events.nukk.net 자연어 질의 UI

- worktree: false
- 승인: 자동 승인 (/start autonomous)
- 시작일: 2026-07-28

## 요구사항 재진술

### 핵심 요구사항
1. events.nukk.net 홈에서 자연어 질문("다음 달 서울 AI 행사 뭐 있어?")을 입력하면
   매칭 행사 목록을 카드로 보여준다.
2. 데이터 경로는 이미 공개된 같은-origin `/mcp` 엔드포인트의 `ask_events` 도구
   하나만 사용한다 (단일 JSON-RPC POST, 무상태). 새 백엔드 API를 만들지 않는다.
3. 쿼터 초과(429)·0건·타임아웃·백엔드 미구성 오류를 각각 한국어 안내로 처리한다.
4. 기존 read API, MCP 계약, 쿼터, openapi.yaml/llms.txt 는 변경하지 않는다.

### 범위 (Scope)
**포함**: static/index.html 질의 섹션, 신규 static/ask.js, CSS 소폭, embed.go
export, api.go 라우트, root_test.go 기대값, 배포(별도 게이트).

**제외**: api.Router 신규 엔드포인트(금지 — read path LLM-free 유지), MCP 서버
변경, eventscout/eventagent UI, 검색 히스토리/세션 저장, 스트리밍 응답.

### 가정사항
- `/mcp` 는 프로덕션에서 같은 origin 이므로 CORS 불필요 (Caddy가 3008로 프록시).
- ask_events 응답 `result.content[0].text` 는 `{count, events[]}` JSON 문자열
  (로컬 stdio + 라이브 curl로 확인됨).
- solar-open2 키는 7/31까지 유효. 이후 ask_events 는 오류를 반환할 수 있으므로 UI 는
  이를 서비스 점검 안내로 우아하게 표시해야 한다 (Stage 2 크레딧으로 재개 예정).

## 위험 분석

| 위험 | 영향도 | 발생 가능성 | 대응 방안 |
|------|--------|-------------|----------|
| 공개 UI가 운영자 Solar 키 소모 가속 | 중간 | 높음 | 기존 서버측 쿼터(10회/10분/IP)가 이미 상한. UI는 요청 중 버튼 비활성 + 429 안내로 연타 억제. 신규 상한 추가 없음 |
| 7/31 키 만료 후 기능 정지 | 중간 | 확실 | 오류 응답을 "잠시 후 다시" 안내로 처리, UI 골격 유지. 키 갱신은 Stage 2 이후 별도 작업 |
| 이벤트 데이터 XSS | 높음 | 낮음 | 기존 `EventIntelUI.escapeHtml` 로 전 필드 이스케이프, innerHTML 조립 시 원문 삽입 금지 |
| 임베드 자산 4-레이어 누락 (asset/export/route/test) | 중간 | 중간 | Task 1.2 에서 4곳 동시 수정 + root_test.go 로 검증 (생성 후 통합 검증 규칙) |
| 로컬에서 /mcp 부재로 E2E 불가 | 낮음 | 확실 | 로컬은 렌더·오류경로 확인까지, 실질의 E2E 는 배포 후 라이브 검증으로 분리 |

## 의존성 분석

### 내부 의존성
| 모듈 | 의존성 | 변경 필요 |
|------|--------|----------|
| static/ (index.html, ask.js 신규, index.css) | 없음 (vanilla JS) | 예 |
| static/embed.go | //go:embed export 추가 | 예 |
| internal/api/api.go | `/assets/ask.js` 라우트 1줄 | 예 |
| internal/api/root_test.go | 기대 자산 목록에 ask.js 추가 | 예 |
| cmd/eventmcp, internal/agent | 호출만 함 | 아니오 |

### 외부 의존성
새 패키지 없음.

### 영향 범위
- **직접**: static/index.html, static/ask.js(신규), static/index.css,
  static/embed.go, internal/api/api.go, internal/api/root_test.go
- **간접**: 배포 바이너리(임베드 재빌드), deploy/verify.sh 체크 목록

## 에이전트 구성
- orchestrator 단독 (단일 작업 흐름, 파일 6개, 병렬 이득 없음)

## 구현 계획

### Phase 0: 사전 검증 (예상: XS)

#### Task 0.1: 라이브 /mcp 단일 POST tools/call 검증 `[PoC]`
- **작업 내용**:
  - [x] initialize 없이 `tools/call` `search_events` 단일 POST가 200 + 유효 결과를
        반환하는지 라이브 curl 확인 (ask_events와 동일 경로, 쿼터 미소모)
- **검증 방법**: `result.content[0].text` 에 count 필드 존재

### Phase 1: UI 구현 (예상: M)

#### Task 1.1: 디자인·카피 게이트 `[Manual]`
- **작업 내용**:
  - [x] design-governance 스킬 로드 → 질의 섹션의 배치(기존 필터 위? 아래?),
        우선순위(정확성>사용목적>접근성) 판단, Design Brief 1문단
  - [x] korean-copy 스킬 로드 → placeholder·버튼·오류 문구 8대 금지 패턴 점검
- **검증 방법**: Brief 와 카피 초안이 PROGRESS.md 에 기록됨

#### Task 1.2: ask.js + 4-레이어 연결 `[TDD]`
- test: `internal/api/root_test.go`
- impl: `static/ask.js`, `static/embed.go`, `internal/api/api.go`,
  `static/index.html`
- **작업 내용**:
  - [x] RED: root_test.go 에 `/assets/ask.js` 서빙 + index.html 내
        `src="/assets/ask.js?v=` 기대 추가 → 실패 확인
  - [x] GREEN: static/ask.js 작성 (IIFE, `window.EventIntelAsk`) — 같은 origin
        `/mcp` 로 `tools/call ask_events` 단일 POST, 응답 파싱, 카드 렌더
  - [x] embed.go export + api.go 라우트 + index.html 질의 섹션·script 태그
        (캐시버스팅 `?v=20260728-askui`)
  - [x] 오류 경로: HTTP 429(Retry-After 초 → "N분 뒤 다시"), JSON-RPC error,
        네트워크 실패(fetch reject), 비-JSON 응답(프록시 5xx HTML) 파스 가드,
        count=0("조건에 맞는 행사가 없어요")
  - [x] 클라이언트 타임아웃: AbortController 35s (서버 30s 상한보다 여유)
  - [x] 요청 중 입력·버튼 비활성화 + 진행 표시("찾는 중…"), 응답 전 필드 전부
        escapeHtml, 입력 maxlength 200자
- **검증 방법**: `go test ./internal/api/ -run TestRoot` GREEN

#### Task 1.3: 질의 섹션 스타일 `[Manual]`
- impl: `static/index.css` (또는 신규 최소 블록)
- **작업 내용**:
  - [x] 기존 theme.css 토큰(색·간격) 재사용, 카드 목록은 list.css 클래스 재사용
  - [x] `word-break: keep-all` 상속 확인 (한글 렌더링 규칙)
- **검증 방법**: 로컬 serve 후 브라우저 렌더 확인 (레이아웃 안 깨짐)

### Phase 2: 로컬 검증 (예상: S)

#### Task 2.1: 회귀 + 로컬 렌더 확인 `[Manual]`
- **작업 내용**:
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` 전체 통과
  - [x] 로컬 `eventsintel serve` → 브라우저에서 질의 섹션 렌더·오류 경로
        (로컬 /mcp 404 → 오류 안내 표시) 확인
- **검증 방법**: 전체 테스트 GREEN + 로컬 화면 확인

### Phase 3: 배포 (예상: S) — ⛔ AskUserQuestion 게이트 (프로덕션 비가역)

#### Task 3.1: 배포 승인 확인 `[Manual]`
- **작업 내용**:
  - [x] AskUserQuestion 으로 배포 진행 확인 (프로덕션 배포는 자율 진행 예외)
- **검증 방법**: 사용자 승인 기록

#### Task 3.2: developer-vps 배포 + 검증 `[Manual]`
- **작업 내용**:
  - [x] `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o eventsintel-linux ./cmd/eventsintel`
  - [x] 바이너리 교체 + `eventsintel-api` 재시작 (memory: nukk-net-vps-deploy 절차)
  - [x] `deploy/verify.sh` → ALL CHECKS PASSED (memory: deploy-verify-mandatory)
  - [x] 라이브 스모크: events.nukk.net 에서 실제 질문 1건 → 카드 렌더 확인
- **검증 방법**: verify.sh 전체 통과 + 라이브 질의 1건 성공

## 노력도 추정

| Phase | 규모 | 태스크 수 |
|-------|------|----------|
| Phase 0 | XS | 1 |
| Phase 1 | M | 3 |
| Phase 2 | S | 1 |
| Phase 3 | S | 2 |
| **합계** | **M** | **7** |

## 테스트 전략

### 단위 테스트
- [ ] root_test.go: `/assets/ask.js` 서빙(Content-Type, 내용 마커
      `EventIntelAsk`), index.html 이 ask.js·질의 섹션 포함

### 통합 테스트
- [ ] `go test ./...` 전체 회귀 (기존 read API 불변 확인 포함)

### E2E 테스트
- [ ] 라이브(배포 후): 실제 질문 1건 → 카드 렌더 (Playwright 미도입 프로젝트라
      수동 브라우저 확인으로 대체)

### 수동 테스트
- [ ] 429 안내 문구(쿼터 11번째 호출), 0건 안내, 로컬 /mcp 404 오류 안내
- [ ] 모바일 폭(375px) 레이아웃

## Acceptance Criteria (인수 기준)

- [ ] AC-1: Given events.nukk.net 홈, When 질문 입력 후 제출, Then 건수와 행사
      카드(행사명·기간·장소·링크)가 표시된다
- [ ] AC-2: Given 같은 IP 로 10분 내 10회 소진, When 11번째 질의, Then
      Retry-After 기반 대기 안내가 표시되고 페이지가 깨지지 않는다
- [ ] AC-3: Given 매칭 0건 질의, When 제출, Then "결과 없음" 안내가 표시된다
- [ ] AC-4: Given 이번 변경 전체, When `go test ./...`, Then 전부 통과하고
      openapi.yaml·llms.txt·eventmcp 코드는 diff 0 이다
- [ ] AC-5: Given 배포 완료, When `deploy/verify.sh`, Then ALL CHECKS PASSED

## Verification Strategy (검증 전략)

| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1 | 배포 후 라이브 수동 | 질문 1건에 카드 ≥1 + 건수 표기 |
| AC-2 | 라이브 수동 (curl 로 쿼터 소진 후 브라우저) | 429 시 안내 문구 렌더 |
| AC-3 | 라이브 수동 | 무의미 질의에 0건 안내 |
| AC-4 | `go test ./...` + `git diff --stat` | 전체 GREEN, 계약 파일 diff 0 |
| AC-5 | deploy/verify.sh | 스크립트 출력 ALL CHECKS PASSED |

## 병렬 실행 그룹
- 그룹 A: Task 0.1
- 그룹 B (A 후): Task 1.1 → 1.2 → 1.3 (순차 — 카피·디자인이 마크업 선행)
- 그룹 C (B 후): Task 2.1
- 그룹 D (C 후, 사용자 게이트): Task 3.1 → 3.2

---

## 리뷰 로그

> 리뷰 일시: 2026-07-28

### 총평
백엔드 무변경·기존 공개 경계 재사용이라 위험 표면이 작다. 오류 경로 완결성만
보강하면 구현 준비 완료.

### 반영 완료
- [x] [중간][에러] 네트워크 실패·비-JSON 응답 가드, AbortController 35s → Task 1.2
- [x] [중간][UX] 진행 표시("찾는 중…") + 입력 maxlength 200자 → Task 1.2

### 미반영 (참고용)
- [낮음] 예시 질문 칩 — YAGNI, 사용 데이터 보고 결정
- [낮음] 결과 카드에서 기존 상세 모달 연결 — source_url 직링크로 충분

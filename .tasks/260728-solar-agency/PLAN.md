# PLAN: solar-agency — 도구 선택형 발견 루프 + 발견→PR 연결

- worktree: false
- 승인: 자동 승인 (/start autonomous) + 방향 AskUserQuestion 확인 완료 (2026-07-28, "둘 다 — 루프 먼저")
- 시작일: 2026-07-28
- 마감 맥락: Upstage Solar Agent Partner Stage 1 제출 2026-07-31

## 요구사항 재진술

### 핵심 요구사항
1. `internal/agent`의 discovery 루프에서 propose→search→judge 고정 안무를 제거하고,
   매 턴 Solar가 `search{query}` / `open{url}` / `accept{selections}` / `done` 중
   다음 행동을 선택하는 액션 루프로 재작성한다.
2. `open{url}`은 후보 테이블에 이미 있는 canonical URL만 대상으로 하며, 기존
   publicdiscovery의 bounded fetch(robots, 크기 상한, `candidateURLAllowed`
   SSRF 가드)를 재사용해 페이지를 읽고 후보의 title/근거를 갱신한다. 모델이
   지어낸 임의 URL은 절대 fetch하지 않는다.
3. 모든 예산 상한(모델 호출, completion 토큰, 턴 수, open 횟수)은 코드가
   집행하고, 초과 시 truncation/terminal_reason을 기록하며 종료를 보장한다.
4. 익명 서버의 `yield_trace` 공개 계약(고정 필드)은 의미를 유지한 채 매핑하고,
   `open_calls`는 count-only additive 필드로 확장해 DECISIONS에 기록한다.
5. (Stretch) 주간 발견 타이머가 채택 소스를 `-promote` 산출물과 함께 GitHub
   PR로 자동 오픈한다. 머지는 사람 — DECISIONS 2026-07-28 "Source promotion
   goes through code review" 경계 유지.

### 범위 (Scope)
**포함**: internal/agent 루프 재작성, internal/publicdiscovery opener 구현,
cmd/eventscout + internal/eventscoutserver 배선, yield_trace 확장 문서화,
DECISIONS.md 기록, 라이브 스모크, 배포+verify, README 갱신,
(stretch) run-scout-discovery.sh PR 자동화.

**제외**: read API(`internal/api`) 변경 일절 없음 (LLM-free 유지), ingest
파이프라인의 enricher 경로 변경 없음, eventagent 멀티홉 루프 변경 없음,
Tavily provider 동작 변경 없음, 후보 prefilter 요건 완화 없음.

### 가정사항
- solar-open2의 JSON-in-text 응답 파싱(`decodeLastModelObject`)은 기존
  propose/judge 경로에서 검증됨 — 같은 프로토콜이므로 별도 PoC 불필요, 대신
  라이브 스모크를 게이트로 둔다.
- `reasoning_effort=minimal` 정책은 액션 호출에도 동일 적용.
- fixture/tavily 모드에는 opener가 없어 open 액션이 비노출되어도 discovery는
  성립한다 (search/accept/done만으로 기존 동작 수준 유지).

## 위험 분석

| 위험 | 영향도 | 발생 가능성 | 대응 방안 |
|------|--------|-------------|----------|
| 마감(7/31) 전 미완성 브랜치로 기한 도달 | 높음 | 중간 | Phase 경계마다 배포 가능 상태 유지. Phase 1만 끝나도 제출 가능. 사용자 사전 확인 완료 (2026-07-28 AskUserQuestion — "둘 다 (루프 먼저)" 선택 시 리스크 고지됨) |
| 모델이 액션을 비효율적으로 선택해 라이브 수율 하락 | 중간 | 중간 | 프롬프트에 상태·잔여 예산 명시, 라이브 스모크로 기존 수율(accepted=5) 이상 확인, 미달 시 프롬프트 조정 |
| yield_trace 의미 변화로 계약 소비자 혼동 | 중간 | 낮음 | 기존 필드 의미 보존 매핑 + additive `open_calls`만 추가, DECISIONS 기록 |
| 테스트 자산 대량 갱신 중 회귀 놓침 | 중간 | 중간 | 파일 단위 bite-size Task, 기존 compatibility/yield 테스트를 삭제하지 않고 갱신 |
| open이 60s 서버 데드라인 잠식 | 중간 | 낮음 | 서버 open 예산 2회 고정, 기존 per-fetch 타임아웃 재사용 |
| GitHub 토큰을 VPS에 두는 stretch 작업의 시크릿 노출 | 높음 | 낮음 | Phase 4 착수 전 AskUserQuestion으로 토큰 방식 확인 (fine-grained PAT, contents+PR 최소 권한) |

### 주요 위험 상세

#### 위험 1: 마감 전 미완성
- **설명**: 루프 재작성은 이 저장소에서 가장 테스트가 두꺼운 모듈을 건드린다.
- **영향**: 7/31까지 배포 불가 상태면 제출물이 기존(고정 안무) 상태로 나감.
- **완화**: Phase 1(안무 이양)만으로도 "모델이 다음 행동을 정한다"가 성립하므로
  제출 서사가 확보됨. Phase 2(open)는 그 위의 가산점.
- **대응**: 7/30 정오까지 Phase 2 미완이면 Phase 1 상태로 배포·검증·제출 전환.

## 위험 패턴 자동 감지

### [DOUBT] 부여 (Stage 2 통과 — 키워드 + 5조건)
| Task | 매칭 키워드 | 만족 조건 |
|------|-----------|----------|
| Task 1.5 | "public API", "invariant", "production" | 1(분기 도입), 2(agent↔publicdiscovery↔server 경계 횡단), 5(공개 진단 계약 확장) |

## 의존성 분석

### 내부 의존성
| 모듈 | 의존성 | 변경 필요 |
|------|--------|----------|
| internal/agent (discover.go, discovery_model.go, discovery_types.go, discovery_yield.go) | 없음 (최하위) | 예 — 루프 재작성 |
| internal/agent/discovery_open.go (신규) | PageOpener 인터페이스 (자체 정의) | 예 |
| internal/publicdiscovery (opener.go 신규) | internal/fetch, internal/agent | 예 |
| internal/eventscoutserver | internal/agent, internal/publicdiscovery | 예 — opener 배선 + open 예산 |
| cmd/eventscout | internal/agent, internal/publicdiscovery | 예 — opener 배선 + flag |
| internal/api | — | **아니오 (불변 게이트)** |
| internal/pipeline, internal/solarenrich | — | 아니오 |

### 외부 의존성
새 패키지 없음. (Phase 4의 `gh` CLI는 VPS 기존 도구 확인 후 사용.)

### 영향 범위
- **직접**: internal/agent/{discover,discovery_model,discovery_types,discovery_yield,discovery_action*,discovery_open*}.go 및 테스트,
  internal/publicdiscovery/opener*.go, internal/eventscoutserver/discovery.go,
  cmd/eventscout/main.go
- **간접**: cmd/eventscout/README.md, cmd/eventscout-server/README.md,
  DECISIONS.md, README.md, scripts/smoke-solar-public-discovery.sh,
  deploy/run-scout-discovery.sh (stretch)

## 에이전트 구성
- orchestrator 단독 (모듈 간 순차 의존이 강해 병렬 이득 없음; 루프 재작성이
  선행되어야 opener·배선·배포가 의미를 가짐)

## 구현 계획

### Phase 1: 액션 루프 — 안무를 모델로 이양 (예상: M-L)
> 목표: opener 없이 search/accept/done 3액션으로 루프 재작성. 이 Phase만으로
> "다음 행동을 모델이 정한다"가 코드·테스트로 성립.

#### Task 1.1: 액션 응답 파싱 `[TDD]`
- test: `internal/agent/discovery_action_test.go`
- impl: `internal/agent/discovery_action.go`
- **작업 내용**:
  - [x] RED: `{"action":"search","query":...}` / `{"action":"accept","selections":[{url,reason}...]}` / `{"action":"done"}` 파싱, 미지원 액션·malformed JSON·빈 query·빈 selections의 오류 분류 테스트
  - [x] GREEN: `parseActionResponse` — `decodeLastModelObject` 재사용, 액션별 검증 (리뷰 반영: Query/Reason에 `boundedRedactedText` 적용)
- **검증 방법**: 신규 테스트 GREEN, `go vet ./internal/agent`

#### Task 1.2: 액션 프롬프트 빌더 `[TDD]`
- test: `internal/agent/discovery_action_test.go` (같은 파일에 추가)
- impl: `internal/agent/discovery_model.go`
- **작업 내용**:
  - [x] RED: 프롬프트에 목표, 시도한 쿼리, 현재 후보 테이블(번호+제목+URL), 채택 목록, 잔여 예산(검색/open/모델 호출), 사용 가능 액션 목록이 포함되고 — opener 부재 시 open이 목록에서 빠짐을 검증
  - [x] GREEN: `actionModelRequest(profile, state)` 구현, 기존 proposal/judge 프롬프트 상수는 제거 대상으로 표시
  - [x] 후보 테이블 렌더링에 항목 수·문자 길이 상한 적용 (`boundedRedactedText` 재사용) — 리뷰 반영: `promptSafeLine`(개행·꺾쇠 제거)로 untrusted 블록 탈출 차단 + 회귀 테스트. 후속 보강: snippet/date/location 행 렌더링(메타데이터 상한)과 profile QueryTemplates/Locale/Language 힌트를 프롬프트에 복원
- **검증 방법**: 신규 테스트 GREEN

#### Task 1.3: run() 액션 루프 재작성 `[TDD]`
- test: `internal/agent/discovery_choreography_test.go` (신규) + `internal/agent/discover_test.go` (갱신)
- impl: `internal/agent/discover.go`
- **작업 내용**:
  - [x] RED: 스크립트된 fake 모델로 (a) search→search→accept→done, (b) 즉시 accept, (c) accept를 search보다 먼저 — 세 순서 모두 루프가 모델 반환 순서대로 실행함을 검증 (고정 안무 부재의 증명; (b)는 accept→search→accept→done 형태로 구현 — 빈 테이블 accept가 드롭 후 루프 지속)
  - [x] RED: 모든 경로에서 종료 보장 — done 없이 예산 소진 시 truncation 기록 후 종료
  - [x] GREEN: 턴 루프로 재작성 — 모델 호출 → dispatch(search/accept/done) → 상태·예산 갱신. 기존 `proposalModelRequest`/`judgeModelRequest` 고정 순서 제거 (grep 0건 확인)
  - [x] malformed 액션 응답은 1회 한정 재프롬프트(예산에 카운트), 재실패 시 terminal_reason 기록 후 종료 (`malformed_action` 신규 enum; 파싱 불능 2연속은 오류 반환, 허용 외 액션 2연속은 채택분 보존한 채 정상 종료)
  - [x] yield_trace 매핑: search/done을 반환한 호출 → `proposal_calls`, accept를 반환한 호출 → `judge_calls`, accept selections 파싱 → `judge_entries_parsed/dropped`. 의미 주석으로 명시 (11개 고정 필드 전수 유지)
- **검증 방법**: choreography 테스트 3종 GREEN + 기존 discover_test GREEN

#### Task 1.4: 예산 상한 재정의와 호환 테스트 갱신 `[TDD]`
- test: `internal/agent/discovery_compatibility_test.go`, `internal/agent/discovery_yield_trace_test.go` (갱신)
- impl: `internal/agent/discovery_types.go`
- **작업 내용**:
  - [x] `MaxSearches`(기존 MaxRounds 의미 승계, 하드캡 2 유지) + `MaxTurns` 하드캡(전 액션 합산, 기본 8) 정의, `DefaultDiscoverOptions` 갱신 + `MaxOpens` 타입 선점(기본 0, Phase 2 전까지 비활성)
  - [x] 기존 `Discover(...maxRounds...)` 시그니처 호환 유지 (maxRounds→MaxSearches 매핑)
  - [x] yield_trace 고정 필드 전수 존재 테스트 갱신 — 필드 삭제 없음을 검증
  - 이월 항목 → Task 2.3: `hardMaxDiscoveryModelCalls` 4→8 상향 (현재 모델 호출 상한이 MaxTurns보다 먼저 걸려 open 턴 여유가 없음; 서버 요청당 최악 비용 2배는 2req/10min quota 안에서 수용)
- **검증 방법**: `go test ./internal/agent -count=1` 전체 GREEN

#### Task 1.5: 액션 루프 안전성 검증 `[DOUBT]`
- claim: 재작성된 액션 루프는 모델이 어떤 액션 순서·비정상 응답을 반환해도 (1) 예산 상한(모델 호출·토큰·턴)을 넘지 않고 (2) 반드시 종료하며 (3) yield_trace 공개 계약의 기존 필드 의미를 보존한다.
- artifact: `internal/agent/` diff (Task 1.1~1.4)
- contract: 모든 terminal 경로가 `complete()`를 경유, 상한 초과 시 truncation 기록, yield_trace 고정 필드 전수 유지, 무한 루프 불가능 (MaxTurns 하드캡)
- **검증 방법**: /doubt 통과 (Contract 위반 0건)

### Phase 2: open 액션 — 후보 페이지 열람 (예상: M)
> 목표: 모델이 후보 페이지를 직접 열어 근거를 보강하는 행동을 얻는다.

#### Task 2.1: PageOpener 인터페이스 + open dispatch `[TDD]`
- test: `internal/agent/discovery_open_test.go`
- impl: `internal/agent/discovery_open.go`, `internal/agent/discover.go`
- **작업 내용**:
  - [x] RED: open 대상은 후보 테이블의 canonical URL만 허용 — 테이블 밖 URL은 fetch 없이 거부·카운트, opener nil이면 open 반환 시 무시·기록
  - [x] RED: open 성공 시 후보 title/근거 갱신 + prefilter 재평가로 judgeable 승격, open 예산 소진 시 액션 목록에서 제외
  - [x] GREEN: `PageOpener` 인터페이스(`Open(ctx, url) (OpenedPage, error)`) + dispatch 구현, `open_calls` 카운트
  - [x] open으로 읽은 본문은 고정 상한으로 잘라 프롬프트에 반영 (`maxPageRunes` 패턴 재사용)
- **검증 방법**: 신규 테스트 GREEN

#### Task 2.2: publicdiscovery opener 구현 `[TDD]`
- test: `internal/publicdiscovery/opener_test.go`
- impl: `internal/publicdiscovery/opener.go`
- **작업 내용**:
  - [x] RED: `candidateURLAllowed` 가드 통과 URL만 fetch, robots 불허·크기 초과·비HTML은 분류된 오류 반환, title/본문 요약 추출(기존 parser_html 재사용), 연락처 제거(`StripContacts`) 후 반환
  - [x] GREEN: 기존 bounded fetch 경로 재사용 구현
- **검증 방법**: 신규 테스트 GREEN, `go vet ./internal/publicdiscovery`

#### Task 2.3: CLI·서버 배선 `[TDD]`
- test: `internal/eventscoutserver/discovery_test.go` (갱신)
- impl: `cmd/eventscout/main.go`, `internal/eventscoutserver/discovery.go`
- **작업 내용**:
  - [x] CLI: public provider일 때 opener 주입, `-opens` flag (기본 3, 상한 3); fixture/tavily는 opener 없음
  - [x] `-rounds` flag 호환 유지 (MaxSearches로 매핑) — 배포된 `deploy/run-scout-discovery.sh`가 `-rounds 2 -promote`로 호출하므로 flag 제거·의미 변경 금지
  - [x] 서버: opener 주입 + open 예산 2 고정 (60s 데드라인 보호), 응답 `yield_trace.open_calls` 노출
  - [x] 서버 응답 계약 테스트: 기존 고정 필드 전수 + `open_calls` additive 검증
- **검증 방법**: `go test ./internal/eventscoutserver ./cmd/eventscout -count=1` GREEN

#### Task 2.4: 계약 문서화 + 결정 기록 `[Manual]`
- **작업 내용**:
  - [x] DECISIONS.md: 안무 이양 결정 (context/decision/consequences — 모델이 행동을 선택, 코드는 예산·경계 집행), yield_trace `open_calls` additive 확장, open의 SSRF·robots 경계 + 토큰 예약 500 결정 + 서버 오류 시 결과 폐기 기존 동작 명시
  - [x] cmd/eventscout/README.md, cmd/eventscout-server/README.md 갱신 (Task 2.2+2.3 구현자가 동기화)
- **검증 방법**: 문서에 새 액션 프로토콜·예산·계약 필드가 빠짐없이 기재

### Phase 3: 검증·배포 (예상: S-M)

#### Task 3.1: 로컬 전체 게이트 `[Manual]`
- **작업 내용**:
  - [ ] `go test ./...`, `go vet ./...`, `go build ./cmd/...`
  - [ ] `go test -race ./internal/agent/... ./internal/publicdiscovery/... ./internal/eventscoutserver/...`
  - [ ] `internal/api` diff 0건 확인 (read API 불변 게이트)
- **검증 방법**: 전부 GREEN, diff-check 통과

#### Task 3.2: 라이브 스모크 — 주체성의 실증 `[Manual]`
- **작업 내용**:
  - [ ] 운영자 키로 bounded 라이브 실행 (`scripts/smoke-solar-public-discovery.sh` 갱신 포함): accepted ≥ 기존 수율(5) 확인
  - [ ] 모델이 실제로 open을 선택한 실행 기록 1건 확보 → `.tasks/260728-solar-agency/evidence/`에 카운트·terminal_reason만 저장 (URL·본문 미포함, 기존 로그 정책 준수)
  - [ ] keyless 경로: 키 없이 스모크가 SKIPPED_CREDENTIAL_UNAVAILABLE로 안전 종료
- **검증 방법**: 증거 파일 존재 + 수율 회귀 없음

#### Task 3.3: 프로덕션 배포 `[Manual]`
- ⚠️ 프로덕션 배포 — 착수 직전 AskUserQuestion으로 확인 후 진행
- **작업 내용**:
  - [ ] linux/amd64 `eventscout` 빌드 → VPS 교체 (기존 바이너리 백업) — 배포 대상은 주간 타이머(`eventscout-discovery.timer`)가 부르는 CLI 바이너리
  - [ ] `deploy/run-scout-discovery.sh` 수동 1회 실행으로 신규 루프 동작 확인 (OK candidates=N 출력)
  - [ ] `deploy/verify.sh` ALL CHECKS PASSED
  - [ ] 참고: 익명 서버 `/v1/discover`는 현재 공개 라우팅되어 있지 않음 (Caddy는 `/mcp`만 프록시). 공개 노출은 별도 운영 결정(DECISIONS 2026-07-20)이라 이 작업의 범위 외 — `open_calls` 계약은 로컬 서버 테스트(Task 2.3)로 검증
- **검증 방법**: verify.sh 출력 + 타이머 스크립트 수동 실행 OK

#### Task 3.4: README·후기 갱신 `[Manual]`
- **작업 내용**:
  - [ ] README 아키텍처·후기 문단을 새 사실(모델이 행동 선택)로 갱신 — korean-copy 스킬 규칙 적용
- **검증 방법**: 후기 본문이 코드 사실과 일치 (choreography 테스트 이름 인용 가능 수준)

### Phase 4 (Stretch): 발견→PR 자동 연결 (예상: M)
> 목표: 주간 타이머의 채택 소스가 사람 머지 대기 PR로 자동 도착.
> 착수 조건: Phase 3 완료가 7/30 정오 이전일 것. 미충족 시 이 Phase는 제출 후로 이월.

#### Task 4.1: 토큰·권한 확인 `[Manual]`
- ⚠️ VPS 시크릿 추가 — 착수 전 AskUserQuestion (fine-grained PAT 발급 주체·권한 범위)
- **작업 내용**:
  - [ ] contents:write + pull_requests:write 최소 권한 PAT, VPS 환경 파일 0600 저장

#### Task 4.2: run-scout-discovery.sh 확장 `[Manual]`
- **작업 내용**:
  - [ ] discovery 채택 소스 → `-promote` 산출물 → 기존 카탈로그·열린 PR과 대조해 신규만 → 브랜치 생성 → PR 오픈 (본문에 Solar 판정 근거 + 산출물 3종)
  - [ ] 중복 실행 시 새 PR 0건 (idempotent) 확인
- **검증 방법**: 수동 1회 실행으로 PR 생성 확인, 재실행 시 미생성 확인, DECISIONS 기록

## 노력도 추정

| Phase | 규모 | 태스크 수 |
|-------|------|----------|
| Phase 1 | M-L | 5 |
| Phase 2 | M | 4 |
| Phase 3 | S-M | 4 |
| Phase 4 (stretch) | M | 2 |
| **합계** | **L (stretch 포함 L+)** | **15** |

## 테스트 전략

### 단위 테스트
- [ ] 액션 파싱: 정상 3액션 + open, malformed·미지원·빈 필드 분류
- [ ] choreography: 모델 주도 순서 3종이 그대로 실행됨 (안무 부재 증명)
- [ ] 예산: 각 상한 초과 시 truncation·terminal_reason·종료
- [ ] opener: SSRF 가드·robots·크기 상한·연락처 제거

### 통합 테스트
- [ ] eventscoutserver: 응답 계약 (기존 필드 + open_calls), 60s 데드라인 내 동작
- [ ] cmd/eventscout: provider별 opener 유무에 따른 액션 노출

### 수동 테스트
- [ ] 라이브 스모크 수율 (accepted ≥ 5), open 선택 실증 기록
- [ ] deploy/verify.sh ALL CHECKS PASSED

## Acceptance Criteria (인수 기준)

- [ ] AC-1: Given 스크립트된 fake 모델이 (즉시 accept), (search 2연속 후 accept), (search→accept→search→done) 순서를 반환할 때, When Discover 실행, Then 각 순서 그대로 dispatch가 실행되고 결과가 반영된다 (고정 안무 코드 경로 부재).
- [ ] AC-2: Given 모델이 done 없이 액션을 계속 반환, When 모델 호출·토큰·턴·open 중 어느 상한이든 도달, Then 루프가 종료되고 해당 truncation reason과 terminal_reason이 기록된다.
- [ ] AC-3: Given 익명 서버의 성공 discovery 응답, Then yield_trace에 기존 고정 필드 전부와 `open_calls`가 존재하고 모두 count-only다.
- [ ] AC-4: Given opener가 없는 provider(fixture/tavily), Then 프롬프트 액션 목록에 open이 없고, 모델이 open을 반환해도 fetch 없이 무시·기록된다.
- [ ] AC-5: Given 모델이 후보 테이블에 없는 URL의 open을 반환, Then fetch가 발생하지 않고 거부가 카운트된다.
- [ ] AC-6: `go test ./...` GREEN + `-race` 대상 패키지 GREEN + internal/api diff 0건.
- [ ] AC-7: 배포 후 `deploy/verify.sh`가 ALL CHECKS PASSED를 출력하고, VPS에서 `run-scout-discovery.sh` 수동 1회 실행이 OK로 끝난다 (`open_calls` 계약은 로컬 서버 통합 테스트로 증명).
- [ ] AC-8 (stretch): 주간 스크립트 1회 실행이 신규 채택 소스 PR을 만들고, 같은 입력으로 재실행 시 새 PR이 0건이다.

## Verification Strategy (검증 전략)

| AC | 검증 방법 | 측정 기준 |
|----|-----------|-----------|
| AC-1 | 단위 테스트 (choreography 3종) | 3 시나리오 전부 GREEN |
| AC-2 | 단위 테스트 (예산별) | 상한 4종 각각 truncation 기록 + 종료 |
| AC-3 | 통합 테스트 (서버 응답 계약) | 필드 전수 존재, 타입 count |
| AC-4 | 단위 테스트 | fetch mock 호출 0회 |
| AC-5 | 단위 테스트 | fetch mock 호출 0회 + 거부 카운트 1 |
| AC-6 | `go test ./...` / `-race` / git diff | 전부 GREEN, internal/api diff 0 |
| AC-7 | 수동 (배포 스모크) | ALL CHECKS PASSED 출력 |
| AC-8 | 수동 (스크립트 2회 실행) | 1회차 PR 1+, 2회차 PR 0 |

## 병렬 실행 그룹
- 그룹 A (순차): Task 1.1 → 1.2 → 1.3 → 1.4 → 1.5
- 그룹 B (A 완료 후, 2.1 → {2.2 ∥ 2.4 문서 초안} → 2.3 → 2.4 확정)
- 그룹 C (B 완료 후 순차): Task 3.1 → 3.2 → 3.3 → 3.4
- 그룹 D (C 완료 후, 착수 조건 충족 시): Task 4.1 → 4.2

---

## 리뷰 로그

> 리뷰 일시: 2026-07-28

### 총평
구조는 탄탄하나 배포 표면 오인(공개 /v1/discover 부재)과 액션 루프 특유의
내성(재프롬프트·프롬프트 상한) 두 갈래를 보강해야 실행 가능한 계획.

### 반영 완료
- [x] [중간][계약] AC-7·Task 3.3이 존재하지 않는 공개 `/v1/discover` 경로를 참조 → 배포 대상을 주간 타이머용 eventscout CLI로 정정, open_calls 검증은 로컬 서버 테스트로 이동
- [x] [중간][에러 핸들링] malformed 액션 1건이 실행 전체를 종료 → Task 1.3에 예산 카운트되는 1회 재프롬프트 추가
- [x] [중간][성능·보안] 턴마다 후보 테이블·open 본문이 프롬프트를 무한히 키울 수 있음 → Task 1.2, 2.1에 렌더링·본문 상한 항목 추가
- [x] [중간][호환] 배포된 run-scout-discovery.sh의 `-rounds 2 -promote` 호출 → Task 2.3에 flag 호환 유지 항목 추가

### 미반영 (참고용)
- [낮음] `-opens` flag 이름은 `-open-budget`이 더 명시적이나 기존 `-rounds` 명명 관성 유지
- [낮음] 라이브 스모크 증거에 액션 분포(search/open/accept 횟수) 기록 권장 — Task 3.2 수행 시 자연 포함될 것

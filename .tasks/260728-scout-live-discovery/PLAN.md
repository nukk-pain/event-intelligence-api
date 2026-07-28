# PLAN: scout-live-discovery — keyless 주간 자동 발견 (seed 확장 + VPS 타이머)

- worktree: false
- 승인: 자동 승인 (/start autonomous). 사용자 결정: 서드파티 검색 API 불사용
  (Tavily 대안 조사 결과 Brave는 2026-02부터 카드 필수 — 전부 기각),
  실행 위치는 VPS systemd 타이머.
- 시작일: 2026-07-28

## 요구사항 재진술

### 핵심 요구사항
1. 서드파티 검색 API 없이 발견 범위를 넓힌다: public provider의 seed 카탈로그에
   국내 허브를 추가하고 (AKEI 전시산업진흥회, EXCO — robots 200 확인.
   GEP는 robots 전면 금지로 제외, BEXCO는 robots 302라 보류) `MaxSeeds`를
   6→8로 상향 (seed 수 == MaxSeeds 정확 일치 불변식을 유지하기 위해 headroom 없이).
2. VPS systemd 주간 타이머: eventscout(public provider, solar backend)를 돌려
   `-promote` 패킷을 `/srv/developer/events-intel/discovery/<날짜>/`에 저장.
   Solar 키 없으면 스모크 스크립트처럼 깨끗한 스킵(exit 0).
3. 승인 흐름 불변: 패킷은 사람이 검토, 추가 커밋은 코드 리뷰 경유.
4. eventscout 바이너리를 VPS에 배포 대상으로 추가 (linux 빌드).

### 범위 제외
검색 API 프로바이더 추가(사용자 기각), BEXCO/GEP seed, Telegram 봇 연동
(패킷+journal로 시작, 필요 시 후속), 승격 자동 커밋.

## 위험 분석
| 위험 | 영향 | 대응 |
|---|---|---|
| seed 증가로 크롤 예산 소진·수확 저하 | 중간 | 기존 MaxHTMLPages 등 총예산 불변 — 예산 내 분배. yield_trace로 관측 |
| 신규 seed robots 변경 | 낮음 | robots_unavailable 계측이 이미 존재, 크롤 안 함 |
| 7/31 후 Solar 키 만료로 타이머 실패 | 확실 | 키 부재 시 exit 0 스킵. 만료 키(401)는 journal에 실패 기록 — Stage 2 키로 교체 시 자동 복구 |

## 구현 계획

### Task 1: seed 확장 + MaxSeeds 상향 `[TDD]`
- test: `internal/publicdiscovery/catalog_test.go` (기존 파일 확인 후 추가)
- impl: `internal/publicdiscovery/catalog.go`, `internal/publicdiscovery/types.go`
- **작업 내용**:
  - [x] RED: 카탈로그에 AKEI·EXCO 포함 + seed 수 8 + MaxSeeds=8 기대 테스트
  - [x] GREEN: seeds 추가(버전 문자열 갱신), MaxSeeds 6→8
- **검증**: publicdiscovery 전체 테스트 GREEN

### Task 2: 주간 타이머 아티팩트 `[Manual]`
- impl: `deploy/eventscout-discovery.service`, `deploy/eventscout-discovery.timer`,
  `deploy/run-scout-discovery.sh`
- **작업 내용**:
  - [x] 스크립트: 키 부재 시 스킵(exit 0), 실행 시 `-rounds 2 -promote
        /srv/developer/events-intel/discovery/$(date +%F)/` + journal 요약
  - [x] service(oneshot, solar.env EnvironmentFile) + timer(주 1회, Persistent)
- **검증**: shellcheck 수준 육안 + VPS 1회 수동 기동

### Task 3: 로컬 검증 + 배포 `[Manual]` — 배포는 AskUserQuestion 게이트
- **작업 내용**:
  - [x] 로컬에서 확장 카탈로그로 라이브 1회: 신규 seed에서 후보가 yield_trace에
        잡히는지 확인
  - [x] go build/vet/test 전체 GREEN
  - [x] (게이트) eventscout linux 빌드 → VPS 배치 → units 설치 → 수동 1회 기동
        → 패킷 생성 확인 → timer enable
- **검증**: VPS journal에 outcome 기록 + discovery 디렉토리에 3파일

## Acceptance Criteria
- [ ] AC-1: 확장 카탈로그 파싱이 유효하고 seed 8개, publicdiscovery 테스트 GREEN
- [ ] AC-2: 로컬 라이브 실행의 seed_outcomes 합계가 8이고 신규 seed가 계정됨
- [ ] AC-3: VPS 수동 1회 기동에서 discovery/<날짜>/에 패킷 3파일 생성
      (채택 0건이면 journal에 스킵 사유 기록)
- [ ] AC-4: Solar 키 없는 환경에서 스크립트가 exit 0 스킵
- [ ] AC-5: go test ./... 전체 GREEN

## Verification Strategy
| AC | 방법 | 기준 |
|---|---|---|
| AC-1 | go test | GREEN |
| AC-2 | 로컬 실행 yield_trace | seed_outcomes 합=8 |
| AC-3 | VPS journal + ls | 파일 존재 또는 스킵 사유 |
| AC-4 | env 비운 로컬 실행 | exit 0 |
| AC-5 | go test ./... | GREEN |


---

## 리뷰 로그 (구현 후 통합 리뷰, 2026-07-28)
- [x] [중간] run.json → run.txt (배너+JSON 혼합 출력이라 .json 오해 소지) + 같은 날 재실행 덮어쓰기 허용 주석
- [x] [중간] PLAN의 6→10 표기를 실제 선택(6→8, 정확 일치 불변식)으로 정정
- [낮음] 같은 날 재실행 덮어쓰기 — 주석으로 수용 명시

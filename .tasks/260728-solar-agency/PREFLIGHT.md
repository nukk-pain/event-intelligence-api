# PREFLIGHT: solar-agency

> 조사 일시: 2026-07-28

## AC 검증

| AC | 판정 | 비고 |
|----|------|------|
| AC-1 | 통과 | fake 모델 3시나리오 — 측정 가능, 독립 검증 가능 |
| AC-2 | 통과 | 상한 4종(모델 호출·토큰·턴·open) 각각 truncation 검증 |
| AC-3 | 통과 | 서버 응답 계약 테스트로 기계 검증 |
| AC-4 | 통과 | fetch mock 호출 0회 — 측정 가능 |
| AC-5 | 통과 | 동일 |
| AC-6 | 통과 | 명령 그대로 실행 가능 |
| AC-7 | 수정됨 | 공개 /v1/discover 부재 발견 → 타이머 스크립트 실행 + verify.sh로 정정 |
| AC-8 | 통과 (stretch) | 2회 실행 대조 — 측정 가능 |

## 영향 범위 매핑

- **직접 (수정)**: internal/agent 7~9개 파일, internal/publicdiscovery 2개(신규),
  internal/eventscoutserver/discovery.go, cmd/eventscout/main.go
- **간접 (갱신)**: cmd/eventscout/README.md, cmd/eventscout-server/README.md,
  DECISIONS.md, README.md, scripts/smoke-solar-public-discovery.sh
- **소비자(cross-surface)**:
  - `deploy/run-scout-discovery.sh` — `-rounds 2 -goal ... -promote` 호출.
    flag 호환 유지 필수 (Task 2.3에 반영됨). `-promote` 산출물 스키마는 불변.
  - `internal/eventscoutserver` — `agent.DiscoverWithOptions` 소비.
    `DefaultDiscoverOptions` 의미 변화는 옵션 매핑으로 흡수.
  - `internal/api` — 무관 (불변 게이트, AC-6 diff 0건).
  - `internal/pipeline`의 enricher 경로 — `agent.Run`(enrich.go) 사용,
    discovery 루프와 별개 코드 경로라 영향 없음. 회귀는 `go test ./...`로 커버.

## 데이터 감사

- 스키마 변경 없음 (SQLite 스토어 무관 — discovery는 store에 쓰지 않음).
- `-promote` 산출물(JSONL/snippet/allowlist diff) 스키마 불변.
- 로그 정책: 신규 open 액션도 기존 count-only 정책 승계 — URL·본문·목표 문장을
  구조화 로그·증거 파일에 남기지 않는다 (Task 3.2에 명시됨).

## 비즈니스 규칙 확인

- read API LLM-free (CLAUDE.md 하드 제약) — internal/api 무변경 게이트로 보장.
- "Source promotion goes through code review" (DECISIONS 2026-07-28) —
  Phase 4의 PR 자동화도 사람 머지 필수로 설계됨. 위반 없음.
- 익명 서버 quota·로그 정책 (DECISIONS 2026-07-20) — 서버 quota 미변경,
  yield_trace는 additive 확장만.
- 연락처 제거(StripContacts) — open 본문에도 적용 (Task 2.2에 명시됨).

## 발견 사항 (PLAN 반영 완료)

1. 익명 서버는 공개 라우팅되어 있지 않음 (Caddy `/mcp`만). 배포 표면 정정.
2. 배포 스크립트의 flag 의존 (`-rounds`) — 호환 항목 추가.
3. 추가 Task 필요 없음 — 기존 Task 보강으로 충분.

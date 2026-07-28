# PREFLIGHT: ask-ui

> 조사 일시: 2026-07-28

## AC 검증
- AC-1~AC-5 모두 측정 가능·독립 검증 가능. 모호어 없음. ✅
- AC-2 는 검증 시 운영자 쿼터 10회를 실제 소진함 — 라이브 검증은 1회로 제한하고
  429 경로는 코드 리뷰 + 로컬 스텁으로 갈음 가능 (검증 전략에 반영됨).

## 영향 범위 매핑
- 직접(6): static/index.html, static/ask.js(신규), static/index.css,
  static/embed.go, internal/api/api.go, internal/api/root_test.go
- 간접(2): 배포 바이너리(정적 자산 임베드), deploy/verify.sh 는 변경 불필요
  (`/` 200 + HTML 체크로 커버, 자산 서빙은 root_test.go 가 커버)
- cross-service(0): eventmcp·Caddy·systemd 무변경. DECISIONS.md "Remote MCP
  endpoint at events.nukk.net/mcp (2026-07-27)" 결정이 이미 공개 경계를 승인 —
  본 작업은 그 경계 위의 프론트일 뿐, 새 결정 불필요.

## 데이터 감사
- 해당 없음 (스키마·DB 변경 없음).

## 비즈니스 규칙 확인
- "read API 는 LLM-free 유지" — api.Router 에 신규 엔드포인트 없음, 준수.
- ask_events 쿼터(10/10분, 60/일)·키 운영자 소유 — 무변경, 준수.
- API-First(전역 규칙 7) — 기능의 API 표면은 기존 /mcp ask_events 가 이미 담당,
  UI 는 그 위의 thin client. api-catalog 갱신 대상 아님(API 신설·변경 없음).

## 추가 Task
- 없음 (기술 리뷰 반영분 외 신규 발견 없음).

## 판정
✅ 준비 완료 — /execute-plan 진행 가능. Phase 3(프로덕션 배포)만 AskUserQuestion
게이트에서 확인.

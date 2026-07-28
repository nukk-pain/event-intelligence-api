# PREFLIGHT: scout-promote

> 조사 일시: 2026-07-28

## AC 검증
- AC-1~5 측정 가능·독립 검증 가능. AC-4는 리뷰에서 같은 빌드 기준으로 재정의됨. ✅

## 영향 범위 매핑
- 직접(6): cmd/eventscout/{main.go,promote.go,promote_test.go},
  internal/fetch/{production_hosts.go,production_hosts_test.go},
  cmd/eventsintel/main.go
- 간접(2): cmd/eventscout/README.md, DECISIONS.md
- cross-service(0): ingest 런타임·read API·MCP·배포 무변경. allowlist는 위치만
  이동하고 내용 동일(테스트로 잠금).

## 데이터 감사
- 해당 없음 (DB·스키마 무관, 파일 생성은 사용자가 지정한 로컬 디렉토리만).

## 비즈니스 규칙 확인
- 공개 API read-only·cache-first: 무변경. ✅
- SSRF allowlist 감사 가능성: 코드 내 exported 변수로 유지, env 확장 없음. ✅
- missing-field honesty: 빈 필드 유지 + invalid_url/slug_collision 명시 표기. ✅
- API-First 예외 근거 PLAN 명시됨 (운영자 전용 코드 리뷰 워크플로우). ✅

## 추가 Task
- 없음.

## 판정
✅ 준비 완료 — /execute-plan 진행 가능. 배포 게이트 없음.

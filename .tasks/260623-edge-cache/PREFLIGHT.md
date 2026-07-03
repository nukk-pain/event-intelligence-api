# PREFLIGHT: edge-cache

> 2026-06-23 — /start autonomous 자동 사전조사

## AC 검증
- AC-1~8 전부 측정 가능·테스트 가능. 모호어 없음. ✅ 통과
- 참조 대상 존재 확인: `writeJSON`(events.go:164), `Respond`(render.go:60), `handleOpenAPI/handleLLMsTxt`(meta.go:182,190), `handleListSources`(detail.go:69), ETag 헤더 복사(middleware.go:129-133), `runIngest` rep 반환(main.go:167-172), `FromEnv`(config.go:89) — 모두 실재.

## 영향 범위
- 직접: internal/api/{render,events,detail,meta,api}.go, internal/config/config.go, cmd/eventsintel/main.go, 신규 internal/cfpurge/
- 간접: static/openapi.yaml, deploy/(README + cache-rule 문서)
- cross-service: 없음 (단일 바이너리). 외부 의존: Cloudflare API(런타임 ingest 시점, 비치명적)

## 데이터 감사
- 스키마 변경 **없음**. DB 마이그레이션 **없음**. 데이터 이상 점검 대상 아님 (응답 헤더/배포 설정 변경뿐).

## 비즈니스 규칙 / 불변식 확인
- read-only·cache-first 유지 ✅ (오히려 강화)
- no live LLM on reads 유지 ✅
- clinic/EMR/medical-platform 데이터 무관 ✅
- 공개 API 계약: 응답 **본문** 불변, 헤더만 추가(Cache-Control) → breaking change 아님 ✅
- ETag/304/Vary/quota/동시성 동작 보존 (테스트로 고정)

## 추가 Task
- 없음 (리뷰 반영분은 기존 Task에 흡수)

## 판정
✅ 구현 준비 완료. Phase 1·2는 자율 진행, Phase 3.2/3.3은 게이트.

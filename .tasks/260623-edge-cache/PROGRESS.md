# PROGRESS: edge-cache

## 상태: 완료 ✅ (AC 8/8) — feat/edge-cache 브랜치, 프로덕션 라이브

## AC 결과 (8/8)
- AC-1~2 Cache-Control 전 읽기 엔드포인트: ✅ (테스트 + origin/edge 라이브)
- AC-3 에러 무캐시: ✅ (테스트 + 라이브 404)
- AC-4 304 Cache-Control/Vary 보존: ✅ (테스트)
- AC-5 purge env 미설정 no-op: ✅ (테스트/cfpurge)
- AC-6 변경 시 purge 비치명: ✅ (단위 테스트 + purge 토큰 라이브 검증)
- AC-7 재방문 cf-cache-status=HIT: ✅ (라이브: events/HTML/schema 전부 MISS→HIT)
- AC-8 make test/vet green: ✅

## 라이브 결과
- 엣지 HIT 시 TTFB ~0.45s, 오리진 왕복 제거. 이전: 매 요청 DYNAMIC 0.6~1.2s + 변동.
- Cache Rule 적용(success:true), MD bypass 동작, purge_everything success:true
- ingest unit: EnvironmentFile=/srv/developer/events-intel/cf-purge.env (drop-in) → 데이터 변경 시 자동 purge

## 남은 사항 (사용자 판단)
- feat/edge-cache 브랜치 미머지: 프로덕션 바이너리는 이미 새 코드지만 main은 아직. 머지 시점 사용자 결정.

## 포커스
프로덕션 페이지 딜레이 개선: 데이터 엔드포인트 Cache-Control + Cloudflare 엣지 캐싱 + ingest 후 purge

## 근거 (라이브 측정, 2026-06-23)
- `/`(HTML)·`/api/v1/events`: cf-cache-status=DYNAMIC → 매 오픈마다 DO VPS 오리진 풀 왕복(TTFB 0.6~1.2s)
- `/assets/*`만 엣지 캐시(HIT)
- 코드상 Cache-Control은 handleRoot·staticAsset에만(api.go:99,118), 데이터 핸들러 전무
- ETag 미들웨어는 304 직전 버퍼 헤더 복사 → 핸들러 Cache-Control이 200·304 보존됨

## 완료
- PLAN.md / PREFLIGHT.md 작성, 구조검증 APPROVED, 9-렌즈 리뷰 반영
- [x] Phase 1: Cache-Control (cache.go const+helper, writeJSON/Respond-MD/openapi/llms/root/asset 배선)
- [x] Phase 1: cache_test.go — 헤더 존재/에러 무캐시/MD/304·Vary 보존 (4 test, 전부 PASS)
- [x] Phase 2: config CFPurge 필드 + FromEnv + config_test (PASS)
- [x] Phase 2: internal/cfpurge (PurgeEverything, env-gated/no-op/non-2xx, httptest) (PASS)
- [x] Phase 2: cmd ingest 배선 ingestChangedData + purge 호출 + main_test (PASS)
- [x] Phase 3.1: deploy/cloudflare-cache-rule.md + apply-cache-rule.sh (dry-run 기본)
- [x] Phase 3.4: DECISIONS.md 결정 기록 + deploy/README.md 운영 절차

## 검증 (Iron Law)
- `go build ./...` OK, `go vet ./...` OK
- `go test -race ./...` → 신규 코드 전부 PASS. 실패는 **pre-existing root_test 2건뿐**
  (에셋 ?v= 버전버스팅 후 root_test.go 단언 미갱신 — commit 8fcb4c7 drift, 본 작업과 무관)

## Doubt 결과 (Task 1.3 [DOUBT][TDD])
- claim: Cache-Control 부여 후 304 보존 + Accept 협상 미오염 + 신선도 위반 없음
- RECONCILE: (a) 304 Cache-Control/Vary 보존 → 테스트로 증명(valid, 충족). (b) Accept 협상 충돌 → origin은 Vary:Accept 부여, 엣지 충돌은 Cache Rule의 text/markdown bypass로 차단(valid trade-off, Phase 3.1 문서화). (c) 신선도 → s-maxage 상한 + ingest purge(valid, 충족). 다음 액션 없음.

## 완료 (게이트 승인 후)
- [x] root_test drift 수정 → 별도 커밋 ed4a0f8, 전체 스위트 green
- [x] edge-cache 커밋 9dae00c (코드+테스트+문서, apply 스크립트 수정 amend)
- [x] Task 3.3 바이너리 배포: 리눅스 크로스컴파일 → developer-vps scp+백업+재시작
  - origin/공개 URL에 Cache-Control + Vary 노출, 404 무캐시 확인 ✓
  - 브라우저 캐싱(max-age=120) 즉시 활성

## 블로커 — Cloudflare 토큰 권한
- Task 3.2 Cache Rule 적용 시도 → `Authentication error (code 10000)`: 기존 토큰
  (~/.config/cloudflare/nukk-net-token)은 **Zone DNS edit 전용**. Cache Rules·Cache Purge 권한 없음.
- 필요: Zone(nukk.net) → Cache Rules:Edit + Cache Purge:Purge 권한 신규 토큰.
- 이게 있어야 (a) 엣지 캐싱(cf-cache-status HIT, 최대 효과) (b) ingest purge env 설정 가능.
- 그 전까지: 엣지는 DYNAMIC 유지, 브라우저 캐싱만 효과.

## 다음 단계
- 사용자가 신규 CF 토큰 발급 → `deploy/apply-cache-rule.sh --apply` + ingest unit purge env 설정 → HIT 검증

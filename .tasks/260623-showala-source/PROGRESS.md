# PROGRESS: SHOWALA 소스 어댑터 + cross-source dedup

## 상태: 완료 ✅ (AC 7/7) — 커밋 대기

## 완료
- 원인 분석: 로보월드가 list.do에 미등록이라 KINTEX 어댑터가 발견 못 함
- 발견 경로 결정: SHOWALA 포털 어댑터 추가 (사용자 선택)
- 중복 처리 결정: ingest-time 콘텐츠키 dedup, venue 권위 소스 우선 (사용자 선택)
- PLAN/PREFLIGHT 작성, 구조검증 APPROVED
- Phase 0: 실제 showala HTML fixture 캡처 (목록/proc fragment/로보월드 상세)
- Task 1.0: Fetcher에 Referer 헤더 additive 추가 (+회귀 테스트)
- Task 1.1-1.3: showala 어댑터 (목록 KINTEX필터+조기종료+AJAX Referer 페이징, 상세 raw 파싱) + 골든 테스트
- Task 2.1: normalize venue-name 매핑 (showala KINTEX → venue_id "kintex") + 회귀 테스트
- Task 3.x: cross-source dedup — migration(0007/0008/0009), ContentKey+이름정규화, reconcileCluster(권위랭크), ApplyBatch 통합, 읽기경로(list/detail/sources/change feed) superseded 제외
- Task 3.0: ADR 0003 작성 (docs/decisions/0003-cross-source-content-key-dedup.md)
- Task 5.1: 실어댑터→normalize→store 통합 테스트
- Task 4: main.go 배선 (register + allowlist showala.com + SourceConfig)
- Task 5.2: 공개 계약 동기화 (llms.txt aggregator/dedup 노트; provenance type=aggregator)
- 라이브 스모크: 실제 showala.com에서 KINTEX 미래 53건 발견, 로보월드(showala-3219) 포함 확인

## Doubt 결과 (Task 3.4 [DOUBT])
fresh-context adversarial review가 3개 실제 위반 발견 → 전부 수정 + 회귀 테스트:
1. canonical의 content_key 변경 시 옛 클러스터가 0 canonical로 고아화 → oldKey 재reconcile (loadCanonical이 content_key 반환). 테스트: TestDedupCanonicalKeyChangeSelfHeals
2. change feed가 superseded 이벤트 변경을 노출 → ListChanges에 EXISTS(superseded=0) 필터. 테스트: TestDedupChangeFeedExcludesSuperseded
3. 마이그레이션 부분적용 크래시 창 → 컬럼별 파일 분리(0007/0008) + idempotent 인덱스 파일(0009). 테스트: TestMigrateIdempotentWithDedupColumns
- editionEnRe word-boundary 강화(false-merge 방지). B/C/D 항목: B 방어적 수정, C 기존 dead code(범위 밖), D 설계상 의도.

## AC 달성 (7/7)
- AC-1 로보월드 발견·파싱: TestParseRobotworldDetail, 라이브 스모크 ✅
- AC-2 KINTEX 스코프+조기종료: TestDiscoverKintexScopeEarlyStopAndReferer ✅
- AC-3 cross-source 1건: TestDedupReadPathHidesSuperseded, TestShowalaEndToEndDedup ✅
- AC-4 SHOWALA 단독 노출: 동상 + 라이브 스모크 ✅
- AC-5 기존 단일소스 회귀 없음: 전체 스위트 green, TestNormalize_SingleVenueSourceMapsByID ✅
- AC-6 idempotent: TestDedupIdempotent ✅
- AC-7 live LLM 없음/headless 없음: 정적 goquery만, read 경로 LLM 없음 ✅

## 검증
- make build / make vet / make test 통과
- go test -race ./internal/store ./internal/fetch ./internal/normalize ./internal/sources/showala ./internal/pipeline 통과

## 다음 단계
- 커밋 (프로덕션 배포는 별도 확인 게이트 — 미실행)
- 배포 시: 첫 ingest에서 기존 kintex/coex 행에 content_key 채워지고 dedup 활성화

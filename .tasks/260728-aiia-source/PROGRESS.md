# PROGRESS: aiia-source

## 상태: 계획 수립 완료 — 자동 승인 (/start autonomous)

## 완료
- 승격 결정 (AskUserQuestion): AIIA 승격, KIRIA 보류
- 코드베이스 조사: Source 계약, coex/showala 패턴, SourceConfig, 등록 지점
- PLAN.md 작성

## 다음 단계
- 구조 검증 → 기술 리뷰 → preflight → 구현
- Phase 3 배포는 AskUserQuestion 게이트

## 상태: 구현·검증 완료 — 배포 게이트 대기 (2026-07-28)

- Task 0.1 [PoC]: 실페이지 fixture 3종 + 합성 dated fixture 확보
- Task 1.1~1.2 [TDD]: Discover(키워드 필터)·Parse(본문 한정 날짜) RED→GREEN
- Task 1.3 [TDD]: allowlist(k-ai.or.kr)·SourceConfig·Register, race 포함 GREEN
- Task 1.4 [TDD] (발견 작업): k-ai.or.kr가 ECDHE 없는 RSA-KEX 전용 서버라
  Go 기본 TLS로 접속 불가 → WithLegacyTLSHosts 호스트 한정 옵션 구현
  (평문 다운그레이드 대신 PFS 없는 암호화 선택, ingest fetcher에만 배선)
- Task 2.1 [Manual]: aiia 단독 라이브 ingest — discovered=2 parsed=2 stored=2,
  ingest_error 0. aiia-408 Tech-Day → ["ai"]/excluded=0,
  aiia-406 설명회 → taxonomy 밖 excluded=1 (정책 일치)
- Task 2.2 [Manual]: go vet/test 전체 GREEN, CLAUDE.md 어댑터 목록 갱신,
  REVIEW-NOTES 승격 결과 기록
- AC-1~4 PASS. AC-5는 배포 후.

## 상태: 완료 ✅ (2026-07-28)
- 배포: 바이너리 교체 + 수동 ingest 1회 — source=aiia stored=2, 타 소스 정상,
  Solar 보강 dates 18/actions 21 filled
- AC-5 PASS: deploy/verify.sh ALL CHECKS PASSED, /api/v1/events/aiia-408 라이브
  서빙 확인 (date_confidence=low, 일정 미정 — 이미지 공고라 정직한 상태)
- 2-stage review: Spec APPROVED / Quality가 낡은 날짜 오추출 가능성 지적 →
  라벨 우선·모호 시 nil 전략으로 수정, 회귀 테스트 2건 추가 후 GREEN

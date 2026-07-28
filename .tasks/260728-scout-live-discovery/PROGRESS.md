# PROGRESS: scout-live-discovery

## 상태: 계획 수립 완료 — 자동 승인 (/start autonomous)

## 완료
- 검색 API 대안 조사: Brave 무료 티어 폐지(2026-02, 카드 필수), Naver/Serper는
  키 필요 → 사용자 결정으로 API 전면 기각, keyless 설계로 전환
- 허브 robots 실사: AKEI 200 허용, EXCO 200(Googlebot 한정 규칙), BEXCO 302
  보류, GEP 전면 금지 제외
- 제약 확인: MaxSeeds=6 상한, strict 모드는 robots 미확보 origin 크롤 안 함
- PLAN.md 작성

## 다음 단계
- 구조 검증 → Task 1(TDD) → Task 2 → 로컬 검증 → 배포 게이트

## 상태: 완료 ✅ (2026-07-28)
- Task 1: seed 8개(AKEI·EXCO 추가) + MaxSeeds 6→8, publicdiscovery GREEN
- Task 2: run-scout-discovery.sh + systemd service/timer (키 부재 스킵 exit 0)
- Task 3: 로컬 라이브 — seed_outcomes 합 8, 채택 6건 중 **akei.or.kr가 진짜
  신규 호스트로 패킷에 등장** (keyless 발견 E2E 증명). VPS 배포 + 수동 1회
  기동 candidates=6 패킷 생성, 타이머 enable (다음: 2026-08-03 월 06:35 UTC)
- 통합 리뷰: run.json→run.txt 개명, PLAN 6→10 표기 정정, 덮어쓰기 수용 주석
- AC-1~5 전부 PASS
- 참고: 7/31 키 만료 후 타이머는 journal에 실패 기록, Stage 2 키 교체 시
  자동 복구. 첫 검토 대상 후보는 AKEI(akei.or.kr) — 승격 여부는 사용자 판단

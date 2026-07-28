# PROGRESS: scout-promote — 코드 경유 소스 승격 경로

## 상태: 리뷰+사전조사 완료 — 자동 승인 (/start autonomous)
- 구조적 검증 APPROVED, 기술 리뷰 중간 3건 반영, PREFLIGHT 준비 완료 판정

## 완료
- 아키텍처 확정 (AskUserQuestion): 코드 경유 승격, 런타임 seed 로딩 기각
- 코드베이스 조사: catalogEvent 구조, allowlist 위치, DiscoveredSource 필드
- PLAN.md 작성

## 진행 중
- 구조적 검증 → 기술 리뷰 → preflight 자동 체이닝

## 다음 단계
- `/execute-plan .tasks/260728-scout-promote/PLAN.md`
- 배포 게이트 없음 (로컬 CLI 기능)

## 상태: 완료 ✅ (2026-07-28)

## 구현 및 AC 달성
- Task 1.1: allowlist를 fetch.ProductionAllowedHosts로 SSOT 추출, 45개 호스트
  1:1 동일 검증 (git show 대조 diff 0)
- Task 1.2: writePromotionFiles — 3파일 생성, %q 이스케이프, go/format 검증,
  슬러그 충돌·invalid_url·already_allowlisted 플래그
- Task 1.3: -promote 플래그, 미지정 시 호출 없음
- Task 2.1: fixture 라이브 실행(Solar 판정 2콜)으로 3파일 생성 + 스니펫을
  catalog_promoted.go로 라운드트립 → go build 통과 → 원상 복구
- Task 2.2: README promote 절 + DECISIONS.md 결정 기록
- 2-stage review: Spec APPROVED / Quality가 실제 엣지 버그 2건 발견
  (깊은 슬러그 충돌 시 접미사 누적 -2-3, 심볼 전용 제목의 bare benchmark- ID)
  → 회귀 테스트 추가(RED 재현) 후 고정 stem 카운터·host/untitled 폴백으로 수정,
  전체 GREEN
- AC-1 PASS(fixture 실행: 4소스 → jsonl 4행 + 3파일), AC-2 PASS(라운드트립 빌드),
  AC-3 PASS(신규 호스트만: www.k-ai.or.kr, www.kiria.org), AC-4 PASS(구조 stdout
  diff 0 — reason 문구 차이는 플래그와 무관한 모델 실행 간 변동임을 두 번 실행
  대조로 확인), AC-5 PASS(go test 전체 GREEN + 호스트 목록 1:1)

## 미커밋
- 변경은 로컬에만 있음. /secure-commit 대기.

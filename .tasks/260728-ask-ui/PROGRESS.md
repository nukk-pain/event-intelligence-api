# PROGRESS: ask-ui — 자연어 질의 UI

## 상태: 리뷰+사전조사 완료 — 자동 승인 (/start autonomous)

## 완료
- 범위 확정 (AskUserQuestion): 자연어 질의 UI, events.nukk.net 공개
- 코드베이스 조사: /mcp 무상태 단일 POST 확인, 임베드 자산 4-레이어 구조 파악
- PLAN.md 작성
- 구조적 검증 통과 (plan-document-reviewer: APPROVED)
- 기술 리뷰 완료 (중간 2건 반영: 오류 경로 가드, 진행 표시/maxlength)
- 사전 조사 완료 (PREFLIGHT.md — 준비 완료 판정, 추가 Task 0건)

## 다음 단계
- `/execute-plan .tasks/260728-ask-ui/PLAN.md` 즉시 실행
- Phase 3 배포는 AskUserQuestion 게이트에서 별도 확인

## Phase 0 Gate
✅ PASS at 2026-07-28 — Task 0.1 [PoC]: initialize 없이 라이브 /mcp 단일
tools/call POST 200 + count 필드 확인 (search_events, 쿼터 미소모).

## Task 1.1 산출물 — Design Brief
- 산출물 유형: 웹 페이지 섹션 (기존 행사 브라우저 홈에 추가)
- 대상 사용자: 행사를 찾는 창업자, 운영자 등 일반 방문자
- 사용 환경: 데스크톱, 모바일 브라우저
- 첫 화면에서 알아야 할 것: 질문 한 줄로 행사를 찾을 수 있다는 사실. 기존
  브라우저(필터+그리드)의 주인공 자리는 유지
- 가장 중요한 행동: 질문 입력 후 "행사 찾기" 버튼
- 반드시 보여야 할 정보: 결과 건수, 행사명, 기간, 장소, 원문/등록 링크
- 배치 결정: 헤더 아래, 필터 위에 한 줄짜리 가벼운 ask 바. 결과는 ask 섹션
  안에 자체 목록으로 렌더하고 "지우기"로 닫음. 기존 그리드는 건드리지 않음
- 원칙 5개: (1) ask 섹션의 주 행동은 버튼 하나 (2) 오류는 원인과 다음 행동을
  함께 (3) 색·간격은 theme.css 토큰 역할 재사용 (4) word-break keep-all 상속
  확인 (5) 로딩, 빈 결과, 429, 네트워크 오류 상태 전부 처리
- 검증 방법: 로컬 렌더(데스크톱+375px) + 배포 후 라이브 질의 1건

## Task 1.1 산출물 — 카피 (korean-copy 8패턴 자가 점검 통과)
- 리드: "궁금한 걸 그대로 물어보세요"
- placeholder: "다음 달 서울 AI 행사 뭐 있어?"
- 버튼: "행사 찾기"
- 진행: "찾는 중이에요"
- 0건: "조건에 맞는 행사가 없어요. 질문을 조금 바꿔보세요."
- 429: "질문이 몰려 잠시 쉬어가요. 약 N분 뒤에 다시 물어보세요."
- 일반 오류: "지금은 답을 가져오지 못했어요. 잠시 뒤 다시 시도해 주세요."

## 구현 완료 (2026-07-28)
- [x] Phase 0: PoC PASS (무상태 단일 POST 확인)
- [x] Task 1.1: Design Brief + 카피 확정 (위 섹션)
- [x] Task 1.2: TDD RED(2건 실패 확인) → GREEN — static/ask.js 신규,
      index.html 질의 섹션, embed.go AskJS, api.go 라우트, root_test.go 기대값
- [x] Task 1.3: index.css .ask 블록 (theme.css 토큰 재사용, 다크 테마 변수 호환)
- [x] Task 2.1(부분): go build/vet/test 전체 GREEN. 로컬 serve(18123, 임시
      마이그레이션 DB)에서 ask 마크업·ask.js 서빙 확인
- [x] 2-stage review: Spec Compliance APPROVED, Code Quality APPROVED
      (XSS 경로는 normalize의 http/https URL 검증으로 차단됨을 확인)
- [ ] Task 2.1(잔여): 실제 브라우저 렌더 확인 — chrome-cdp 스킬이 명시적 사용자
      승인 필요라 배포 게이트에서 함께 확인
- [ ] Phase 3: 배포 (AskUserQuestion 게이트 대기)

## 상태: 완료 ✅ (2026-07-28)

## 배포 및 AC 달성
- 배포: linux 빌드 → developer-vps 바이너리 교체 → eventsintel-api 재시작(active)
- deploy/verify.sh: ALL CHECKS PASSED (12/12)
- AC-1 PASS: 라이브 ask_events "다음 달 서울 AI 행사" → count 7, 마크업·ask.js
  서빙 확인 (브라우저 육안 렌더는 다음 방문 때 확인 권장)
- AC-2 PASS(코드 리뷰 갈음, PREFLIGHT 허용): 429 Retry-After 분 단위 안내 구현,
  2-stage 리뷰 통과. 라이브 쿼터 소진 테스트는 운영자 키 보호 위해 생략
- AC-3 PASS: 라이브 무의미 질의 → count 0, events null도 `|| []` 가드로 처리
- AC-4 PASS: go test ./... 전체 GREEN, openapi.yaml·llms.txt·eventmcp diff 0
- AC-5 PASS: verify.sh ALL CHECKS PASSED

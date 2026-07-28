# 승격 검토 패킷 — 최종 (2026-07-28)

URL 확정·파싱 가능성·robots 확인까지 도구 측 작업은 완료. 남은 것은 아래
행사 목록 샘플을 보고 "수집 가치가 있는가"의 승인/기각 판단뿐이다.

## 후보 1: AIIA 한국인공지능산업협회 — 승격 권고

- 확정 URL: `https://k-ai.or.kr/bbs/board.php?tbl=bbs41` (공지사항 게시판)
  - fixture의 `www.k-ai.or.kr/events`는 스테일. `www.` 서브도메인 자체가
    응답하지 않으므로 allowlist에는 bare `k-ai.or.kr`을 넣어야 한다
- 실제 게시물 샘플 (2026-07-28 확인, 현재 진행형 공고 포함):
  - [고려대학교] HIAI연구원 2026 Tech-Day 행사 안내
  - [행정안전부/NIA] 공공부문 AI·첨단기술 실증 우수사례 제출 안내(~7/31)
  - [그로스타] ETRI-신용보증기금 사업화 유망기술 설명회
  - [NHN CLOUD] AI 팩토리 GPU 가속 MLOps 스쿨 교육생 모집
- 기술 확인: 정적 PHP 게시판 HTML(JS 렌더 아님) → goquery 결정적 파싱 가능.
  robots.txt 없음(404) → 기본 허용. 단 서버 응답이 느릴 때가 있어(간헐 12초+)
  fetcher 타임아웃 여유 필요
- 성격: 행사 전용이 아닌 공지 게시판이라 행사/모집/일반 공지가 섞임 —
  어댑터의 분류 단계에서 걸러야 함

## 후보 2: KIRIA 한국로봇산업진흥원 — 보류 권고

- 확정 URL: `https://www.kiria.org/portal/info/portalInfoEventList.do`
  (행사 목록, 200 확인. fixture의 `/notice`는 스테일)
- 실제 행사 샘플: 2026년 로봇활용 제조혁신 지원사업 설명회(2025-11-26),
  마이스터 로봇화 교육 컨퍼런스(2025-11-04), … 이하 2023~2020년 행사
- 기술 확인: 서버 렌더 정적 테이블 → 결정적 파싱 가능. robots는
  `/portal/search/`·`/info/` 등을 막지만 `/portal/info/`는 허용(경로 상이)
- 보류 사유: 최신 행사가 2025-11월로 8개월 전이고 전 행사가 "완료" 상태.
  갱신 빈도가 낮아 지금 수집 가치가 낮다. 신규 행사가 올라오면 재검토

## 라이브 public provider 실행 (candidates/)
- 채택 5건 전부 `already_allowlisted` (MEDICA, NVIDIA, BIO, 코엑스, 킨텍스).
  seed 카탈로그 순회 구조상 예상된 결과. 승격 대상 없음

## 승인 시 다음 단계 (도구/에이전트가 수행)
1. `fetch.ProductionAllowedHosts`에 `k-ai.or.kr` 추가
2. AIIA는 benchmark 카탈로그(단일 행사) 형식이 아니라 *목록형 소스*라
   benchmark seed가 아닌 신규 Source 어댑터(coex/kintex 패턴)가 정도(正道) —
   구현 범위는 승격 승인 후 별도 /start로

## 승격 결과 (2026-07-28)
- AIIA: 승인 → `internal/sources/aiia` 어댑터로 구현 완료
  (.tasks/260728-aiia-source). 라이브 검증에서 행사성 2건 수집.
- KIRIA: 보류 확정 (갱신 빈도 낮음). 신규 행사 게시 시 재검토.

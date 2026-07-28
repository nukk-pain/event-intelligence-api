# PROGRESS: akei-source

## 상태: 구현·로컬 검증 완료, 배포 진행 중 (2026-07-28)
- Discover: 월 3개 창 × 페이지 조기중단, wr_id dedupe — fixture 15건 정확
- Parse: .bbs_schedule_view 표 파싱 (한/영 행사명·주최·ISO 기간·장소·전시분야
  →ClassifyText·홈페이지). 전화번호·이메일 필드 미추출 검증 테스트 포함
- 등록 3종 + race GREEN. 라이브 단독 ingest: 238건 저장, 오류 0,
  taxonomy 매칭 15건 (부산로봇기술산업전, 국제 디지털 의료기기전 등 —
  기존 커버리지 밖 지방 행사 최초 유입)
- 알려진 한계: 분류기 키워드 오탐 1건 관찰 ("로봇랜드 베이비페어"→robotics).
  어댑터 아닌 classifier 기존 특성, excluded 정책으로 노출은 통제됨
- 배포 승인 후 바이너리 교체 완료(active), 수동 ingest 백그라운드 진행 중

## 상태: 완료 ✅ (2026-07-28)
- 프로덕션 ingest: source=akei stored=240, 전 소스 정상, CF 캐시 purge 확인
- 실행 중 발견·수정: list=venue가 venue_id IN('coex','kintex') 하드코딩이라
  전국 행사(akei 137건 venue_id 없음, aiia 포함)가 UI 목록에서 비가시.
  파티션을 "NOT LIKE 'benchmark-%'"(국내=비벤치마크)로 재정의 — TDD로 잠금,
  openapi.yaml·llms.txt 계약 문구 동기화, ~/.ai/api-catalog.md 신규 등재
- 진단 각주: 페이지1 akei 0건은 keyset 정렬(updated_at 오름차순, 신규 행이
  마지막 페이지) 특성이지 버그 아님 — 카테고리 필터로 가시성 실증
- AC-5 PASS: verify.sh ALL CHECKS PASSED + 라이브 list=venue&category=
  medical-devices에 akei 행사(국제 병원 및 헬스테크 박람회) 확인

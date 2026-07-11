# PLAN — Solar-backed autonomous event-intelligence agent

## Context

Upstage Solar Agent Partner 프로그램(신청 마감 2026-07-15, Stage 1 빌드 7/17~7/31)
제출용. Solar를 뇌로 삼아, 기존 결정적 크롤 파이프라인 위에 **스스로 판단·행동하는
에이전트 루프**를 얹는다. 상세 배경/지원 전략은 워크스페이스 문서 참조:
`~/Developer/docs/workspace/planning/2026-07-11-upstage-solar-agent-partner-application.md`

현 상태: COEX/KINTEX/benchmark 결정적 인제스트 + read-only API가 events.nukk.net에
배포돼 있고, read 경로에 LLM이 없다(deterministic goquery). 이 태스크는 **ingest
쪽에만** Solar를 붙인다. read 경로 LLM-free 원칙은 그대로 보존한다.

## Non-negotiable constraints (Upstage 데이터 정책 + repo 하드 제약)

- **개인정보 미유입**: 추출 대상은 행사 사실 필드로 한정(행사명·일정·장소·카테고리·
  등록/부스 링크·출처). 담당자 전화·이메일, 발표자 개인정보는 저장/출력 금지.
  Solar 입력 전 텍스트에서 연락처 패턴(전화·이메일) 사전 제거.
- **건강·주민번호·계좌 정보 원천 배제**: 임상·환자·EMR·medical-platform 사설
  데이터는 이 repo/배포에 절대 넣지 않는다(기존 CLAUDE.md 하드 제약 유지).
- **저작권·크롤링 예의**: robots.txt 준수(기존 fetch 정책). 저작권 있는 원문
  통째 저장 금지 — 짧은 사실 요약 + 출처만. 사실 데이터는 저작권 대상 아님.
- **공개 데이터 전용**: 비공개·사설망·인증 필요 소스 금지.
- **read 경로 LLM-free 유지**: Solar 호출은 배치 ingest 안에서만.
- **비용 캐스케이드**: 결정적 파서(무료) 먼저 → 실패/모호한 것만 Solar. 같은 입력을
  로컬 Qwen3.6와 A/B로 재고, 어려운 판단에만 상위 모델.

## Scope

### Stage 1 (7/17~7/31) — 우선 구현

- [ ] **Solar 클라이언트** — `internal/llm/solar` (OpenAI 호환 `api.upstage.ai/v1`).
      structured output(JSON schema) 강제. 키는 `EVENTSINTEL_SOLAR_API_KEY` 등
      `EVENTSINTEL_*` env로만 주입(secret은 `json:"-"`). `.env.example` 참조.
- [ ] **루프 ① 소스 자율 발견** — 목표(예: 국내 AI·로봇 행사)만 주면 Solar가
      검색→신규 주최사/학회 페이지 발견→행사 소스 여부 판단→다음 크롤 대상 결정.
      후보는 기존 `fetch` allowlist/robots 경계 안에서만 확장.
- [ ] **루프 ② 한 행사 멀티홉 깊이 파기** — 잡은 행사에서 링크 추적으로 등록 마감·
      부스 신청·스타트업 프로그램 창을 캐고 여러 출처 교차검증 → 창업자용 액션
      브리핑 조립. 실패/불명은 빈 칸으로 정직하게 표시(기존 missing_fields 관례).
- [ ] **A/B 벤치마크** — 동일 입력에 대해 결정적 파서 / 로컬 Qwen3.6 / Solar Open 2
      추출 정확도 + 건당 토큰 비용 측정. 결과는 후기 + `model-registry.json` 반영.
- [ ] **200자+ 후기** 초안 완성(수치 채움). 초안: 위 워크스페이스 문서 §4-1.

### Stretch / Stage 2 (8/6~) — 미룸

- [ ] **루프 ③ MCP 도구 노출** — Hermes/Claude Code가 자연어로 행사 질의 → Solar가
      질의 변환 → 응답. `ullage` MCP 서버 경험 재사용.
- [ ] 소스 폭 확장 + 어려운 판단 구간 상위 모델 라우팅(Solar Pro 4 크레딧 활용).

## Verification

- `make test` / `make vet`, 파이프라인·fetch 변경은 `-race`.
- 새 소스/추출 경로는 결정적 fixture(`testdata/`)로 회귀 테스트.
- 개인정보 사전 제거·저작권 요약 정책은 유닛 테스트로 고정(연락처 스트립 검증).
- A/B는 재현 가능한 스크립트 + 고정 입력 세트로.

## Out of scope

- read 경로에 LLM 투입(원칙 위반).
- 유료 CRM/리드/참석자 스크래핑/사설 데이터.
- 실제 구현은 선정·Stage 1 시작 시. 이 PLAN은 준비용 스캐폴드.

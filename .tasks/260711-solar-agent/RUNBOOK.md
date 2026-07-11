# RUNBOOK — 복귀 후(~7/27) Solar 마무리 절차

여행(7/17~~7/27) 동안 Stage 1이 진행된다. 제출물은 **7/31 마감**이고 매일 활동은
필요 없다. 핵심 에이전트는 여행 전에 로컬(qwen36-dwq)로 완성·검증해 두었으므로,
복귀 후 남는 일은 **Solar 붙이기 → 측정 → 튜닝 → 후기**뿐이다. 순서대로 실행.

## 0. 사전 상태 (여행 전 완료됨)

- `feat/solar-agent` 브랜치에 전부 커밋됨.
- `internal/agent` — LLM 클라이언트 + 추출 + 멀티홉 보강(Solar-agnostic).
- `cmd/eventagent` — 에이전트 CLI (main.txt + links/ 케이스 실행).
- `cmd/abbench` — A/B 벤치마크. 로컬 baseline 저장됨(`baseline-local-*.md`).
- 준수사항(연락처 사전 제거·공개데이터·저작권) 코드에 반영됨.

## 1. Solar 키 발급 (7/17 이후, 5분)

1. console.upstage.ai 로그인(가입 이메일 `nukkpain@gmail.com`).
2. Open 2 Early Access API 키 발급. 모델 id 확인(예: `solar-open-2-...`).
3. 로컬에서만 export (커밋 금지):

   ```sh
   export EVENTSINTEL_SOLAR_API_KEY=...            # secret
   export EVENTSINTEL_SOLAR_MODEL=<open2-model-id> # 콘솔에서 확인한 값
   # base url 기본값 https://api.upstage.ai/v1 (다르면 EVENTSINTEL_SOLAR_BASE_URL)
   ```

## 2. A/B 측정 (자동, 명령 하나)

```sh
go run ./cmd/abbench -v -fixtures cmd/abbench/fixtures
```

- 이제 `local`과 `solar` 두 행이 함께 나온다. `baseline-local-*.md`(88.9%)와 비교.
- `-v`로 어느 필드에서 Solar가 이기고 지는지 확인 → 프롬프트 튜닝 타깃.

## 3. 에이전트 Solar로 실행

```sh
# 루프 ② 멀티홉 보강 (한 행사 깊이 파기)
go run ./cmd/eventagent -case cmd/eventagent/fixtures/ai-conf -backend solar

# 루프 ① 자율 소스 발견 (fixture 검색으로 로직 확인)
go run ./cmd/eventscout -backend solar -rounds 2
```

- eventagent: 3콜(추출→링크선택→보강)이 Solar로 돌며 founder brief JSON 출력.
- eventscout: 라운드마다 2콜(질의 제안→소스 판별). fixture 검색이라 오프라인 동작.
  **실검색 연결**: `agent.SearchTool`을 구현한 실제 웹검색 어댑터를 만들어
  `cmd/eventscout`의 `fixtureSearch` 대신 주입. robots·공개데이터 준수 유지.

## 4. 튜닝 (반나절)

- Solar가 틀리는 필드에 맞춰 `internal/agent/extract.go`의 `ExtractPrompt`,
  `enrich.go`의 프롬프트를 조정. 로컬용에 맞춘 문구가 Solar엔 최적이 아닐 수 있음.
- structured output(JSON schema)·function-calling을 Solar가 지원하면 그 경로로
  강화(콘솔 docs 참고). read 경로 LLM-free 원칙은 유지.
- fixture 몇 개 더 추가해 통계 신뢰도 보강(선택).

## 5. 후기 작성 (200자+)

- 초안: 워크스페이스 문서 `2026-07-11-...application.md` §4-1.
- `baseline-local-*.md` vs Solar 결과의 실제 수치(정확도·건당 토큰)로 마지막 문단
  채우기. 정직하게 — Solar가 이긴 구간/진 구간 둘 다.

## 6. 제출 (7/31 전)

- `feat/solar-agent`를 `main`에 머지(또는 브랜치 링크 그대로 제출).
- 후기를 README 또는 블로그에 게시.
- 결과물 링크를 Upstage 제출 채널에 제출(신청 시 안내받은 경로).

## 스트레치 (시간 남으면 / Stage 2)

- 루프 ① 자율 소스 발견(검색→신규 소스 판단→크롤 대상 결정).
- 루프 ③ MCP 도구 노출(Hermes/Claude Code가 행사 질의).

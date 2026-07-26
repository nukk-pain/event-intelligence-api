# PROGRESS — 공개 탐색 자율성과 프로덕션 Solar 연결

## Metadata

- Current Status: `implemented`
- Owner: smpain
- Started: 2026-07-27
- Last Updated: 2026-07-27
- Branch: `feat/solar-yield-reasons` (origin 푸시 완료)

## Current focus

구현과 로컬 검증은 끝났다. 남은 것은 `main` 머지와 7/31 전 Upstage 제출이다.
상위 태스크 맥락은 `.tasks/260711-solar-agent/PROGRESS.md`에 있다.

## Status

- [x] **[1] 사이트맵·HTML링크 후보 제목 백필** (`0914202`). 크롤러가 이미 fetch·
      파싱한 페이지의 `<title>`을 후보에 이어줬다. 추가 요청 0건. 프로토콜이 준
      제목은 덮지 않고, 빈 제목은 채우지 않는다.
- [x] **[2] 프로덕션 ingest에 Solar 연결** (`207feb1`). 파이프라인에 선택적
      `EventEnricher` 이음매를 만들고 `internal/solarenrich`를 붙였다. 배포
      바이너리가 이제 Solar를 호출한다.
- [x] **[3] 라운드 상향 — 해당 없음.** 제품은 이미 2라운드였다. 근거는 PLAN 참조.
      스모크만 1라운드이며 그대로 뒀다.
- [x] 계약 문서 갱신: `DECISIONS.md`, `.env.example`.
- [ ] `main` 머지 및 푸시.
- [ ] (~7/31) Upstage 채널 제출.

## Evidence

**[1] 라이브 스모크** (`.omo/evidence/solar-yield-reasons/live-smoke-titlebackfill.txt`,
gitignore이므로 로컬 전용)

| 항목 | 교정 전 | 교정 후 |
|---|---:|---:|
| `prefilter_dropped` | 14 | 0 |
| `prefilter_reason_missing_title` | 14 | 0 |
| 모델에 제안된 후보 | 5 | 9 |
| 채택 | 5 | 5 |

**[2] 실제 ingest 2회**

```
키만 있고 스위치 없음  → solar enrichment 로그 없음 (비활성)
스위치 켬              → solar enrichment: 1 call(s), 0 event(s) filled
```

**게이트**: `go build`/`go vet` 통과, `go test -race -shuffle=on -count=1 ./...`
exit 0, 스모크 privacy 회귀 통과, `git diff --check` 통과, `internal/api` 무변경
확인, 시크릿 스캔 0건.

## Known limitations

- **채택된 소스는 여전히 전부 카탈로그 seed다.** CLI를 직접 돌려 URL을 확인했다.
  Solar가 9건을 보고 5개 venue 홈페이지만 골랐다. 나머지는 같은 도메인의 하위
  페이지지 별개 소스가 아니므로 모델 판단 자체는 합리적이다.
  근본 원인은 frontier가 6개 seed origin을 거의 벗어나지 못하는 것이다. 12
  프로토콜 문서·24 HTML·60초 예산 안에서는 외부 도메인 링크까지 도달하기 어렵다.
  **따라서 "카탈로그에 없던 소스를 발견한다"는 아직 증명되지 않았다.** 별도 설계
  과제다.
- **[2]의 보강 실적은 0건이다.** 24건 표본에서 날짜가 비어 있던 이벤트가 1건뿐이었고
  Solar 답이 ISO 검증을 통과하지 못했다. 보강 대상 표면 자체가 작다. 호출이
  일어난다는 사실은 확인됐으나 유용성은 아직 미검증이다.
- [2]는 로컬 검증까지다. 프로덕션 배포와 배포 후 관측은 하지 않았다.

## Decisions made without asking

`/start` 자율 진행 모드에서 확인 없이 정한 것들이다.

| 결정 | 근거 |
|---|---|
| 보강 범위를 `start_date`/`end_date`로 한정 | ISO 형식으로 객관 검증 가능. 한국 행사 페이지에서 표기가 가장 다양 |
| 비ISO 응답 폐기 | 모델이 산문으로 답해도 이벤트 계약에 못 들어감 |
| 2차 fetch 없이 원문 문자열만 사용 | 정규화가 해석 못 한 정보가 거기 있음. 비용 0 |
| 키 + `EVENTSINTEL_SOLAR_ENRICH=1` 동시 요구 | 키만으로 켜지면 사고 |
| 보강 오류는 비치명적 | 결정적 행이 모델 때문에 깨지면 안 됨 |
| 스모크 라운드 1 유지 | 60초 예산 포화 상태라 늘려도 절단 사유만 증가 |
| `EventEnricher` 인터페이스로 추상화 | 파이프라인이 구체 소스 어댑터를 모르는 기존 원칙과 동일 |

## Next action

`feat/solar-yield-reasons`를 `main`에 머지하고 푸시한다. 제출 링크가 기본 브랜치를
가리키므로 머지 전에는 이번 작업이 심사자에게 보이지 않는다.

이후(Stage 2 후보): frontier가 seed origin 밖으로 나가도록 설계를 바꿔 실제 신규
소스 발견을 증명한다. 현재 예산 안에서 가능한지부터 확인이 필요하다.

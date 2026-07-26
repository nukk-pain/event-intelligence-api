# PLAN — 공개 탐색 자율성과 프로덕션 Solar 연결

## Context

Upstage Solar Agent Partner Stage 1 제출(마감 2026-07-31)을 앞두고, 냉정한 자체
평가에서 두 가지 결함이 확인됐다. 상세 평가는 워크스페이스 문서
`~/Developer/docs/workspace/planning/2026-07-26-solar-agent-stage1-assessment.md`
(비공개)에 있다.

1. 루프 ①이 라이브에서 채택하는 소스가 전부 하드코딩된 카탈로그 seed였다.
   사이트맵에서 나온 실제 발견 후보는 제목이 없어 prefilter에서 전부 탈락했다.
2. 배포된 events.nukk.net이 Solar를 한 번도 호출하지 않았다. Solar를 쓰는 코드는
   전부 독립 CLI에 있었고, 서비스는 결정적 크롤러로만 돌았다.

선행 작업(`feat/solar-yield-reasons` 브랜치)에서 유실 지점을 카운트 전용으로
분해하는 계측을 이미 붙였다. 이 태스크는 그 계측이 지목한 것을 고친다.

## Scope

### [1] 사이트맵·HTML링크 후보 제목 백필

`addAndQueue` 시점에 비seed 후보는 `title=""`로 저장된다. 이후 크롤러가 그 페이지를
실제로 fetch하고 `parseHTML`로 `<title>`을 뽑는데도 기존 후보를 갱신하지 않고
버린다. seed일 때만 새 후보를 만들기 때문이다. 제목은 이미 손에 있으므로 추가 HTTP
비용 없이 이어주기만 하면 된다.

- 프로토콜이 이미 준 제목(피드 엔트리 등)은 권위 있는 값이므로 덮지 않는다.
- 빈 제목은 채우지 않는다. 없는 제목을 지어내지 않는다.

### [2] 프로덕션 ingest에 Solar 한 줄기

파이프라인에 선택적 `EventEnricher` 이음매를 만들고, 정규화 이후 배치 ingest
안에서만 호출한다. 파이프라인은 구체 백엔드를 모른다. 구체 소스 어댑터를 모르는
것과 같은 방식이다.

구현체 `internal/solarenrich`는 의도적으로 좁다.

- `start_date`와 `end_date`만 채운다. ISO 형식으로 객관 검증이 되고, 한국 행사
  페이지에서 표기가 가장 다양한 필드다.
- 비ISO 응답은 저장하지 않고 폐기한다.
- 정규화가 해석하지 못한 원문 문자열만 읽는다. 2차 fetch를 하지 않는다.
- 출처 있는 값을 덮지 않고, 실제로 채운 필드만 `missing_fields`에서 지운다.
- `eventsintel/solar-enrich` provenance를 남기고 `date_confidence`를 낮춘다.

### [3] eventscout 기본 라운드 상향 — 조사 결과 해당 없음

착수 전 조사에서 **제품은 이미 2라운드**임이 확인됐다.

```
internal/agent/discovery_types.go:10       hardMaxDiscoveryRounds = 2
internal/agent/discovery_types.go:178      DefaultDiscoverOptions → MaxRounds: 2
internal/eventscoutserver/discovery.go:55  DefaultDiscoverOptions 사용
cmd/eventscout/main.go:30                  flag 기본값 3 → 상한 2로 클램프
scripts/smoke-solar-public-discovery.sh    "-rounds", "1"   ← 여기만 1
```

1라운드는 운영자 스모크에만 걸린 값이다. 스모크 출력을 제품 동작으로 오독한
것이었다. 스모크는 1라운드로 유지한다. 60초 크롤 예산이 이미 포화(`time_limit`
절단 기록)라 라운드만 늘리면 새 정보 없이 절단 사유만 늘어난다.

## Non-negotiable constraints

- read 경로 LLM-free 유지. `internal/api` 무변경을 게이트로 검증한다.
- 공개 크롤 한도(6 seed, depth 2, 12 프로토콜 문서, 24 HTML, 30 후보, 64 시도,
  6 MiB, 60초)와 모델 한도(2 라운드, 4 콜, 4000 토큰) 인상 금지.
- 개인정보 사전 제거 유지. 모델 전송 전 연락처 패턴 제거.
- Solar 키 없으면 기존 결정적 동작 그대로. 키만으로는 켜지지 않는다.
- 보강한 주장에는 반드시 출처를 남긴다.
- worktree 생성 금지.

## 실행 순서와 근거

**1 → 3 → 2**로 잡았고, 3이 소멸해 실제로는 **1 → 2**로 진행했다.

- 1번이 나머지의 전제다. 이게 없으면 3번은 판정할 새 후보가 없어 무의미하고,
  "자율 발견" 간판도 계속 거짓이다. 비용이 가장 싸고 효과가 가장 크다.
- 2번을 마지막에 둔 이유는 가장 크고 침습적이며 배포 바이너리와 ingest 파이프라인을
  건드리기 때문이다. 시간이 모자라도 1은 확보된다.

## Verification

- red-first 테스트. 기존 관행대로 코드 옆 `*_test.go`.
- `go build ./...`, `go vet ./...`, `go test -race -shuffle=on -count=1 ./...`
- 스모크 privacy 회귀 `bash scripts/test-smoke-solar-public-discovery.sh`
- `git diff --check`, `git diff --exit-code main -- internal/api internal/fetch`
- 운영자 라이브 스모크 `./scripts/smoke-solar-public-discovery.sh`
- 2번은 실제 `eventsintel ingest`를 스위치 off/on 두 번 돌려 호출 여부를 확인한다.
- 커밋은 `secure-commit` 경유.

## Out of scope

- frontier가 seed origin 밖으로 나가게 만드는 설계 변경. 실제 신규 소스 발견은
  여기에 달려 있으나 별건이다. PROGRESS의 Known limitations 참조.
- 프로덕션 배포. 이 태스크는 구현과 로컬 검증까지다.

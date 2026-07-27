# 배포 설계 — ingest 액션 보강

작성 2026-07-27. 구현 전 설계만 정리한 문서다. 아직 배포하지 않았다.

## 현재 프로덕션 조건

```
eventsintel-ingest.timer     OnUnitActiveSec=24h
EVENTSINTEL_INGEST_DEADLINE  20m
EVENTSINTEL_RATE_PER_MIN     30       (호스트당)
EVENTSINTEL_SOURCE_CONCURRENCY 2
EVENTSINTEL_DETAIL_WORKERS   4        (최대 8 동시)
Solar Private Beta           400 RPM / 150,000 TPM
```

보강 대상은 670건 중 액션 필드가 빈 약 550건이다.

## 비용 계산

한 건당 실측치는 `abbench-solar-open2-20260717.md`의 eventagent 값을 쓴다.
3콜, 1,258 입력 + 278 출력 토큰, 5.3초.

| 항목 | 550건 전체 |
|---|---:|
| Solar 콜 | ~1,650 |
| 토큰 | ~845,000 |
| 400 RPM 하한 | 4.1분 |
| 150k TPM 하한 | 5.6분 |
| 모델 시간 (8 동시) | ~6분 |
| **링크 fetch** | **~1,100회** |

**결론: Solar가 병목이 아니다. 벤더 사이트 fetch가 병목이다.**

에이전트가 고른 링크를 읽으려면 official fetcher를 타는데 호스트당 30회/분이다.
1,100회가 소수 호스트(코엑스·킨텍스)에 몰리면 최악의 호스트 하나만으로 30분을
넘긴다. 기존 상세 페이지 크롤과 같은 예산을 나눠 쓴다.

즉 **20분 데드라인 안에 550건을 한 번에 처리하는 설계는 불가능**하다. 데드라인을
늘리는 것도 답이 아니다. 24시간 주기 안에서 크론이 겹치지 않게 하려는 것이 그
데드라인의 목적이다.

## 설계

### 1. 증분 처리 — 한 번에 전체를 하지 않는다

실행당 보강 대상을 정액으로 끊는다.

```
EVENTSINTEL_SOLAR_MAX_CALLS=60     # 실행당 보강 시도 60건
```

60건이면 Solar 콜 180회, 링크 fetch 약 120회, 추가 소요 4~6분이다. 현재 크롤이
2~5분에 끝나므로 20분 데드라인 안에 들어간다.

커버리지는 550 ÷ 60 ≈ **10일**이면 1회전이고 이후는 신규·변경분만 유지하면 된다.
Stage 1 마감(7/31) 전에 전량을 채우는 것은 어차피 불가능하므로, 부분 커버리지를
정직하게 보여주는 편이 맞다.

### 2. 재시도 방지 — 이게 제일 중요하다

**현재 구현의 치명적 결함**: 보강기는 신호가 nil인 모든 이벤트에 대해 시도한다.
채우지 못한 이벤트는 다음 날에도 nil이므로 **같은 이벤트를 매일 다시 시도한다.**
예산 60건이 첫 60건에 영원히 묶이고 나머지 490건에는 도달하지 못한다.

시도 상태를 저장해야 한다. 스키마 마이그레이션이 필요하다.

```sql
-- internal/store/migrations/0010_action_enrich_attempt.sql
ALTER TABLE events ADD COLUMN action_enriched_at TEXT;
ALTER TABLE events ADD COLUMN action_enrich_result TEXT;  -- filled|empty|error
```

선정 규칙:
- `action_enriched_at IS NULL`인 것을 먼저 (미시도)
- `result='empty'`는 공식 페이지 내용이 바뀌었을 때만 재시도
- `result='error'`는 3일 후 재시도
- `result='filled'`는 재시도하지 않음

### 3. 우선순위 — 지난 행사를 채우지 않는다

지난 행사의 등록 마감은 가치가 없다. 선정 쿼리에 다음을 건다.

```
start_date >= today  AND  action 필드가 비어 있음
ORDER BY start_date ASC
```

임박한 행사부터 채운다. 창업자가 실제로 필요한 순서다.

### 4. 데드라인 상호작용

파이프라인은 소스·ref 사이에서 ctx를 확인하고 `Report.Truncated`를 세운다. 보강이
데드라인을 먹으면 **크롤 자체가 잘려 discovery floor가 오염될 수 있다.**

보강에 별도 하위 데드라인을 준다. 전체 데드라인의 절반을 넘지 않게 한다.

```
보강 예산 = min(SOLAR_MAX_CALLS, 남은 시간이 허용하는 건수)
```

크롤이 끝난 뒤 남은 시간으로 보강하는 순서가 안전하다. 현재는 상세 파싱 도중에
인라인으로 돈다. 이 순서 변경은 별도 작업이다.

### 5. 관측

기존 로그 한 줄로는 부족하다. 배포 후 다음을 확인할 수 있어야 한다.

```
solar enrichment: attempts=N filled=M empty=K error=E budget_exhausted=bool
                  elapsed=Xs venue_fetches=Y
```

`deploy/verify.sh`에 보강 로그 존재 확인을 추가한다.

### 6. 롤백

`EVENTSINTEL_SOLAR_ENRICH` 미설정이면 즉시 기존 결정적 동작이다. 코드 롤백 없이
환경변수만 지우고 유닛을 재시작하면 된다. 이미 그렇게 되어 있다.

## 배포 순서

1. 마이그레이션 `0010`과 시도 상태 기록 구현
2. 선정 쿼리(미시도 우선, 임박순, 지난 행사 제외) 구현
3. 보강을 크롤 이후 단계로 이동, 하위 데드라인 부여
4. 로그 확장과 `deploy/verify.sh` 체크 추가
5. `EVENTSINTEL_SOLAR_MAX_CALLS=10`으로 프로덕션 1회 수동 실행, 로그·DB 확인
6. 문제 없으면 60으로 올리고 타이머에 맡김
7. 3일간 매일 로그 확인. 커버리지가 실제로 전진하는지(매일 다른 이벤트를
   시도하는지) 확인

5번을 건너뛰면 안 된다. 재시도 방지가 안 되어 있으면 커버리지가 전진하지 않는데,
그건 로그를 봐야만 드러난다.

## 하지 않기로 한 것

- 데드라인을 20분 이상으로 늘리기. 24시간 주기와 flock 단일 실행 보장을 지키려는
  값이다. 늘리면 크론이 겹칠 위험이 생긴다.
- 호스트당 30회/분 완화. 크롤링 예의 정책이고 벤더 차단 위험이 있다.
- 전체 550건 일괄 처리. 위 계산대로 불가능하다.

# abbench — 프롬프트 튜닝 후 A/B (2026-07-17)

`ExtractPrompt` 튜닝(제목 연도 유지, name_en 원문 그대로, 날짜 원문 표기 유지 +
범위 연/월 보완, venue/city 구분) 후 전체 A/B.

⚠️ **모델 주의**: 이 런의 `solar` 열은 `.env`에 `EVENTSINTEL_SOLAR_MODEL`이
없던 시점에 시작되어 기본값 **`solar-pro`**로 측정된 것이다. `solar-open2`는
이 키에서 400 invalid model로 거절되어 아직 미측정 (계정/권한 확인 중 —
승인된 신청 계정인지 확인 필요. `/v1/models` 목록: solar-pro3,
solar-pro2, solar-mini, syn-pro 계열만).

```
backend  field acc  avg in tok  avg out tok  avg latency  fails
-------  ---------  ----------  -----------  -----------  -----
local    100.0%     577         2494         22189ms      0
solar    100.0%     556         126          1129ms       0    (= solar-pro)
```

- 6케이스 전부 9/9. 튜닝 전: local 94.4% / solar(-pro) 94.4%.
- 튜닝으로 고친 실패: name_en 연도 덧붙임(kintex-robot-expo), 범위 끝 날짜
  연도 누락(smartfactory-expo), 제목 뒤 연도 누락(world-ai-congress),
  지역명을 venue로 오인(ai-semi-summit, local).
- 튜닝 중 회귀 1회: 날짜 범위 보완 예시가 점 표기 재포맷을 유발 →
  "원문 표기 그대로, 재포맷 금지" 규칙 + 년월일 스타일 예시로 해결.
- 비용 프록시: solar-pro 건당 출력 126tok / 1.1s vs local(qwen36-dwq)
  2494tok / 22.2s.

다음: solar-open2 접근 열리면 solar-only 재측정
(`EVENTSINTEL_LOCAL_BASE_URL=off go run ./cmd/abbench -v -fixtures cmd/abbench/fixtures`).

> 2026-07-17 후속 측정 완료: `solar-open2` 권한이 열렸고 결과는
> `abbench-solar-open2-20260717.md`에 기록했다.

# abbench — Solar Open 2 최종 측정 (2026-07-17)

## 설정

- 모델: `solar-open2`
- 백엔드: Upstage `https://api.upstage.ai/v1`
- `reasoning_effort`: `minimal`
- fixture: 한국어 행사 공지 6건, 건당 9개 평가 필드
- 명령: `EVENTSINTEL_LOCAL_BASE_URL=off go run ./cmd/abbench -v -fixtures cmd/abbench/fixtures`

`reasoning_effort`를 생략하면 간단한 JSON 요청에도 hidden reasoning 726토큰을
사용했고, 작은 completion 예산에서는 `content=null`로 종료됐다. `minimal`을
명시하면 같은 요청이 reasoning 0토큰으로 바로 JSON을 반환했다.

## 최종 결과

```text
backend  field acc  avg in tok  avg out tok  avg latency  fails
-------  ---------  ----------  -----------  -----------  -----
solar    100.0%     604         118          1903ms       0
```

- 6케이스 전부 9/9, 총 54/54 필드 정답.
- 초기 Solar Open 2 측정은 `city`에 국가 접두어를 포함해 53/54(98.1%)였고,
  도시명만 반환하도록 프롬프트를 명확히 한 뒤 54/54가 됐다.
- 앞선 `solar-pro` 비교 측정도 54/54였지만, 이 문서의 수치는 프로그램 대상 모델인
  `solar-open2`를 직접 호출한 결과다.

## 세 에이전트 루프 실행 증거

- `eventagent`: 추출→링크 선택→보강 3콜 성공. 1,258 입력 + 278 출력토큰,
  총 5.3초. 등록 마감, 부스 신청, 스타트업 프로그램 브리핑 생성.
- `eventscout`: 2라운드 4콜 성공. COEX, 인공지능산업협회, KINTEX,
  한국로봇산업진흥원 후보 4개 발견. 1,116 입력 + 571 출력토큰, 총 10.2초.
- `eventmcp`: 자연어 `다음 달 서울 AI 행사`를 2026-08 서울/AI 필터로 변환하고
  라이브 `events.nukk.net` read API에서 행사 6건 반환.

## 해석

Solar Open 2는 짧은 구조화 추출에서 평균 출력 118토큰으로 100% 정확도를 냈다.
이 워크로드에서는 추론 깊이를 높이는 것보다 `reasoning_effort=minimal`로 출력
예산을 실제 JSON에 집중시키는 편이 더 정확하고 저렴했다.

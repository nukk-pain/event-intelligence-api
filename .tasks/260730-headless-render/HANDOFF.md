# HANDOFF: 데이터층 하루 1회 헤드리스 렌더링

> 발주: event-strategy-agent 세션 (2026-07-29). 새 세션이 이 문서만 읽고 실행할 수 있게 쓴다.
> 먼저 이 repo의 `AGENTS.md`와 `.tasks/260729-deadline-coverage/PLAN.md`(직전 마감 커버리지 작업)를 읽을 것.

## 왜

JS 전용 페이지는 정적 fetch로 원천 확인이 불가능하고, 이게 마감·비용 커버리지의
마지막 구조적 구멍이다. 라이브에서 확인된 사례:

- khospital.org/registration/#/pre-reg/891 — fragment 라우팅 SPA, 정적 텍스트에 마감 없음
- kofurn (sofurn-kofurn.bizforu.net) — countdown이 JS 렌더링 ("~2026.08.26"이 브라우저에서만 보임)
- 소비자(event-strategy-agent)는 얇은 판단 레이어라 헤드리스를 넣지 않기로 결정
  (그쪽 DECISIONS.md 2026-07-29). 크롤링이 합법이고 결과가 캐시되는 이 repo가 올바른 자리다.

## 무엇을

ingest의 공식 페이지 fetch 경로에 **선택적 헤드리스 렌더링 폴백**을 추가한다:

1. 정적 fetch 결과가 "JS 셸"로 판정되면(정적 텍스트 < N자 || fragment(#/) URL || noscript 안내)
   해당 URL만 헤드리스로 1회 렌더링해 DOM 텍스트를 얻는다.
2. 렌더링된 텍스트는 기존 파이프라인에 **정적 텍스트와 동일한 자격**으로 공급:
   deterministic extractor(enrich.ExtractActions, DeadlineOnActionPage)와
   Solar ActionEnricher의 readTexts(증거 게이트 dateEvidence/typedDateEvidence의
   대조 텍스트) 양쪽.
3. 하루 1회 ingest 타이머 안에서만 실행. per-run 렌더 상한(예: 30페이지)과
   페이지당 타임아웃(예: 15s)을 코드가 집행.

## 지켜야 할 불변식 (이 repo의 헌법)

- **증거 게이트 불변**: 렌더링은 "읽을 수 있는 텍스트"를 늘릴 뿐, 마감 저장 조건
  (문자 그대로의 날짜 존재 + 유형 문맥, solarenrich/deadline.go typedDateEvidence)은
  그대로다. 게이트를 우회하는 경로를 만들지 말 것.
- carry-forward 래칫(store/diff.go)·wrong_type 마이그레이션 패턴 유지.
- robots 준수(internal/fetch/robots.go 정책과 일관), 렌더링에도 UA 명시.
- 헤드리스 실패는 non-fatal — 정적 결과로 계속.

## 구현 힌트

- 라이브러리: chromedp(순수 Go, CDP) 권장. rod도 가능. 외부 프로세스 의존
  (chromium 설치)은 VPS에 필요 — `deploy/README.md`에 설치 절차 추가할 것
  (developer-vps는 1GB급이므로 메모리 확인, `--disable-gpu --no-sandbox` 필요).
- 통합 지점: `internal/pipeline/source_enrich.go`의 officialFetcher 사용부.
  Fetcher 인터페이스를 건드리기보다 "shell 판정 → 렌더 폴백" 헬퍼를 pipeline에 두는 게 좁다.
- shell 판정 휴리스틱: stripHTML 후 텍스트 < 400 rune, 또는 URL에 `#/`, 또는
  `<div id="root">`/`<div id="app">`만 있는 body.
- VPS 리소스가 안 되면 대안: 로컬(이 맥)에서 렌더 전용 보조 배치를 돌려 결과를
  DB에 반영하는 구조는 **금지에 가깝게 신중히** — DB는 VPS가 SSOT다. 차라리
  렌더 상한을 낮춰 VPS에서 돌려라.

## 완료 기준

- [ ] khospital 사전등록·kofurn 마감이 증거 게이트를 통과해 DB에 저장된다
- [ ] `deadline coverage: upcoming events with a deadline = N` 로그가 렌더 도입
      전보다 상승 (현재 기준선: 17, 2026-07-29 01:54 배치)
- [ ] ingest 총 소요가 타이머 데드라인(20m) 안에 유지
- [ ] `make eval-report` 재감사에서 wrong_type 0 유지
- [ ] deploy/verify.sh ALL CHECKS PASSED, DECISIONS.md에 결정 기록

## 참고 경로

- 증거 게이트: `internal/solarenrich/deadline.go`, `internal/solarenrich/actions.go:151-172`
- 결정론 추출: `internal/enrich/actions.go`
- 커버리지 로그: `cmd/eventsintel/main.go` (store.CountUpcomingWithDeadline)
- 감사 도구: `eval/audit.py`, `eval/README.md`

## 추가 발견 (2026-07-29 오후)

- themedtechconference.com 은 JS 렌더링이 아니라 **Cloudflare 봇 차단(403
  Attention Required)** — 정적 fetch가 원천 불가한 별도 클래스다. 소비자 쪽은
  이를 "blocked"로 분류해 보고서에 명시하도록 했다.
- 헤드리스 도입 시 이 클래스의 처리 방침을 여기서 결정할 것: managed challenge는
  실제 브라우저에서 통과되기도 하지만, 명시적 봇 차단의 우회는 안티봇 회피로
  읽힐 수 있다. 권고: robots·차단 신호를 존중하고, 이런 행사는 benchmark 카탈로그
  수동 필드(사람이 확인한 마감)로 채우는 절차를 두는 쪽이 제품 정직성과 일관된다.

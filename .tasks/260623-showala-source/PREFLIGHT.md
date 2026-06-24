# PREFLIGHT: SHOWALA 소스 어댑터 + cross-source dedup

> 작성: 2026-06-24 · PoC는 실제 showala.com HTML/curl로 검증

## 1. Discovery 경로 (확정)

- **1페이지(서버 렌더)**: `GET https://showala.com/ex/ex_list.php?place[]=1` (`place[]=1` = 경기 지역. KINTEX의 상위집합 — 수원메쎄/수원컨벤션/일산 등 포함).
- **2페이지+ (AJAX, plain HTML fragment)**: `GET https://showala.com/ex/ex_proc.php?action=exPagingNew&page=N&qstr=place%5B%5D%3D1`
  - **필수 헤더**: `Referer: https://showala.com/ex/ex_list.php?place[]=1` — 없으면 `{"result":"fail","detail":"ErrorRef"}`.
  - fragment 끝 토큰: `:::<nextPage>:::<totalPages>` (split `:::`). page당 5건.
- **정렬**: 앞 ~16페이지 = 다가오는 행사 **오름차순**, 이후 과거 행사 내림차순. → **조기 종료 가능**: 행 단위로 start date를 보고 **첫 과거 날짜(< today) 행을 만나면 페이징 중단**. (16페이지 경계는 미래꼬리+과거머리 혼재 → per-row 판정, per-page 아님.)
- **junk 필터**: 테스트/온라인 더미 행 존재(예: title "aaaaaaa", venue `온라인 http://...`, 날짜 `2021-08-01~2022-06-30`). venue가 킨텍스가 아니거나 날짜 비정상이면 자연 제외.

### KINTEX 스코프 (목록 단계에서)
목록 행에 venue(`div.ex_place`, 개최장소)와 날짜(`div.ex_date`, 전시기간)가 이미 있음 → **목록에서 개최장소가 `킨텍스`/`KINTEX` 포함 + 미래 행사인 행만 Ref로 emit**. 상세 fetch는 그 매칭 건만(2-hop). 비용: ~16 목록 페이지 + 매칭 상세 수십 건 → ingest당 1~2분, 30분 데드라인 내 안전.

### 목록 행 셀렉터
- 반복 단위: `li.ex_item`
- 상세 앵커: `a.ex_tit_a` 또는 `a.btn_ex_detail` → `/ex/ex_detail.php?idx=N`
- 제목: `a.ex_tit_a`, 영문: `p.ex_e_tit`
- 날짜: `div.ex_date` (라벨 span 제거 후, `2026-11-04 ~ 2026-11-07`)
- venue: `div.ex_place`

## 2. 상세 페이지 셀렉터 (확정, idx=3219 = 2026 로보월드)

`<div class="ba_info"><ul>` 안 `<li class="<key>">`:
| 필드 | 셀렉터 | 값(예) |
|---|---|---|
| 한글명 | `li.kor_tit` | 2026 로보월드 |
| 영문명 | `li.eng_tit` | ROBOTWORLD 2026 |
| 전시기간 | `li.date p.des` | `2026-11-04 ~ 2026-11-07` (ISO 하이픈, ` ~ ` 구분) |
| 개최장소 | 첫 `li.where p.des` (라벨 개최장소) | **킨텍스 (KINTEX)** |
| 세부장소 | 둘째 `li.where p.des` (라벨 세부장소) | 제 1전시장 3~5 Hall |
| 홈페이지 | `li.homp a[href]` | https://www.robotworld.or.kr/ |
| 주최 | `li.opener p.des` | 산업통상… |
| 주관 | `li.opener2 p.des` | 한국AI•로봇산업협회 |
| 후원 | `li.found p.des` | (첫 found) |

**파서 주의**:
- 라벨셀이 `<p class="tit">…</dt>`로 잘못 닫힘 → goquery(HTML5 lenient) OK. `li.<class> > p.des`로 타겟, 라벨 텍스트 동등비교 의존 금지.
- `li.where`·`li.found`가 각 2개 → `p.tit` 라벨 또는 위치로 구분.
- 날짜 포맷 `2026-11-04` → normalize `dateLayouts`의 `"2006-01-02"`로 이미 파싱됨(추가 불필요).

## 3. 정적 vs JS — 전부 정적
대상 필드 전부 raw HTML(curl)에 서버 렌더. JS는 SNS 공유·더보기 버튼·venue 이미지맵 등 비필수만. → headless 불필요(효율 원칙 충족).

## 4. robots.txt — 크롤 허용
```
User-agent: *
Disallow: /adm/
Disallow: /member/
```
`/ex/`, `/page/`, `/ex/ex_proc.php` 모두 허용.

## 5. Fetcher 영향 (코드 변경 필요 — CRITICAL)

`internal/fetch/fetch.go`:
- `Fetch(ctx, rawURL, cond Conditional)` — 현재 커스텀 요청 헤더 불가. 헤더는 `do()`(246-254)에서 하드코딩(User-Agent/Accept/조건부 검증자만).
- **AJAX 페이지네이션의 `Referer`를 보내려면 Fetcher API 확장 필요** (additive). 옵션: `Conditional`에 `Referer string`(또는 `Headers map[string]string`) 추가가 최소 침습. **모든 소스 공유 경로이므로 빈 값=무동작(기존 동작 불변) 보장 + 회귀 테스트 필수.**
- 레이트리밋: per-host `rate.Limiter`, 전역 `perMinute`(기본 30=2초/req). per-host 상향은 코드 변경 필요하나 **조기 종료로 페이지 수가 ~16으로 줄어 불필요**.

## 6. config (확정)
- `IngestDeadline` 기본 `30m`(env `EVENTSINTEL_INGEST_DEADLINE`), `RateLimitPerMinute` 기본 `30`(env `EVENTSINTEL_RATE_PER_MIN`).
- `MaxDiscoverPerSource` 기본 400(env `EVENTSINTEL_MAX_DISCOVER`) — KINTEX 매칭 건은 수십이라 여유.
- `SourceConfig{ID,Name,BaseURL,Enabled}` — `Default()`에 showala row 추가.
- 파이프라인은 ctx 데드라인을 source/ref 사이에서 체크, cut-short 시 baseline 미갱신(floor 보호).

## 7. dedup venue_id 매핑 (확정 방향)
SHOWALA의 KINTEX 행사 `venue_id == "kintex"` 필요(콘텐츠키가 list.do와 일치하도록). **venue-name 기반 매핑** 채택(개최장소 텍스트 `킨텍스`/`KINTEX` → venue_id "kintex"). 소스 기반(`venueIDForSource["showala"]`)은 SHOWALA가 향후 COEX 등 다른 venue를 담으면 깨지므로 부적합.

## 8. PLAN 반영 사항
- **신규 Task (Phase 1 앞)**: Fetcher에 Referer(또는 Headers) 추가 — additive, 회귀 테스트. `[TDD]`.
- **Task 1.2 개정**: AJAX 페이지네이션(Referer) + 행단위 조기 종료(첫 과거날짜 중단) + 목록단계 KINTEX/미래 필터 + junk 행 제외. 상세는 매칭 건만.
- **AC-2 개정**: KINTEX 스코핑은 목록 단계 개최장소 기준(상세 fetch 전).
- **Task 2.1 개정**: venue-name 기반 venue_id 매핑 채택.
- dedup(Phase 3)은 PoC로 바뀐 것 없음 — 그대로.

## 미해결/주의
- AJAX fragment 정확한 row 마크업은 `ex_list.php` 1페이지와 동일(`li.ex_item`) — fixture로 1페이지 + fragment 1개 캡처 권장.
- 조기 종료 경계(16페이지)의 혼재 처리 테스트 필요.

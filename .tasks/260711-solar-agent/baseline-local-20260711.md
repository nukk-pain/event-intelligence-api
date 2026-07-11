# Local baseline v2 (venue-containment, qwen36-dwq) — 2026-07-11 19:29

```
A/B extraction benchmark — 6 case(s), 1 backend(s)

  [local] ai-semi-summit         7/9 fields  in=268 out=2637  22874ms
        ✗ venue_name   want="" got="판교"
        ✗ city         want="판교" got=""
  [local] bio-materials-expo     9/9 fields  in=253 out=1966  17579ms
  [local] coex-ai-conf           9/9 fields  in=349 out=2455  23139ms
  [local] kintex-robot-expo      9/9 fields  in=326 out=2064  20400ms
  [local] smartfactory-expo      9/9 fields  in=275 out=2908  40738ms
  [local] world-ai-congress      8/9 fields  in=250 out=1951  19030ms
        ✗ name         want="세계 인공지능 대회 2027" got="세계 인공지능 대회"

backend  field acc  avg in tok  avg out tok  avg latency  fails
-------  ---------  ----------  -----------  -----------  -----
local    94.4%      286         2330         23960ms      0

Note: 'avg out tok' is the per-event cost proxy. Contact info is stripped pre-send.
```

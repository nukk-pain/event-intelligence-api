# Local baseline (qwen36-dwq) — 2026-07-11 19:13

```
A/B extraction benchmark — 6 case(s), 1 backend(s)

  [local] ai-semi-summit         7/9 fields  in=214 out=1800  15509ms
  [local] bio-materials-expo     9/9 fields  in=199 out=1670  14418ms
  [local] coex-ai-conf           8/9 fields  in=295 out=1550  13504ms
  [local] kintex-robot-expo      8/9 fields  in=272 out=2131  18671ms
  [local] smartfactory-expo      8/9 fields  in=221 out=1864  16788ms
  [local] world-ai-congress      8/9 fields  in=196 out=1913  17692ms

backend  field acc  avg in tok  avg out tok  avg latency  fails
-------  ---------  ----------  -----------  -----------  -----
local    88.9%      232         1821         16097ms      0

Note: 'avg out tok' is the per-event cost proxy. Multiply by your
per-token price to get won-per-event. Contact info is stripped pre-send.
```

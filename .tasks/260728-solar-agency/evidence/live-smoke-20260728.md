# Live smoke — action loop on VPS (2026-07-28)

- Binary: eventscout linux/amd64 SHA256 91b952958200e61756c7a2180a7e63247e2b721f2b52ee7b286b39fe2ee15f48
- Run: /tmp on developer-vps, operator solar.env, service untouched
- Flags: -backend solar -rounds 2 -opens 3 -goal "(운영 표준 goal)"
- Policy: counts only — no URL, page text, or goal echo in this file

## yield_trace (verbatim counts)
- outcome=accepted, terminal_reason=round_limit
- crawler_validated=14, offered=10, prefilter_dropped=0
- proposal_calls=5, judge_calls=3, open_calls=3
- judge_entries_parsed=10, judge_entries_dropped=0, accepted=10

## Cost/latency
- 8 model calls, 5224 in + 590 out tokens (~74 out/call), 77.8s total

## Reading
- The model drove all 8 turns itself, interleaving search/open/accept until
  the turn budget bound (round_limit) — no fixed choreography.
- open was chosen 3 times voluntarily (evidence gathering before judging).
- accepted=10 vs. the previous fixed-loop live yield of 5 (AC "accepted ≥ 5"
  satisfied with margin).
- 590 total output tokens confirms the 500/call reservation is ample.

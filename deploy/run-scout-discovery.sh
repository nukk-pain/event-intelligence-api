#!/usr/bin/env bash
# Weekly keyless source discovery on the VPS: crawl the curated seed catalog,
# let Solar judge candidates, and write a promotion review packet for a human.
# Promotion itself stays a reviewed code change (DECISIONS.md 2026-07-28).
#
# Without a Solar key this is an intentional, successful skip (exit 0), the
# same posture as scripts/smoke-solar-public-discovery.sh. A present-but-dead
# key (e.g. after the 7/31 beta expiry) fails loudly in the journal instead,
# and recovers on its own once solar.env carries a fresh key.
set -uo pipefail

BIN="${EVENTSCOUT_BIN:-/srv/developer/events-intel/eventscout}"
OUT_ROOT="${EVENTSCOUT_DISCOVERY_DIR:-/srv/developer/events-intel/discovery}"
GOAL="${EVENTSCOUT_GOAL:-2026년 이후 국내외 AI 로봇 바이오 의료기기 공식 행사 소스 찾기}"

if [[ -z "${EVENTSINTEL_SOLAR_API_KEY:-}" ]]; then
  echo "scout-discovery: SKIPPED_CREDENTIAL_UNAVAILABLE (no Solar key; no model or network call made)"
  exit 0
fi
if [[ ! -x "$BIN" ]]; then
  echo "scout-discovery: FAIL eventscout binary missing at $BIN" >&2
  exit 1
fi

OUT="$OUT_ROOT/$(date +%F)"
mkdir -p "$OUT"

# run.txt is the CLI's human-readable output with one embedded JSON blob —
# not pure JSON, hence not named .json. A same-day rerun overwrites the
# packet; accepted, since a rerun supersedes the packet it replaces.
"$BIN" -backend solar -rounds 2 -goal "$GOAL" -promote "$OUT" > "$OUT/run.txt" 2> "$OUT/run.log"
status=$?

if [[ $status -ne 0 ]]; then
  echo "scout-discovery: FAIL exit=$status (see $OUT/run.log)" >&2
  exit $status
fi
if [[ -f "$OUT/seed-candidates.jsonl" ]]; then
  count=$(wc -l < "$OUT/seed-candidates.jsonl" | tr -d ' ')
  echo "scout-discovery: OK candidates=$count packet=$OUT (review then promote via code)"
else
  echo "scout-discovery: OK candidates=0 (no accepted sources this run)"
fi

# With a GitHub token the packet also arrives as a human-review PR; without
# one this line is a no-op and the packet stays on disk as before. A PR
# failure must not fail the discovery run that already produced its packet.
if [[ -n "${EVENTSCOUT_PROMOTE_PR_TOKEN:-}" && -f "$OUT/seed-candidates.jsonl" ]]; then
  "${EVENTSCOUT_PROMOTE_PR_SCRIPT:-/srv/developer/events-intel/open-promotion-pr.sh}" "$OUT" || \
    echo "scout-discovery: WARN promotion PR step failed (packet still at $OUT)" >&2
fi

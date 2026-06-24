#!/usr/bin/env bash
# Post-deploy verification for events.nukk.net.
#
# MANDATORY final step of every deploy: confirms the live site is actually
# serving the new build correctly — not just that a process is up. Checks every
# public entry point, the Accept-negotiated root (HTML for browsers / JSON for
# agents — the exact thing that broke when the edge cached one variant), edge
# caching of the API surface, and that the dataset is non-empty.
#
# Usage:
#   deploy/verify.sh                         # checks https://events.nukk.net (edge)
#   deploy/verify.sh http://127.0.0.1:3005   # checks the origin directly
#
# Exits 0 only if every check passes; non-zero (and prints FAIL lines) otherwise.
set -uo pipefail

BASE="${1:-https://events.nukk.net}"
BROWSER_ACCEPT='text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'
UA='Mozilla/5.0 (verify.sh)'
fails=0
pass() { printf '  PASS  %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; fails=$((fails+1)); }

echo "== verifying $BASE =="

# 1. Every public entry point returns 200.
for p in / /healthz /llms.txt /api/v1 /api/v1/schema /api/v1/openapi.yaml /api/v1/events /api/v1/events/changes; do
  code=$(curl -s -A "$UA" -o /dev/null -w '%{http_code}' "$BASE$p")
  [ "$code" = "200" ] && pass "200 $p" || fail "$p returned $code (want 200)"
done

# 2. Root as a BROWSER must be the HTML UI (not the JSON service index). This is
#    the negotiation/edge-cache regression guard.
ct=$(curl -s -A "$UA" -H "Accept: $BROWSER_ACCEPT" -o /dev/null -w '%{content_type}' "$BASE/")
body=$(curl -s -A "$UA" -H "Accept: $BROWSER_ACCEPT" "$BASE/")
if printf '%s' "$ct" | grep -qi 'text/html' && printf '%s' "$body" | grep -qi '<!DOCTYPE html'; then
  pass "/ as browser -> HTML UI"
else
  fail "/ as browser -> not HTML (Content-Type=$ct); edge may be serving the JSON variant"
fi

# 3. Root as an AGENT must be the JSON service index.
jbody=$(curl -s -A "$UA" -H 'Accept: application/json' "$BASE/")
printf '%s' "$jbody" | grep -q '"service":"event-intelligence-api"' \
  && pass "/ as agent -> JSON service index" \
  || fail "/ as agent -> not the JSON service index"

# 4. The event list is non-empty (deploy didn't ship an empty/unmigrated DB).
ev=$(curl -s -A "$UA" "$BASE/api/v1/events")
if printf '%s' "$ev" | grep -q '"event_id"'; then
  pass "/api/v1/events has events"
else
  fail "/api/v1/events returned no events"
fi

# 5. Edge caching of the API surface still works (skip when hitting the origin
#    directly, which has no CDN in front).
case "$BASE" in
  https://events.nukk.net*)
    curl -s -A "$UA" -o /dev/null "$BASE/api/v1/events"  # prime
    cstat=$(curl -s -A "$UA" -D - -o /dev/null "$BASE/api/v1/events" | tr -d '\r' | awk -F': ' 'tolower($1)=="cf-cache-status"{print $2}')
    case "$cstat" in
      HIT|MISS|EXPIRED|REVALIDATED) pass "API edge cache active (cf-cache-status=$cstat)" ;;
      *) fail "API not edge-cached (cf-cache-status=${cstat:-none})" ;;
    esac ;;
esac

echo
if [ "$fails" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
  exit 0
fi
echo "$fails CHECK(S) FAILED"
exit 1

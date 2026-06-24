#!/usr/bin/env bash
# Create/replace the Cloudflare cache rule for events.nukk.net.
#
# Dry-run by default: prints the zone id, target endpoint, and rule JSON without
# mutating anything. Pass --apply to actually PUT the ruleset.
#
# Requires CF_API_TOKEN with Zone -> Cache Rules -> Edit on zone nukk.net.
# See deploy/cloudflare-cache-rule.md for the rationale.
set -euo pipefail

ZONE_NAME="nukk.net"
HOST="events.nukk.net"
APPLY=0
[[ "${1:-}" == "--apply" ]] && APPLY=1

: "${CF_API_TOKEN:?set CF_API_TOKEN (Zone->Cache Rules->Edit)}"
api() { curl -s -H "Authorization: Bearer $CF_API_TOKEN" -H "Content-Type: application/json" "$@"; }

ZONE_JSON=$(api "https://api.cloudflare.com/client/v4/zones?name=${ZONE_NAME}")
ZONE_ID=$(printf '%s' "$ZONE_JSON" | python3 -c 'import json,sys; r=json.load(sys.stdin).get("result") or []; print(r[0]["id"] if r else "")')
[[ -n "$ZONE_ID" ]] || { echo "ERROR: could not resolve zone id for ${ZONE_NAME} (token scope?). Response:"; echo "$ZONE_JSON"; exit 1; }

# NOTE: the bare "/" is deliberately NOT cached here. It is Accept-negotiated
# (HTML for browsers, JSON for agents) at one URL, and a URL-keyed edge cache (CF
# Free ignores Vary: Accept) would serve one variant to everyone. The root handler
# marks "/" private (browser-only) for the same reason; keep the two in sync.
EXPR='(http.host eq "'"$HOST"'" and not any(http.request.headers["accept"][*] contains "text/markdown") and (starts_with(http.request.uri.path, "/api/v1/") or http.request.uri.path eq "/llms.txt"))'

PAYLOAD=$(EXPR="$EXPR" python3 -c '
import json, os
print(json.dumps({"rules": [{
    "description": "events.nukk.net edge cache (JSON/HTML, respect origin TTL)",
    "expression": os.environ["EXPR"],
    "action": "set_cache_settings",
    "action_parameters": {
        "cache": True,
        "edge_ttl": {"mode": "respect_origin"},
        "browser_ttl": {"mode": "respect_origin"},
    },
}]}, indent=2))')

ENDPOINT="https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/rulesets/phases/http_request_cache_settings/entrypoint"

echo "zone:     ${ZONE_NAME} (${ZONE_ID})"
echo "endpoint: PUT ${ENDPOINT}"
echo "rule expression:"
echo "  ${EXPR}"
echo "payload:"
echo "${PAYLOAD}"

if [[ "$APPLY" -eq 0 ]]; then
  echo; echo "DRY-RUN — nothing applied. Re-run with --apply to PUT the ruleset."
  exit 0
fi

echo; echo "Applying..."
api -X PUT "$ENDPOINT" --data "$PAYLOAD"
echo; echo "Done. Verify: curl -sD- -o /dev/null https://${HOST}/api/v1/events | grep -i cf-cache-status (2nd hit = HIT)"

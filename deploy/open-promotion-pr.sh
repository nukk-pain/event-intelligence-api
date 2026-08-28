#!/usr/bin/env bash
# Open a human-review pull request carrying the day's discovery packet.
# Called by run-scout-discovery.sh only when EVENTSCOUT_PROMOTE_PR_TOKEN is
# present; without the token the daily run keeps its packet-on-disk behavior.
#
# The PR delivers the review packet — it never edits catalog code or the crawl
# allowlist itself. Merging the packet changes nothing at runtime; a human
# still lands the actual source as a normal reviewed commit (DECISIONS.md
# 2026-07-28, "Source promotion goes through code review").
#
# Idempotent: one branch per packet date. If the branch already exists on the
# remote, the run exits 0 without a new push or PR.
set -euo pipefail

PACKET_DIR="${1:?usage: open-promotion-pr.sh <packet-dir>}"
TOKEN="${EVENTSCOUT_PROMOTE_PR_TOKEN:?EVENTSCOUT_PROMOTE_PR_TOKEN is required}"
REPO_SLUG="${EVENTSCOUT_PROMOTE_REPO:-nukk-pain/event-intelligence-api}"
WORK_REPO="${EVENTSCOUT_PROMOTE_WORKDIR:-/srv/developer/events-intel/promo-repo}"
API="https://api.github.com/repos/${REPO_SLUG}"

PACKET_DATE="$(basename "$PACKET_DIR")"
BRANCH="scout/packet-${PACKET_DATE}"

if [[ ! -s "$PACKET_DIR/seed-candidates.jsonl" ]]; then
  echo "promote-pr: SKIP no candidates in $PACKET_DIR"
  exit 0
fi
CANDIDATES=$(wc -l < "$PACKET_DIR/seed-candidates.jsonl" | tr -d ' ')

auth=(-H "Authorization: Bearer ${TOKEN}" -H "Accept: application/vnd.github+json")

if [[ ! -d "$WORK_REPO/.git" ]]; then
  git clone --quiet --depth 1 "https://github.com/${REPO_SLUG}.git" "$WORK_REPO"
fi
git -C "$WORK_REPO" fetch --quiet --depth 1 origin main

# One packet, one branch. An existing remote branch means this packet was
# already delivered; rerunning the same day must not stack pushes or PRs.
if git -C "$WORK_REPO" ls-remote --exit-code --heads origin "$BRANCH" >/dev/null 2>&1; then
  echo "promote-pr: SKIP branch $BRANCH already delivered"
  exit 0
fi

git -C "$WORK_REPO" checkout --quiet -B "$BRANCH" origin/main
DEST="discovery-packets/${PACKET_DATE}"
mkdir -p "$WORK_REPO/$DEST"
for f in seed-candidates.jsonl catalog-snippet.go.txt allowlist-hosts.txt; do
  [[ -f "$PACKET_DIR/$f" ]] && cp "$PACKET_DIR/$f" "$WORK_REPO/$DEST/$f"
done
git -C "$WORK_REPO" add "$DEST"
if git -C "$WORK_REPO" diff --cached --quiet; then
  echo "promote-pr: SKIP packet adds nothing new"
  exit 0
fi

git -C "$WORK_REPO" \
  -c user.name="eventscout" -c user.email="eventscout@events.nukk.net" \
  commit --quiet -m "scout: deliver discovery packet ${PACKET_DATE} (${CANDIDATES} candidates)"
git -C "$WORK_REPO" push --quiet "https://x-access-token:${TOKEN}@github.com/${REPO_SLUG}.git" "$BRANCH"

NEW_HOSTS="none"
if [[ -s "$PACKET_DIR/allowlist-hosts.txt" ]]; then
  NEW_HOSTS="$(tr '\n' ' ' < "$PACKET_DIR/allowlist-hosts.txt")"
fi
BODY=$(python3 - "$PACKET_DATE" "$CANDIDATES" "$NEW_HOSTS" <<'PY'
import json, sys
date, count, hosts = sys.argv[1], sys.argv[2], sys.argv[3]
body = (
    f"Daily discovery delivered {count} model-accepted source candidate(s) "
    f"on {date}.\n\n"
    f"- Packet: `discovery-packets/{date}/` (candidates, paste-ready catalog "
    f"snippet, allowlist diff)\n"
    f"- New hosts not yet in the production allowlist: {hosts}\n\n"
    "Merging this PR changes no runtime behavior. To promote a source, review "
    "the packet against the official page, fill the missing fields, and land "
    "the catalog/allowlist change as a normal reviewed commit "
    "(DECISIONS.md 2026-07-28).\n\n"
    "Opened automatically by the eventscout daily timer."
)
print(json.dumps({
    "title": f"scout: discovery packet {date} ({count} candidates)",
    "head": f"scout/packet-{date}",
    "base": "main",
    "body": body,
}))
PY
)
HTTP=$(curl -sS -o /tmp/promote-pr-response.json -w '%{http_code}' "${auth[@]}" -X POST "$API/pulls" -d "$BODY")
if [[ "$HTTP" != "201" ]]; then
  echo "promote-pr: FAIL create PR http=$HTTP (see /tmp/promote-pr-response.json)" >&2
  exit 1
fi
PR_URL=$(python3 -c 'import json,sys;print(json.load(open("/tmp/promote-pr-response.json"))["html_url"])')
echo "promote-pr: OK ${PR_URL}"

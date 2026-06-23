#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROMO_DIR="$ROOT/docs/promo"
HTML="$PROMO_DIR/twitter-promo.html"
FRAMES="$PROMO_DIR/frames"
OUT="$PROMO_DIR/twitter-promo.gif"
PALETTE="$PROMO_DIR/twitter-promo-palette.png"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

if [[ ! -x "$CHROME" ]]; then
  echo "Google Chrome not found at $CHROME" >&2
  exit 1
fi

rm -rf "$FRAMES"
mkdir -p "$FRAMES"

PROFILE="$(mktemp -d /tmp/eventsintel-promo-chrome.XXXXXX)"
trap 'rm -rf "$PROFILE"' EXIT

for i in $(seq -w 0 63); do
  "$CHROME" \
    --headless=new \
    --disable-gpu \
    --disable-background-networking \
    --disable-sync \
    --disable-features=MediaRouter,OptimizationHints,AutofillServerCommunication \
    --no-first-run \
    --no-default-browser-check \
    --user-data-dir="$PROFILE" \
    --window-size=1200,675 \
    --screenshot="$FRAMES/frame-$i.png" \
    "file://$HTML?frame=$((10#$i))" >/dev/null 2>&1
done

ffmpeg -y -hide_banner -loglevel error \
  -framerate 12 \
  -i "$FRAMES/frame-%02d.png" \
  -vf "fps=12,scale=1200:-1:flags=lanczos,palettegen=max_colors=128" \
  "$PALETTE"

ffmpeg -y -hide_banner -loglevel error \
  -framerate 12 \
  -i "$FRAMES/frame-%02d.png" \
  -i "$PALETTE" \
  -lavfi "fps=12,scale=1200:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" \
  -loop 0 \
  "$OUT"

rm -f "$PALETTE"
echo "$OUT"

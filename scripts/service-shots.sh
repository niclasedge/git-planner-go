#!/usr/bin/env bash
#
# Takes one screenshot per reachable service and drops it where git-planner
# serves it from.
#
# The app decides what to photograph — it asks over /api/shots, which only lists
# services that answered their last probe. A service that is down would just
# yield a picture of an error page, and overwriting yesterday's working shot with
# that loses the one thing the thumbnail is for.
#
# Run it wherever the services are reachable. For the local Docker stack that is
# the Mac itself, not the container the app runs in: shot-scraper needs Chromium,
# which a 24 MB image has no business carrying.
#
# Usage:
#   scripts/service-shots.sh [--planner URL] [--out DIR] [--width N] [--height N]
#
# Exit codes: 0 all shots taken, 1 setup problem (no planner, no browser),
# 2 some shots failed — the run still wrote everything that worked.

set -uo pipefail

PLANNER="${PLANNER_URL:-http://127.0.0.1:8092}"
OUT=""
WIDTH=1280
HEIGHT=800
# Long enough for a JS dashboard to paint its first data — glance came out blank
# at 2500ms — and short enough that a dozen services stay inside a minute.
WAIT_MS=4000
# Per page. A dozen services must not be able to turn into a run that never ends.
PAGE_TIMEOUT_MS=20000

while [ $# -gt 0 ]; do
  case "$1" in
    --planner) PLANNER="$2"; shift 2 ;;
    --out)     OUT="$2"; shift 2 ;;
    --width)   WIDTH="$2"; shift 2 ;;
    --height)  HEIGHT="$2"; shift 2 ;;
    --wait)    WAIT_MS="$2"; shift 2 ;;
    --timeout) PAGE_TIMEOUT_MS="$2"; shift 2 ;;
    -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

# Default output: the repo's own data directory, which is the volume the
# container mounts. Resolved from this script's location so cron and Ansible do
# not have to care about the working directory.
if [ -z "$OUT" ]; then
  OUT="$(cd "$(dirname "$0")/.." && pwd)/data/shots"
fi

die() { echo "service-shots: $*" >&2; exit 1; }

# shot-scraper is a Python tool and often lives in a user bin that a
# non-interactive shell does not have on its PATH.
SHOT="$(command -v shot-scraper || true)"
for candidate in "$HOME/.local/bin/shot-scraper" /opt/homebrew/bin/shot-scraper; do
  [ -n "$SHOT" ] && break
  [ -x "$candidate" ] && SHOT="$candidate"
done
[ -n "$SHOT" ] || die "shot-scraper not found — install it with: uv tool install shot-scraper && shot-scraper install"

command -v jq >/dev/null 2>&1 || die "jq not found"

TARGETS="$(curl -fsS -m 15 "$PLANNER/api/shots")" \
  || die "cannot reach $PLANNER/api/shots — is git-planner running?"

COUNT="$(printf '%s' "$TARGETS" | jq 'length')"
if [ "$COUNT" = "0" ]; then
  echo "service-shots: no reachable service to photograph"
  exit 0
fi

mkdir -p "$OUT" || die "cannot create $OUT"

# One YAML file for shot-scraper multi, so all shots share a single browser
# start. Thirteen separate invocations would each pay Chromium's startup again.
# Explicit template: `mktemp -t name` is fine on macOS but GNU mktemp rejects it
# for want of X's, and this script runs under both.
SPEC="$(mktemp "${TMPDIR:-/tmp}/service-shots.XXXXXX")" || die "cannot create a temp file"
trap 'rm -f "$SPEC"' EXIT

printf '%s' "$TARGETS" | jq -r --arg out "$OUT" --argjson w "$WIDTH" --argjson h "$HEIGHT" --argjson wait "$WAIT_MS" '
  .[] | "- output: \($out)/\(.slug).png\n  url: \(.url)\n  width: \($w)\n  height: \($h)\n  wait: \($wait)"
' > "$SPEC"

echo "service-shots: $COUNT service(s) → $OUT"

# No --fail and no --skip: a service answering 500 on some asset should still be
# photographed, because the picture is the point, not the HTTP transcript. The
# timeout is per page, so one hanging service cannot hold up a nightly run.
if "$SHOT" multi "$SPEC" --timeout "$PAGE_TIMEOUT_MS"; then
  echo "service-shots: done"
  exit 0
fi

# multi reports a non-zero exit if any single shot failed, so count what landed
# rather than calling the whole run a failure.
WROTE=0
for slug in $(printf '%s' "$TARGETS" | jq -r '.[].slug'); do
  [ -s "$OUT/$slug.png" ] && WROTE=$((WROTE + 1))
done
echo "service-shots: $WROTE of $COUNT shots present after errors" >&2
exit 2

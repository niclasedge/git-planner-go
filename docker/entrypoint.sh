#!/bin/sh
# Container start: prepare the project directory, then bind the network
# listener to the container's own address. --host rejects 0.0.0.0 by design,
# so the concrete interface address is looked up here.
set -eu

REPO="$HOME/repo"
mkdir -p "$REPO"
# The image carries no .git. A bare repo with the real origin is enough for
# the GitHub card (it only reads `git remote get-url origin`).
if [ -n "${GKP_ORIGIN:-}" ]; then
  [ -d "$REPO/.git" ] || git init -q "$REPO"
  git -C "$REPO" remote remove origin 2>/dev/null || true
  git -C "$REPO" remote add origin "$GKP_ORIGIN"
fi

export CC_KNOWLEDGE_ROOTS="${CC_KNOWLEDGE_ROOTS:-$HOME}"
ADDR="$(hostname -i | awk '{print $1}')"
exec /usr/local/bin/git-planner --host "$ADDR" --port 8765 --cwd "$REPO" --no-open "$@"

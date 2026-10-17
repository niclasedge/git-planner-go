#!/usr/bin/env bash
# Regenerate docs/screenshots/*.png from the demo mode (fictional data only).
# Needs: go, git, bd, graphviz (dot), shot-scraper (with a Chromium installed).
set -euo pipefail
cd "$(dirname "$0")/.."
PORT="${PORT:-8791}"
OUT="docs/screenshots"
mkdir -p "$OUT"

go build -o /tmp/git-planner-demo-bin .
TMPDIR=/tmp /tmp/git-planner-demo-bin --demo --no-open --port "$PORT" >/tmp/git-planner-demo.log 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do
  curl -fsS -o /dev/null "http://127.0.0.1:$PORT/api/repos" && break
  sleep 1
done
sleep 5 # let the first scan of the demo workspace land

BASE="http://127.0.0.1:$PORT"
shot() { # name url height js
  shot-scraper "$2" -w 1400 -h "$3" --wait 1500 ${4:+--javascript "$4"} -o "$OUT/$1.png"
}
PICK='document.querySelector("[data-repo=\"acme/webshop\"]").click(); await new Promise(r=>setTimeout(r,2500));'

shot 01-repo-view "$BASE/" 1000 "(async()=>{ $PICK return 1 })()"
shot 02-issue-map "$BASE/" 1100 "(async()=>{ $PICK document.querySelector('.kn-karte-kopf').click(); await new Promise(r=>setTimeout(r,2500)); return 1 })()"
shot 03-issue-detail "$BASE/" 1100 "(async()=>{ $PICK [...document.querySelectorAll('.kn-e')].find(x=>x.textContent.includes('Search results page')).querySelector('.kn-e-details').click(); await new Promise(r=>setTimeout(r,3000)); return 1 })()"
shot 04-beads-map "$BASE/webshop/reports" 1100 ""
shot 05-evidence "$BASE/webshop/reports" 1300 "(async()=>{ const c=document.querySelector('input[type=checkbox]'); if(c.checked) c.click(); await new Promise(r=>setTimeout(r,800)); [...document.querySelectorAll('[data-rp-bead]')].find(x=>x.textContent.includes('Audit the current')).click(); await new Promise(r=>setTimeout(r,2500)); return 1 })()"
ls -la "$OUT"

# git-planner — bauen, testen, als Docker-Container betreiben.
#
# Lokal:  `just deploy`          Gate -> Image -> Container auf 127.0.0.1:8765 -> prüfen
#
# Kein systemd, keine gh-CLI: GitHub spricht der Dienst direkt über die
# GraphQL-API, mit GITHUB_PAT_PERSONAL / GITHUB_PAT_WORK aus .env. Compose reicht
# nur die dort genannten Variablen in den Container (compose.yml).
#
# Bind-Adresse: der Entrypoint (docker/entrypoint.sh) bindet an die Adresse
# des Containers, weil --host 0.0.0.0 ablehnt. POST-Routen (/mcp, /action/*)
# bleiben laut Flag-Vertrag auf Loopback, ausser GKP_WRITE_CLIENTS gibt sie frei
# (hinter Portmapping und Traefik passt der Host-Header nie, also faktisch aus).
port := "8765"
repo_dir := justfile_directory()
compose_local := "docker compose -f compose.yml -f compose.local.yml"
container := "git-planner"

# Gate -> Image bauen -> Container starten -> prüfen. Idempotent.
deploy: gate
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{ repo_dir }}"
    {{ compose_local }} up -d --build --remove-orphans
    just port={{ port }} verify

# Svelte-Bundle bauen (frontend). Laeuft NUR hier, nie zur Laufzeit.
#
# Warum das Ergebnis EINGECHECKT wird und nicht bei Bedarf entsteht:
# //go:embed auf einen fehlenden Pfad ist ein COMPILE-Fehler. Ein reiner
# generate-Schritt wuerde also `go build ./...` auf einem frischen Clone ohne
# Node nicht bloss ohne Frontend, sondern gar nicht bauen. Und ein
# Platzhalter, der kompiliert aber nichts ausliefert, waere schlimmer: der
# Dienst startete, nur die neue Ansicht bliebe leer — die stille
# Halb-Funktion, die diese Datei an mehreren Stellen zu vermeiden versucht.
#
# Der Preis ist Drift zwischen Quelle und Artefakt. Bezahlt wird er hier: das
# Gate baut das Bundle vor jedem go test neu, getestet wird also immer der
# frische Stand. Was nach dem Bau noch offen ist, ist ein fehlender Commit —
# dafuer der Hinweis am Ende.
#
# Das Image bleibt davon unberuehrt. Es bekommt KEIN Node: das
# Binary traegt das Bundle in sich.
frontend:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{ repo_dir }}/web"
    # Harter Abbruch statt Ueberspringen. Ein "Node fehlt, baue halt nicht"
    # liefe auf ein Gate hinaus, das gruen meldet und ein altes Bundle testet.
    command -v pnpm >/dev/null || {
      echo "pnpm fehlt — das Frontend-Bundle kann nicht gebaut werden." >&2
      echo "Auf dieser Maschine kommt node aus mise: 'mise install node'." >&2
      echo "Nur Go bauen (mit dem eingecheckten Bundle) geht weiterhin ohne Node: 'just build'." >&2
      exit 1
    }
    # pnpm install --frozen-lockfile statt pnpm install: reproduzierbar aus der Lockfile. Nur wenn
    # noetig, sonst kostet jedes Gate den vollen Neuaufbau von node_modules.
    if [ ! -d node_modules ] || [ pnpm-lock.yaml -nt node_modules/.modules.yaml ]; then
      echo "-> pnpm install --frozen-lockfile"
      pnpm install --frozen-lockfile
    fi
    echo "-> vite build"
    pnpm run build
    cd "{{ repo_dir }}"
    if [ -n "$(git status --porcelain -- internal/assets/static/app 2>/dev/null)" ]; then
      echo "Hinweis: das gebaute Bundle weicht vom eingecheckten Stand ab — internal/assets/static/app mit committen."
    fi

# Build-Gate: Frontend-Bundle + Go-Gate. Roter Stand bricht ab vor jedem
# Deploy. 'frontend' laeuft VOR dem Go-Gate, damit go test das frische Bundle
# sieht und nicht das vom letzten Mal.
gate: frontend gate-go

# Go-Gate allein: go build/vet/test + gofmt. Braucht kein Node — das Bundle
# ist eingecheckt; so läuft es auch in einem frisch geclonten Cloud-Container.
gate-go:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{ repo_dir }}"
    echo "-> go build ./..."
    go build ./...
    echo "-> go vet ./..."
    go vet ./...
    echo "-> go test ./..."
    go test ./...
    echo "-> gofmt -l ."
    fmt_out="$(gofmt -l .)"
    if [ -n "$fmt_out" ]; then
      echo "gofmt: nicht formatiert:" >&2
      echo "$fmt_out" >&2
      exit 1
    fi
    echo "Gate ok."

# Zustand pruefen: Container healthy, Ansicht erreichbar, Tokens gültig.
verify:
    #!/usr/bin/env bash
    set -euo pipefail
    status=""
    for _ in $(seq 1 30); do
      status="$(docker inspect -f '{{{{.State.Health.Status}}' {{ container }} 2>/dev/null || true)"
      [ "$status" = "healthy" ] && break
      sleep 1
    done
    if [ "$status" != "healthy" ]; then
      echo "FEHLER: {{ container }} ist '$status' statt healthy" >&2
      docker logs --tail 40 {{ container }} >&2 || true
      exit 1
    fi
    echo "healthy: {{ container }}"
    curl -fsS -o /dev/null "http://127.0.0.1:{{ port }}/"
    echo "erreichbar: http://127.0.0.1:{{ port }}/"
    # Tokens pruefen: der Dienst fragt die API selbst (frisch=1 umgeht den
    # Cache), so verlaesst kein Token-Wert den Prozess, und geprueft wird genau
    # der Stand, den die Ansicht zeigt.
    curl -fsS --max-time 60 "http://127.0.0.1:{{ port }}/api/repos?frisch=1" | python3 -c '
    import json, sys
    konten = json.load(sys.stdin).get("konten") or []
    if not konten:
        sys.exit("FEHLER: keine GitHub-Konten konfiguriert (GITHUB_PAT_PERSONAL/GITHUB_PAT_WORK in .env?)")
    fehler = 0
    for k in konten:
        pat, anzahl, grund = k.get("pat"), k.get("anzahl", 0), k.get("grund", "")
        if grund or not anzahl:
            fehler += 1
            print("FEHLER:", pat, "·", grund or "0 Repos", k.get("detail", ""), file=sys.stderr)
        else:
            print("token ok:", pat, "->", anzahl, "Repos")
    sys.exit(1 if fehler else 0)
    '

# Container-Status.
status:
    cd "{{ repo_dir }}" && {{ compose_local }} ps

# Logs folgen.
logs:
    cd "{{ repo_dir }}" && {{ compose_local }} logs -f --tail=200

# Container stoppen (Volume mit Cache und Reports bleibt).
down:
    cd "{{ repo_dir }}" && {{ compose_local }} down

# Go tests only
test:
    @go test ./...

# Build the binary
build:
    go build -o git-planner .

# Facts

Kurze, dauerhaft nützliche Erkenntnisse zu diesem Repo.

## Betrieb

- **`-env` ist CWD-relativ.** Default ist `.env` im Arbeitsverzeichnis. Wird das
  Binary aus einem anderen Verzeichnis gestartet (z. B. einem Build-Ordner),
  fehlen die Tokens *lautlos* bis auf eine WARN-Zeile
  (`token "work" skipped: environment variable GITHUB_PAT_WORK is empty`), und
  die App läuft mit einem Token weiter. Beim Start immer `-env <repo>/.env`
  mitgeben und die Startzeile prüfen: `tokens=2`.
- Fine-grained PATs können `/notifications` grundsätzlich nicht lesen (nur
  Classic-PAT). Die WARN beim Start ist erwartet, kein Fehler.
- **`.env` endet ohne Newline.** Ein `>> .env` klebt die neue Zuweisung an die
  letzte Zeile (`GITHUB_PAT_WORK=…SEMAPHORE_PASSWORD=…`) und macht damit *den
  vorherigen Token kaputt* — sichtbar nur als
  `token work: bad credentials`. Immer `printf '\n%s=%s\n'` anhängen und danach
  `awk -F= '{print NR": "$1}' .env` prüfen (zeigt nur die Namen, keine Werte).

## Semaphore

- Projekt heißt `IaC-Stack`, Projekt-ID 1, Instanz
  `http://100.109.141.47:3001` (Tailnet-IP von netcup3, kein Geheimnis).
  Passwort: `just semaphore-password` im IaC-Stack-Repo.
- API: `POST /api/auth/login` mit `{"auth":…,"password":…}` setzt eine
  Session-Cookie; Endpunkte darunter sind `/api/projects`,
  `/api/project/<pid>/templates`, `/api/project/<pid>/tasks?limit=N`,
  `/api/project/<pid>/tasks/<id>/output`. Alternativ `Authorization: Bearer`.
- Zeitstempel kommen als String und sind bei nie gestarteten Tasks `""` —
  darum `apiTime` statt `time.Time`.
- Deep-Links in die SPA: `/project/<pid>/templates/<tid>` und
  `/project/<pid>/history`.

## Tailnet

- `server.tailnet: true` hängt einen zweiten Listener an die Tailscale-Adresse
  dieses Rechners. Erkannt wird sie über die Ranges `100.64.0.0/10` und
  `fd7a:115c:a1e0::/48`, nicht über den Interface-Namen (`utun3` wechselt bei
  jedem Reconnect).
- Auf diesem MacBook ist Tailscale die App-Store-Variante: CLI liegt unter
  `/Applications/Tailscale.app/Contents/MacOS/Tailscale`, ein `tailscale` im PATH
  gibt es nicht.
- MagicDNS ist im Tailnet aktiv (`tail9b46fb.ts.net`), aber der **lokale**
  Resolver dieses Macs nutzt ihn nicht: `dig @100.100.100.100 <name>` antwortet,
  `ping <name>` nicht. Andere Geräte lösen normal auf — lokal die IP nehmen.
- Die eigene Tailnet-**IPv6**-Adresse ist vom selben Rechner aus nicht
  erreichbar (Connect-Timeout), IPv4 schon. Der Listener steht laut `lsof`; für
  Tests lokal immer v4 nehmen.

## Schreibpfad (Issue-Edit)

- Schreibende Requests sind unkonditional, gehen nicht durch den ETag-Cache und
  kosten Rate-Limit. Eine Write-Antwort darf nie in den Read-Cache — der Cache
  heilt sich danach selbst (Body geändert → ETag geändert → 200).
- Bei `403` nennt der Response-Header `X-Accepted-GitHub-Permissions` die
  fehlende Berechtigung des fine-grained PAT. Für Issue-Edits:
  `Issues: Read and write`.
- Das Due-Date lebt als kanonische Zeile `target date: YYYY-MM-DD` am Anfang des
  Bodys (`SetDue`/`ParseDue`/`StripDue`). Inline-Formen in Prosa
  (`… bis due: 2026-08-01 fertig`) werden bewusst nicht angefasst.

## CSS

- Spezifität schlägt Reihenfolge: `.ed-newlb .in { flex: 0 1 200px }` (zwei
  Klassen) gewinnt gegen ein späteres `.ed-color { flex: 0 0 32px }` (eine
  Klasse). Der Color-Swatch trägt deshalb *kein* `.in`.

## Container (Docker Compose)

- Auf diesem Mac läuft **OrbStack**, nicht Docker Desktop (`docker info` →
  `OperatingSystem: OrbStack`). Relevant, weil Autostart und Host-Networking
  anders funktionieren als in jeder Docker-Desktop-Anleitung.
- `host.docker.internal` löst in OrbStack auf `0.250.250.254` auf und erreicht
  auch Dienste, die auf dem Mac **nur an 127.0.0.1** gebunden sind — geprüft
  gegen den eigenen published Port. Für die Bridge-Variante steht trotzdem
  `extra_hosts: host.docker.internal:host-gateway` im Compose, damit es auf
  Linux ebenfalls auflöst.
- OrbStack akzeptiert `-p <IP>:port:port` auf eine IP, die es auf dem Host **noch
  gar nicht gibt**, ohne Startfehler (getestet mit `100.64.99.99`). Damit ist die
  Reihenfolge Tailscale-vs-Daemon beim Boot kein Problem: der Bind auf die
  Tailnet-IP scheitert nicht, wenn Tailscale noch nicht hochgekommen ist.
- `restart: unless-stopped` greift nach `docker kill` **nicht** — Docker merkt
  sich das als manuellen Stopp. Zum Verifizieren taugt das also nicht. Und
  `docker exec … kill -9 1` tut ebenfalls nichts: PID 1 im eigenen
  PID-Namespace ist gegen Signale ohne Handler immun.
- `GITPLANNER_BIND` überschreibt `server.bind`. Existiert nur, damit dieselbe
  `config.yaml` nativ auf Loopback und im Container auf `0.0.0.0` bindet — die
  zwei erlaubten Interfaces macht dann das Port-Publishing.
- Im Monitor-Widget stehen auch die *lokalen* Dienste mit der Tailnet-IP dieses
  Macs, nicht als `localhost`: im Container wäre `localhost` der Container. Alle
  lokalen Docker-Dienste publishen auf `0.0.0.0` und sind darüber erreichbar.
- `grinde-viewer` ist die Ausnahme: der Prozess bindet **innerhalb** seines
  Containers auf `localhost:8091`, deshalb leitet der Publish
  (`8091 -> 100.123.180.46:8091`) nichts weiter. Der Container meldet trotzdem
  `healthy`, weil sein Healthcheck intern läuft. Von außen komplett tot.

## Ollama

- Läuft nativ auf diesem Mac, Version 0.32.5, `127.0.0.1:11434` — **nur
  Loopback**, über die Tailnet-IP nicht erreichbar. Aus dem Container also
  `host.docker.internal:11434`.
- API entspricht der CLI: `/api/tags` = `ollama list`, `/api/ps` = `ollama ps`,
  `/api/version`. Beide Listen liegen unter `{"models":[…]}`, ein geladenes
  Modell ist dasselbe Objekt mit `size_vram` und `expires_at` dazu.
- **Einen Ladezeitpunkt gibt die API nicht her.** `expires_at` wird von *jeder*
  Anfrage nach vorn geschoben, ist also der Zeitpunkt der letzten Nutzung plus
  `keep_alive` — und `keep_alive` selbst ist über die API nicht auslesbar. Wer
  „läuft seit X" zeigen will, muss selbst beobachten und die Untergrenze
  kennzeichnen.
- `details.parameter_size` fehlt bei MLX-Modellen (`format: safetensors`) und ist
  bei kleinen Modellen ein Absolutwert (`999.89M`, `137M`), nicht immer „7B".

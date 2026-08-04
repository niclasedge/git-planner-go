# git-planner-go

Ein GitHub-Dashboard aus einer einzigen Go-Binary: Issues über alle Repos, ein
Actions-Tracker und beliebig viele weitere Seiten, die aus `config.yaml` kommen.

Der eigentliche Punkt ist nicht die Oberfläche, sondern **wie oft man GitHub
fragen darf**. Jeder Request ist bedingt (`If-None-Match`), ETags überleben den
Neustart in SQLite, und GitHub berechnet eine `304 Not Modified` nicht auf das
Rate-Limit. Ein Poll, der nichts Neues findet, ist damit gratis.

Gemessen gegen die echte API, über **224 Repos**:

```
kalte Runde    592 Requests   remaining 5000 → 4377      (voller Preis, einmal)
warme Runde    588 × 304      remaining 3710 → 3710      (Delta 0)
zweite Runde   588 × 304      remaining 3710 → 3710      (Delta 0)
```

1176 bedingte Requests, Rate-Limit-Verbrauch **exakt 0**. Bezahlt wird nur, was
sich wirklich geändert hat: jede echte Änderung ist genau ein 200er.

Nachprüfbar unter `/api/status` — `cache_hits_304` steigt, `remaining` bleibt
stehen. Und weil beim Rendern nie gefetcht wird, kommt die Actions-Seite mit 48
Repo-Karten in **9 ms**, ein Filterwechsel unter 1 ms.

Die laufende Last ist inzwischen noch kleiner, weil nicht mehr jede Runde jedes
Repo anfragt. Gemessen über **225 Repos** mit 451 Issues:

```
5-Minuten-Poll     1 Request     changed=0                     (inkrementell)
Abgleich (1×/h)  225 Requests    cached_304=225  fetched_200=0  (gratis)
```

## Aufbau

```
main.go              Start, Flags, Graceful Shutdown
internal/gh          GitHub-Client (bedingte Requests), Issues, Actions, Repos, Inbox,
                     inkrementeller Poll (REST) + Issue-Summen (GraphQL)
internal/store       SQLite: ETag + Body pro (Token, URL)
internal/sched       Refresh-Intervall + quadratischer Backoff
internal/hub         hält den Live-Stand im RAM, refresht im Hintergrund
internal/panel       Widgets der config-getriebenen Seiten
internal/web         Templates, HTMX-Fragmente, Static-Assets (embed)
internal/config      config.yaml + .env
```

Die Regel, der das ganze Design dient: **beim Seitenaufruf wird nie gefetcht.**
Ein Request liest den letzten Stand aus dem Speicher und antwortet sofort.
Refreshes laufen auf einem Ticker in einer eigenen Goroutine.

Vier Dinge halten die Kosten unten:

- **Inkrementell statt Rundumschlag.** Alle 5 Minuten fragt *ein* Request
  `GET /issues?filter=all&state=all&since=…` nach allem, was sich seit dem letzten
  Poll geändert hat, und mergt es in den Bestand — statt 225 Repos einzeln
  anzuklopfen. Einmal pro Stunde läuft trotzdem ein voller Abgleich: der
  inkrementelle Endpunkt sieht nur Issues, die dieses Konto angelegt hat, dem sie
  zugewiesen sind, in denen es erwähnt wurde oder die es abonniert hat. Alles
  außerhalb dieser Auswahl findet nur der Abgleich — und der ist gratis, weil jede
  seiner 225 Anfragen bedingt ist.
- **`/notifications` als Änderungs-Trigger.** Ein 304 auf dem Postfach ist ein
  freier Request, der eine ganze Refresh-Runde erspart. Bewusst *ohne*
  `since=`-Parameter — ein wandernder Zeitstempel würde die URL bei jedem Poll
  ändern und den ETag-Cache aushebeln.
- **Ein Rate-Limit-Budget pro Token**, mit Reserve: der Hintergrund-Refresher
  darf einen interaktiven Request nicht aushungern.
- **Backoff bei Fehlern** (1, 4, 9, 16, 25 Minuten), aber nie später als der
  reguläre Termin — ein vorübergehender Fehler darf eine Seite nicht seltener
  aktualisieren als normal.

`search/issues` wird absichtlich nicht benutzt: eigener Bucket mit nur 30
Requests/Minute. Gefiltert wird im Speicher, das kostet Mikrosekunden statt
Rate-Limit.

Ein Detail, das man erst beim Messen sieht: **paginiert wird mit `&page=N`, nicht
über den `Link`-Header.** GitHub schickt `Link` bei einem 304 nicht mit — eine
Header-getriebene Kette bricht also genau dann ab, wenn der Cache funktioniert.
Hier war der Effekt, dass die Repo-Entdeckung nach einem Neustart lautlos von 224
auf 100 Repos schrumpfte.

Zwei weitere Befunde aus dem Messen:

- **`/issues?since=…` beachtet `If-None-Match` nicht** — eine identische
  Wiederholung liefert 200 und einen neuen ETag. Dieser eine Aufruf umgeht den
  Cache deshalb bewusst. Das ist ohnehin richtig: `since=` wandert mit der Uhr,
  jeder Poll wäre ein neuer Cache-Key, und der Store hätte alle 5 Minuten eine
  ~800-KB-Zeile mehr.
- **Die offen/in-Arbeit/geschlossen-Summen im Planner kommen über GraphQL.**
  `totalCount` fragt keine Nodes ab, kostet also ~1 Punkt aus dem *separaten*
  5000/h-GraphQL-Budget; 20 Repos pro Query. 225 Repos = 12 Anfragen, ohne einen
  einzigen REST-Request. Über REST wäre „wie viele geschlossene Issues hat dieses
  Repo" eine eigene Anfrage pro Repo gewesen.

## Start

```bash
cp .env.example .env      # Token eintragen, .env ist gitignored
go build -o git-planner .
./git-planner             # -config config.yaml  -env .env  -v
```

Dann `http://127.0.0.1:8092`. (Nicht 8090 — das ist der Port von `files-tauri`, und
der Monitor-Widget auf der letzten Seite beobachtet ihn.)

### Im Tailnet erreichbar machen

```yaml
server:
  bind: 127.0.0.1:8092
  tailnet: true
```

Damit hört der Server zusätzlich auf der Tailscale-Adresse dieses Rechners, auf
demselben Port. Die Adresse wird beim Start aus den Interfaces gelesen (alles in
`100.64.0.0/10` bzw. `fd7a:115c:a1e0::/48`) — es gibt nichts zu konfigurieren und
nichts synchron zu halten. Die Startzeile nennt jede Adresse:

```
INFO  listening addr=http://127.0.0.1:8092 tokens=2 pages=4
WARN  also listening on the tailnet — no authentication addr=http://100.x.y.z:8092
```

Auf dem Handy dann `http://100.x.y.z:8092` oder, mit aktiviertem MagicDNS,
`http://<rechnername>.<tailnet>.ts.net:8092`.

**Bewusst nicht `bind: 0.0.0.0`.** Die App hat keinen Login, ein Wildcard-Bind
würde das Issue-Editing jedem Netz anbieten, in das der Rechner sich einbucht —
Café-WLAN inklusive. So sind es genau zwei Interfaces: Loopback und Tailnet; ein
Verbindungsversuch aus dem lokalen LAN läuft ins Leere.

Was bleibt: **jedes Gerät im Tailnet darf mit den Tokens aus `.env` Issues
ändern.** Das ist das ganze Sicherheitsmodell — das Tailnet *ist* die
Authentifizierung. Schreibende Requests brauchen zusätzlich den `HX-Request`-Header
und einen Origin, der zum Host passt; das hält Browser davon ab, über
DNS-Rebinding auf die Tailnet-Adresse zu schreiben, ersetzt aber keinen Login.

Läuft Tailscale beim Start nicht, sagt eine WARN-Zeile das und die App bedient nur
Loopback — nach dem Start von Tailscale einmal neu starten.

### Als Dienst: Docker Compose

```bash
# Einmalig: die Tailscale-IPv4 dieses Rechners nach .env. Die App-Store-Variante
# von Tailscale legt kein `tailscale` in den PATH, daher der volle Pfad.
printf 'TAILSCALE_IP=%s\n' \
  "$(/Applications/Tailscale.app/Contents/MacOS/Tailscale ip -4)" >> .env

docker compose up -d --build
```

Die Adresse steht auch in der WARN-Zeile eines nativen Starts. Endet `.env` ohne
Zeilenumbruch, klebt das `>>` die neue Zuweisung an die letzte Zeile und zerstört
den Token davor — vorher prüfen: `tail -c 1 .env | xxd`.

`restart: unless-stopped` heißt: kommt nach einem Crash und nach einem Reboot von
selbst zurück, sobald der Docker-Daemon läuft. Was es *nicht* überlebt, ist ein
`docker compose down` — genau das ist der Unterschied zu `always`.

Zwei Dinge sind im Container anders, und beide sind gelöst statt umgangen:

**Der Bind.** Docker veröffentlicht Ports über das Container-Interface, ein
Loopback-Bind wäre von außen unerreichbar — der Container braucht also
`0.0.0.0`. Dieselbe Datei nativ gestartet darf das nicht. Deshalb überschreibt
`GITPLANNER_BIND` (in `docker-compose.yml` gesetzt) den Wert aus `config.yaml`;
die Datei behält ihr sicheres `127.0.0.1`. Wer die Ports erreicht, entscheidet
dann das Publishing, nicht die App:

```yaml
ports:
  - "127.0.0.1:8092:8092"
  - "${TAILSCALE_IP}:8092:8092"
```

Also genau dieselben zwei Interfaces wie beim nativen Start — und bewusst nicht
`0.0.0.0`. Die WARN-Zeile „server.tailnet ignored" im Container-Log ist korrekt:
den zweiten Listener macht hier Docker.

**`localhost` ist im Container der Container.** Die lokalen Dienste im
Monitor-Widget stehen deshalb mit der Tailnet-IP dieses Rechners in
`config.yaml` — die gilt nativ wie im Container. Einzige Ausnahme sind Dienste,
die nur auf Loopback lauschen und gar keinen Docker-Publish haben: die erreicht
`host.docker.internal` (per `extra_hosts` verdrahtet, damit es auch auf Linux
auflöst).

`config.yaml` und `.env` werden gemountet, nicht ins Image gebacken — das Image
trägt keinen Token. `./data` (SQLite-Cache) ist ein Volume. Der Healthcheck
fragt `/healthz`.

## Token

Tokens stehen **ausschließlich** in `.env`. `config.yaml` verweist über den
*Namen* der Umgebungsvariable darauf und ist deshalb committebar.

Bei einem **Classic PAT** braucht man diese Scopes:

| Scope           | wofür                                            |
|-----------------|--------------------------------------------------|
| `repo`          | Issues, auch in privaten Repos                   |
| `actions:read`  | Workflow-Runs und Jobs                           |
| `read:org`      | Organisations-Repos                              |
| `notifications` | der Postfach-Poll als Änderungs-Trigger          |

Bei einem **Fine-grained PAT** sind es Repository-Permissions statt Scopes, und
zwar pro Repo freigegeben:

| Permission                      | Stufe | wofür                  |
|---------------------------------|-------|------------------------|
| Repository → Metadata           | Read  | Repo-Liste             |
| Repository → Issues             | Read  | Issues und Planner     |
| Repository → Actions            | Read  | Actions                |

Den Änderungs-Trigger gibt es hier **nicht**: `/notifications` unterstützt
ausschließlich Classic PATs. Es fehlt keine Permission, die man suchen müsste —
GitHub antwortet `403 Resource not accessible by personal access token` und lässt
den Header `x-accepted-github-permissions` weg, mit dem sonst die nötige
Permission benannt wird. Die Doku sagt es direkt: „These endpoints only support
authentication using a personal access token (classic)."

Wichtig ist außerdem die Repo-Auswahl selbst: „All repositories" oder die
gewünschten Repos explizit anhaken. Ein Repo, das nicht ausgewählt ist, ist für
den Token nicht vorhanden (`404`, sogar für die Metadaten).

Der häufigste Stolperstein: ein Fine-grained PAT, bei dem *Actions* fehlt oder
nur einzelne Repos ausgewählt sind. Metadata reicht dann für die Repo-Liste, die
Runs kommen aber als `403` zurück. Das Log sagt genau das:

```
level=WARN msg="repo refresh failed" section=actions \
  err="niclasedge/foo: GET .../actions/runs?…: forbidden (check the PAT scopes)"
```

Ohne Postfach-Zugriff schaltet der Poll sich mit einer Warnung ab und die Seiten
refreshen auf ihrem eigenen Intervall weiter. Das ist kein Notbetrieb: der
Trigger zieht einen Refresh nur *vor*, er ist die Voraussetzung für keine
Funktion. Am Rate-Limit ändert er nichts — die Intervall-Refreshes sind ohnehin
304er. Was er spart, sind HTTP-Roundtrips, und die zählen aufs **sekundäre**
Limit (900 Punkte/Minute), von dem 304er *nicht* befreit sind. Bei vielen Repos
ist der wirksamere Hebel deshalb nicht der Trigger, sondern ein größeres
Intervall oder eine explizite `repos:`-Liste.

Repos, bei denen ein Feature schlicht ausgeschaltet ist (Actions aus, Issues
aus), zählen als `skipped` und nicht als Fehler.

### `401 bad credentials`, obwohl der Token in `.env` frisch ist

Fast immer nicht der Token, sondern die *Quelle*. `.env` gilt nach
dotenv-Konvention nur, wenn die Variable noch nicht gesetzt ist — eine alte
`export GITHUB_PAT=…` in der Shell gewinnt also gegen die Datei, die man gerade
bearbeitet hat. Das Log sagt es beim Start:

```
level=WARN msg="environment overrides .env" key=GITHUB_PAT \
  hint="the value from the environment is used; run `unset GITHUB_PAT` to use the file"
```

`unset GITHUB_PAT` (oder einmalig `env -u GITHUB_PAT ./git-planner`) behebt es.
Ausgegeben werden nur Variablennamen, niemals Werte.

## Seiten

**Seite 1 — Issues.** Eine flache Liste über alle Repos, neueste Aktivität oben.
Filter für Repo, Token, Label, Assignee und Freitext (matcht auch `#42`). Gefiltert
wird im Speicher, jeder Filterwechsel ist ein HTMX-Swap ohne API-Request.

**Seite 2 — Planner.** Drei Spalten, per Splitter verschiebbar: links Token-Switch,
Agenda und die Repo-Liste, in der Mitte die Issues *des gewählten Repos*, rechts
ein Issue im Detail (Markdown gerendert, Labels, Assignees, Target-Date).

Jede Repo-Zeile trägt ihre Kennzahlen: `offen/in Arbeit/geschlossen` plus einen
Segment-Balken, dazu 🎯 für Issues mit Target-Date und 🔀 für offene Pull Requests.
Repos ohne offene Issues sind ausgeblendet — „alle" zeigt sie.

Die Agenda ist repo-übergreifend und gruppiert nach Fälligkeit (überfällig, heute,
diese Woche, später). Das Datum kommt aus dem Issue-Body:

```
target date: 2026-08-01     ← die kanonische Form, ganze Zeile, wird aus der
                              Anzeige entfernt statt doppelt dargestellt
due: 2026-08-01             @due(2026-08-01)      📅 2026-08-01
fällig: 01.08.2026          ← deutsche Tag-zuerst-Schreibweise
```

Erste Übereinstimmung gewinnt; fehlt jede, gilt das Fälligkeitsdatum des
Milestones. Ein reguläres Ausdrucksmuster reicht dabei nicht — `2026-02-31` sieht
wie ein Datum aus, deshalb entscheidet Gos Parser. GitHubs eigene Issue-Felder
werden **nicht** benutzt: sie liefert die API nur in Repositories mit aktivierten
Custom Fields, und hier kamen sie unter beiden API-Versionen leer zurück.

**Seite 3 — Actions.** Eine Karte pro Repo: Erfolgsrate, eine Sparkline aus den
letzten Runs (Höhe = Laufzeit, wurzelskaliert, damit ein 40-Minuten-Ausreißer
nicht zwanzig 30-Sekunden-Runs zu Strichen plattdrückt) und die Run-Zeilen mit
Step-Dots. Filterbar nach Repo, Token und Status.

**Seite 4+ — was in `config.yaml` steht.** Widget-Typen: `monitor`, `semaphore`,
`ollama`, `bookmarks`, `iframe`, `html`. Ein neuer Typ ist ein Struct plus ein Template —
`Kind()` ist gleichzeitig der Template-Name, gesucht wird nichts.

### Widget `semaphore`

Zeigt den jüngsten Lauf jedes Templates eines Ansible-Semaphore-Projekts: oben
die roten mit den Log-Zeilen, die den Fehler erklären, darunter der Rest.

```yaml
- type: semaphore
  title: IaC-Stack · Semaphore
  url: http://100.109.141.47:3001
  project: IaC-Stack        # Name, nicht ID; Groß/Kleinschreibung egal
  user: admin
  password-env: SEMAPHORE_PASSWORD   # Variablenname, nicht der Wert
  # token-env: SEMAPHORE_TOKEN       # Alternative: API-Token statt Login
  limit: 400                # Größe des Task-Fensters
  cache: 2m
```

Semaphore führt eine flache Task-Historie, ein Template kann darin also mehrfach
vorkommen. „Was ist gerade rot“ heißt deshalb: pro Template nur den neuesten Lauf
behalten. `limit` muss dafür deutlich größer sein als die Zahl der Templates.

Vier Requests pro Aktualisierung (Projekte, Templates, Tasks, plus ein
Log-Abruf je rotem Lauf, maximal sechs). Die Session-Cookie wird
wiederverwendet, ein abgelaufener Cookie kostet genau einen neuen Login.

Fehlt das Passwort, verschwindet das Widget nicht — es zeigt einen Banner mit dem
Namen der Variable, die leer ist.

Jede Zeile ist aufklappbar und holt dann das vollständige Log des Laufs — erst
beim ersten Öffnen (`hx-trigger="toggle once"`), sonst wären 20 Templates 20
Requests, die niemand angefordert hat. Angezeigt werden die letzten 500 Zeilen,
mit Hinweis auf die ausgelassenen und einem Link in die Semaphore-UI. Abrufbar
sind nur Läufe, die gerade auf der Seite stehen — der Endpunkt ist damit kein
Leser für die ganze Instanz. ANSI-Farbcodes werden entfernt, statt sie als Markup
aus Fremdtext neu zu bauen.

### Widget `beads`

Zeigt die [beads](https://github.com/gastownhall/beads)-Task-Graphen der
genannten Repos **full-screen in drei Panes wie die Planner-Seite**: links die
Repo-Liste mit `offen / ready / erledigt`, in der Mitte oben die Ready-Queue
(offene, unblocked, kinderlos-Beads — „was kann ich jetzt tun") und darunter
der Baum (Epics mit eingerückten Kind-Tasks, Prioritäten, Tags), rechts die
Details des angewählten Beads inklusive Beschreibung, Kind-Aufgaben und — bei
einem per `external_ref: gh-<n>` migrierten Issue — dem Link dorthin. Jede
Zeile ist **einzeilig**: Titel links, Labels und `ID · P<prio>` rechts. Ein
Bead, das auf ein anderes wartet, hängt eingerückt unter seinem Blocker —
dieselbe Einrückung wie bei Kind-Tasks, der gelbe Punkt markiert das Warten,
und der Tooltip des Punkts nennt die Blocker (das gilt als Reihenfolge: erst
den Blocker erledigen, dann das Wartende). Die
Panels sind wie beim Planner per Drag verschiebbar und merken sich ihre Breite.

```yaml
- type: beads
  title: Beads · Task-Graph
  token-env: GITHUB_PAT     # Variablenname, nicht der Wert
  cache: 5m
  repos:
    - niclasedge/IaC-Stack
```

Gelesen wird **nicht** die Dolt-Datenbank (die erreicht GitHub nur als
`refs/dolt/data`, für einen Viewer unbrauchbar), sondern der committete
JSONL-Export `.beads/issues.jsonl` — den erzeugt `export.auto` in der
`.beads/config.yaml` des jeweiligen Repos. Der Abruf läuft über die
Contents-API mit `If-None-Match`; ein unveränderter Export ist damit ein
gratis-304 wie überall sonst in dieser App.

Die Repo-Liste ist bewusst explizit statt entdeckt: jedes Repo *ohne* die
Datei würde pro Runde einen 404 kosten, und 404er sind — anders als 304er —
nie gratis. Ein gelistetes Repo ohne Export verschwindet nicht, sondern sagt
„keine Beads-DB": ein Tippfehler in der Liste soll sichtbar sein.

### Widget `ollama`

Beantwortet die zwei Fragen, die `ollama list` und `ollama ps` beantworten: was
liegt auf der Platte, und ist gerade etwas geladen. Das geladene Modell steht
oben in einem eigenen Block, der Rest darunter nach Pull-Datum.

```yaml
- type: ollama
  title: Ollama
  url: http://host.docker.internal:11434   # nativ: http://127.0.0.1:11434
  timeout: 10s
  cache: 5m
```

Ollama lauscht nur auf Loopback, ein Container erreicht es deshalb über
`host.docker.internal`. Drei Requests pro Aktualisierung (`/api/tags`,
`/api/ps`, `/api/version`); scheitert `/api/ps`, bleibt das Inventar trotzdem
stehen und nur die Ladeinfo fehlt.

**„läuft seit" kommt aus eigener Beobachtung, nicht von Ollama.** Die API nennt
keinen Ladezeitpunkt — `expires_at` wird von jeder Anfrage nach vorn geschoben.
Das Widget merkt sich also, wann es ein Modell zuerst geladen gesehen hat; die
Auflösung ist damit das Refresh-Intervall. War das Modell schon beim ersten
Abruf geladen, steht dort **„mind. X"**, denn dann kann es beliebig viel länger
laufen. Entlädt ein Modell, wird der Zeitstempel verworfen — ein Neuladen erbt
das Alter nicht.

Die Entladezeit steht als **Uhrzeit** da, nicht als Countdown: die Daten sind bis
zu ein Intervall alt und Ollamas `keep_alive` ist selbst fünf Minuten, ein
gerechnetes „entlädt in 4m" wäre also etwa so oft falsch wie richtig.

### Widget `monitor`

HTTP-Checks, entweder als flache Liste (`sites:`) oder gruppiert (`groups:`) —
„läuft hier“ und „läuft auf dem Server“ sind zwei Fragen, die eine gemeinsame
Liste nicht beantwortet. Beides lässt sich mischen; die flache Liste wird zur
ersten, titellosen Gruppe.

```yaml
- type: monitor
  title: Dienste
  cache: 60s
  failures-only: false      # true blendet gesunde Zeilen aus, Gruppen inklusive
  groups:
    - title: Lokal · Docker
      sites:
        - title: glance
          url: http://localhost:3000
          timeout: 3s
    - title: netcup3 · Docker
      sites:
        - title: Semaphore
          url: http://100.109.141.47:3001
        - title: Kibana
          url: http://localhost:5601
          check-url: http://localhost:5601/api/status   # Link ≠ Health-Endpunkt
```

Unter `failures-only` verschwindet eine Gruppe komplett, wenn alles grün ist —
Überschrift eingeschlossen. Die Latenz steht in Millisekunden, und damit wird die
Gruppierung selbst zur Aussage: lokal ~10–30 ms, netcup3 ~80–140 ms.

#### Öffentlich erreichbar oder nur im Tailnet?

Bei einem Host, der manches über Traefik ins Internet hängt und manches nur ins
Tailnet, sagt der interne Check nichts über die Erreichbarkeit von außen. Dafür
gibt es `public-url` — dieselbe Anwendung, wie die Welt sie sieht:

```yaml
- title: ntfy
  url: http://100.109.141.47:8080          # intern, über das Tailnet
  public-url: https://ntfy.niclasedge.com  # die Traefik-Route
```

Daraus wird ein Badge neben dem Namen, in vier Zuständen:

| Badge | Bedeutung |
|---|---|
| `intern` | keine `public-url` konfiguriert — nur über das Tailnet |
| `öffentlich` | antwortet öffentlich, **ohne** Authentifizierung (2xx/3xx) |
| `öffentlich · Auth` | antwortet öffentlich, verlangt aber Zugangsdaten (401/403) |
| `Route defekt` | `public-url` konfiguriert, antwortet aber nicht |

`öffentlich · Auth` ist bewusst ein eigener Zustand und nicht in `öffentlich`
eingerechnet: beide heißen, dass der Port von außen offen ist, aber nur einer
heißt, dass jeder hineinkommt. Deshalb ist auch nur `öffentlich` farblich
hervorgehoben — ob das richtig ist, kann das Widget nicht wissen, aber es soll
niemanden überraschen.

Geprüft wird die öffentliche Adresse in einem eigenen, langsamen Takt
(`public-interval`, Default 15m): eine Traefik-Route ändert sich, wenn jemand sie
ändert, und diese Requests verlassen die Maschine. Ein Durchlauf, der den Check
überspringt, lässt den letzten Befund stehen.

Eine Gruppe fremder Dienste bekommt `external: true` und damit **kein** Badge —
„ist das öffentlich erreichbar“ ist eine Frage über eigene Hosts; `api.github.com`
als „intern“ zu bezeichnen wäre eine Aussage über eine Installation, die nicht
uns gehört.

Welche Domain zu welchem Dienst gehört, steht im IaC-Stack in `services.yml`
(`domain_suffix: niclasedge.com`); wer dort kein `domain:` hat, ist bewusst
intern.

#### Screenshots je Dienst

Jede Zeile kann ein Vorschaubild tragen, das beim Überfahren größer wird. Die App
**macht die Bilder nicht selbst** — dafür bräuchte sie einen Browser, und ein
24-MB-Image hat in Chromium nichts zu suchen. Sie liefert nur zwei Endpunkte:

```
GET /api/shots            [{"slug":"lokal-docker-glance","url":"http://…"}, …]
GET /shots/<slug>.png     das Bild
```

`/api/shots` listet **nur erreichbare** Dienste — ein Bild der Fehlerseite an die
Stelle eines funktionierenden zu schreiben verliert genau das, wofür das
Vorschaubild da ist. Der Slug enthält den Gruppennamen, weil derselbe Dienstname
in mehreren Gruppen vorkommt: „SearXNG“ läuft hier *und* auf dem Server.

Bilder erzeugt `scripts/service-shots.sh` mit
[shot-scraper](https://github.com/simonw/shot-scraper) — ein Browserstart für
alle Dienste via `shot-scraper multi`:

```bash
scripts/service-shots.sh                       # nach data/shots/
scripts/service-shots.sh --planner http://…    # anderer Planner
```

Das Script muss dort laufen, wo die Dienste erreichbar sind — für den lokalen
Docker-Stack also auf dem Mac, nicht im Container. Im IaC-Stack erledigt das
täglich um 6:30 der Semaphore-Job **Planner Service Shots**
(`ansible/playbooks/jobs/local/planner_service_shots.yml`, `hosts: macbook`).

`server.shots-dir` sagt der App, wo sie nachsehen soll (Default `./data/shots`,
im Container das gemountete Volume). Gelesen wird beim Refresh des Widgets, nicht
beim Rendern: ein `stat` pro Dienst pro Seitenaufruf hat im Request-Pfad nichts
verloren. Die Bild-URL trägt die `mtime` als `?v=`, damit ein neues Bild den
Browser-Cache sicher durchbricht — und darf deshalb eine Woche gecacht werden.

## config.yaml

```yaml
server:
  bind: 127.0.0.1:8092
  db-path: ./data/git-planner.db

tokens:
  - name: personal
    env: GITHUB_PAT       # Name der Variable, nicht der Wert
    label: niclasedge
  - name: work            # zweites Konto: eigener Cache, eigenes Rate-Limit
    env: GITHUB_PAT_WORK
    label: niclasedge-dgk

github:
  repos: []               # leer = alles entdecken, was der Token sieht
  refresh:
    notifications: 60s    # GitHubs X-Poll-Interval gewinnt, wenn höher
    issues: 5m            # inkrementell: ein Request für alles Geänderte
    actions: 5m           # kein „runs since"-Endpunkt, bleibt ein bedingter Sweep
    repos: 30m
    full: 1h              # voller Abgleich + GraphQL-Summen
  actions:
    runs-per-repo: 10
    jobs-per-repo: 3      # Kostenschraube: ein Request pro Run für die Step-Dots

pages:
  - title: Issues
    type: issues
  - title: Planner
    type: planner
  - title: Actions
    type: actions
  - title: Monitoring
    columns:
      - size: small
        widgets:
          - type: monitor
            sites:
              - title: GitHub API
                url: https://api.github.com
                check-url: https://api.github.com/zen
```

Jedes Token bekommt sein eigenes Rate-Limit-Budget, seinen eigenen
Cache-Namensraum und seinen eigenen Postfach-Poll. Ein Arbeitskonto frisst also
nicht die 5000/h des privaten. Der Token-Switch links oben im Planner erscheint
erst, sobald mehr als ein Token konfiguriert ist; bei einem bleibt die Zeile leer.

`repos: []` entdeckt bis zu 300 Repos pro Token (neuester Push zuerst, archivierte
übersprungen). Bei vielen Repos lohnt es, sie explizit zu nennen — das ist der
größte Hebel auf die Requestzahl.

## Endpunkte

| Pfad                   | Zweck                                        |
|------------------------|----------------------------------------------|
| `/`, `/issues`         | Seite 1                                      |
| `/planner`             | Seite 2 (`?repo=`, `?issue=`, `?token=`, `?empty=1`) |
| `/actions`             | Seite 3                                      |
| `/p/{slug}`            | config-definierte Seiten                     |
| `/htmx/…`              | Fragmente für die Swaps                      |
| `POST /refresh/{what}` | manueller Refresh (`issues`/`actions`/`counts`/`all`) |
| `/api/status`          | Rate-Limit, Cache-Größe, Stand pro Sektion   |
| `/healthz`             | Liveness                                     |

`/api/status` ist das Messinstrument für die Kernaussage — dort sieht man
`cache_hits_304` steigen, während `remaining` stehen bleibt.

## Abhängigkeiten

`yaml.v3`, `modernc.org/sqlite` (pures Go, kein CGO) und `goldmark` für die
Markdown-Vorschau im Detail-Bereich. HTMX 2.0.4 liegt lokal im Repo, kein CDN,
ebenso die Schriften (IBM Plex Sans und Mono, SIL OFL 1.1, Lizenztext liegt bei).
Kein Web-Framework, kein Build-Step fürs Frontend — die Static Assets werden per
`embed` in die Binary gelegt und unter einem Hash-Pfad `immutable` ausgeliefert.

## Herkunft

[glance](https://github.com/glanceapp/glance) war die Inspiration für den
`config.yaml`-Ansatz, die Widget-Struktur und die Idee des Backoffs. Übernommen
wurden nur Konzepte — Code, CSS und Templates sind hier eigenständig geschrieben.
glance steht unter AGPL-3.0, dieses Projekt unter MIT; deshalb die klare Trennung.

Die Actions-Optik geht auf ein eigenes HTML-Mockup zurück, das Planner-Layout auf
die GitHub-Seite von `files-tauri` — beides eigener Code. Die Farben sind
[Catppuccin](https://catppuccin.com) (Mocha und Latte, MIT), umgesetzt über zwei
CSS-Ebenen: Paletten-Variablen und darauf aufsetzende Rollen-Variablen. Der
Theme-Umschalter tauscht nur `data-theme` auf `<html>`; die Voreinstellung folgt
`prefers-color-scheme`.

## Lizenz

MIT — siehe [LICENSE](LICENSE).

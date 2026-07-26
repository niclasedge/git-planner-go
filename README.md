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

**Seite 4+ — was in `config.yaml` steht.** Widget-Typen: `monitor`, `bookmarks`,
`iframe`, `html`. Ein neuer Typ ist ein Struct plus ein Template — `Kind()` ist
gleichzeitig der Template-Name, es gibt kein Switch zum Erweitern.

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

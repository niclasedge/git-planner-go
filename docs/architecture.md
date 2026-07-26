# git-planner-go — Architektur

Ein Go-Binary, HTMX-Frontend. Kombiniert GitHub-Issue-Planer, Actions-Tracker
und ein config-getriebenes Monitoring-Dashboard.

## Stand (2026-07-25)

**Gebaut und live verifiziert:** bedingte Requests mit SQLite-ETag-Cache,
Multi-Token, Hintergrund-Refresh mit Backoff, `/notifications` als
Change-Trigger, Seite 1 (Issues, lesend), Seite 2 (Actions), config-getriebene
Seiten mit `monitor`/`bookmarks`/`iframe`/`html`, `/api/status`.

Messung des Kernversprechens: 14 bedingte Requests gegen den warmen Cache,
`remaining` unverändert — **304 kostet 0 Rate-Limit**.

**Zurückgestellt** (bewusst, MVP-Schnitt): Issue-Mutationen (schließen, Label,
Assignee), Projects v2 / GraphQL, `custom-api`-Widget, `default-filter` in der
Config. Die Abschnitte unten beschreiben insoweit den Entwurf, nicht den Code.

## Seitenmodell

| Seite | Quelle | Inhalt |
|---|---|---|
| 1 `/` | eingebaut | GitHub Issues über alle getrackten Repos. Filter, Mutationen (schließen, Label, Assignee). |
| 2 `/actions` | eingebaut | Workflow-Runs über alle Repos. Filter nach **Token/Account** und nach **Repo**. Design: `runs-overview.html`. |
| 3…n | `config.yaml` | Frei komponierte Widget-Seiten (Monitoring, Bookmarks, eigene APIs). |

Seite 1 und 2 sind eingebaut, aber über `config.yaml` parametrisierbar
(welche Repos, welche Default-Filter). Ab Seite 3 bestimmt allein die Config.

## Multi-Account

„Filter nach GitHub PAT Token" heißt: mehrere Identitäten gleichzeitig
(`gh auth status` zeigt lokal `niclasedge` und `niclasedge-dgk`). Jeder Token
bekommt:

- **eigenen Rate-Limit-Bucket** — 5000/h gilt pro Token, nicht pro App
- **eigenen ETag-Namespace** im Cache — dieselbe URL liefert je Token andere Sichtbarkeit
- **eigenes `/notifications`-Polling** mit eigenem `X-Poll-Interval`

Tokens stehen **nur** in `.env`; `config.yaml` referenziert sie per Env-Namen
und ist damit committbar.

## Refresh-Strategie

Zwei Regime, weil die APIs sich unterscheiden (live gemessen — siehe
`knowledge/glance-inspiration.md`):

1. **REST mit ETag** (Issues, PRs, Workflow-Runs): `If-None-Match` → 304 kostet
   **0** Rate-Limit. Darf aggressiv gepollt werden.
2. **GraphQL** (Projects v2, kein ETag): 1 Punkt pro Query von 5000/h. Langsamere
   Kadenz, dafür günstig.

**Change-Trigger:** `/notifications` liefert ETag + `Last-Modified` +
`X-Poll-Interval` (aktuell 60 s). Ein Poll dort sagt, *welche* Repos sich bewegt
haben — nur die werden nachgezogen. Alles andere bleibt aus dem SQLite-Cache.

**Persistenz:** SQLite hält Daten *und* ETags. Nach einem Restart ist der erste
Request eine Serie von 304ern → Kaltstart kostet fast nichts. Das ist der
Hauptunterschied zu glance, das alles nur im RAM hält.

## Config-Schema (Entwurf)

```yaml
server:
  bind: 127.0.0.1:8090

# Identitäten. Werte kommen aus .env, hier stehen nur die Env-Namen.
tokens:
  - name: personal
    env: GITHUB_PAT
    label: niclasedge
  - name: work
    env: GITHUB_PAT_DGK
    label: niclasedge-dgk

github:
  # leer = alle Repos, die der jeweilige Token sieht
  repos: []
  refresh:
    notifications: 60s   # respektiert X-Poll-Interval
    issues: 2m
    actions: 1m
    projects: 5m

pages:
  - title: Issues
    type: issues              # eingebaut
    default-filter:
      state: open
      assignee: "@me"

  - title: Actions
    type: actions             # eingebaut
    default-filter:
      token: all
      status: all

  # ab hier frei
  - title: Monitoring
    columns:
      - size: small
        widgets:
          - type: monitor
            sites:
              - title: kite-public
                url: https://…
      - size: full
        widgets:
          - type: custom-api
            title: Beliebige JSON-Quelle
            url: https://…
            template: |
              {{ range .JSON.Array "items" }}…{{ end }}
```

## Widget-Umfang ab Seite 3

Bewusst **nicht** glance' 25 Widgets nachbauen. Stattdessen die generischen, die
das meiste abdecken:

- `custom-api` — beliebige JSON-Quelle + Go-Template. Der Arbeitspferd-Typ:
  damit baut man sich in der Config fast alles selbst.
- `monitor` — HTTP-Uptime-Checks
- `bookmarks`, `iframe`, `html` — trivial

Weitere Typen kommen dazu, wenn sie konkret gebraucht werden (YAGNI).

## Design

Basis ist `runs-overview.html` (eigener Code, lizenzfrei nutzbar) — GitHub-Dark:

```
--bg:#0d1117  --surface:#161b22  --border:#30363d
--text:#e6edf3  --muted:#8b949e  --accent:#2f81f7
--green:#3fb950  --red:#f85149   (skipped: #6e7681)
```

Vokabular daraus: Card + `card-h`-Header, Stats-Zeile (große Zahl + kleines
Label), SVG-Sparkline (Balkenhöhe = Dauer, Farbe = Status, `<title>` als
Tooltip), Run-Zeile mit Step-Dots, `font-variant-numeric: tabular-nums`.
Bemerkenswert: die Referenz kommt **ohne JavaScript** aus — Interaktivität
kommt ausschließlich über HTMX dazu.

Von glance übernommen wird die *Struktur* (Spalten-Layout, Widget-Rahmen,
Seiten-Navigation), nicht der Code — siehe Lizenz.

## Lizenz

glance ist **AGPL-3.0**. Übernommen werden Architektur-*Ideen* (Interface-Zuschnitt,
Backoff-Formel, Config-Schema-Form) — die sind nicht schutzfähig. **Nicht**
übernommen werden CSS, Templates oder Theme-Code; die wären ein abgeleitetes
Werk und würden AGPL-3.0 auf dieses Repo erzwingen, inkl. §13 (Netzwerk-Nutzung
verpflichtet zur Quelloffenlegung).

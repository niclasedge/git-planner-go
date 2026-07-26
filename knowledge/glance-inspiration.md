# glance als Referenz — was wir übernehmen

> Analyse-Notiz von vor der Implementierung. Übernommen wurden ausschließlich
> Konzepte; Code, CSS und Templates in diesem Repo sind eigenständig geschrieben.
> glance steht unter AGPL-3.0, dieses Projekt unter MIT — daher die Trennung.
> Die Zeile „Echte POST/PATCH-Handler" unten ist Entwurf: Mutationen sind im
> MVP zurückgestellt.

Quelle: `glanceapp/glance` @ main, gecacht unter
`~/.opensrc/repos/github.com/glanceapp/glance/main`. 10.2k LOC Go, keine
Web-Framework-Dependency.

## 1:1 übernehmen

| Muster | Ort in glance | Warum |
|---|---|---|
| Pures `net/http` + Go-1.22-`ServeMux`-Pattern-Routing | `glance.go:439-465` | `GET /api/pages/{page}/content/{$}` braucht kein Gin/Chi. `go.mod` hat null Web-Deps. |
| `embed.FS` + MD5-Hash der Static-FS im URL-Pfad | `embed.go:41-49`, `glance.go:427` | Assets mit `max-age=24h` ausliefern; der Hash im Pfad invalidiert automatisch. Single-Binary-Deploy. |
| Panel-Interface `initialize / requiresUpdate(now) / update(ctx) / Render()` | `widget.go:126-139` | Klarer Vertrag. Bei uns: Issues, Projects, Actions, Notifications. |
| `nextUpdate time.Time` als Cache-Gate statt TTL-Map | `widget.go:173-183` | Kein Eviction, keine Map-Locks. Ein Feld pro Panel. |
| Exponential Backoff bei Fehlern | `widget.go:350-367` | `retries²` Minuten, gedeckelt bei 5 (=25 min), aber **nie später als der reguläre Update**. Sehr elegant. |
| `errPartialContent` vs `errNoContent` | `widget-utils.go:20-21`, `widget.go:293-325` | Panel zeigt Teilinhalt + Notice statt komplett auszufallen. Bei Multi-Repo-Fetch essenziell. |
| Generischer, reihenfolge-erhaltender Worker-Pool | `widget-utils.go:141-241` | 10 Worker default, Ergebnisse per `index` sortiert, Fast-Path für `len==1`. |
| Lazy Page Load: Shell sofort, Content nachgeladen | `glance.go:334-367` + `static/js/page.js:6-9` | Genau das HTMX-Muster — nur handgeschrieben. |

## Bewusst anders machen

| glance | Problem | Unser Ansatz |
|---|---|---|
| Kein ETag / Conditional Request | `decodeJsonFromRequest` behandelt **alles != 200 als Fehler** (`widget-utils.go:78`) — ein 304 würde als Fehler durchschlagen | `If-None-Match` + 304-Pfad als Erstbürger. Gemessen: **3× 304 = 0 Rate-Limit-Verbrauch**. |
| Keine Persistenz — Cache nur im RAM | Nach Restart wird alles neu gezogen | SQLite: ETags **und** Daten überleben den Restart. Kaltstart kostet dann fast nichts. |
| Nutzt `search/issues` | Search-API-Limit ist **30 req/min** (live gemessen) — der knappste Bucket überhaupt | `/repos/{o}/{r}/issues?since=…&sort=updated` → Core-Bucket (5000/h) **und** ETag-fähig. |
| Read-only, keine Mutations | Todo-Widget ist reines localStorage | Echte POST/PATCH-Handler für Issue-/Project-Mutationen. |
| `page.mu.Lock()` sperrt die **ganze Seite** beim Update | glance' eigenes TODO: `glance.go:404-407` — deshalb ist `handleWidgetRequest` dort `501 Not Implemented` | Ein HTMX-Endpoint + ein Lock **pro Panel**. Damit fällt genau der Block weg, an dem glance hängt. |
| 24 KB handgeschriebenes `page.js` | Macht deklarativ, was HTMX per Attribut löst | HTMX + kleine Inseln Vanilla-JS. |

## Die zwei Freshness-Regime (live gemessen, 2026-07-25)

| Surface | API | Conditional | Budget |
|---|---|---|---|
| Issues, PRs, Commits, Actions-Runs | REST | **ETag → 304 = 0 Kosten** | 5000/h |
| Projects v2 | **GraphQL only** (REST gibt 404) | kein ETag | 5000 Punkte/h — Projekt-Query kostet **1** |
| Suche | `search/issues` | — | **30/min** — meiden |
| `/notifications` | REST | ETag + `Last-Modified` + `X-Poll-Interval: 60` | eigener Bucket |

**Konsequenz für „nur laden wenn Änderung":** `/notifications` ist der billige
Change-Trigger — GitHub liefert die Poll-Kadenz selbst mit (`X-Poll-Interval`).
Ein Poll dort sagt, *welche* Repos sich bewegt haben; nur die werden per
ETag-Request nachgezogen. Projects v2 kostet nur 1 Punkt pro Query, das
fehlende ETag ist also weniger schlimm als befürchtet — 120 Polls/h = 120 von
5000 Punkten.

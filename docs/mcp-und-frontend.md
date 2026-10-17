# MCP und Web-Frontend: wie ein Report vom Agenten in die Ansicht kommt

Diese Datei beschreibt den Weg eines Fortschritts-Reports: was ein Agent über
MCP schicken kann, wo das landet, und wie es im Browser ankommt. Alle
Zahlen und Pfade unten sind am laufenden Dienst nachgemessen, nicht aus der
Absicht abgeleitet.

Der Endpunkt hat **zwei Hälften mit entgegengesetztem Charakter**, und die
Unterscheidung trägt den Rest der Datei:

| | `report` | `beads_*` (fünf Werkzeuge) |
|---|---|---|
| Was es anfasst | den Report-Store auf der Platte | den beads-Tracker (Dolt) |
| Form | append-only, nichts wird je überschrieben | Mutation, mit Vorher/Nachher |
| Bei Teilfehlern | **fail-open** — ein unbrauchbares Bild wird als `missing` vermerkt, der Report gilt | **fail-loud** — bds eigene Meldung kommt zurück, nichts wird beschönigt |
| Wirkt auf den Bead-Status | nein | ja, das ist der Zweck |

Historisch war nur die erste Hälfte da. Die zweite entstand, nachdem in einer
Sitzung dreimal ein Bead als `closed` ohne Beleg in der Ansicht stand: `bd
close` und der Report waren zwei unabhängige Handlungen, gekoppelt nur durch
das Erinnern des Agenten. `beads_close` koppelt sie.

## Der Kern: MCP und Frontend reden nicht miteinander

Das ist die eine Sache, die man verstanden haben muss, weil sonst jede
Fehlersuche in die falsche Richtung läuft:

**Es gibt keine Verbindung zwischen MCP-Endpunkt und Browser.** Kein WebSocket,
kein Server-Sent-Events, kein Push, kein gemeinsamer Prozessspeicher. Beide
Seiten sprechen ausschliesslich über **die Ablagen auf der Platte**:

```
Agent (Claude Code)                              Browser
      │                                             ▲
      │ POST /mcp   tools/call                      │ GET /<repo>/reports
      │   "report"  |  "beads_query|create|…"       │ HTML + window.DATA
      ▼                                             │
┌───────────────────────────────────┐    ┌──────────┴───────────┐
│ internal/mcp                      │    │ internal/server      │
│   report  → reports.Append        │    │ appctx.Collect…      │
│   beads_* → bdops.Run → bd (Dolt) │    │   liest beide Seiten │
└───────────────────────────────────┘    └──────────────────────┘
             │ schreibt                              │ liest
             ▼                                       ▼
  ~/.claude/reports/<repo>/<map>.jsonl + assets/*.png   ← Report-Store
  .beads/  (eigene Dolt-DB, im Repo)                    ← beads-Tracker
```

Die beiden Ablagen bleiben getrennt — und `beads_close` ist die einzige Stelle,
die beide in einer Handlung anfasst: erst den Beleg in die JSONL, dann
`bd close`. In dieser Reihenfolge, siehe unten.

Daraus folgt zweierlei, und beides ist in der Praxis wichtig:

- Ein Report ist **sofort dauerhaft**, auch wenn gerade kein Browser offen ist.
  Der Store ist die Wahrheit, die Ansicht nur eine Projektion.
- Die Ansicht aktualisiert sich **nicht von selbst**. Sie holt ihren Stand beim
  Seitenaufruf. Ohne Reload sieht ein offener Tab einen neuen Report nie.

## Was der Agent schicken kann

Sechs Werkzeuge. Der Endpunkt kennt genau drei JSON-RPC-Methoden
(`initialize`, `tools/list`, `tools/call`), spricht `protocolVersion`
`2025-06-18` festverdrahtet und nimmt maximal **1 MiB** Request-Body.

| Werkzeug | Was es tut |
|---|---|
| `report` | Ein Eintrag in den Report-Store. Der Rest dieses Abschnitts. |
| `beads_query` | Lesen: `list`, `show`, `ready`, `blocked`, `stats`, `dep_tree` — plus `gaps`, das bd nicht kann. |
| `beads_create` | Issues **und** Blocking-Kanten in einem `bd`-Aufruf. |
| `beads_update` | Feldsatz auf eine Id-Liste. Ids sind Pflicht. |
| `beads_close` | Schliessen — nur mit aktuellem `done`-Beleg, und der muss **sichtbar** sein (Bild oder `.dot`/`.svg`). |
| `beads_dep` | Kanten setzen und lösen. |

Details zu den fünf `beads_*` weiter unten, [Das beads-Schreib-API](#das-beads-schreib-api).

### Felder von `report`

| Feld | Pflicht | Bedeutung |
|---|---|---|
| `cwd` | ja | Absoluter Pfad. Daraus werden `repo`, `worktree` und `branch` abgeleitet — der Agent schickt sie **nicht** selbst. |
| `map` | ja | beads-Epic-Id der Karte, z.B. `gkp-ar7`. |
| `bead` | ja | Der bewegte Sub-Bead, z.B. `gkp-ar7.13`. |
| `kind` | ja | `plan` (Bead geht auf in_progress) oder `done` (Bead wird geschlossen). Nichts sonst. |
| `title` | ja | Kurztitel, eine Zeile. |
| `impact` | nein | Ein Satz Wirkung/Warum. |
| `proof` | nein, aber der Punkt der Sache | Liste von Belegen, siehe unten. |
| `notes` | nein | Freitext. |
| `supersedes` | nein | Id eines früheren Eintrags, den dieser korrigiert. |

### Belege (`proof`)

Ein Objekt je Beleg, **genau eine Variante pro Eintrag**:

| Variante | Form | Grenze |
|---|---|---|
| Kommando + Ausgabe | `{"cmd": "go test ./...", "out": "ok"}` | je Feld 256 KiB, darüber wird abgeschnitten und markiert |
| Codestelle | `{"file": "internal/mcp/mcp.go:243"}` | — |
| Screenshot | `{"image": "/abs/pfad.png"}` | nur PNG/JPEG/GIF/WebP, max. 10 MiB |

Bilder werden **kopiert**, nicht verlinkt: die Datei landet als
`assets/<entry-id>-<index>.<ext>` im Store, der Eintrag zeigt danach auf die
Kopie. Ein Pfad, der nicht existiert, zu gross ist oder kein echtes Bild ist
(Magic-Bytes, nicht Dateiendung), wird als `"missing": true` vermerkt — der
Report selbst schlägt deswegen **nicht** fehl.

### Was der Server dazutut

Der gespeicherte Eintrag hat mehr Felder als die Nutzlast: `id`, `at`
(RFC3339 UTC), `repo`, `worktree`, `branch`. Die drei Letzteren kommen aus
`cwd` — auch aus einem Worktree heraus korrekt, geprüft: Aufruf aus
`/tmp/gkp-wt-abnahme` ergab `worktree=gkp-wt-abnahme`,
`branch=gkp-ar7.2-abnahme`, während `repo` weiter `git-planner` blieb.

## Beispiel-Workflow

Ein Sub-Bead von Anfang bis Ende. Karte ist `gkp-ar7`, Bead ist `gkp-ar7.13`.

### 1. Arbeit beginnt — `kind: "plan"`

```json
{
  "jsonrpc": "2.0", "id": 1, "method": "tools/call",
  "params": {
    "name": "report",
    "arguments": {
      "cwd": "/home/user/work/git-planner",
      "map": "gkp-ar7",
      "bead": "gkp-ar7.13",
      "kind": "plan",
      "title": "graphviz nachinstallieren",
      "impact": "Ohne dot rendert kein Plandiagramm."
    }
  }
}
```

Antwort:

```json
{"content": [{"type": "text",
  "text": "report gespeichert - http://127.0.0.1:8765/git-planner/reports"}],
 "isError": false}
```

### 2. Arbeit fertig — `kind: "done"`, jetzt mit Belegen

```json
{
  "cwd": "/home/user/work/git-planner",
  "map": "gkp-ar7", "bead": "gkp-ar7.13", "kind": "done",
  "title": "graphviz installiert, Plandiagramme rendern",
  "proof": [
    {"cmd": "dot -V", "out": "dot - graphviz version 16.0.0 (0)"},
    {"file": "internal/collectors/beads.go:31"},
    {"image": "/tmp/plandiagramm.png"}
  ]
}
```

### 3. Etwas war falsch — `supersedes`

```json
{
  "cwd": "…", "map": "gkp-ar7", "bead": "gkp-ar7.13", "kind": "done",
  "title": "Korrektur: Version ist 16.0.0, nicht 15.x",
  "supersedes": "20260904T182516.123456789-abcd1234"
}
```

Der alte Eintrag bleibt sichtbar und wird als überholt markiert. Der Store ist
append-only — es wird nie eine Zeile überschrieben oder gelöscht.

### 4. Nachsehen

`http://127.0.0.1:8765/git-planner/reports` → Karte `gkp-ar7`,
darunter `gkp-ar7.13` mit beiden Einträgen.

## Das beads-Schreib-API

Fünf Werkzeuge, die den Tracker ändern. Sie teilen `internal/bdops`, das drei
Eigenschaften garantiert — jede davon ist eine Antwort auf einen konkreten
Fallstrick, nicht Vorsicht auf Vorrat:

1. **Fehler werden gemeldet, nicht geschluckt.** `shared.Run`, das die
   Collectors benutzen, verwirft `stderr` und kollabiert jeden Fehlschlag auf
   `("", false)`. Für die Ansicht ist das richtig — ein fehlendes `bd` darf die
   Seite nicht zerstören. Hier ist es falsch: ein Close, der nicht passiert ist,
   darf nie aussehen wie einer, der passiert ist. `bdops.Result` trägt
   `args`, `stdout`, `stderr`, `exit_code` und einen `reason`
   (`exit` | `timeout` | `not-found`).
2. **Keine Shell, nie.** Alles geht über `exec` mit einer Argumentliste, und vor
   den positionalen Ids steht ein `--`, damit ein Wert nie als Flag gelesen wird.
3. **Kein impliziter Zielbead.** `bd update` und `bd close` **ohne** Id treffen
   „das zuletzt angefasste Issue". Am Terminal ist das bequem, in einem API ist
   es eine Wanderfalle: eine leere Id-Liste hätte den Bead getroffen, den ein
   unabhängiger früherer Aufruf zufällig berührt hat. Jede Mutation verlangt
   mindestens eine ausdrückliche, geprüfte Id.

### `beads_close`: die Reihenfolge ist der Inhalt

```
0. Karte prüfen                   → map ist ein EXISTIERENDES Epic und JEDER
                                     Bead hängt darunter (beliebig tief)?
                                     Sonst: Ablehnung, VOR dem ersten Schreiben
1. mitgegebene Belege schreiben   → schlägt das fehl, bleibt der Bead OFFEN
2. Store neu lesen                → und JEDEN Bead im Aufruf prüfen,
                                     nicht nur den mit dem frischen Report
3. kein done-Eintrag → ablehnen   → ausser force: true
   done ohne Bild → ablehnen      → eigene Meldung, anderer Fix
4. erst dann bd close             → gruppiert nach gleichem Grund
```

#### Schritt 0: das Kartentor

Neu seit `gkp-few`, und die Null ist keine Kosmetik: der Store ist append-only.
Am 2026-09-14 lief ein `beads_close` mit `map: "gkp-yy5"` — eine Id, die es nie
gab. Das Werkzeug legte `gkp-yy5.jsonl` an, schrieb die Belege hinein, prüfte
das Beleg-Tor **gegen genau diese Datei** und schloss beide Beads mit
Erfolgsmeldung. Ein Tippfehler erzeugt sich seinen eigenen Zeugen. Unter der
echten Karte standen beide Beads danach als Lücke — geschlossen, ohne Beleg.

Deshalb steht die Prüfung **vor** Schritt 1 und nicht dazwischen: ein Eintrag,
der erst geschrieben und dann als fehlplatziert erkannt wird, lässt sich nicht
zurücknehmen. `bdops.CheckMapMembership`, aufgerufen von allen drei Türen —
`beads_close`, `report` und der Route `POST /action/task/schliessen`.

Was gilt:

- **Die Karte muss ein Epic sein.** Eine existierende Id, die kein Epic ist,
  wird abgelehnt: ein Tippfehler, der zufällig auf eine echte Task-Id fällt,
  ist gefährlicher als einer ins Leere, weil er plausibel aussieht. Das Label
  `wayfinder:map` wird *nicht* geprüft — es wird von `bd create --parent`
  vererbt und ist deshalb unbrauchbar.
- **Beliebig tief** darf der Bead unter der Karte hängen; die Elternkette wird
  nach oben gelaufen, ein `bd`-Aufruf je Ebene. Die Karte selbst zählt mit.
- **`force: true` hilft hier nicht.** Es ist die Ausrede für einen fehlenden
  Beleg, nicht für die falsche Akte.
- **Antwortet bd nicht, wird abgelehnt**, nicht durchgewunken. Ein abgelehnter
  Aufruf ist wiederholbar, ein falsch abgelegter Eintrag nicht. Praktische
  Folge: steht der Dolt-Server, schreibt auch `report` nichts mehr.

Die Ablehnung nennt **beide** Ids und, wo auffindbar, die wirkliche Elternkette
— also die Karte, die gemeint war:

```
bead "gkp-3oh" hängt nicht unter der Karte "gkp-ar7" — seine Elternkette ist
gkp-3oh → gkp-4e1. Es wurde nichts in den Report-Store geschrieben und nichts
geschlossen. Entweder ist die Karte falsch genannt (dann die aus der Kette
nehmen) oder der Bead hängt am falschen Elternteil (dann `bd update --parent`).
```

Kosten, gemessen: 473 ms für Karte + 1 Bead gegen dieses Repo, 646 ms für zwei.
Welche Karten es gibt, sagt `beads_query op=list type=epic limit=0`.

„Aktuell" heisst: der `done`-Eintrag liegt **nach** dem letzten `plan`-Eintrag.
bd erlaubt mehrfaches Schliessen, und ein `plan`, der einen `done` überholt,
*ist* ein Reopen — ein Beleg aus einer früheren Schliessung entschuldigt die
nächste also nicht. Die Regel steht in `reports.HasCurrentDoneEntry`, absichtlich
neben `Entry` und nicht in der Ansicht: die Ansicht rechnet damit ihre Lücken
aus, das Werkzeug seine Sperre, und zwei Kopien wären irgendwann uneinig.

#### Der Beleg muss sichtbar sein

Seit 2026-09-04 reicht ein Text-Beleg nicht mehr. `beads_close` verlangt im
aktuellen `done`-Eintrag mindestens **eine** dieser beiden Formen
(`reports.HasValidatedDoneEntry`):

| zählt | zählt nicht |
|---|---|
| `{"image": "/abs/pfad.png"}` — Screenshot des laufenden Stands oder gerendertes Diagramm | dasselbe Bild, wenn der Store es als `"missing": true` markiert hat (Pfad weg, zu gross, keine Magic-Bytes) |
| `{"file": "….dot"}` oder `{"file": "….svg"}`, mit oder ohne `:zeile` | `{"cmd", "out"}`, auch ausführlich |
| | `{"file": "….go:41"}` und jede andere Endung |

Grund: ein `{cmd,out}`-Beleg stammt von derselben Instanz, die „fertig"
behauptet, und nichts hindert sie daran, plausibel klingende Ausgabe zu
schreiben, die nie gelaufen ist. Ein Diagramm oder ein Screenshot muss aus dem
tatsächlichen Zustand erzeugt werden — teuer zu fälschen, mit einem Blick zu
prüfen. Die zwei Ebenen (kein `done` / `done` ohne Bild) melden **getrennt**,
weil die Reparatur verschieden ist: einmal einen Report schreiben, einmal ein
Bild an den vorhandenen hängen.

Die **Ansicht** bleibt bei der milderen Regel. Sie beschreibt Geschichte, und 22
Beads wurden unter der alten Regel legitim geschlossen; sie heute nachträglich
als Lücke zu malen, würde ein echtes Signal in Rauschen verwandeln. Streng beim
Schreiben, mild beim Lesen — die beiden Funktionen stehen deshalb direkt
nebeneinander in `internal/reports/evidence.go`.

**Die Sperre hängt am Werkzeug, nicht am Bead.** Ein `bd close` von der Hand
geht weiter durch; einen Close-Hook gibt es in bd 1.2.2 nicht (`bd hooks`
verwaltet ausschliesslich git-Hooks). Das ist offen als `gkp-7ng`.

### Kantenrichtung: `blocked`/`blocker`, nicht `from`/`to`

bd schreibt dieselbe Kante auf zwei entgegengesetzte Weisen —
`bd dep add <blocked> <blocker>` und `bd dep <blocker> --blocks <blocked>` —
und die Graph-Plan-Form invertiert es ein drittes Mal: `{from_key: A, to_key:
B, type: "blocks"}` speichert *„A hängt von B ab"*, also **B blockiert A**,
das Gegenteil der naheliegenden Leseweise.

In dieser Implementierung waren zuerst alle zehn Kanten verdreht; gefunden hat
das `bd blocked`, nicht ein Test. Deshalb heissen die Felder im API
`blocked`/`blocker` — das lässt sich nicht falsch lesen — und die Übersetzung
passiert an genau einer Stelle, festgehalten von
`TestBuildPlanEdgeOrientation`.

### Was `bd create --graph` still verschluckt

| Feld | Verhalten |
|---|---|
| `acceptance`, `design`, `notes`, `estimate` | vom `--graph`-Pfad **verworfen**, nur eine Warnung. `applyExtras` schickt dafür pro Issue ein nachgelagertes `bd update`. |
| `priority` | im Plan ein **int**, obwohl das CLI-Flag `"2"` und `"P2"` nimmt. `Priority.Level()` rechnet um; im Plan-Knoten ein `*int`, weil `0` (P0) ein echter Wert ist und `omitempty` ihn verschlucken würde. |

Alle drei Punkte sind gemessen, indem bd selbst nach unbekannten Feldern
gefragt wurde — nicht aus einer Doku abgeleitet, die es dafür nicht gibt.

### Unbekannte Felder werden abgewiesen

`encoding/json` verwirft unbekannte Felder standardmässig. Für ein Lesewerkzeug
ist das harmlos; hier heisst ein vertipptes Feld, dass der Wert bd nie erreicht
und **nichts das sagt**. Gemessen am allerersten echten `beads_create`-Aufruf:
er schickte `parent_key`/`parent_id` — bds interne Namen, nicht die dieses
Schemas, das `parent` heisst. Ohne Abweisung wären zwei elternlose Beads
ausserhalb jeder Map entstanden, gemeldet als Erfolg. `runOrError` dekodiert
deshalb mit `DisallowUnknownFields` und nennt das Feld:

```
Fehler: arguments: json: unknown field "parent_id" —
die erlaubten Felder stehen im inputSchema dieses Werkzeugs
```

### `beads_query op=gaps`

Die eine Abfrage, die bd grundsätzlich nicht beantworten kann: geschlossene
Sub-beads einer Karte **ohne** aktuellen `done`-Beleg. bd kennt den
Report-Store nicht. Sie geht absichtlich nicht über `appctx.CollectReportsData`,
sondern direkt über `bd show --children` — die Ansicht rendert dabei auch den
Plan-Graphen über `dot`, und eine Frage nach fehlenden Belegen soll kein
graphviz starten.

```json
{"map":"gkp-ar7","repo":"git-planner","gaps":[],"checked_closed_beads":18}
```

## Was nach dem Schreiben passiert

Zwei Dinge, unabhängig voneinander:

**a) Der Bead bekommt einen Kommentar.** `notifyBead` hängt die Report-URL per
`bd comments add` an den Bead — asynchron, 5 s Timeout, **fail-open**. Fehlt
`bd` im PATH oder hängt Dolt, wird das still verworfen und nur geloggt; der
Report bleibt gültig. Der Bead behält damit den Verlauf, der Store die Belege.

**b) Die Ansicht wird beim nächsten Aufruf neu berechnet.** Der Projekt-Kontext
hängt hinter einem Cache mit 15 min TTL (`CC_KNOWLEDGE_TTL_MS`), aber jeder
Zugriff vergleicht zusätzlich einen `reportsFingerprint` — Dateizahl und
neuester mtime der `*.jsonl` des Repos. Ein frischer Report ändert den
Fingerprint und erzwingt eine Neuberechnung, auch innerhalb der TTL.

Nachgemessen, mit warmem Cache und ohne Neustart: der Eintrag war **4,6 s**
später in `window.DATA` — die Zeit geht für `bd`- und `dot`-Subprozesse drauf,
nicht fürs Warten auf einen Cache-Ablauf.

> Kein Cache-Leeren und kein Dienst-Neustart nötig, um einen neuen Report zu
> sehen. Ein **Reload der Seite** dagegen schon, weil das Frontend nicht pollt.

Der Fingerprint deckt aber **nur den Report-Store**, nicht die beads-Seite — und
mit dem Schreib-API läuft das jetzt in zwei Richtungen auseinander:

- **`beads_close` entwertet den Cache selbst.** Es schreibt den Beleg *in* den
  Store, bevor es schliesst; damit ändert sich die JSONL, der Fingerprint sieht
  das, die Ansicht rechnet neu. Das war der häufigste Fall.
- **`beads_create`, `beads_update` und `beads_dep` nicht.** Sie ändern
  bd-Zustand, ohne eine `*.jsonl` anzufassen. Ein neuer Sub-Bead, ein geänderter
  Titel oder eine neue Blocking-Kante sind bis zu 15 min unsichtbar
  (`gkp-ar7.19`). Die Kante ist der ärgerliche Fall, weil sie den gerenderten
  Plan-Graphen ändert — also genau das, was man nach einem `dep`-Aufruf ansieht.

Dasselbe gilt für Projektliste und Plandiagramm.

Zum Leeren muss man wissen, dass es **zwei** Caches gibt:

- **Aggregate-Cache**, auf Platte: `~/.cache/cc-knowledge/cache.json`. Löschen
  wirkt sofort und prozessübergreifend — die Datei ist für alle Instanzen
  gemeinsam.
- **Projekt-Kontext-Cache**, nur im **Prozessspeicher**
  (`projectContextCache` in `internal/cache`). Genau hier hängen die
  Reports-Karte und die Sub-bead-Liste. Die Datei zu löschen bewirkt dafür
  **nichts**; es hilft nur ein `systemctl --user restart`, der Ablauf der TTL
  oder ein Report-Write über den Fingerprint.

Praktisch heisst das: nach einer bd-Änderung, die man sofort sehen will, den
Dienst neu starten. Nur `cache.json` zu löschen sieht wie ein Cache-Reset aus,
ist für diese Ansicht aber keiner.

## Wie das Frontend es liest

Der Server rendert eine Seite mit eingebettetem `window.DATA`; die
Reports-Karte ist `DATA.reports`, gebaut von `appctx.CollectReportsData`
(`bd list/show/graph` + `dot` + die JSONL-Dateien). `browser.js` rendert nur
noch, was dort steht — die Baumlogik (Lücken, supersedes) liegt bewusst in Go
und ist ohne Browser testbar.

Zwei Routen, nicht zu verwechseln:

| Route | Was |
|---|---|
| `GET /<repo>/reports` | die **Ansicht**. Das ist die URL, die `tools/call` zurückgibt. |
| `GET /reports/<repo>/assets/<datei>` | die **Beleg-Bilder**. Genau vier Segmente, `image/*` aus Magic-Bytes plus `X-Content-Type-Options: nosniff`. |

`reports` ist ein reserviertes Segment: `/reports/…` geht *ausschliesslich* an
den Asset-Handler und erreicht das Projekt-Routing nie. Eine Ansicht liegt
deshalb niemals unter `/reports/…`.

Eine dritte Route hängt daneben und gehört nicht zum Report-Store:
`GET /github?project=<name>` liefert offene Issues und Pull Requests des
`origin`-Remotes als JSON (Konto aus dem Remote-Host, 2 min TTL, Details im
README). Sie steht ausserhalb von `BuildContext`, weil zwei Netzaufrufe nicht
in den Pfad gehören, der ein Projekt öffnet — gemessen kalt 0,8–1,0 s.
`github` ist deshalb ebenfalls ein reserviertes Segment.

## Die Svelte-Schiene: Node zur Bauzeit, nie zur Laufzeit

Neben `browser.js` steht seit gkp-a1k eine zweite Frontend-Welt. Auf ihr faehrt
seit things-oberflaeche (#12) die Aufgabenansicht im Things-Design unter
`GET /things`; die Schiene traegt:

| Stelle | Was |
|---|---|
| `web/src/*.svelte`, `web/src/main.js` | die Quellen |
| `just frontend` (vite) | baut, laeuft auch als Abhaengigkeit von `just gate` |
| `internal/assets/static/app/app.js` | das Ergebnis, **eingecheckt**, CSS inline |
| `assets.AppJS` (`//go:embed`) | im Binary |
| `GET /assets/app/app.js` | ausgeliefert, ueber den schon vorhandenen Asset-Handler |

**Das Artefakt liegt bewusst im Repo.** `//go:embed` auf einen fehlenden Pfad
ist ein *Compile*-Fehler (gemessen: `pattern static/app/app.js: no matching
files found`). Ein reiner `go:generate`-Schritt wuerde also `go build ./...`
auf einem frischen Clone ohne Node nicht bloss ohne Frontend, sondern gar nicht
uebersetzen; und ein Platzhalter, der uebersetzt aber nichts ausliefert, waere
schlimmer — der Dienst startete, nur die neue Ansicht bliebe leer. Der Preis ist
Drift; bezahlt wird er dadurch, dass das Gate das Bundle vor jedem `go test`
neu baut und danach auf einen fehlenden Commit hinweist.

**Die systemd-Unit bleibt unveraendert.** Kein Node in ihrem PATH, kein neuer
Eintrag in der Liste, die im `justfile` bei `install-unit` begruendet ist. Der
Beleg dafuer ist ein Test, kein Versprechen:
`internal/assets/e2e_binary_test.go` baut das Binary, startet es in einem
leeren Verzeichnis mit einem node-freien PATH und holt sich das Bundle ueber
HTTP.

**Beide Welten teilen sich die Seite.** `main.js` montiert ausschliesslich in
`#kn-svelte-root` und tut sonst nichts; fehlt der Knoten — unter `/` immer —,
passiert nichts. `browser.js` bleibt die IIFE, die sie war.

### Die Things-Oberflaeche unter `/` (repo-leiste, #25)

`GET /` ohne Parameter (und weiter `/things`) liefert `render.ThingsPage`: eine
Huelle mit `dash.css`, `#kn-svelte-root` und `/assets/app/app.js`, ohne
`window.DATA` und ohne `browser.js`. Die bisherige Uebersicht steht unter
`/ueberblick`, `/?project=X` und `/<projekt>/…` bleiben die alte Ansicht.
`things` und `ueberblick` stehen in `reservedSegments`.

**Woher die Daten kommen.** Alles ueber die GitHub-REST-API (`internal/ghrest`,
net/http, kein gh und kein GraphQL), mit den PATs aus `GITHUB_PAT_WORK` und
`GITHUB_PAT_PERSONAL`:

| Route | Liefert | GitHub-Aufrufe |
|---|---|---|
| `GET /api/repos` | Kategorien (Areas aus `areas.json`, Schluessel `owner/repo`, `eingeklappt`) mit ihren Repos, dazu `konten` je PAT | `GET /user/repos` je PAT, seitenweise; dazu die GitHub-origins der lokalen Clones |
| `GET /api/arbeit?repo=` | `zuletzt` (fuenf zuletzt geschlossene Issues samt archiviertem Change), `reihe` (offene Issues in Abarbeitungsfolge mit Status und Change), `ohne_issue`, `prs`, `fortschritt`, `quellen` | Issues offen/geschlossen, `dependencies/blocked_by` nur fuer Issues mit Blockern, Branches, `git/trees/SHA?recursive=1` fuer den Default-Branch und jeden `change/*`-Branch, `git/blobs/SHA` fuer proposal/tasks/belege |
| `GET /api/detail?repo=&art=issue&nummer=` | Titel, Labels, Text (Markdown) | `GET /repos/…/issues/N` |
| `GET /api/detail?repo=&art=change&kennung=` | `proposal`, `tasks`, `design`, `specs`, `belege` | Blobs aus dem Baum, von dem der Change gelesen wurde |
| `GET /api/blob?repo=&sha=&name=` | Belegbild; nur png/jpg/gif/svg/webp, svg mit `CSP: sandbox` | `git/blobs/SHA` |
| `GET /api/beleg-galerie?repo=` | alle Belege aller Changes, juengster zuerst, je Beleg mit Blob-`sha`; dasselbe Bild eines Changes (Name ohne `N-`) nur mit der juengsten Einlieferung | keine eigenen, derselbe Stand wie `/api/arbeit` |
| `GET /api/geaenderte-bilder?repo=` | Bilddateien (png/jpg/jpeg/gif/webp/svg) der letzten 30 Commits des Default-Branches, je Pfad die neueste Aenderung, ohne Geloeschte, ohne Pfade unter `openspec/changes/**/belege/` und ohne Dateien mit dem Blob-SHA eines Belegs (beleg-bilder, #105) | `GET /repos/…/commits?sha=STANDARD&per_page=30`, je Commit `GET /repos/…/commits/SHA` (Dateien je sha ohne Ablauf gespeichert) |
| `GET /api/html-dateien?repo=` | HTML-Dateien (html/htm) aus dem Baum der Spitze des Standard-Branches, gleich wie alt (html-dateien-baum, #114; hoechstens die ersten 200 nach Pfad, sonst nennt der Grund das), nach letzter Aenderung die neuesten 10, je Datei `erstellt` (Commit mit `status: added` unter den letzten 30, sonst aeltester Commit des Pfad-Verlaufs, sonst leer), Adresse unter `/api/datei/` auf den Commit der letzten Aenderung und Bildzaehler (eingebettet / im Repo vorhanden oder fehlt / extern) mit fertigem Text (html-dateien, #112). Ohne Treffer leere Liste ohne Grund; die Oberflaeche zeigt dann „Keine HTML-Dateien im Repo“ | Spitze aus demselben Commit-Speicher wie `/api/geaenderte-bilder`, `git/trees/SPITZE?recursive=1` (je sha gespeichert), je Pfad `GET /repos/…/commits?sha=STANDARD&path=PFAD&per_page=1` (gespeichert, bis sich die Spitze aendert), je Datei `git/trees/COMMIT` und `git/blobs/SHA`, hilfsweise `GET /repos/…/commits?sha=COMMIT&path=PFAD` |
| `GET /api/datei/OWNER/REPO/COMMIT/PFAD` | Datei aus dem Baum des Commits, gerendert: html/htm als `text/html; charset=utf-8`, Bilder mit ihrem Typ, css als `text/css`, sonst `application/octet-stream`; immer `CSP: sandbox` (ohne allow-same-origin/allow-scripts) und `nosniff`; relative Verweise laufen gegen denselben Commit. `.`/`..`, leere Glieder, fremde Repos: 404 ohne GitHub-Anfrage | `git/trees/COMMIT`, `git/blobs/SHA` |
| `POST /action/task/bereich/einklappen` | schreibt `eingeklappt` in `areas.json` (Netz-Gate wie alle Schreibrouten) | — |

Die Reihenfolge entsteht in `internal/appctx/repoarbeit.go`: erst `im_pr`, dann
`fertig`, dann `in_arbeit`, danach topologisch ueber `blocked_by` (ein Blocker
steht immer vor dem, was er blockiert), bei Gleichstand nach Nummer. Ein
Change haengt am Issue ueber `GitHub-Issue #N` in `proposal.md` oder ueber den
Titel `KENNUNG: …`. Ein `change/KENNUNG`-Branch ueberlagert den Stand auf dem
Default-Branch, ausser der Change ist dort schon archiviert (dann ist der Branch
nur nicht geloescht worden). Die Leseregeln (Kaestchen, Gates, Belege) sind
dieselben wie lokal: `collectors.OpenspecChangesAus` ueber einen
`OpenspecLeser` — Dateisystem oder GitHub-Baum.

Blockiert ist ein Issue streng wie in `lage` (karte-blocker-streng, #101): mit
einem bekannten offenen Blocker oder wenn `issue_dependencies_summary.blocked_by`
mehr offene Blocker zaehlt, als `blockiert_durch` nennt (Abfrage gescheitert,
Blocker ausserhalb der offenen Liste). `blockiert` geht vor dem Stand des
Changes. Scheitert eine `dependencies/blocked_by`-Abfrage, traegt die Quelle
`abhaengigkeiten` in `quellen` den Grund.

Zwischengespeichert: Konten 5 min, Arbeitsliste 90 s, Baeume und Blobs ohne
Ablauf (sie sind ueber ihren sha unveraenderlich). `?frisch=1` umgeht die TTL.

**Bildansicht** (beleg-bilder, #105). Jedes Belegbild (Galerie „Belegt“, Zeile
eines Eintrags, Detailspalte) und jede Kachel unter „Geaenderte Bilder“ oeffnet
im selben Fenster `web/src/BildModal.svelte`: eingepasst (contain), Zoom ueber
+, −, „Einpassen“, „100 %“, Mausrad und Doppelklick, Verschieben mit gedrueckter
Maus, Blaettern mit ← und →, Schliessen per Klick daneben, `Esc` oder ×. Die
Komponente bekommt die Liste fertig und zeichnet nur.

**Log.** Jeder Abruf schreibt eine Zeile nach stderr, also ins Journal
(`just logs`), nie mit Tokenwert:

```
repo-leiste: pat=GITHUB_PAT_WORK quelle=umgebung repos=12
repo-arbeit: repo=owner/repo pat=GITHUB_PAT_WORK quelle=umgebung issues=2 prs=1 changes=7 zuletzt=5
```

**Schreiben auf Issues** (issue-verwaltung, #26). Unter `/action/issue/`, also unter dem
Netz-Gate (nur ueber 127.0.0.1), mit Origin-Pruefung und `DisallowUnknownFields`
(`internal/server/issueroutes.go`, Aufrufe in `internal/ghrest/schreiben.go`):

| Route | Rumpf | GitHub |
|---|---|---|
| `POST /action/issue/anlegen` | `repo, titel, text, labels, zustaendig` | `POST /repos/…/issues` |
| `POST /action/issue/aendern` | `repo, nummer` + beliebig `titel, text, zustand (open/closed), grund (completed/not_planned), labels, zustaendig` | `PATCH /repos/…/issues/N`, nur die gesendeten Felder; Labels und Zustaendige ersetzen |
| `POST /action/issue/kommentieren` | `repo, nummer, text` | `POST /repos/…/issues/N/comments` |
| `POST /action/issue/blocker` | `repo, nummer, blocker, entfernen` | `POST`/`DELETE …/issues/N/dependencies/blocked_by` mit der Datenbank-Id des Blockers |
| `GET /api/repo-meta?repo=` | — | Labels und moegliche Zustaendige fuer die Auswahl |

`GET /api/detail?art=issue` liefert dazu `kommentare`, `blockiert_durch` und `zustand`. Nach
jedem erfolgreichen Schreiben verwirft der Dienst die Arbeitsliste des Repos aus dem
Zwischenspeicher; im Journal steht `issue-schreiben: repo=… pat=… aktion=… nummer=…
ergebnis=ok` (oder `fehler=…`), nie der Tokenwert. In der Oberflaeche schliesst die Checkbox
ein Issue (bzw. oeffnet es wieder), „+ Neues Issue“ steht unter *Als Naechstes*, und der
Reiter *Issue* der Detailspalte (`IssueBearbeiten.svelte`) bearbeitet Titel, Text, Labels,
Zustaendige, Blocker und Kommentare.

**Pull Requests** (pr-aktionen, #32). `prs` in `/api/arbeit` kommt aus `GET /repos/…/pulls`
plus je PR `GET /repos/…/pulls/N` (`mergebar`, `merge_status`, `draft`, `head`, `head_repo`,
`head_sha`, `base`); scheitert die Liste, bleiben die PR-Eintraege aus den Issues. Aktionen
unter `/action/pr/` (Netz-Gate, Origin, unbekannte Felder abgelehnt; `prroutes.go`):

| Route | Rumpf | Was passiert |
|---|---|---|
| `POST /action/pr/freigeben` | `repo, nummer` | nur fuer einen Entwurf: GraphQL `markPullRequestReadyForReview` (REST kennt keinen Endpunkt dafuer) |
| `POST /action/pr/mergen` | `repo, nummer, branch_loeschen` | kein Entwurf, offen: `PUT …/pulls/N/merge` mit `merge_method: merge` und dem gerade gelesenen Head-sha; danach optional den Branch loeschen |
| `POST /action/pr/branch-loeschen` | `repo, nummer` | nur fuer einen gemergten/geschlossenen PR, nur im selben Repo, nie der Default-Branch: `DELETE …/git/refs/heads/BRANCH` |

Den Stand des PRs liest jede Route selbst bei GitHub nach, statt dem Browser zu glauben. Im
Journal steht `pr-schreiben: repo=… pat=… aktion=… nummer=… ergebnis=ok`. In der Oberflaeche
zeigt die PR-Zeile `Entwurf`, `mergebar`/`Konflikt`/`wird geprueft` und den Branch; die
aufgeklappte Karte (`PRAktionen.svelte`) bietet Freigeben bzw. Mergen mit Rueckfrage und
vorausgewaehltem „Branch danach loeschen“. Jede Zeile mit Change traegt ein Bild-Symbol,
gruen bei Bildbeleg (`data-beleg="ja"`), durchgestrichen ohne (`data-beleg="nein"`).

**Karte** (repo-karte, #36; als Folge seit repo-karte-folge, #99, Entwurf G).
`GET /api/karte?repo=` liefert einen Folge-Linienplan statt einer Zeitleiste. Eingabe sind die
offenen Issues, die zuletzt geschlossenen (`state=closed`, nach `closed_at`), je Map (Issue mit
`sub_issues_summary.total > 0`) die Sub-Issues (`issues/{n}/sub_issues`) und die `reihe` aus
dem Stand der Arbeitsliste; offene Blocker, Status, Branches und Change kommen aus der Reihe,
die Karte fragt `blocked_by` nicht selbst ab. `internal/appctx/karte.go` baut daraus:
- `zonen` `erledigt` (die letzten 10 geschlossenen Issues ohne PRs und Maps, aeltestes links,
  `marke` = Tag.Monat), `jetzt` (Reihe mit Status `in_arbeit`/`fertig`/`im_pr` oder aktivem
  Branch; `branch`, `stufe` gate1/gate2/im_pr, `aufgaben_erledigt`/`aufgaben_gesamt`, `pr`,
  `checks`) und `naechste` (die naechsten 10 der Reihe ohne jetzt, Fragen und Maps, `rang`
  1..10). Jede Station hat `zone`, `slot`, `linie`, `wartet_auf`, `gesperrt`, `bereit`
  (nur, wenn ihr Eintrag in der Reihe `bereit` heisst; karte-rollen, #102), `kritisch`.
- `linien`: je Map mit Station `titel`, `erledigt`/`gesamt` (aus den Sub-Issues bzw.
  `sub_issues_summary`), `aktiv`, `gesperrt`; Linie 0 heisst `ohne Map` und zaehlt ihre
  Stationen. Nach Nummer, 0 zuletzt.
- `fragen`: offene Issues mit Label `question`/`frage` (egal wie geschrieben) mit `wortlaut`
  (erste nicht leere Zeile), `wer`, `seit_tagen`, `sperrt` (direkt), `hebel` (Stationen, die
  transitiv ueber die Blocker der Reihe an ihr haengen) und `prio` (1 bei Hebel > 0); sortiert
  nach Prio, Hebel absteigend, Nummer.
- `kanten` `{von, nach, art, kritisch}` mit `art` `frage`, `aktiv` (von einer Station in jetzt)
  oder `folge`; `kritisch` am Kopf ist die Prio-1-Frage mit dem groessten Hebel, an ihr haengt
  der kritische Pfad.
Die Route hat einen eigenen Zwischenspeicher (90 s, `?frisch=1`), die Arbeitsliste fragt nie
nach Sub-Issues. `Karte.svelte` ist eingeklappt, laedt beim Aufklappen genau einmal, merkt den
Klappzustand je Repo im localStorage (`kn-karte-offen`), nutzt die ganze mittlere Spalte
(`container-type` an `.kn-mitte`) und scrollt waagerecht, wenn sie nicht reicht. Gezeichnet
werden Zonenbaender, Linien, Stationen, Frage-Karten, rot gestrichelte Frage-Kanten mit
Schranke, Schraffur (gesperrt), Leuchtrand (kritisch) und Gruen (bereit); ein Klick auf eine
Station, eine Frage oder den Titel einer Map-Linie oeffnet die Detailspalte. Log:
`repo-karte: repo=… pat=… maps=… erledigt=… jetzt=… naechste=… fragen=… kanten=…`.

**Rollen-Labels** (karte-rollen, #102). Hat weder ein offener Blocker noch der Stand des
Changes (`im_pr`, `fertig`, `in_arbeit`) entschieden, teilt `internal/appctx/rollen.go` ein
offenes Issue wie `lage` nach seinen Rollen ein: `ready-for-agent` und die Menschen-Labels
`ready-for-human`, `needs-info`, `needs-triage`, `s15-approval`, `human-verify`,
`question`/`frage` (eine Rolle). Keine, mehrere Rollen oder ein Parent-Issue, das nicht offen
ist, ergeben `hygiene` mit `grund` (`Rolle fehlt`, `mehrere Rollen: …`, `Parent #N nicht
offen`); ein Menschen-Label ergibt `wartet_auf_mensch` mit dem Label als `grund`; bei
`ready-for-agent` heisst es mit `in-arbeit` `in_arbeit`, sonst `bereit` mit und `ohne_change`
ohne Change. Die Zeile zeigt `Wartet auf Mensch` bzw. `Aufräumen` samt `grund`. Fragen bleiben
auf der Karte die Fragen-Liste.

**Sync und Zwischenspeicher** (sync-status, #38).
- `internal/ghrest/etag.go` merkt sich je Token-Fingerabdruck und URL den ETag und die
  Antwort. Jeder GET schickt `If-None-Match`, ein 304 liefert die gemerkte Antwort und kostet
  kein Rate-Limit. Schreibaufrufe gehen nie aus dem Speicher.
- Global gezaehlt werden Aufrufe, davon 304, Fehler und das Rate-Limit aus den Koepfen.
- `/api/repos`, `/api/arbeit` und `/api/karte` liefern nach Ablauf der TTL (Konten 5 min,
  Arbeit und Karte 90 s; `GKP_SYNC_TTL` in Sekunden fuer alle) sofort den letzten Stand mit
  `sync.laeuft: true` und erneuern genau einmal im Hintergrund. `?frisch=1` erneuert synchron.
- Jede Antwort traegt `sync`: `zeit` (letzter abgeschlossener Abruf), `dauer_ms`, `aufrufe`,
  `unveraendert`, `geaendert` (Inhalt anders als zuvor), `laeuft`, `fehler`.
- `GET /api/sync` fasst alle Quellen, die GitHub-Zaehler, die Konten (kurzer `grund`, voller
  Text in `detail`) und die TTL zusammen.
- Gemessen gegen dieses Repo: Arbeitsliste kalt 37 Aufrufe in 3,0 s, frisch danach 6 Aufrufe,
  alle 304, in 1,4 s, aus dem Speicher 1 ms.
- Oberflaeche: `SyncAnzeige.svelte` steht in der Kopfzeile der Leiste (Alter des letzten
  Syncs, Punkt `data-sync` ok/laeuft/fehler, Klick holt frisch). Die Settings zeigen den
  Abschnitt *Sync*.
- Bei sichtbarem Tab aktualisiert die Seite jede Minute bedingt und beim Zurueckkehren in den
  Tab. Neu gezeichnet wird nur, wenn sich der Inhalt geaendert hat. Eine Antwort mit `laeuft`
  loest nach 2,5 s genau eine Nachfrage aus.

**Faelligkeit: Heute, Demnaechst, Irgendwann** (faelligkeit, #39).
- GitHub hat kein Feld fuer ein Faelligkeitsdatum. Es steht deshalb als letzte Zeile
  `Fällig: JJJJ-MM-TT` im Issue-Text und bleibt so auch auf github.com lesbar. Gelesen werden
  auch `Faellig:` und `Due:`.
- `internal/appctx/faellig.go` enthaelt drei Funktionen:
  - `FaelligAus` liest das Datum;
  - `TextMitFaellig` ersetzt alle Faellig-Zeilen am Textende durch eine neue, der Rest des
    Textes bleibt;
  - `UebersichtBauen` teilt die offenen Issues ein, gerechnet in Ortszeit.
- Die Einteilung:
  - *Heute*: ueberfaellig und heute, aeltestes Datum zuerst;
  - *Demnaechst*: nach Datum;
  - *Irgendwann*: ohne Datum, nach Repo und Nummer.
- Jede Zeile der Arbeitsliste traegt `faellig`, `termin` und `tage`.
- `GET /api/uebersicht` fragt die offenen Issues aller nicht archivierten Repos der Leiste
  bedingt ab (ETag), laesst PRs weg und liefert `heute`, `demnaechst`, `irgendwann`,
  `zaehler`, `quellen` (je Repo ein Grund, wenn der Abruf scheiterte) und `sync`.
  Zwischenspeicher, TTL und `?frisch=1` sind dieselben wie bei der Arbeitsliste. Jedes
  Schreiben verwirft den Speicher.
- `POST /action/issue/anlegen` und `/aendern` nehmen `faellig` an (`JJJJ-MM-TT`, leer
  entfernt das Datum), jedes andere Format gibt 400. Beim Aendern ohne `text` liest der Dienst
  den aktuellen Text und ersetzt nur die Zeile. `/api/detail` liefert `faellig` mit.
- In der Oberflaeche:
  - Oben in der Leiste stehen *Heute* (gelber Stern, Zaehler ueberfaellig + heute, rot, wenn
    etwas ueberfaellig ist) und *Irgendwann* (`data-ansicht`); *Demnaechst* ist seit
    heute-logbuch (#42) Teil von *Heute*.
  - `Uebersicht.svelte` zeigt Heute in den Abschnitten *Ueberfaellig*, *Heute* und je Tag
    danach, Irgendwann je Repo (`data-abschnitt`). Jede Zeile hat Checkbox,
    Titel, Repo, Labels, Datum und den Details-Knopf; die Detailspalte arbeitet mit dem Repo
    der Zeile.
  - `IssueBearbeiten.svelte` setzt das Datum ueber ein Datumsfeld oder die Knoepfe *Heute*
    und *Morgen*; × entfernt es. *Neues Issue* hat ein Datumsfeld.
  - `Faellig.svelte` zeigt das Datum an den Zeilen: rot mit „seit N Tagen“, gelb mit
    „Heute“, sonst „Morgen“, den Wochentag oder das Datum.
- Log: `uebersicht: repos=… ueberfaellig=… heute=… demnaechst=… irgendwann=…`.

**Heute mit den naechsten Tagen, Logbuch** (heute-logbuch, #42).
- Die Ansicht *Demnaechst* entfaellt. *Heute* zeigt *Ueberfaellig*, *Heute*, *Morgen* und
  danach je Tag das Datum als Ueberschrift, hoechstens `fenster` Tage (Vorgabe 30).
  `GET /api/uebersicht?tage=N` (1 bis 365) schneidet `demnaechst` mit
  `appctx.FensterAnwenden` zu und meldet `fenster` und `weitere`. Gespeichert wird die volle
  Einteilung, das Fenster gilt je Anfrage. *Mehr laden* fragt mit 30 Tagen mehr.
- *Logbuch* (unter *Irgendwann*) zeigt die geschlossenen Issues aller nicht archivierten Repos
  nach Tag des Schliessens (Ortszeit), neuester zuerst: `GET /api/logbuch?tage=N` (Vorgabe 14,
  bis 365) liefert `tage` (je `datum` und `eintraege`), `fenster`, `anzahl`, `quellen`,
  `sync`. Je Repo ein Abruf `issues?state=closed&since=…` mit `since` = Mitternacht des ersten
  Tages, damit die URL einen Tag lang gleich bleibt und der ETag greift; `closed_at` wird
  nachgeprueft (`since` filtert bei GitHub nach `updated_at`). Gebaut in
  `internal/appctx/issuelogbuch.go` (`IssueLogbuch…`, getrennt von der Auswahl `LogbuchID`
  der alten Ansicht in `logbuch.go`). Speicher je Fenster; *Mehr laden* holt 14 Tage mehr.
- In *Heute*, *Irgendwann* und im *Logbuch* steht der Repo-Name klein unter dem Titel
  (`.kn-u-projekt`), wie Things den Projektnamen zeigt.
- Log: `logbuch: repos=… tage=… erledigt=… fehler=…`.

**Farbthemen und Stand je Repo** (things-farben-fortschritt, #41).
- Themen *Hell*, *Dunkel*, *Automatisch* mit den Farben von Things 3, als CSS-Variablen an
  einer Stelle in `App.svelte` (`--t-bg`, `--t-leiste`, `--t-blau`, `--t-gelb`, `--t-rot`,
  `--t-beige`, …). Der Knopf unten in der Leiste (`.kn-thema`, `data-thema`) schaltet reihum
  Hell → Dunkel → Automatisch; die Wahl steht in `localStorage` als `kn-theme`
  (`light`, `dark`, `auto`). `web/src/thema.js` setzt das wirksame Thema als `data-theme` an
  `<html>` und folgt bei *Automatisch* `prefers-color-scheme`, auch waehrend die Seite offen
  ist. Die Huelle (`internal/render/things.go`) setzt es schon vor dem Bundle, ohne Wahl nach
  dem System.
- `GET /api/repo-stand` liefert je nicht archiviertem Repo der Leiste `offene_issues`,
  `offene_prs`, `erledigte_issues` und `anteil` (erledigt / (erledigt + offen), vier Stellen)
  samt `sync`. Die offenen Zahlen kommen aus der offenen Liste (dieselbe URL wie
  `/api/uebersicht`, meist 304). Die geschlossenen zaehlt `ghrest.GeschlosseneAnzahl` ohne
  Liste: `issues?state=closed&per_page=1` minus `pulls?state=closed&per_page=1`, die Zahl
  steht im `Link`-Kopf unter `rel="last"` (mit dem ETag gemerkt). Speicher, TTL und
  `?frisch=1` wie bei der Uebersicht; jedes Schreiben verwirft ihn.
- In der Leiste ist der Kreis vor jedem Repo ein Tortendiagramm des Anteils (`data-anteil`,
  Tooltip mit Prozent und Zahlen). Rechts stehen das PR-Symbol mit der Zahl offener PRs
  (`.kn-r-prs`, nur wenn es welche gibt) und die Zahl offener Issues ohne PRs.
- Log: `repo-stand: repos=… fehler=…`.

**Funktionstest.** `internal/e2e` startet den echten Handler gegen eine Fake-GitHub-API mit
Zustand (`fakegithub_test.go`, mit ETag/304) und bedient in headless Chromium: Leiste mit
zwei Konten, Kategorie, Einklappen, Aufklappen mit Beleg, Detail von Change und Issue,
Kommentar, Label, Blocker, Schliessen, neues Issue, Karte, PR freigeben und mergen,
Sync-Klick, neues Issue mit Datum, Heute, Datum auf morgen, Neuladen. Danach prueft er den Zustand der Fake-API und `areas.json`.

**Komponenten** (`web/src/`):

| Komponente | Zeigt |
|---|---|
| `App.svelte` | drei Spalten (Leiste, Arbeitsliste, Detail); Auswahl in `localStorage` (`kn-things-repo`); Farben der Themen; traegt den Bundle-Marker |
| `RepoLeiste.svelte` | oben Heute und Irgendwann mit Zaehlern, Logbuch; Kategorien wie Areas, einklappbar; je Repo eine Zeile mit Fortschrittskreis, PR- und Issue-Zahl; Hinweise je PAT gekuerzt, voller Text im Tooltip |
| `Uebersicht.svelte` | Heute (Ueberfaellig, Heute, Morgen, je Tag bis zum Fenster, Mehr laden), Irgendwann je Repo, ueber alle Repos |
| `Logbuch.svelte` | erledigte Issues aller Repos je Tag, Mehr laden |
| `Arbeitsliste.svelte` | Kopf mit Fortschrittskreis und Settings-Knopf; Abschnitte *Zuletzt erledigt*, *Als Naechstes*, *Changes ohne Issue*, *Pull Requests* |
| `Eintrag.svelte` | Zeile mit Checkbox, Titel, Labels, Status, `#Nummer`, Details-Knopf; aufgeklappt eine Karte mit den Aufgaben als Subtasks und den Belegbildern |
| `Detailspalte.svelte` | dritte Spalte: Issue-Text oder Change-Dateien, Markdown ueber `marked`, bereinigt mit `DOMPurify` |
| `Einstellungen.svelte` | Repo-Details, Links zur alten Projektansicht; Kategorie waehlen, neu anlegen, loesen |

Die Oberflaeche rechnet nicht: Gruppierung, Reihenfolge, Status und Zahlen
stehen, wie Go sie schickt.

Getestet wird das gebaute Bundle in `internal/assets/repo_leiste_browser_test.go`:
headless Chromium gegen eine Fake-API, die jede Anfrage protokolliert. Gesucht
wird Chromium ueber `GKP_CHROMIUM`, den `PATH` und die Versionsordner unter
`PLAYWRIGHT_BROWSERS_PATH` bzw. `~/.cache/ms-playwright`. Ohne Chromium
ueberspringt sich der Test — die Oberflaeche ist dann **nicht** belegt. Die
Routen laufen in `internal/server/repo_api_test.go` gegen eine Fake-GitHub-API.

**Screenshots.** `just screenshots` startet einen eigenen Dienst aus dem
Arbeitsbaum auf einem freien Port, nimmt die Oberflaechen mit shot-scraper auf
und legt sie mit `dienst.log` unter `docs/messungen/screens/JJJJ-MM-TT_HHMMSS/`
ab. `SCREENSHOT_REPO=owner/repo` waehlt das Repo.

## Die Historie: vier Spuren, eine Zeitleiste

Die Sektion **Historie** ist chronologisch quer über alle Karten und je Eintrag
nach Herkunft beschriftet — sie beantwortet „was ist passiert, und von wem",
nicht nur „was wurde berichtet". Filterbar über die Chips im Kopf der Sektion,
die Zahl daneben ist der Bestand der Spur.

| Spur | Quelle | „von wem" | Wann geholt |
|---|---|---|---|
| `report` | der Report-Store (`~/.claude/reports/<repo>/*.jsonl`) | — der Store schreibt keinen Autor | im Seitenaufbau |
| `commit` | `git log` im Repo-Root, letzte 400 Commits | Commit-Autor (`%an`) | im Seitenaufbau |
| `tracker` | `<repo>/.beads/interactions.jsonl` | bds `actor`, plus `--reason` | im Seitenaufbau |
| `pr` | `GET /github` (2 min TTL) | PR-Autor (`author.login`) | erst beim Ansehen |

Die `tracker`-Spur ist die einzige Stelle, an der steht, **wer** einen Bead
geschlossen hat: `bd show` zeigt den Endzustand, nicht den Weg dahin. Ein per
Hand geschlossener Bead — der Fall, den `gkp-7ng` verfolgt — ist ausschliesslich
hier zu sehen. Gefiltert wird auf `field == "status"`; jede Feldänderung
mitzunehmen würde die Zeitleiste mit Titel- und Notiz-Edits fluten.

Die `pr`-Spur wird bewusst **nicht** serverseitig gemischt: sie kostet einen
Netzaufruf, und der gehört nicht in den Pfad, der ein Projekt öffnet (derselbe
Grund wie bei der GitHub-Karte). `browser.js` holt sie beim Ansehen und ordnet
sie zu; dafür — und nur dafür — schickt `DATA.reports.bead_ids` die Liste der
bekannten Bead-Ids mit.

### Zuordnung Commit/PR → Bead

Ein Commit gehört zu einem Bead, wenn er dessen Id nennt: als Präfix im Subject
(`gkp-uc7.8: …`, mehrere komma-getrennt) oder irgendwo im Body (`Refs: gkp-80s`).
Ein PR über Titel oder Branch-Namen.

Zugeordnet wird per **exaktem Abgleich gegen die bekannten Ids** mit
Grenzprüfung links und rechts (`collectors.BeadRefsIn`), nicht per Regex auf das
Id-Muster. Ein Muster wie `[a-z]+-[a-z0-9]{3,}` liest sich harmlos, trifft aber
`orbit-eigene` in einem echten Commit-Subject und hängt den Commit an einen Bead,
den es nicht gibt — und ein erfundener Bead sieht in der Ansicht genauso aus wie
ein echter. Die Grenzprüfung rechnet `.` zur Id, damit `gkp-ar7` in `gkp-ar7.14`
nicht als Treffer des Elternbeads gilt.

Die Menge der bekannten Ids ist die Vereinigung aus bds Epics samt Kindern **und**
den Ids im Audit-Log. Der zweite Teil ist nötig, weil `bd list -t epic`
geschlossene Epics ausblendet — genau die stehen in den älteren Commits. Die
Grenze davon ist gemessen: `orbit-7ys` ist bd nicht mehr bekannt und hat keinen
Audit-Eintrag, deshalb bleibt der Commit `04eb4b9` („orbit-7ys, orbit-j9k: …") nur
unter `orbit-j9k` sichtbar. Das ist der Preis dafür, keine Phantom-Beads zu
erfinden.

Nennt keiner der durchsuchten Commits eine Id, sagt die Spur das mitsamt der
Konvention — sonst sieht ein Repo ohne die Konvention wie ein Defekt der
Zuordnung aus. Dieses Repo war am 2026-09-05 genau so: sieben Commits, ein
einziger Treffer, und der über den Body.

Jede Spur wird **einzeln** auf 100 Einträge gekappt. Bei einem gemeinsamen
Deckel würde die dichteste Spur die anderen verdrängen; in einem Repo mit vielen
Commits bliebe von den Reports nichts übrig, obwohl die Gegenüberstellung der
Zweck der Unterteilung ist.

Ein Commit ist **ein** Eintrag, auch wenn er mehrere Ids nennt; die Liste hängt
als Eigenschaft daran und steht in der Herkunftszeile (ab der fünften nur noch
gezählt). Zuerst stand hier das Gegenteil — ein Eintrag je genanntem Bead —, und
das ist am ersten echten Fall zerbrochen: der Sammel-Commit `f1ec67b` nennt 18
Ids und erzeugte 18 identische Zeilen hintereinander. Der Nutzen wäre ein Filter
je Bead gewesen, den diese Sektion nicht hat: gefiltert wird nach Herkunft,
gelesen wird chronologisch. In dieser Lesart ist ein Commit ein Ereignis, nicht
ein Ereignis je Bead.

## Was die Ansicht von sich aus auffällig macht

Vier Dinge, die keine Fehler des Ingests sind, sondern bewusst erst in der
Ansicht entstehen:

- **Lücke (`gap`)** — ein *geschlossener* Bead ohne `done`-Beleg. Gezählt wird
  nur ein `done` ab dem neuesten `plan`, damit ein Beleg aus einer früheren
  Schliessung eine erneute nicht entschuldigt.
- **Unbekannte Karte (`unknown`)** — eine `map`, zu der kein beads-Epic
  existiert (Tippfehler, umbenanntes Epic). Die Datei wird trotzdem gezeigt,
  alle Einträge landen unter „Other". Verifiziert: eine erfundene `map` taucht
  mit `unknown: true` auf, statt unsichtbar zu bleiben.
- **`other`** — Einträge, deren `bead` kein aktuelles Kind der Karte ist.
- **`skipped`** — Zeilen, die beim Lesen unbrauchbar waren (kaputtes JSON oder
  ≥ 8 MiB). Ein lückenhafter Lesevorgang wird angezeigt, nicht verschluckt.

Als Karten gelten **alle** Epics, beschriftet oder nicht: `bdListEpics` holt
`bd list -t epic --json --limit 0` in einem Aufruf, ohne Label-Filter
(`collectors/beads.go:251`). `wayfinder:map` ist dabei nur noch ein Abzeichen
an der Karte — es reist als `IsMap` mit, statt die Auswahl zu treffen
(`collectors/beads.go:223-225`). Ein Epic ohne das Label verschwindet deshalb
nicht, es bekommt den Hinweis „Epic ohne Karte" (`browser.js:2201`).

Vorher war der Label ein **Filter mit Fallback**: erst `-l wayfinder:map`, und
nur wenn das leer zurückkam, alle Epics. Ein einziges beschriftetes Epic
reichte damit, um jedes unbeschriftete dauerhaft zu verdecken — gemessen in
`orbit` am 2026-09-04: zwei Epics, eines beschriftet, und `orbit-61z`, das
in_progress-Modul mit 12 Beads, stand schlicht nicht auf der Seite. Von den
drei Fällen (alle beschriftet / keines / gemischt) waren nur die beiden
langweiligen je geprüft, dabei ist *gemischt* der Normalzustand, sobald ein
Repo eine Karte zeichnet und später ein Epic ohne `/wayfinder` anlegt. Behoben
in `gkp-uc7.1`.

Als Filter taugt das Label ohnehin nicht, denn `bd create --parent` **vererbt
die Labels des Elternteils an die Kinder**. In diesem Repo am 2026-09-13: 46
von 99 Beads tragen `wayfinder:map`, aber nur 3 davon sind Epics — die übrigen
43 sind Kinder, die es geerbt haben. Das Label sagt also nicht „ich bin eine
Karte", sondern „irgendwo über mir wurde eine gezeichnet"; eine Auswahl
darüber hätte nie verlässlich Karten von Tickets getrennt.

## Grenzen

- **`POST /mcp` geht nie ins Netz.** Bindet `--host` eine LAN-Adresse, bleibt
  der MCP-Endpunkt auf `127.0.0.1`; über die Netz-Adresse kommen nur GETs
  durch. Der Agent läuft lokal und erreicht Loopback ohnehin.
- **Ein `report` ist kein beads-Statuswechsel.** Der Bead wird davon nicht
  `in_progress` oder `closed`. `kind` beschreibt, was der Agent *tut*, es
  steuert nichts. Für den Statuswechsel gibt es `beads_update` und
  `beads_close` — getrennte Werkzeuge, absichtlich: `kind: "done"` zu schicken
  darf nicht heimlich einen Bead schliessen.
- Fachliche Fehler (Pflichtfeld fehlt, `kind` unbekannt, Beleg fehlt, bd
  verweigert) kommen als normales Ergebnis mit `isError: true` zurück, damit der
  Agent den Text sieht — bei den `beads_*` ist das bds **eigene** Meldung, weil
  bds Diagnostik besser ist als alles, was diese Schicht erfinden könnte. Ein
  echter JSON-RPC-Fehler ist strukturellen Problemen vorbehalten (unbekanntes
  Werkzeug, unparsbare Parameter).
- **`beads_update` ist kein Umweg um die Beleg-Sperre**, aber auch keine Mauer:
  `status: "closed"` kommt durch. Die Schemabeschreibung sagt das und verweist
  auf `beads_close`. Eine harte Sperre wäre am Bead statt am Werkzeug — `gkp-7ng`.
- Jedes `beads_*` verlangt **`cwd`**. Es entscheidet, welche beads-Datenbank
  angefasst wird; ein Default auf das Arbeitsverzeichnis des Dienstes würde im
  Zweifel den Tracker des falschen Repos ändern.

## Selbst nachprüfen

```bash
curl -sS -X POST http://127.0.0.1:8765/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | python3 -m json.tool
```

Das `inputSchema` aus dieser Antwort **ist** die verbindliche Doku der Felder —
ein Agent füllt den Aufruf daraus allein. Weicht die Tabelle oben davon ab,
hat das Schema recht. `TestToolsListCatalog` hält deshalb nicht nur die Menge
der Werkzeuge fest, sondern verlangt für **jedes** Schemafeld eine nichtleere
Beschreibung: bei diesen Werkzeugen ist das Schema die Doku.

Die Sperre selbst prüft man mit einem `beads_close` auf einen Bead ohne Beleg
— `arguments: {cwd, map, closes: [{bead: <id>}]}`, kein `report`, kein `force`.
So sah das am `gkp-n77` aus, bevor er seinen Beleg hatte:

```
isError: true
Fehler: kein aktueller done-Beleg für gkp-n77 — entweder 'report' im selben
Aufruf mitgeben oder force:true setzen und den Grund in reason schreiben.
```

`bd show gkp-n77` sagte danach weiter `in_progress`. **Eine Ablehnung fasst
nichts an** — das ist der Teil, der zählt: die Prüfung läuft, bevor `bd close`
überhaupt gebaut wird. Wer das selbst nachstellt, nimmt dafür sinnvollerweise
einen Bead, den er ohnehin schliessen will, nicht einen fremden offenen.

Das **Kartentor** stellt man mit derselben Anfrage nach, nur mit einer `map`,
die es nicht gibt. Es antwortet, bevor irgendetwas geschrieben ist — prüfbar
daran, dass unter `~/.claude/reports/<repo>/` **keine** neue `.jsonl` entsteht.
Genau das ist die Zeile, die vor `gkp-few` entstand.

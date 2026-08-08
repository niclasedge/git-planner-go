# Planner und Beads zu einem Interface — Design

Datum: 2026-08-08
Repo: git-planner-go
Tracker: IaC-Stack beads, Epic siehe unten

## Problem

Das Web-Frontend hat zwei Oberflächen für dieselbe Frage „was ist zu tun":
die Planner-Seite (GitHub-Issues, editierbar, Repo-zentriert) und die
Beads-Seite (read-only, eigener State, eigenes Layout). Die iOS-App hat dieses
Problem nicht — sie führt beide Quellen in **einem** `Issue`-Modell mit einem
`source`-Flag zusammen, und jede Oberfläche (Agenda, Suche, Widgets) arbeitet
seitdem auf einer Liste.

Zusätzlich unterscheidet sich der Zuschnitt: das Web ist repo-zentriert (erst
ein Repo wählen, dann dessen Issues), die iPad-App ist listen-zentriert
(Inbox/Heute/Logbuch quer über alle Repos, Repos nur als Filter darunter).

## Entscheidungen

| Frage | Entscheidung | Grund |
|---|---|---|
| Merge-Tiefe | Ein gemeinsames Modell, eine Liste, Beads-Seite raus | wie iOS; nur eine Oberfläche zu pflegen |
| Beads-Seite / Widget | Seite raus aus `config.yaml`, Widget-Typ bleibt | derselbe Datenlayer, kostet nichts |
| Priority GitHub | Label `P1`–`P4` | echter GitHub-Zustand, Schreibweg existiert (`ensureLabel`), iOS liest Labels ohnehin |
| Priority Beads | read-only aus dem Export | die Dolt-DB ist über die API nicht schreibbar |
| Target Date auf Beads | nur anzeigen, nicht setzbar | kein zweiter Wahrheitsort; gesetzt wird mit `bd update --due` |
| Repo-Gruppen | `repo-groups.yml`, iOS-Format, im Web editierbar | eine Datei für beide Apps |
| Smart Lists | Inbox, Heute, Logbuch | aus vorhandenen Daten ableitbar |
| Tasks (GTD) | **nicht** portiert | reine Gerätedaten, bräuchte eigenen Speicher ohne Gegenwert |

### Drei Zwänge, die das Design formen

**Editierbare Datei vs. Deploy.** `rsync-build` löscht auf dem Ziel, was im
Repo nicht steht. Eine im Repo-Wurzelverzeichnis liegende `repo-groups.yml`
würde bei jedem Deploy die Änderungen aus der Web-App überschreiben. Die
schreibbare Kopie liegt deshalb in `data/` — dort steht schon die SQLite-DB,
`data/` ist gitignored und damit vom rsync ausgeschlossen. Die Datei im
Repo-Wurzelverzeichnis ist der **Seed** für den ersten Start.

**Logbuch kostet Nutzlast.** Der Planner holt heute `state: open`. Für ein
Logbuch braucht es `state: all` und `per-repo: 100`: gleiche Anzahl Requests
(einer pro Repo), doppelte Nutzlast. Beads bringen ihre geschlossenen Einträge
im Export ohnehin mit, dort ist das Logbuch vollständig; bei GitHub ist es die
letzten 100 nach `updated`.

**„Heute" ist eine Empfehlung, keine Liste.** Ein Datum allein füllt die Seite
nicht — die meisten Beads tragen keins. Deshalb zwei Abschnitte, siehe unten.

## Architektur

### `internal/plan` — das gemeinsame Modell

```go
type Source string // "github" | "beads"

type Item struct {
    Source    Source
    Repo      string // owner/repo
    Key       string // "gh:o/r#123" | "bd:o/r:gv-1ui.2" — stabil, quellenübergreifend eindeutig
    Ref       string // "#123" | "gv-1ui.2" — was die Zeile zeigt
    Number    int    // GitHub; 0 bei Beads
    BeadID    string // Beads; "" bei GitHub
    Title     string
    Body      string
    State     string // open | in_progress | closed
    Labels    []gh.Label
    Priority  int    // 1..4, 0 = keine
    Type      string // beads issue_type; "" bei GitHub
    Planned   *time.Time
    Due       *time.Time
    CreatedAt time.Time
    UpdatedAt time.Time
    ClosedAt  *time.Time
    ParentKey string
    BlockedBy []string // Keys
    Unblocks  []string // Keys — die Gegenrichtung, für "entblockt N"
    Editable  bool     // false bei Beads
    URL       string
}
```

Zwei Mapper:

- `FromGitHub(gh.Issue) Item` — Priority aus dem Label `P1`–`P4`,
  `Planned`/`Due` über das vorhandene `gh.ParsePlanned`/`gh.ParseDue` aus den
  Body-Markern, `BlockedBy` aus Sub-Issues und der `blocked`-Label-Konvention.
- `FromBead(beads.Bead) Item` — Priority und `Due` aus dem Export,
  `ParentKey`/`BlockedBy` aus den `parent-child`- und `blocks`-Dependencies,
  `Editable: false`, `URL` aus `external_ref` der Form `gh-<n>`.

`Unblocks` wird nach dem Mappen über die gesammelte Menge berechnet: die
Umkehrung von `BlockedBy`. Ein Item kennt seine Blocker aus der Quelle, aber
nicht seine Wartenden.

### `internal/beads` — Extraktion

`Bead`, `Parse`, Fetch, Verzeichnis-Probe und Discovery ziehen aus
`internal/panel/beads.go` in ein eigenes Paket. `panel.Beads` wird eine dünne
Hülle darüber, damit `type: beads` in `config.yaml` unverändert weiterläuft.
Der Planner nutzt denselben Layer statt einer zweiten Kopie.

### `repo-groups.yml` — Gruppen

Format identisch zum iOS-Export (`RepoGroupConfig`):

```yaml
version: 1
groups:
  - name: AKTIV
    icon: folder
    color: "#FF9500"
    repos: [niclasedge/IaC-Stack, niclasedge/glance]
collapsed: [INAKTIV]
```

Gelesen wird `data/repo-groups.yml`; existiert sie nicht, wird die Datei im
Repo-Wurzelverzeichnis einmalig dorthin kopiert. Geschrieben wird nur nach
`data/`. Ein Repo, das keine Gruppe nennt, landet in „Ohne Gruppe" — die Datei
muss also nur enthalten, was sortiert werden soll.

Die Sidebar gruppiert **zweistufig**: erst nach Source (dem Token, mit dem das
Repo entdeckt wurde — „Work"/„Privat" sind die `label:`-Werte aus `config.yaml`),
dann nach diesen Gruppen.

## Oberfläche

### Sidebar

- Kopf: Titel, Einstellungen, Refresh, Sidebar-Toggle
- Schnellsuche (filtert Repos und Titel)
- Vier KPI-Kacheln: Issues · Beads · PRs · Overdue
- Smart Lists: Inbox · Heute · Logbuch, je mit Zähler
- Agenda (aufklappbar): terminierte offene Einträge, Datums-Chip rechts,
  überfällig rot
- Repo-Baum: Source › Gruppe › Repo, je Repo ein Fortschrittsring und die Zahl
  der offenen Einträge

### Listen-Pane

Vier Modi: **Inbox** (alles Offene über beide Quellen), **Heute**,
**Logbuch** (Geschlossenes, nach Schließdatum), **Repo** (ein Repo, wie heute).
Darüber ein Segment **Work | Privat** (die zwei Tokens).

Zeilenaufbau wie iOS: Status-Checkbox · `BEAD`-Badge bei Beads · Repo-Kürzel
(4 Zeichen, Großbuchstaben) · Ref monospace · Titel · rechts relative Zeit,
optionaler Datums-Chip, P-Badge (P1 rot, P2 orange, P3 blau, P4 grau).
Einrückung mit `⊢` für Kind und `↳` für „wartet auf"; die vorhandenen
Gutter-Linien des Webs bleiben, sie zeichnen den Baum genauer als iOS' Indent.

Ein Repo mit beiden Quellen bekommt **zwei Abschnitte**: „Beads"
(Claude-Agenten-Arbeit) zuerst, darunter „Wayfinder" und „Issues" wie heute.
Ein Alarm-Issue mischt sich damit nicht in einen Task-Baum.

### „Heute"

1. **Terminiert** — offene Einträge mit `Due` oder `Planned` in den nächsten
   14 Tagen, Überfälliges zuerst.
2. **Empfohlen** — die nächsten 20 **unblockierten** offenen Einträge, sortiert
   nach Priorität aufsteigend, bei Gleichstand das älteste (`CreatedAt`) zuerst.
   Jede Zeile trägt ein Badge **„entblockt N"** mit den betroffenen Refs im
   Tooltip, sofern N > 0.

Blockiert heißt: mindestens ein Eintrag in `BlockedBy` ist noch offen. Ein
geschlossener Blocker blockiert nicht, ein aus dem Export wegkompaktierter
ebenfalls nicht.

### Detail-Pane

Kopf wie iOS: Ref zentriert, „Edit" rechts. Darunter der Titel, eine Meta-Zeile
(Assignee · Start · **Planen** · **Fällig setzen**) und zwei Aktionen
(**Complete** grün, **Cancel** grau — `state_reason` `completed` bzw.
`not_planned`). Dann `DESCRIPTION` als gerendertes Markdown und `LABELS`.

Ein Bead zeigt denselben Kopf **ohne** die Aktionsleiste und ohne Edit: keine
Kommentare (beads hält sie in der Dolt-DB), kein Schreibweg. Stattdessen Typ,
Priorität, Blocker, Kinder und der `gh-<n>`-Link, falls migriert.

### Priority schreiben

Nur GitHub. Select im Edit-Formular (keine · P1–P4). Beim Speichern legt
`ensureLabel` das Label bei Bedarf mit fester Farbe an, entfernt die übrigen
P-Labels und setzt das gewählte. Sortierung überall: Priorität aufsteigend vor
der bisherigen Ordnung, 0 (keine) ganz hinten.

## Tests

- `plan`: Mapper-Tabellentests — Priority aus Label, Daten aus Body-Markern,
  Bead-Dependencies zu `ParentKey`/`BlockedBy`, `Unblocks` als Umkehrung,
  `Editable` je Quelle.
- `plan`: Blockiert-Regel gegen geschlossene und fehlende Blocker.
- Empfehlung: 20er-Kappung, Sortierung Priorität → Alter, blockierte Einträge
  fehlen, „entblockt N" zählt nur offene Wartende.
- Repo-Gruppen: YAML-Roundtrip gegen eine echte iOS-Exportdatei; Seed-Kopie
  passiert genau einmal; Schreiben landet in `data/`, nie im Repo.
- Templates: Bead-Zeile trägt Source- und P-Badge und **keine** Edit-Affordanz;
  ein Repo mit beiden Quellen rendert zwei Abschnitte in der Reihenfolge
  Beads → Wayfinder → Issues.
- Priority-Schreiben: P2 → P1 entfernt P2 und setzt P1; „keine" entfernt alle.

## Nicht im Umfang

- Tasks (GTD-Scratchpad) — reine Gerätedaten.
- Drag & Drop im Gruppen-Editor — Stufe 2 bekommt eine Zuordnungsliste.
- Schreiben in Beads (weder Datum noch Priorität noch Status).
- Kommentare an Beads.

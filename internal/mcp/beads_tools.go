// Beads write API for the MCP endpoint. The report tool in mcp.go stays what
// it was — an append-only journal entry — while the five tools here can change
// the issue tracker. That difference in character drives two rules that hold
// throughout this file:
//
//   - A failed mutation is reported with bd's own message, never swallowed.
//     internal/bdops guarantees the mechanics; this layer must not undo it by
//     collapsing errors into a friendly summary.
//   - beads_close will not close a bead that has no current "done" evidence in
//     the report store, unless the caller says force. That check is the one
//     thing bd genuinely cannot do — it has no idea the store exists — and it
//     is the failure this whole API was asked for after "closed ohne Beleg"
//     showed up twice in one session.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/bdops"
	"github.com/niclasedge/git-planner-go/internal/closeops"
	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/reports"
	"github.com/niclasedge/git-planner-go/internal/shared"
)

// bdWeg is this package's single way to bd, shared by BOTH doors onto the
// report store: beads_close (through internal/closeops) and the report tool in
// mcp.go, which appends without closing anything. One variable rather than two
// because the two doors have to answer the same question the same way — "does
// this map exist, and does this bead hang under it" — and a second seam is a
// second thing that can be stubbed out on its own.
//
// It is a var for the same reason notifyBeadFunc is: a test has to be able to
// walk the gate without a beads database. Everything that is not a test uses
// the real bd.
var bdWeg closeops.Schliesser = closeops.EchtesBd()

// ---------------------------------------------------------------- schemas

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// prioProp declares the priority field as string-or-number. The description
// says "0-4", so a caller writes a number; declaring only "string" made the
// obvious payload invalid. bdops.Priority accepts both.
func prioProp(desc string) map[string]any {
	return map[string]any{"type": []string{"string", "integer"}, "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func strArrayProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

const cwdDesc = "Absoluter Pfad im Repo; bestimmt, welche beads-Datenbank angesprochen wird."

// dateFormats is spelled out in both date descriptions because tools/list is
// the only documentation these fields will ever have, and a format written
// down nowhere gets guessed. The relative half is passed through to bd
// unchecked — its refusal is the better message. The absolute half is checked
// by bdops.checkDateSpec, and it is ZONE-LESS: bd throws a named zone away and
// reads a layout it does not know as a clock time today, so all three accepted
// forms belong here rather than just one.
const dateFormats = "Formate relativ: +6h, +1d, +2w, tomorrow, next monday. " +
	"Absolut nur ohne Zonenangabe: 2025-01-15, 2025-01-15T23:00:00, \"2025-01-15 23:00:00\"."

const dueDesc = "Deadline (due_at). " + dateFormats

// deferDesc names the effect, not just the field: defer_until is the one date
// with a consequence — bd hides the bead from `bd ready` until then and puts it
// on status=deferred, which is what makes it Things' "When" rather than a note.
const deferDesc = "Verstecken bis (defer_until): der Bead ist bis dahin aus " +
	"`bd ready` ausgeblendet und steht auf status=deferred. " + dateFormats

// edgeSchema is shared by beads_create and beads_dep. The field names are the
// API's answer to bd's ambiguity: bd spells the same edge two opposite ways
// (`dep add <blocked> <blocker>` vs `dep --blocks <blocked>`), and its
// graph-plan form inverts it a third time. blocked/blocker cannot be read
// backwards.
var edgeSchema = map[string]any{
	"type": "array",
	"description": "Blockier-Kanten. Bewusst blocked/blocker statt from/to benannt — " +
		"die Richtung ist die häufigste Verwechslung.",
	"items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"blocked": strProp("Der Bead, der WARTET (hängt ab)."),
			"blocker": strProp("Der Bead, der ZUERST fertig sein muss."),
		},
		"required": []string{"blocked", "blocker"},
	},
}

var beadsQuerySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cwd": strProp(cwdDesc),
		"op": map[string]any{
			"type": "string",
			"enum": []string{"list", "show", "ready", "blocked", "stats", "dep_tree", "gaps"},
			"description": "list/show/ready/blocked/stats/dep_tree geben bds JSON unverändert zurück. " +
				"gaps kann bd NICHT: geschlossene Sub-beads einer Map ohne aktuellen done-Eintrag im Report-Store.",
		},
		"id":       strProp("Bead-Id. Pflicht für show und dep_tree."),
		"map":      strProp("Epic-Id der Map. Pflicht für gaps."),
		"status":   strArrayProp("Nur list: Status-Filter, z.B. ['open','in_progress']."),
		"type":     strProp("Nur list: bug|feature|task|epic|chore|decision."),
		"labels":   strArrayProp("Nur list/ready: Label-Filter (UND-verknüpft)."),
		"parent":   strProp("list/ready/blocked: auf Kinder bzw. Nachfahren dieses Beads einschränken."),
		"limit":    map[string]any{"type": "integer", "description": "list/ready: Höchstzahl (0 = unbegrenzt)."},
		"children": boolProp("Nur show: statt des Beads seine Kinder zeigen."),
		"all":      boolProp("Nur list: geschlossene mit einbeziehen."),
	},
	"required": []string{"cwd", "op"},
}

var beadsCreateSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cwd": strProp(cwdDesc),
		"issues": map[string]any{
			"type":        "array",
			"description": "Ein oder mehrere Issues. Werden zusammen mit den Kanten in EINEM bd-Aufruf angelegt.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": strProp("Nur innerhalb dieses Aufrufs: Name, unter dem edges und parent " +
						"dieses Issue ansprechen, bevor bd eine Id vergeben hat. Optional."),
					"title":       strProp("Pflicht."),
					"type":        strProp("bug|feature|task|epic|chore|decision. Default task."),
					"priority":    prioProp("0-4 bzw. P0-P4, 0 = höchste. Default 2."),
					"description": strProp("Warum das Issue existiert und was zu tun ist."),
					"parent":      strProp("Eltern-Bead: entweder ein 'key' aus diesem Aufruf oder eine bestehende Id."),
					"labels":      strArrayProp("Labels."),
					"assignee":    strProp("Zuständig."),
					"acceptance":  strProp("Abnahmekriterien."),
					"design":      strProp("Design-Entscheidungen."),
					"notes":       strProp("Ergänzende Notizen."),
					"due":         strProp(dueDesc),
					"defer":       strProp(deferDesc),
				},
				"required": []string{"title"},
			},
		},
		"edges":   edgeSchema,
		"dry_run": boolProp("Nur prüfen und berichten, nichts anlegen."),
	},
	"required": []string{"cwd", "issues"},
}

var beadsUpdateSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cwd": strProp(cwdDesc),
		"changes": map[string]any{
			"type": "array",
			"description": "Je Eintrag ein Feldsatz, der auf ALLE genannten ids angewandt wird. " +
				"Unterschiedliche Werte brauchen mehrere Einträge — bd kann pro Aufruf nur einen Feldsatz.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ids":           strArrayProp("Pflicht, mindestens eine. Ohne id würde bd das zuletzt angefasste Issue treffen."),
					"status":        strProp("open|in_progress|blocked|deferred|closed. Zum Schliessen besser beads_close (prüft den Beleg)."),
					"claim":         boolProp("Atomar übernehmen: assignee=ich, status=in_progress."),
					"priority":      prioProp("0-4 bzw. P0-P4."),
					"title":         strProp("Neuer Titel."),
					"description":   strProp("Neue Beschreibung (ersetzt)."),
					"notes":         strProp("Notizen (ersetzt)."),
					"append_notes":  strProp("An bestehende Notizen anhängen."),
					"design":        strProp("Design-Notizen."),
					"acceptance":    strProp("Abnahmekriterien."),
					"assignee":      strProp("Zuständig."),
					"type":          strProp("Neuer Typ."),
					"parent":        strProp("Neuer Eltern-Bead; leerer String löst die Elternschaft."),
					"due":           strProp(dueDesc + " Leerer String löscht die Deadline."),
					"defer":         strProp(deferDesc + " Leerer String löscht das Datum; der Bead steht danach wieder auf open."),
					"add_labels":    strArrayProp("Labels ergänzen."),
					"remove_labels": strArrayProp("Labels entfernen."),
					"set_labels":    strArrayProp("Labels ersetzen."),
				},
				"required": []string{"ids"},
			},
		},
	},
	"required": []string{"cwd", "changes"},
}

var beadsCloseSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cwd": strProp(cwdDesc),
		"map": strProp("Epic-Id der Map, unter der die Belege im Report-Store liegen. Pflicht. " +
			"Wird GEPRÜFT, bevor irgendetwas geschrieben wird: die Id muss ein existierendes Epic sein " +
			"und jeder genannte bead muss darunter hängen (beliebig tief). Passt das nicht, wird abgelehnt — " +
			"auch mit force, und auch wenn der Beleg tadellos ist. Welche Karten es gibt: " +
			"beads_query op=list type=epic limit=0."),
		"closes": map[string]any{
			"type": "array",
			"description": "Je Eintrag ein Bead. Der Beleg kann direkt mitgegeben werden — dann wird erst der " +
				"Report geschrieben und danach geschlossen. Ein Bead schliesst nur mit einem SICHTBAREN Beleg: " +
				"ein Bild (png/jpg/gif/webp) oder eine .dot/.svg-Datei. Text allein genügt nicht.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bead":   strProp("Pflicht: die zu schliessende Bead-Id."),
					"reason": strProp("Optional: Grund, wird von bd am Bead vermerkt."),
					"report": map[string]any{
						"type": "object",
						"description": "Optional: der done-Report für diesen Bead. Fehlt er UND liegt kein " +
							"aktueller done-Eintrag mit sichtbarem Beleg vor, verweigert das Werkzeug " +
							"(ausser force).",
						"properties": map[string]any{
							"title":  strProp("Pflicht, wenn report gesetzt ist."),
							"impact": strProp("Ein Satz Wirkung/Warum."),
							"notes":  strProp("Freitext (Markdown)."),
							"proof": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "object"},
								"description": "Wie bei 'report': {cmd,out} | {file} | {image}. Zum Schliessen " +
									"muss mindestens ein Element sichtbar sein: {\"image\":\"/abs/pfad.png\"} " +
									"(Screenshot des laufenden Stands oder gerendertes Diagramm) oder " +
									"{\"file\":\"...dot\"} bzw. \"...svg\". Ein {cmd,out}-Beleg zählt dafür " +
									"nicht, auch ein ausführlicher nicht. Die Datei MUSS existieren und darf " +
									"nicht leer sein: ein relativer Pfad wird gegen cwd und gegen die Wurzel " +
									"des Worktrees aufgelöst, in dem cwd liegt; was sich nicht auflösen lässt, " +
									"wird als missing markiert und schliesst keinen Bead.",
							},
							"supersedes": strProp("Id eines früheren Eintrags, den dieser korrigiert."),
						},
						"required": []string{"title"},
					},
				},
				"required": []string{"bead"},
			},
		},
		"force": boolProp("Ohne Beleg bzw. ohne sichtbaren Beleg schliessen — für 'wird nicht gemacht'-Fälle. " +
			"reason ist dann PFLICHT und muss den Grund nennen (entfallen, ersetzt durch …, Annahme " +
			"widerlegt). 'kein Bild zur Hand' ist kein Grund, sondern der Fehlbetrag — fehlt nur das " +
			"Bild, ist der Weg ein Bild, kein force."),
	},
	"required": []string{"cwd", "map", "closes"},
}

var beadsDepSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cwd":   strProp(cwdDesc),
		"op":    map[string]any{"type": "string", "enum": []string{"add", "remove"}, "description": "Kante setzen oder entfernen."},
		"edges": edgeSchema,
	},
	"required": []string{"cwd", "op", "edges"},
}

// beadsToolDefs is the catalog entry list, kept next to the schemas so a new
// tool cannot be added without a description.
func beadsToolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "beads_query",
			"description": "Liest beads: list/show/ready/blocked/stats/dep_tree — plus 'gaps', geschlossene Beads ohne Beleg im Report-Store.",
			"inputSchema": beadsQuerySchema,
		},
		{
			"name":        "beads_create",
			"description": "Legt Issues an, samt Eltern-Kind-Zuordnung, Blockier-Kanten und Terminen (due/defer), in einem Aufruf. Gibt die vergebenen Ids je key zurück.",
			"inputSchema": beadsCreateSchema,
		},
		{
			"name":        "beads_update",
			"description": "Ändert Felder an einem oder mehreren Beads (Status, claim, Priorität, Text, Labels, Eltern, due/defer).",
			"inputSchema": beadsUpdateSchema,
		},
		{
			"name":        "beads_close",
			"description": "Schliesst Beads — nur mit aktuellem done-Beleg im Report-Store. Der Beleg kann im selben Aufruf mitgegeben werden.",
			"inputSchema": beadsCloseSchema,
		},
		{
			"name":        "beads_dep",
			"description": "Setzt oder entfernt Blockier-Kanten (blocked hängt an blocker).",
			"inputSchema": beadsDepSchema,
		},
	}
}

// ---------------------------------------------------------------- dispatch

// handleBeadsTool routes the five beads tools. Returns handled=false for any
// other name so mcp.go's own dispatch keeps ownership of "unknown tool".
func handleBeadsTool(r *http.Request, name string, args json.RawMessage) (result any, handled bool) {
	switch name {
	case "beads_query":
		return runOrError(args, beadsQuery), true
	case "beads_create":
		return runOrError(args, beadsCreate), true
	case "beads_update":
		return runOrError(args, beadsUpdate), true
	case "beads_close":
		return runOrError(args, func(p beadsCloseParams) (string, error) { return beadsClose(r, p) }), true
	case "beads_dep":
		return runOrError(args, beadsDep), true
	}
	return nil, false
}

// runOrError decodes arguments into P and turns the (text, error) pair into a
// tools/call result. Domain failures become isError:true with the message
// intact, matching handleToolsCall's existing contract — the calling agent
// needs to read bd's refusal, not a generic code.
func runOrError[P any](args json.RawMessage, fn func(P) (string, error)) any {
	var p P
	if len(args) == 0 {
		return toolError("arguments fehlt")
	}
	// Unknown fields are rejected, not ignored. encoding/json drops them by
	// default, and for a read tool that is harmless — but here a mistyped or
	// invented field name means the value never reaches bd and nothing says so.
	// Measured on the first live beads_create call: it passed "parent_key" and
	// "parent_id" (names from bd's internal graph schema, not from this one,
	// which calls the field "parent"), and the silent drop would have created
	// two parentless top-level beads under no map at all, reported as success.
	// A rejection naming the field is recoverable; a lost field is not.
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return toolError("arguments: " + err.Error() + " — die erlaubten Felder stehen im inputSchema dieses Werkzeugs")
	}
	text, err := fn(p)
	if err != nil {
		return toolError(err.Error())
	}
	return toolText(text)
}

// ---------------------------------------------------------------- query

type beadsQueryParams struct {
	Cwd      string   `json:"cwd"`
	Op       string   `json:"op"`
	ID       string   `json:"id"`
	Map      string   `json:"map"`
	Status   []string `json:"status"`
	Type     string   `json:"type"`
	Labels   []string `json:"labels"`
	Parent   string   `json:"parent"`
	Limit    *int     `json:"limit"`
	Children bool     `json:"children"`
	All      bool     `json:"all"`
}

func beadsQuery(p beadsQueryParams) (string, error) {
	if p.Cwd == "" {
		return "", fmt.Errorf("cwd fehlt")
	}
	if p.Op == "gaps" {
		return queryGaps(p.Cwd, p.Map)
	}
	args, err := queryArgs(p)
	if err != nil {
		return "", err
	}
	res, err := bdops.Query(p.Cwd, args)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// queryArgs maps the tool's parameters onto bd's flags. Pure, so the mapping
// is testable without a database.
func queryArgs(p beadsQueryParams) ([]string, error) {
	switch p.Op {
	case "list":
		args := []string{"list"}
		if len(p.Status) > 0 {
			// bd: "repeating -s/--status silently overwrites the previous
			// value — always use the comma-separated form".
			args = append(args, "--status", strings.Join(p.Status, ","))
		}
		if p.Type != "" {
			args = append(args, "--type", p.Type)
		}
		if len(p.Labels) > 0 {
			args = append(args, "--label", strings.Join(p.Labels, ","))
		}
		if p.Parent != "" {
			if !bdops.ValidID(p.Parent) {
				return nil, fmt.Errorf("ungültige parent-id %q", p.Parent)
			}
			args = append(args, "--parent", p.Parent)
		}
		if p.All {
			args = append(args, "--all")
		}
		if p.Limit != nil {
			args = append(args, "--limit", fmt.Sprint(*p.Limit))
		}
		return append(args, "--json"), nil

	case "show":
		if !bdops.ValidID(p.ID) {
			return nil, fmt.Errorf("show braucht eine gültige id, war %q", p.ID)
		}
		// --id= rather than a positional: bd documents this form for ids that
		// look like flags, and it removes the question entirely.
		args := []string{"show", "--id=" + p.ID}
		if p.Children {
			args = append(args, "--children")
		}
		return append(args, "--json"), nil

	case "ready":
		args := []string{"ready"}
		if p.Parent != "" {
			if !bdops.ValidID(p.Parent) {
				return nil, fmt.Errorf("ungültige parent-id %q", p.Parent)
			}
			args = append(args, "--parent", p.Parent)
		}
		if len(p.Labels) > 0 {
			args = append(args, "--label", strings.Join(p.Labels, ","))
		}
		if p.Limit != nil {
			args = append(args, "--limit", fmt.Sprint(*p.Limit))
		}
		return append(args, "--json"), nil

	case "blocked":
		args := []string{"blocked"}
		if p.Parent != "" {
			if !bdops.ValidID(p.Parent) {
				return nil, fmt.Errorf("ungültige parent-id %q", p.Parent)
			}
			args = append(args, "--parent", p.Parent)
		}
		return append(args, "--json"), nil

	case "stats":
		return []string{"stats", "--json"}, nil

	case "dep_tree":
		if !bdops.ValidID(p.ID) {
			return nil, fmt.Errorf("dep_tree braucht eine gültige id, war %q", p.ID)
		}
		return []string{"dep", "tree", "--", p.ID}, nil
	}
	return nil, fmt.Errorf("unbekannte op %q", p.Op)
}

// gapReport is what queryGaps returns per bead. Deliberately its own shape
// rather than bd's: the whole point is the half bd does not know about.
type gapReport struct {
	Map     string   `json:"map"`
	Repo    string   `json:"repo"`
	Gaps    []string `json:"gaps"`
	Checked int      `json:"checked_closed_beads"`
	Note    string   `json:"note,omitempty"`
}

func queryGaps(cwd, mapID string) (string, error) {
	if !bdops.ValidID(mapID) {
		return "", fmt.Errorf("gaps braucht eine gültige map-id, war %q", mapID)
	}
	children, err := mapChildren(cwd, mapID)
	if err != nil {
		return "", err
	}
	repo := collectors.ReportsRepoName(cwd)
	// LoadMap(repo, mapID) und NICHT reports.LoadBead (gkp-6l2, Abgrenzung zu
	// gkp-e9h): dieses Werkzeug beantwortet "hat DIESE Karte ihre eigene
	// Buchfuehrung vollstaendig", nicht "hat der Bead irgendwo im Repo einen
	// Beleg". Der Unterschied ist keine Feinheit — er ist der ganze Grund, warum
	// es dieses Werkzeug gibt: gkp-few (docs/spec-aufgaben-dashboard.md §7.1)
	// beschreibt woertlich, dass "beads_query op=gaps map=gkp-4e1" den Vorfall
	// GEMELDET HAETTE, weil der Beleg unter der falschen Karte (gkp-yy5) lag und
	// unter der richtigen (gkp-4e1) fehlte. Eine ueber reports.LoadBead
	// kartenuebergreifend gesuchte Antwort haette genau das verdeckt: sie haette
	// den Beleg irgendwo gefunden und "kein Gap" gemeldet, obwohl die gepruefte
	// Karte selbst leer war. Gegenprobe: TestQueryGapsSiehtNurEintraegeUnterDerEigenenKarte.
	entries, _, err := reports.LoadMap(repo, mapID)
	if err != nil {
		return "", fmt.Errorf("report-store für %s/%s nicht lesbar: %w", repo, mapID, err)
	}
	byBead := map[string][]reports.Entry{}
	for _, e := range entries {
		byBead[e.Bead] = append(byBead[e.Bead], e)
	}

	out := gapReport{Map: mapID, Repo: repo, Gaps: []string{}}
	for _, c := range children {
		if c.Status != "closed" {
			continue
		}
		out.Checked++
		if !reports.HasCurrentDoneEntry(byBead[c.ID]) {
			out.Gaps = append(out.Gaps, c.ID)
		}
	}
	sort.Strings(out.Gaps)
	if len(out.Gaps) > 0 {
		out.Note = "Diese Beads sind geschlossen, ohne aktuellen done-Eintrag im Report-Store."
	}
	blob, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}

type childInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// mapChildren reads a map's sub-beads. It shells out to bd directly rather
// than going through collectors.CollectBeads, which would also render the plan
// SVG through graphviz — a needless subprocess for a status lookup.
func mapChildren(cwd, mapID string) ([]childInfo, error) {
	res, err := bdops.Query(cwd, []string{"show", "--id=" + mapID, "--children", "--json"})
	if err != nil {
		return nil, err
	}
	// Same envelope collectors.parseBdChildren handles: the children live
	// under a key named after the map, alongside "schema_version".
	raw, ok := shared.ParseJSON[map[string]json.RawMessage](res.Stdout)
	if !ok {
		return nil, fmt.Errorf("bd show --children lieferte kein JSON-Objekt")
	}
	blob, ok := raw[mapID]
	if !ok {
		return nil, fmt.Errorf("map %q hat keine Kinder im bd-Ergebnis", mapID)
	}
	children, ok := shared.ParseJSON[[]childInfo](string(blob))
	if !ok {
		return nil, fmt.Errorf("Kinder von %q nicht lesbar", mapID)
	}
	return children, nil
}

// ---------------------------------------------------------------- create

type beadsCreateParams struct {
	Cwd    string           `json:"cwd"`
	Issues []bdops.NewIssue `json:"issues"`
	Edges  []bdops.Edge     `json:"edges"`
	DryRun bool             `json:"dry_run"`
}

func beadsCreate(p beadsCreateParams) (string, error) {
	if p.Cwd == "" {
		return "", fmt.Errorf("cwd fehlt")
	}
	res, err := bdops.Create(p.Cwd, p.Issues, p.Edges, p.DryRun)
	if err != nil {
		return "", err
	}
	if p.DryRun {
		return "dry_run: " + res.Calls[0].Stdout, nil
	}
	blob, _ := json.Marshal(res.Keys)
	return fmt.Sprintf("%d Issue(s) angelegt, %d Kante(n): %s", len(res.IDs), len(p.Edges), blob), nil
}

// ---------------------------------------------------------------- update

type beadsUpdateParams struct {
	Cwd     string         `json:"cwd"`
	Changes []bdops.Change `json:"changes"`
}

func beadsUpdate(p beadsUpdateParams) (string, error) {
	if p.Cwd == "" {
		return "", fmt.Errorf("cwd fehlt")
	}
	calls, err := bdops.Update(p.Cwd, p.Changes)
	if err != nil {
		// Name how far it got: a batch that failed halfway must not read as a
		// batch that did nothing.
		return "", fmt.Errorf("%w (%d von %d Änderungen ausgeführt)", err, len(calls)-1, len(p.Changes))
	}
	n := 0
	for _, c := range p.Changes {
		n += len(c.IDs)
	}
	return fmt.Sprintf("%d Bead(s) in %d Änderung(en) aktualisiert", n, len(p.Changes)), nil
}

// ---------------------------------------------------------------- close

type closeReport struct {
	Title      string              `json:"title"`
	Impact     string              `json:"impact"`
	Notes      string              `json:"notes"`
	Proof      []reports.ProofItem `json:"proof"`
	Supersedes string              `json:"supersedes"`
}

type closeItem struct {
	Bead   string       `json:"bead"`
	Reason string       `json:"reason"`
	Report *closeReport `json:"report"`
}

type beadsCloseParams struct {
	Cwd    string      `json:"cwd"`
	Map    string      `json:"map"`
	Closes []closeItem `json:"closes"`
	Force  bool        `json:"force"`
}

// beadsClose is the one tool that does something bd cannot: it insists on
// evidence. The insisting itself no longer lives here — it moved to
// internal/closeops (gkp-bq9), because the view got a second door to the same
// decision and a re-implemented gate is a gate that can drift. What stays here
// is the translation: MCP's vocabulary in, closeops' vocabulary out.
//
// The pre-flight checks below are deliberately kept at this boundary even
// though closeops repeats them. They are the ones whose MESSAGE names a field
// of THIS schema — cwd, map, closes[i], report.title. An agent that sent "cwd"
// must not be told that "pfad" is missing; that is a message it cannot act on.
func beadsClose(r *http.Request, p beadsCloseParams) (string, error) {
	if p.Cwd == "" || p.Map == "" {
		return "", fmt.Errorf("cwd und map sind Pflicht")
	}
	if len(p.Closes) == 0 {
		return "", fmt.Errorf("keine beads angegeben")
	}
	for i, c := range p.Closes {
		if !bdops.ValidID(c.Bead) {
			return "", fmt.Errorf("closes[%d]: ungültige bead-id %q", i, c.Bead)
		}
		if c.Report != nil && strings.TrimSpace(c.Report.Title) == "" {
			return "", fmt.Errorf("closes[%d]: report braucht einen title", i)
		}
	}

	auftrag := closeops.Auftrag{Pfad: p.Cwd, Karte: p.Map, Erzwingen: p.Force}
	for _, c := range p.Closes {
		w := closeops.Wunsch{Bead: c.Bead, Grund: c.Reason}
		if c.Report != nil {
			w.Bericht = &closeops.Bericht{
				Titel: c.Report.Title, Wirkung: c.Report.Impact, Notizen: c.Report.Notes,
				Belege: c.Report.Proof, Ersetzt: c.Report.Supersedes,
			}
		}
		auftrag.Wuensche = append(auftrag.Wuensche, w)
	}

	// The bead comment carrying the report URL is this door's business, not the
	// gate's: closeops has no *http.Request and no opinion about bd comments.
	// It fires per written entry, which is why it is a callback and not
	// something derived from the result afterwards — a refused close still
	// wrote its report, and that report still deserves its link.
	erg, err := closeops.NeuMit(bdWeg).Schliessen(auftrag, func(bead string, e reports.Entry) {
		notifyBeadAsync(p.Cwd, bead, reportURL(r, e))
	})
	if err != nil {
		return "", err
	}

	msg := fmt.Sprintf("%d Bead(s) geschlossen: %s", len(erg.Geschlossen), strings.Join(erg.Geschlossen, ", "))
	if len(erg.Geschrieben) > 0 {
		msg += fmt.Sprintf(" — Belege geschrieben für: %s", strings.Join(erg.Geschrieben, ", "))
	}
	if len(erg.OhneBild) > 0 {
		msg += fmt.Sprintf(" — ohne visuellen Nachweis (force): %s", strings.Join(erg.OhneBild, ", "))
	}
	if len(erg.OhneBeleg) > 0 {
		msg += fmt.Sprintf(" — OHNE Beleg (force): %s", strings.Join(erg.OhneBeleg, ", "))
	}
	return msg, nil
}

// ---------------------------------------------------------------- deps

type beadsDepParams struct {
	Cwd   string       `json:"cwd"`
	Op    string       `json:"op"`
	Edges []bdops.Edge `json:"edges"`
}

func beadsDep(p beadsDepParams) (string, error) {
	if p.Cwd == "" {
		return "", fmt.Errorf("cwd fehlt")
	}
	calls, err := bdops.Dep(p.Cwd, p.Op, p.Edges)
	if err != nil {
		return "", fmt.Errorf("%w (%d von %d Kanten verarbeitet)", err, len(calls)-1, len(p.Edges))
	}
	verb := "gesetzt"
	if p.Op == "remove" {
		verb = "entfernt"
	}
	return fmt.Sprintf("%d Kante(n) %s", len(p.Edges), verb), nil
}

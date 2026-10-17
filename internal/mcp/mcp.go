// Package mcp implements the Streamable-HTTP MCP endpoint (POST /mcp,
// demo-axk.3): JSON-RPC 2.0 by hand, stdlib encoding/json only — no
// MCP SDK, no framework, matching github.com/niclasedge/git-planner-go's stdlib-only go.mod.
//
// Exactly three methods are implemented: initialize, tools/list, tools/call.
// Exactly one tool exists: report, backed by internal/reports.Append. Any
// other method gets a clean JSON-RPC error, never a 500.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"

	"github.com/niclasedge/git-planner-go/internal/reports"
	"github.com/niclasedge/git-planner-go/internal/shared"
)

// maxRequestBodyBytes caps the POST /mcp request body, mirroring the
// gitDiffMax constant pattern in collectors/git.go. Unlike the rest of this
// app (which only ever serves this host's own browser tabs), this payload
// arrives from the network (an MCP client) — an unbounded body is a
// resource-exhaustion vector before it ever reaches reports.Append's own
// limits (maxImageBytes, maxProofTextBytes).
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// protocolVersion is the MCP protocol version this minimal server declares
// in initialize. Fixed rather than negotiated — the scope here is exactly
// three methods, not a full handshake.
const protocolVersion = "2025-06-18"

// JSON-RPC 2.0 error codes (spec-fixed values).
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Handler returns the http.Handler for POST /mcp. The caller (server.go)
// owns the route registration and the CSRF/Origin guard; this handler only
// speaks JSON-RPC.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, rpcResponse{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &rpcError{Code: codeParseError, Message: "ungültiges JSON: " + err.Error()},
			})
			return
		}

		if req.ID == nil {
			// Notification (no "id" member) — JSON-RPC 2.0 forbids a
			// response body. We don't act on any notification method
			// (e.g. notifications/initialized): nothing to do beyond ack.
			w.WriteHeader(http.StatusAccepted)
			return
		}

		result, rpcErr := dispatch(r, req)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		writeJSON(w, resp)
	})
}

func dispatch(r *http.Request, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return handleInitialize(req.Params), nil
	case "tools/list":
		return handleToolsList(), nil
	case "tools/call":
		return handleToolsCall(r, req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("unbekannte Methode %q", req.Method)}
	}
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func handleInitialize(params json.RawMessage) any {
	// Client's requested protocolVersion is accepted but not otherwise
	// used — we always declare our own fixed version below.
	_ = params
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "github.com/niclasedge/git-planner-go", "version": "1"},
	}
}

// reportInputSchema IS the documentation for the report tool — an agent
// fills it from this schema alone, so every field carries a description and
// nothing here is explained a second time anywhere else.
var reportInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cwd": map[string]any{
			"type":        "string",
			"description": "Absoluter Pfad des Arbeitsverzeichnisses; daraus werden repo/worktree/branch abgeleitet.",
		},
		"map": map[string]any{
			"type": "string",
			"description": "beads-Epic-Id der Map, z.B. 'demo-axk'. Wird GEPRÜFT, bevor der Eintrag " +
				"geschrieben wird: die Id muss ein existierendes Epic sein und der 'bead' muss darunter " +
				"hängen (beliebig tief). Passt das nicht, wird abgelehnt und NICHTS geschrieben — der Store " +
				"ist append-only, ein Eintrag unter der falschen Karte liesse sich nicht zurücknehmen. " +
				"Welche Karten es gibt: beads_query op=list type=epic limit=0.",
		},
		"bead": map[string]any{
			"type":        "string",
			"description": "Der bewegte Sub-Bead, z.B. 'demo-axk.3'.",
		},
		"kind": map[string]any{
			"type":        "string",
			"enum":        []string{"plan", "done"},
			"description": "'plan' wenn der Bead auf in_progress geht, 'done' wenn er geschlossen wird.",
		},
		"title": map[string]any{
			"type":        "string",
			"description": "Kurztitel des Reports, eine Zeile.",
		},
		"impact": map[string]any{
			"type":        "string",
			"description": "Optional: ein Satz Wirkung/Warum.",
		},
		"proof": map[string]any{
			"type": "array",
			"description": "Optional, aber empfohlen. Ein Objekt je Beleg — genau eine Variante pro Eintrag: " +
				"{cmd,out} für Befehl+Ausgabe, {file:'pfad:zeile'} für eine Codestelle oder ein Diagramm, " +
				"{image:'/abs/pfad.png'} für einen Screenshot (wird kopiert; nur PNG/JPEG/GIF/WebP, max. 10 MiB, " +
				"sonst als missing markiert). Auch {file} wird beim Schreiben aufgelöst: relativ gegen cwd und " +
				"gegen die Wurzel des Worktrees, in dem cwd liegt — was dort nicht liegt, wird als missing " +
				"markiert und zählt nicht mehr als Beleg. Ein .dot/.svg wird zusätzlich archiviert.",
			"items": map[string]any{"type": "object"},
		},
		"notes": map[string]any{
			"type":        "string",
			"description": "Optional: Freitext-Anmerkung.",
		},
		"supersedes": map[string]any{
			"type":        "string",
			"description": "Optional: Id eines früheren Eintrags, den dieser korrigiert (der alte bleibt sichtbar, durchgestrichen).",
		},
	},
	"required": []string{"cwd", "map", "bead", "kind", "title"},
}

func handleToolsList() any {
	tools := []map[string]any{
		{
			"name":        "report",
			"description": "Hängt einen Fortschritts-Report (plan|done) für einen beads-Sub-Bead an den append-only Report-Store an und gibt die Report-URL zurück.",
			"inputSchema": reportInputSchema,
		},
	}
	return map[string]any{"tools": append(tools, beadsToolDefs()...)}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleToolsCall never returns a JSON-RPC error for domain validation
// failures (missing field, unknown kind) — those come back as a normal
// tools/call result with isError:true so the calling agent sees the exact
// message in its content, per reports.Append's own error text. A JSON-RPC
// error is reserved for structurally broken calls: unknown tool name,
// unparseable params/arguments.
func handleToolsCall(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var call toolCallParams
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "ungültige params: " + err.Error()}
	}
	if result, handled := handleBeadsTool(r, call.Name, call.Arguments); handled {
		return result, nil
	}
	if call.Name != "report" {
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("unbekanntes Tool %q", call.Name)}
	}

	var payload reports.Payload
	if len(call.Arguments) == 0 {
		return nil, &rpcError{Code: codeInvalidParams, Message: "arguments fehlt"}
	}
	if err := json.Unmarshal(call.Arguments, &payload); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "arguments ist kein gültiges JSON-Objekt: " + err.Error()}
	}

	// Shape first, then the card, then the write — and the last two in THAT
	// order, never the other way round (gkp-few).
	//
	// The report store is append-only: a line written under the wrong map cannot
	// be taken back, only superseded, and superseding it requires knowing which
	// file it went into — which is exactly what nobody knows when the map was a
	// typo. So the question "does this card exist, and does this bead hang under
	// it" has to be answered while there is still nothing to regret. It sits at
	// the same boundary as DisallowUnknownFields in beads_tools.go, for the same
	// reason: a mistake that is refused is recoverable, a mistake that is stored
	// is not.
	//
	// Measured 2026-09-14: a call carrying map "gkp-yy5" — an id that never
	// existed — created gkp-yy5.jsonl, and the close gate then checked the
	// evidence against the file the typo had just made.
	if err := reports.Validate(payload); err != nil {
		return toolError(err.Error()), nil
	}
	if _, err := bdWeg.PruefeKartenzugehoerigkeit(payload.Cwd, payload.Map, []string{payload.Bead}); err != nil {
		return toolError(err.Error()), nil
	}

	entry, err := reports.Append(payload)
	if err != nil {
		return toolError(err.Error()), nil
	}

	// Fire-and-forget: the comment is courtesy (demo-axk decision 4), the
	// report entry above has already landed successfully. Must never block
	// this response — see notifyBeadAsync/notifyBead for the reasoning.
	reportLink := reportURL(r, entry)
	notifyBeadAsync(payload.Cwd, payload.Bead, reportLink)
	return toolText(fmt.Sprintf("report gespeichert - %s", reportLink)), nil
}

// reportURL builds the link handed back to the calling agent — and, via
// notifyBead, written into the bead as a permanent comment. It therefore has
// to be a URL this server actually serves.
//
// gkp-ar7.12: it used to return /reports/<repo>/<map>#<id>, which could never
// resolve — every successful report call answered with a 404 link, and
// notifyBead persisted that dead link on the bead. Three independent reasons,
// all in the routing:
//
//   - "reports" is in server.go's reservedSegments, so /reports/... never
//     reaches the project routing that renders the view.
//   - GET /reports/ is dispatched exclusively to reportassets.Handler.
//   - reportassets.parsePath requires exactly four segments with "assets" at
//     index 2; /reports/<repo>/<map> has three and 404s.
//
// The route that does render the view is the project routing's /<project>/
// <view> form, i.e. /<repo>/reports — verified live (HTTP 200,
// DATA.reports.available=true) against the same entry that 404'd before.
//
// Map and entry id are deliberately dropped rather than kept as a fragment:
// browser.js has no location.hash handling at all (no hashchange handler, no
// per-entry anchors), so a "#<id>" would only look like a deep link without
// being one. Real deep-linking is a separate change, not something to imply
// here.
func reportURL(r *http.Request, e reports.Entry) string {
	host := r.Host
	if host == "" {
		host = "127.0.0.1:8765"
	}
	return fmt.Sprintf("http://%s/%s/reports", host, url.PathEscape(e.Repo))
}

// notifyBeadTimeoutMs bounds notifyBead's bd subprocess. It runs off the
// request goroutine (see notifyBeadAsync), so this only protects the daemon
// itself from an indefinitely hanging bd (Dolt lock, network) — not the MCP
// client, which already got its response by the time this fires.
const notifyBeadTimeoutMs = 5000

// beadIDPattern is the second belt, not the first: the "--" flag terminator
// in notifyBead's argv already fully neutralizes finding 2 (a leading-dash
// value can never be parsed as a flag once "--" precedes the positionals),
// so this only needs to reject a leading "-" — nothing more. An earlier,
// narrower version of this pattern (<prefix>-<suffix> / <parent>.<n> only)
// rejected legitimate bd ids: "gt--xyz" (bd's own documented escape form for
// ids that look like flags, per `bd show --help`: "--id=gt--xyz") and
// "demo-axk.1.2" (a nested sub-bead, more than one ".N" level). Staying
// permissive here — anything not starting with "-" — avoids a false
// rejection of exactly the ids the history coupling exists for.
var beadIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func isValidBeadID(id string) bool {
	return beadIDPattern.MatchString(id)
}

// notifyBead appends the report URL as a bd comment (demo-axk decision:
// the bead keeps the history, the report store keeps the proof). Fail open:
// a failing/missing bd binary, an invalid bead id, or a timeout must never
// fail the ingest that already succeeded — the comment is a courtesy, not
// part of the contract. Belt and suspenders against finding 2: bead is
// validated against beadIDPattern up front AND "--" terminates the flags
// before the positionals, so even a future bd/cobra change can't turn a
// crafted bead value into a flag.
func notifyBead(cwd, bead, url string) {
	if !isValidBeadID(bead) {
		log.Printf("mcp: bd comments add übersprungen, bead %q ist keine gültige beads-Id (Kommentar ist Beiwerk, Report-Eintrag bleibt gültig)", bead)
		return
	}
	opts := shared.RunOptions{Cwd: filepath.Clean(cwd), TimeoutMs: notifyBeadTimeoutMs}
	if _, ok := shared.Run([]string{"bd", "comments", "add", "--", bead, url}, opts); !ok {
		log.Printf("mcp: bd comments add %s fehlgeschlagen oder Timeout nach %dms (Kommentar ist Beiwerk, Report-Eintrag bleibt gültig)", bead, notifyBeadTimeoutMs)
	}
}

// notifyBeadFunc is notifyBead by default; it's a var so tests can swap in a
// stub that blocks on a channel, while still exercising the real "go
// notifyBeadFunc(...)" dispatch in notifyBeadAsync below (as opposed to
// swapping notifyBeadAsync itself, which would let a test's stub decide
// whether to be async and defeat the point of the test).
var notifyBeadFunc = notifyBead

// notifyBeadAsync runs notifyBeadFunc in its own goroutine so a hanging bd
// process can never block the tools/call response — the response is only
// waiting on reports.Append, which has already returned by the time this is
// called.
func notifyBeadAsync(cwd, bead, url string) {
	go notifyBeadFunc(cwd, bead, url)
}

func toolText(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": false,
	}
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": "Fehler: " + msg}},
		"isError": true,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "json error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

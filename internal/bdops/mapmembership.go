package bdops

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/shared"
)

// CheckMapMembership answers one question, and it has to be answered BEFORE
// anything is written to the append-only report store: does mapID exist as an
// epic in cwd's database, and does every named bead hang underneath it?
//
// WHY THIS EXISTS (gkp-few, measured 2026-09-14). beads_close was called with
// map "gkp-yy5". No such id existed — the card is "gkp-4e1". The store dutifully
// created gkp-yy5.jsonl, wrote the done proofs into it, checked the evidence
// gate AGAINST THAT VERY FILE, and closed both beads reporting success. Under
// the real card the two beads then stood as "plan without done", i.e. as a gap,
// although they were closed. A mistyped map invents its own witness.
//
// That is worse than a missing proof, and the difference is the whole argument
// for this file: a missing proof BLOCKS — the bead stays open and it is noticed
// at once. This passed and said "done".
//
// THE TWO EDGE CASES, decided here rather than left to the caller:
//
//   - An id that EXISTS but is NOT an epic is rejected. The failure mode being
//     defended against is a typo, and a typo that happens to land on some real
//     task id is no less damaging than one that lands nowhere — it is more,
//     because it looks plausible. "epic" is the strongest discriminator bd
//     offers, it arrives in the same answer that settles existence (so it costs
//     nothing extra), and it matches measured practice: all three cards this
//     repo's store actually holds — gkp-4e1, gkp-ar7, gkp-uc7 — are issue_type
//     epic. The wayfinder:map LABEL is deliberately NOT checked: `bd create
//     --parent` inherits the parent's labels, so on 2026-09-13 46 beads carried
//     it while only 3 were epics. The type is a reliable signal, the label is
//     not.
//
//   - A bead hanging SEVERAL LEVELS under the epic is accepted. Checking only
//     direct parenthood would reject a legitimate close in order to catch a
//     typo — trading a rare loud failure for a common one. The chain is walked
//     upward instead, one bd call per level, until the map is reached.
//
// The map itself counts as a member of its own card: an epic is closed like any
// other bead, and the store already holds such an entry (gkp-uc7.jsonl carries
// one whose bead is gkp-uc7).
//
// The returned Result carries bd's failure Reason when bd could not answer at
// all, and is zero when bd answered and the answer was "no". Callers need the
// difference: the first is not the caller's mistake, the second is.
func CheckMapMembership(cwd, mapID string, beads []string) (Result, error) {
	if !ValidID(mapID) {
		return Result{}, fmt.Errorf("ungültige karten-id %q", mapID)
	}
	if len(beads) == 0 {
		return Result{}, errors.New("keine beads angegeben")
	}
	for _, b := range beads {
		if !ValidID(b) {
			return Result{}, fmt.Errorf("ungültige bead-id %q", b)
		}
	}

	// One call carries the map and every bead: bd's `show` takes a repeated
	// --id, so the common case — a card plus one or two beads, all one level
	// apart — costs exactly one subprocess.
	known := map[string]issueNode{}
	res, err := loadIssues(cwd, append([]string{mapID}, beads...), known)
	if err != nil {
		return res, err
	}

	card, ok := known[mapID]
	if !ok {
		return Result{}, fmt.Errorf(MsgMapUnknown, mapID, strings.Join(beads, ", "), mapID)
	}
	if card.Type != "epic" {
		return Result{}, fmt.Errorf(MsgMapNotAnEpic, mapID, card.Type, card.Title, mapID)
	}

	for _, b := range beads {
		if err := checkChain(cwd, mapID, b, known); err != nil {
			return Result{}, err
		}
	}
	return Result{}, nil
}

// maxChainDepth bounds the upward walk. It guards against a parent cycle (bd
// does not forbid one) and against an accidental fan-out; it is not a statement
// about how deep a card may be. No chain among this repo's 117 beads is longer
// than one link, and the deepest form the codebase knows of is
// "demo-axk.1.2".
const maxChainDepth = 16

// checkChain walks from bead upward until it meets the map. Each level costs
// one bd call, and only for the beads that did not already resolve — a card
// whose beads are all direct children never gets past the first comparison.
func checkChain(cwd, mapID, bead string, known map[string]issueNode) error {
	if bead == mapID {
		return nil // an epic is closed like any other bead
	}
	current := bead
	for depth := 0; depth < maxChainDepth; depth++ {
		node, ok := known[current]
		if !ok {
			if current == bead {
				return fmt.Errorf(MsgBeadUnknown, bead, mapID)
			}
			// A parent bd does not know: the chain breaks before it reaches the
			// card, which is the same verdict as a chain leading elsewhere.
			return fmt.Errorf(MsgBeadNotUnderMap, bead, mapID, chainText(bead, known))
		}
		if node.Parent == "" {
			return fmt.Errorf(MsgBeadNotUnderMap, bead, mapID, chainText(bead, known))
		}
		if node.Parent == mapID {
			return nil
		}
		if _, ok := known[node.Parent]; !ok {
			if _, err := loadIssues(cwd, []string{node.Parent}, known); err != nil {
				return err
			}
		}
		current = node.Parent
	}
	return fmt.Errorf("elternkette von %s ist tiefer als %d Ebenen oder im Kreis geführt — "+
		"unter %s ist sie damit nicht nachweisbar", bead, maxChainDepth, mapID)
}

// chainText renders the chain actually found, so a refusal says where the bead
// DOES hang rather than only where it does not.
func chainText(bead string, known map[string]issueNode) string {
	parts := []string{bead}
	current := bead
	for i := 0; i < maxChainDepth; i++ {
		node, ok := known[current]
		if !ok || node.Parent == "" {
			break
		}
		parts = append(parts, node.Parent)
		current = node.Parent
	}
	if len(parts) == 1 {
		return parts[0] + " (ohne Eltern)"
	}
	return strings.Join(parts, " → ")
}

// MsgMapUnknown and its three siblings are the refusals as format templates.
// They are exported and not inlined for the same reason the ones in
// internal/closeops are: a refusal that does not name the fix produces a second
// failed attempt — and here the second attempt is unusually expensive, because
// the first would have left a line in an append-only store had it been allowed
// to write.
//
// Every one of them names BOTH ids. A reader who only gets one looks at the
// wrong end: the measured case had a correct bead id and a wrong card, and the
// caller was most confident about the card.
const (
	MsgMapUnknown = "karte %q gibt es in dieser beads-Datenbank nicht — " +
		"die Beads %s wurden deshalb NICHT geschlossen und es wurde nichts in den Report-Store geschrieben. " +
		"Ohne diese Prüfung entstünde eine neue Akte %s.jsonl, die Belege landeten darin, und das Beleg-Tor " +
		"prüfte gegen genau die Datei, die der Tippfehler gerade erzeugt hat. " +
		"Die richtige Karte findet `bd list -t epic --limit 0`."

	MsgMapNotAnEpic = "karte %q existiert, ist aber kein Epic, sondern %q (%q) — " +
		"eine Karte ist ein Epic, und der Report-Store legt je Karte eine Akte an. " +
		"Entweder ist die Id verschrieben (`bd list -t epic --limit 0` zeigt die Karten), " +
		"oder %s soll wirklich eine Karte sein — dann ist der Weg `bd update --type epic`, nicht diese Id."

	MsgBeadUnknown = "bead %q gibt es in dieser beads-Datenbank nicht, " +
		"er kann also auch nicht unter der Karte %q hängen. Es wurde nichts geschrieben und nichts geschlossen."

	MsgBeadNotUnderMap = "bead %q hängt nicht unter der Karte %q — seine Elternkette ist %s. " +
		"Es wurde nichts in den Report-Store geschrieben und nichts geschlossen. " +
		"Entweder ist die Karte falsch genannt (dann die aus der Kette nehmen) " +
		"oder der Bead hängt am falschen Elternteil (dann `bd update --parent`)."
)

// issueNode is the slice of bd's `show --json` this file needs. The JSON names
// are bd's own: "issue_type" rather than "type", "parent" rather than
// "parent_id" — the graph-plan schema spells the second one differently, and
// mixing the two is how a field silently reads as empty.
type issueNode struct {
	ID     string `json:"id"`
	Type   string `json:"issue_type"`
	Title  string `json:"title"`
	Parent string `json:"parent"`
}

// loadIssues fetches ids in one `bd show` and merges them into known.
//
// A missing id is NOT an error here, and that is bd's doing rather than a
// shortcut. Measured with bd 1.2.2: `show --id=<present> --id=<gone>` exits 0,
// names the missing one on stderr only, and simply leaves it out of the array;
// with ALL ids missing it exits 1 and answers an OBJECT ({"error": ...})
// instead of an array. Absence from the result is therefore the only reliable
// signal, and the exit code is not one. Both shapes are decoded, and only
// output that is neither counts as "bd could not answer".
func loadIssues(cwd string, ids []string, known map[string]issueNode) (Result, error) {
	args := []string{"--readonly", "show"}
	for _, id := range ids {
		// --id= rather than a positional, the same form the rest of this
		// package uses for values that could look like a flag.
		args = append(args, "--id="+id)
	}
	res := Run(cwd, mapCheckTimeoutMs, append(args, "--json")...)

	if nodes, ok := shared.ParseJSON[[]issueNode](res.Stdout); ok {
		for _, n := range nodes {
			if n.ID != "" {
				known[n.ID] = n
			}
		}
		return Result{}, nil
	}
	if obj, ok := shared.ParseJSON[map[string]json.RawMessage](res.Stdout); ok {
		if _, hasError := obj["error"]; hasError {
			return Result{}, nil // bd knows none of the ids — the absence IS the answer
		}
	}
	if !res.Failed {
		res.Failed, res.Reason = true, ReasonExit
	}
	return res, fmt.Errorf("kartenzugehörigkeit nicht prüfbar, es wurde nichts geschrieben "+
		"und nichts geschlossen: %s", res.Error())
}

// mapCheckTimeoutMs bounds the lookup. It is shorter than DefaultTimeoutMs
// because this is a READ on a path that has to stay cheap — it now sits in
// front of every report write, not only in front of a close — and because a
// caller whose bd is wedged is better served by a refusal in five seconds than
// by half a minute of nothing. The refusal is repeatable; the write it prevents
// would not have been.
const mapCheckTimeoutMs = 5000

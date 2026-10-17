package reports

import (
	"path/filepath"
	"strconv"
	"strings"
)

// HasCurrentDoneEntry decides whether a bead has "done" evidence belonging to
// its CURRENT lifecycle segment. It lives here, next to Entry, rather than in
// the view that first needed it: this is the rule behind the reports view's gap
// warning, and HasValidatedDoneEntry below builds the close tool's stricter
// gate on the same core, so the lifecycle question is answered in exactly one
// place.
//
// The naive version — "any entry, ever, with kind==done" — is wrong in two
// ways that both showed up in practice (demo-axk.4 review, Welle 2,
// finding 3):
//
//   - A single done entry from a bead's FIRST closure permanently silenced the
//     warning, even after a later "plan" entry superseded it. Live repro:
//     e1=done, e2=plan supersedes e1 — the gap stayed hidden with the
//     retracted e1 as the only "evidence".
//   - bd allows closing a bead more than once. A done entry from closure #1
//     must not excuse closure #2 having no evidence of its own.
//
// So a done entry only counts when it is at or after the newest "plan" entry:
// it has to belong to the current open→closed segment. That subsumes the
// explicit Supersedes case without consulting it at all — a "plan" superseding
// a "done" IS a reopen — which is right, because Supersedes means "this
// corrects that entry" generally, while plan/done ordering is what actually
// marks a lifecycle boundary.
//
// A bead with no plan entry at all (report-store ingest started partway
// through its life) falls back to "any done entry counts": there is no reopen
// boundary to compare against, and demanding one would flag history that was
// never recorded.
//
// Entry.At is RFC3339 UTC with fixed-width fields, so string comparison is
// chronological comparison.
func HasCurrentDoneEntry(entries []Entry) bool {
	return hasCurrentDone(entries, func(Entry) bool { return true })
}

// HasValidatedDoneEntry is the stricter rule the MCP close tool applies: the
// current done entry must additionally carry a VISUAL proof — a real image, or
// a .dot/.svg artefact. Text alone does not close a bead any more.
//
// Why a picture and not just more text: a {cmd,out} proof is written by the
// same agent that claims the work is done, and nothing stops it from being a
// plausible-looking paraphrase of a command that was never run. A rendered
// diagram or a screenshot of the running thing has to be produced from the
// actual state, so it is the one kind of proof that is expensive to fake and
// cheap for a human to check at a glance.
//
// Deliberately NOT used by the reports view's gap warning. The view describes
// history, and 22 beads were closed under the older rule (text-only evidence,
// which was legitimate then); applying this retroactively would paint them all
// as gaps and turn a real signal into noise. Strict on write, lenient on read —
// the two functions live next to each other so that difference stays visible
// instead of drifting apart.
func HasValidatedDoneEntry(entries []Entry) bool {
	return hasCurrentDone(entries, HasVisualProof)
}

// hasCurrentDone is the shared core: a done entry at or after the newest plan
// entry, which additionally satisfies extra.
func hasCurrentDone(entries []Entry, extra func(Entry) bool) bool {
	var lastPlanAt string
	for _, e := range entries {
		if e.Kind == "plan" && e.At > lastPlanAt {
			lastPlanAt = e.At
		}
	}
	for _, e := range entries {
		if e.Kind == "done" && e.At >= lastPlanAt && extra(e) {
			return true
		}
	}
	return false
}

// HasVisualProof reports whether an entry carries something one can look at.
//
// Two shapes count, and the exclusions matter as much as the inclusions:
//
//   - {image: "..."} — the screenshot or rendered diagram Append copied into
//     the store.
//   - {file: "x.dot"} or {file: "x.svg"} — the graph source or its rendering.
//     The optional ":line" suffix that file proofs may carry is stripped first.
//
// Neither counts once Append marked it missing:true. A path that did not exist,
// was empty, was too large, or failed the magic-byte check is a record of a
// failed proof attempt, not a proof. Accepting it would make the gate trivially
// bypassable by naming a file that isn't there.
//
// A {cmd,out} proof never counts, however detailed.
//
// THE ASYMMETRY THAT USED TO BE HERE (gkp-bq9 documented it, gkp-2ln closed
// it): an image was resolved at write time and a file proof was not, so
// {"file": "docs/nicht-da.svg"} passed on the strength of its extension alone —
// a one-line bypass of the one gate built against the closing agent's own
// say-so. Both shapes now carry the same marker, set by the same write-time
// check (resolveProofs in store.go).
//
// What did NOT change is where the question is asked. The marker is read; the
// filesystem is not. The store is append-only, and a proof is a statement about
// the moment it was written: re-checking a path on every read would let a bead
// closed with a rendered diagram lose its evidence the day someone renames a
// directory. Measured before the change: five file proofs in the live store
// already point at files that are gone (four under /tmp, one in a deleted
// worktree), across seven closed beads. Under a read-time check those seven
// would have gone red retroactively — which is why the check sits at write time
// and its verdict is stored, exactly as for images. Strict on write, lenient on
// read, one more time.
func HasVisualProof(e Entry) bool {
	for _, p := range e.Proof {
		if isVisualShape(p) && !proofMissing(p) {
			return true
		}
	}
	return false
}

// HasBrokenVisualProof reports whether the CURRENT done evidence tried to show
// something and the store could not keep it: a visual proof marked missing.
//
// It exists so the close gate can tell two refusals apart that otherwise read
// alike but have opposite fixes — "you handed in text only" (attach a diagram)
// versus "the diagram you handed in is not there" (render it, or fix the path).
// A caller told the wrong one simply tries again the same way.
func HasBrokenVisualProof(entries []Entry) bool {
	return hasCurrentDone(entries, func(e Entry) bool {
		for _, p := range e.Proof {
			if isVisualShape(p) && proofMissing(p) {
				return true
			}
		}
		return false
	})
}

// BrokenVisualProofReasons returns what the store recorded about the visual
// proofs it could not keep, for the CURRENT done evidence — the same cycle
// HasBrokenVisualProof answers about, because a refusal that quoted a proof
// from an earlier closure would send the caller to fix the wrong file.
//
// It exists for exactly one caller: the refusal message in internal/closeops.
// That message otherwise has to recite all four possible causes ("not there,
// empty, too large or not a recognised image format") and let the rejected
// agent work out which one it hit. Deliberately NOT for the view — the standing
// product decision is that a missing file proof is shown as plainly as a
// missing image, and WITHOUT a reason.
//
// AN ABSENT REASON DOES NOT MEAN "INTACT". The store is append-only and the
// marker is only ever written at Append time, so three groups of proofs carry
// missing without a reason and always will:
//
//   - everything written before this field existed,
//   - the 163 file proofs written before resolveFileProof existed at all
//     (gkp-2ln; the first entry carrying "copy" is 2026-09-13T22:44) — those
//     were never checked and will never be marked, not even as missing,
//   - anything marked by a future branch that forgets to set it.
//
// Hence: missing is the verdict, reason is a courtesy. Callers must keep
// working when the slice comes back empty.
// It runs through hasCurrentDone rather than re-deriving the cycle boundary:
// collecting is the side effect, and the predicate always answers false so that
// every current done entry is seen instead of the walk stopping at the first.
// A second copy of `e.At >= lastPlanAt` in this file would be a second truth —
// one a mutation of the first would no longer reach.
func BrokenVisualProofReasons(entries []Entry) []string {
	var gruende []string
	hasCurrentDone(entries, func(e Entry) bool {
		for _, p := range e.Proof {
			if !isVisualShape(p) || !proofMissing(p) {
				continue
			}
			if r, ok := p["reason"].(string); ok && strings.TrimSpace(r) != "" {
				gruende = append(gruende, strings.TrimSpace(r))
			}
		}
		return false
	})
	return gruende
}

// isVisualShape asks only what a proof item CLAIMS to be — an image, or a graph
// artefact — without regard to whether it survived the write. Both questions
// above need that half separately.
func isVisualShape(p ProofItem) bool {
	if img, ok := p["image"].(string); ok && strings.TrimSpace(img) != "" {
		return true
	}
	f, ok := p["file"].(string)
	return ok && isGraphArtefact(f)
}

func proofMissing(p ProofItem) bool {
	missing, _ := p["missing"].(bool)
	return missing
}

// isGraphArtefact matches a .dot or .svg file reference.
func isGraphArtefact(ref string) bool {
	ext := strings.ToLower(filepath.Ext(proofFilePath(ref)))
	return ext == ".dot" || ext == ".svg"
}

// proofFilePath strips the optional ":line" suffix a file proof may carry, so
// "docs/plan.dot:12" and "docs/plan.dot" are the same file. Shared with the
// write-side resolution in store.go: a suffix understood in one place and not
// the other would mark half the repo's own proofs as missing.
func proofFilePath(ref string) string {
	path := strings.TrimSpace(ref)
	if i := strings.LastIndex(path, ":"); i > 0 {
		if _, err := strconv.Atoi(path[i+1:]); err == nil {
			path = path[:i]
		}
	}
	return path
}

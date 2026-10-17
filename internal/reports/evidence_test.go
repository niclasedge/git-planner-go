package reports

import (
	"strings"
	"testing"
)

// entry is a shorthand for the only three fields these rules look at.
func entry(kind, at string, proof ...ProofItem) Entry {
	return Entry{Kind: kind, At: at, Proof: proof}
}

func TestHasCurrentDoneEntryLifecycle(t *testing.T) {
	img := ProofItem{"image": "/tmp/a.png"}
	cases := []struct {
		name    string
		entries []Entry
		want    bool
	}{
		{"leer", nil, false},
		{"nur plan", []Entry{entry("plan", "2026-09-01T10:00:00Z")}, false},
		{"done ohne plan zählt", []Entry{entry("done", "2026-09-01T10:00:00Z")}, true},
		{"done nach plan", []Entry{
			entry("plan", "2026-09-01T10:00:00Z"),
			entry("done", "2026-09-01T11:00:00Z", img),
		}, true},
		{"reopen: plan nach done", []Entry{
			entry("done", "2026-09-01T10:00:00Z", img),
			entry("plan", "2026-09-01T11:00:00Z"),
		}, false},
		{"zweiter Zyklus belegt", []Entry{
			entry("done", "2026-09-01T10:00:00Z", img),
			entry("plan", "2026-09-01T11:00:00Z"),
			entry("done", "2026-09-01T12:00:00Z", img),
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasCurrentDoneEntry(c.entries); got != c.want {
				t.Fatalf("HasCurrentDoneEntry = %v, want %v", got, c.want)
			}
		})
	}
}

// TestHasValidatedDoneEntryNeedsSomethingToLookAt pins the difference between
// the two rules: identical entry lists, one with a text proof and one with an
// image, must part ways exactly at the strict gate.
func TestHasValidatedDoneEntryNeedsSomethingToLookAt(t *testing.T) {
	textOnly := []Entry{entry("done", "2026-09-01T10:00:00Z",
		ProofItem{"cmd": "go test ./...", "out": "ok  \tgithub.com/niclasedge/git-planner-go/internal/reports\t0.4s"})}
	withImage := []Entry{entry("done", "2026-09-01T10:00:00Z",
		ProofItem{"cmd": "go test ./...", "out": "ok"},
		ProofItem{"image": "/tmp/shot.png"})}

	if !HasCurrentDoneEntry(textOnly) {
		t.Fatal("lenient rule should accept a text-only done entry")
	}
	if HasValidatedDoneEntry(textOnly) {
		t.Fatal("strict rule must reject a text-only done entry")
	}
	if !HasValidatedDoneEntry(withImage) {
		t.Fatal("strict rule should accept a done entry carrying an image")
	}
}

// TestHasValidatedDoneEntryRespectsLifecycle guards against the strict rule
// being implemented as "any entry ever has an image": a picture from closure #1
// must not excuse closure #2 after a reopen.
func TestHasValidatedDoneEntryRespectsLifecycle(t *testing.T) {
	entries := []Entry{
		entry("done", "2026-09-01T10:00:00Z", ProofItem{"image": "/tmp/old.png"}),
		entry("plan", "2026-09-01T11:00:00Z"),
		entry("done", "2026-09-01T12:00:00Z", ProofItem{"cmd": "true", "out": ""}),
	}
	if HasValidatedDoneEntry(entries) {
		t.Fatal("image from the previous cycle must not validate the current one")
	}
}

func TestHasVisualProof(t *testing.T) {
	cases := []struct {
		name  string
		proof []ProofItem
		want  bool
	}{
		{"kein proof", nil, false},
		{"cmd/out", []ProofItem{{"cmd": "ls", "out": "a"}}, false},
		{"bild", []ProofItem{{"image": "/tmp/a.png"}}, true},
		{"bild leerer pfad", []ProofItem{{"image": "   "}}, false},
		{"bild missing", []ProofItem{{"image": "/tmp/a.png", "missing": true}}, false},
		{"bild missing:false", []ProofItem{{"image": "/tmp/a.png", "missing": false}}, true},
		{"dot-datei", []ProofItem{{"file": "docs/plan.dot"}}, true},
		{"svg mit zeile", []ProofItem{{"file": "docs/plan.svg:12"}}, true},
		{"go-datei", []ProofItem{{"file": "internal/reports/evidence.go:41"}}, false},
		{"gemischt", []ProofItem{{"cmd": "ls", "out": "a"}, {"file": "x.SVG"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasVisualProof(Entry{Kind: "done", Proof: c.proof}); got != c.want {
				t.Fatalf("HasVisualProof = %v, want %v", got, c.want)
			}
		})
	}
}

// TestHasVisualProofFileProofFollowsTheMissingMarker is the gkp-2ln half of
// the asymmetry gkp-bq9 recorded: a file proof the store could not resolve is a
// failed proof attempt, exactly like an image marked missing.
//
// The last two cases are the ones that must NOT change, and they are the whole
// reason the marker is read instead of the filesystem: an entry written before
// this rule carries no "missing" key at all and keeps counting. Measured in the
// live store: five file proofs point at files that are gone (four under /tmp,
// one in a deleted worktree), and re-judging them on read would void seven
// closed beads retroactively.
func TestHasVisualProofFileProofFollowsTheMissingMarker(t *testing.T) {
	cases := []struct {
		name  string
		proof []ProofItem
		want  bool
	}{
		{"dot missing", []ProofItem{{"file": "docs/weg.dot", "missing": true}}, false},
		{"svg mit zeile missing", []ProofItem{{"file": "docs/weg.svg:12", "missing": true}}, false},
		{"missing dot neben gutem bild", []ProofItem{
			{"file": "docs/weg.dot", "missing": true},
			{"image": "/tmp/a.png"},
		}, true},
		{"dot missing:false", []ProofItem{{"file": "docs/da.dot", "missing": false}}, true},
		{"dot ohne marker (vor der Regel geschrieben)", []ProofItem{{"file": "docs/alt.dot"}}, true},
		{"go-datei missing ist kein sichtbarer beleg", []ProofItem{{"file": "x.go:1", "missing": true}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasVisualProof(Entry{Kind: "done", Proof: c.proof}); got != c.want {
				t.Fatalf("HasVisualProof = %v, want %v", got, c.want)
			}
		})
	}
}

// TestHasBrokenVisualProof pins the rule behind the THIRD refusal message: the
// current done entry tried to show something and the store could not keep it.
// That is a different failure from "only text was handed in", and it has a
// different fix — hence a separate question rather than a shade of
// HasValidatedDoneEntry.
func TestHasBrokenVisualProof(t *testing.T) {
	broken := ProofItem{"file": "docs/weg.svg", "missing": true}
	brokenImg := ProofItem{"image": "/tmp/weg.png", "missing": true}
	good := ProofItem{"file": "docs/da.dot"}
	text := ProofItem{"cmd": "go test ./...", "out": "ok"}

	cases := []struct {
		name    string
		entries []Entry
		want    bool
	}{
		{"leer", nil, false},
		{"nur text", []Entry{entry("done", "2026-09-01T10:00:00Z", text)}, false},
		{"kaputte datei", []Entry{entry("done", "2026-09-01T10:00:00Z", broken)}, true},
		{"kaputtes bild", []Entry{entry("done", "2026-09-01T10:00:00Z", brokenImg)}, true},
		{"kaputt neben gut", []Entry{entry("done", "2026-09-01T10:00:00Z", broken, good)}, true},
		{"kaputt im vorigen zyklus", []Entry{
			entry("done", "2026-09-01T10:00:00Z", broken),
			entry("plan", "2026-09-01T11:00:00Z"),
			entry("done", "2026-09-01T12:00:00Z", text),
		}, false},
		{"nur plan", []Entry{entry("plan", "2026-09-01T10:00:00Z", broken)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasBrokenVisualProof(c.entries); got != c.want {
				t.Fatalf("HasBrokenVisualProof = %v, want %v", got, c.want)
			}
		})
	}
}

// TestNurMissingEntscheidet: "reason" ist Beiwerk, kein Urteil.
//
// Der Marker, der zaehlt, ist und bleibt "missing". Ein zusaetzlicher Schluessel
// darf an keiner der beiden Fragen etwas drehen — sonst haette die Ablehnungs-
// Meldung, die den Grund nur WEITERREICHT, unversehens das Tor verschoben. Der
// dritte Fall ist der wichtige: ein Grund OHNE missing ist kein kaputter Beleg.
func TestNurMissingEntscheidet(t *testing.T) {
	cases := []struct {
		name         string
		proof        ProofItem
		wantVisual   bool // HasVisualProof
		wantZerbroch bool // HasBrokenVisualProof
	}{
		{"missing mit grund", ProofItem{"file": "docs/weg.svg", "missing": true, "reason": "x ist leer"}, false, true},
		{"missing ohne grund", ProofItem{"file": "docs/weg.svg", "missing": true}, false, true},
		{"grund ohne missing zaehlt als heil", ProofItem{"file": "docs/da.svg", "reason": "x ist leer"}, true, false},
		{"bild: missing mit grund", ProofItem{"image": "/tmp/weg.png", "missing": true, "reason": "kein Bildformat"}, false, true},
		{"bild: grund ohne missing", ProofItem{"image": "/tmp/da.png", "reason": "irgendwas"}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := entry("done", "2026-09-01T10:00:00Z", c.proof)
			if got := HasVisualProof(e); got != c.wantVisual {
				t.Errorf("HasVisualProof = %v, want %v", got, c.wantVisual)
			}
			if got := HasBrokenVisualProof([]Entry{e}); got != c.wantZerbroch {
				t.Errorf("HasBrokenVisualProof = %v, want %v", got, c.wantZerbroch)
			}
		})
	}
}

// TestBrokenVisualProofReasons: der Grund wird aus demselben Zyklus geholt, aus
// dem auch HasBrokenVisualProof antwortet — sonst beriete die Ablehnung zu einem
// Beleg, um den es gar nicht geht.
//
// Der Fall "missing ohne reason" liefert bewusst NICHTS statt eines Platzhalters:
// der Store ist append-only, und die Belege aus der Zeit vor dieser Aenderung
// tragen keinen Grund. Ein erfundener waere schlechter als keiner.
func TestBrokenVisualProofReasons(t *testing.T) {
	mitGrund := ProofItem{"file": "docs/weg.svg", "missing": true, "reason": "docs/weg.svg ist unter keinem der Bezugsverzeichnisse eine Datei"}
	ohneGrund := ProofItem{"image": "/tmp/weg.png", "missing": true}
	heil := ProofItem{"file": "docs/da.dot"}

	cases := []struct {
		name    string
		entries []Entry
		want    []string
	}{
		{"leer", nil, nil},
		{"heil", []Entry{entry("done", "2026-09-01T10:00:00Z", heil)}, nil},
		{"missing ohne grund", []Entry{entry("done", "2026-09-01T10:00:00Z", ohneGrund)}, nil},
		{"missing mit grund", []Entry{entry("done", "2026-09-01T10:00:00Z", mitGrund)},
			[]string{"docs/weg.svg ist unter keinem der Bezugsverzeichnisse eine Datei"}},
		{"grund aus dem vorigen zyklus zaehlt nicht", []Entry{
			entry("done", "2026-09-01T10:00:00Z", mitGrund),
			entry("plan", "2026-09-01T11:00:00Z"),
			entry("done", "2026-09-01T12:00:00Z", heil),
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BrokenVisualProofReasons(c.entries)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("BrokenVisualProofReasons = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsGraphArtefact(t *testing.T) {
	yes := []string{"a.dot", "a.svg", "a.DOT", "/abs/pfad/plan.svg", "plan.dot:1", "x.svg:9999"}
	no := []string{"", "a.png", "a.dotx", "a.svg.txt", "dot", "notes.md:12", "a.svg:notanumber"}
	for _, s := range yes {
		if !isGraphArtefact(s) {
			t.Errorf("isGraphArtefact(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isGraphArtefact(s) {
			t.Errorf("isGraphArtefact(%q) = true, want false", s)
		}
	}
}

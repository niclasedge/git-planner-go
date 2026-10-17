package server_test

// TestSchliessenTrifftDieRichtigeDatenbank ist das gefaehrlichste Kriterium
// aus gkp-92i (Teil 5, Acceptance Criteria): POST /action/task/schliessen
// schreibt ueber bdops in GENAU EINE bd-Datenbank, und aus einer Sammelansicht
// ueber mehrere Projekte muss die Route die richtige treffen — sonst schreibt
// sie ins falsche Repo.
//
// Der Vertrag, den dieser Test misst, existiert bereits: taskops/closeops
// nehmen den Pfad UNVERAENDERT aus dem Wunsch (pfadPruefen in taskops.go
// tut nichts als ihn zu trimmen), es gibt keine serverseitige Aufloesung
// eines Projektnamens auf einen Pfad fuer diese Route (das waere getTarget,
// und getTarget faellt bei einem unbekannten Namen auf opts.Cwd zurueck —
// siehe der Kommentarkopf an taskroutes.go, "ein Tippfehler im Projektnamen
// wuerde dann still in das Repo des DIENSTES schreiben"). Dieser Test ist
// die Gegenprobe DAGEGEN: er baut zwei ECHTE, unabhaengige bd-Datenbanken und
// beweist ueber deren tatsaechlichen Zustand (nicht ueber die HTTP-Antwort
// allein), dass ein Schliessen mit pfad=repoA repoB nicht anfasst — und dass
// ein Aufruf, der Ids aus repoA gegen pfad=repoB schickt (die Verwechslung,
// die eine Sammelansicht ueber mehrere Projekte erst moeglich macht),
// abgelehnt wird statt irgendwo durchzugehen.
import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// bdIssue ist der Ausschnitt von `bd show --json`, den dieser Test braucht.
func bdIssue(t *testing.T, repo, id string) map[string]any {
	t.Helper()
	cmd := exec.Command("bd", "show", "--id="+id, "--json")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bd show --id=%s in %s: %v", id, repo, err)
	}
	var issues []map[string]any
	if err := json.Unmarshal(out, &issues); err != nil {
		t.Fatalf("bd show --id=%s: keine JSON-Liste: %v (%s)", id, err, out)
	}
	if len(issues) != 1 {
		t.Fatalf("bd show --id=%s in %s: %d Treffer, erwartet 1", id, repo, len(issues))
	}
	return issues[0]
}

// epicUndKind legt in EINEM Repo eine Karte samt Kind ueber die ROUTE an —
// derselbe Weg, den die Ansicht selbst geht (anlegenUeberRoute), nicht `bd`
// direkt daneben (Spec §9.1, Fall 3: ein Test, der am echten Schreibweg
// vorbeibaut, prueft eine Welt, die es nicht gibt).
func epicUndKind(t *testing.T, basis, repo string) (epic, kind string) {
	t.Helper()
	epic = anlegenUeberRoute(t, basis, repo, `"titel":"Karte","typ":"epic"`)
	kind = anlegenUeberRoute(t, basis, repo, `"titel":"Kind","elternteil":`+jsonStr(epic))
	return epic, kind
}

func TestSchliessenTrifftDieRichtigeDatenbank(t *testing.T) {
	repoA := wegwerfRepoFuerRouten(t)
	repoB := wegwerfRepoFuerRouten(t)
	basis, _ := taskServer(t)

	epicA, kindA := epicUndKind(t, basis, repoA)
	_, kindB := epicUndKind(t, basis, repoB)

	// --- Schliessen MIT dem richtigen Paar (pfad=repoA, Ids aus repoA):
	// erzwingen umgeht die Belegpruefung, die nicht Gegenstand dieses Tests
	// ist — hier geht es ausschliesslich darum, GEGEN WELCHE DATENBANK
	// geschrieben wird.
	resp, rumpf := postJSON(t, basis, "/action/task/schliessen",
		`{"pfad":`+jsonStr(repoA)+`,"karte":`+jsonStr(epicA)+`,`+
			`"wuensche":[{"bead":`+jsonStr(kindA)+`,"grund":"test"}],"erzwingen":true}`, nil)
	m := alsJSON(t, resp, rumpf)
	if resp.StatusCode != 200 {
		t.Fatalf("Schliessen (repoA/kindA): Status = %d, erwartet 200 (%s)", resp.StatusCode, rumpf)
	}
	geschlossen, _ := m["geschlossen"].([]any)
	if len(geschlossen) != 1 || geschlossen[0] != kindA {
		t.Fatalf("geschlossen = %v, erwartet [%q]", m["geschlossen"], kindA)
	}

	// Der eigentliche Beweis steht NICHT in der HTTP-Antwort, sondern im
	// tatsaechlichen Zustand beider Datenbanken:
	if status := bdIssue(t, repoA, kindA)["status"]; status != "closed" {
		t.Errorf("repoA/%s: status = %v, erwartet \"closed\"", kindA, status)
	}
	if status := bdIssue(t, repoB, kindB)["status"]; status != "open" {
		t.Errorf("repoB/%s: status = %v, erwartet weiterhin \"open\" — die Route hat die FALSCHE Datenbank angefasst", kindB, status)
	}

	// --- Gegenprobe: dieselben Ids aus repoA, aber pfad=repoB — genau die
	// Verwechslung, die eine Zeile aus der Sammelansicht mit dem falschen
	// AnsichtEintrag.Projekt erzeugen wuerde. bd in repoB kennt weder epicA
	// noch kindA (zwei unabhaengige Datenbanken vergeben eigene Ids), die
	// Route muss also ABLEHNEN statt irgendetwas in repoB zu schliessen.
	resp2, rumpf2 := postJSON(t, basis, "/action/task/schliessen",
		`{"pfad":`+jsonStr(repoB)+`,"karte":`+jsonStr(epicA)+`,`+
			`"wuensche":[{"bead":`+jsonStr(kindA)+`,"grund":"test"}],"erzwingen":true}`, nil)
	if resp2.StatusCode == 200 {
		t.Fatalf("Schliessen (repoB mit Ids aus repoA): Status = 200 — haette ablehnen muessen (%s)", rumpf2)
	}
	m2 := alsJSON(t, resp2, rumpf2)
	if grund, _ := m2["grund"].(string); grund == "" {
		t.Errorf("Ablehnung ohne Grund: %s", rumpf2)
	}

	// repoB bleibt danach unveraendert — der abgelehnte Aufruf darf keine
	// Spur hinterlassen, weder am eigentlichen Kind noch sonstwo in der DB.
	if status := bdIssue(t, repoB, kindB)["status"]; status != "open" {
		t.Errorf("repoB/%s nach der abgelehnten Verwechslung: status = %v, erwartet weiterhin \"open\"", kindB, status)
	}
	if strings.TrimSpace(kindA) == strings.TrimSpace(kindB) {
		t.Fatalf("Testaufbau untauglich: kindA und kindB sind identisch (%q) — die Gegenprobe braeuchte verschiedene Ids", kindA)
	}
}

// TestTerminierenTrifftDieRichtigeDatenbank ist dasselbe Kriterium wie
// TestSchliessenTrifftDieRichtigeDatenbank, fuer die ZWEITE in Teil 5 genannte
// Route: POST /action/task/terminieren. Beide Routen lesen den Pfad
// unveraendert aus dem Wunsch (taskops.pfadPruefen trimmt nur), aber es sind
// zwei verschiedene Code-Pfade (taskops.Dienst.Terminieren vs.
// closeops.Dienst.Schliessen) — ein Test, der nur einen von beiden misst,
// deckt den anderen nicht ab.
func TestTerminierenTrifftDieRichtigeDatenbank(t *testing.T) {
	repoA := wegwerfRepoFuerRouten(t)
	repoB := wegwerfRepoFuerRouten(t)
	basis, _ := taskServer(t)

	idA := anlegenUeberRoute(t, basis, repoA, `"titel":"Aufgabe A"`)
	idB := anlegenUeberRoute(t, basis, repoB, `"titel":"Aufgabe B"`)

	resp, rumpf := postJSON(t, basis, "/action/task/terminieren",
		`{"pfad":`+jsonStr(repoA)+`,"id":`+jsonStr(idA)+`,"frist":"2026-12-24"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("Terminieren (repoA/idA): Status = %d, erwartet 200 (%s)", resp.StatusCode, rumpf)
	}

	if due := bdIssue(t, repoA, idA)["due_at"]; due == nil || due == "" {
		t.Errorf("repoA/%s: due_at = %v, erwartet gesetzt", idA, due)
	}
	if due := bdIssue(t, repoB, idB)["due_at"]; due != nil && due != "" {
		t.Errorf("repoB/%s: due_at = %v, erwartet weiterhin leer — die Route hat die FALSCHE Datenbank angefasst", idB, due)
	}
}

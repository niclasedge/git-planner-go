// Die Herkunft der Historie (gkp-uc7.8): woher ein Schritt an einem Bead kommt
// und von wem. Zwei Quellen liegen im Repo selbst und kosten keinen Netzaufruf,
// also werden sie hier gesammelt und reiten im Projekt-Cache mit:
//
//   - Commits, die eine Bead-Id nennen — Autor aus git.
//   - bd-Statuswechsel aus .beads/interactions.jsonl — actor und Begruendung.
//     Das ist die EINZIGE Quelle, die festhaelt, wer einen Bead geschlossen hat:
//     `bd show` zeigt den Endzustand, nicht den Weg dahin. Ein per Hand
//     geschlossener Bead (der Fall, den gkp-7ng verfolgt) ist nur hier zu sehen.
//
// Die dritte Quelle, Pull Requests, fehlt hier absichtlich: sie kostet einen
// Netzaufruf und bleibt deshalb an GET /github (2 min TTL, github.go) haengen.
package collectors

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HistoryCommitLimit ist die Anzahl Commits, die rueckwaerts durchsucht wird.
// Ohne Grenze waechst der Aufruf mit der Repo-Historie, obwohl die Ansicht nur
// die jungen Commits zeigt; 400 deckt in diesen Repos mehrere Monate ab.
const HistoryCommitLimit = 400

// HistoryTrackerLimit begrenzt, wie viele Zeilen des bd-Audit-Logs gelesen
// werden. Die Datei ist append-only (gemessen 2026-09-05: 3–370 KB je Repo),
// waechst also monoton — irgendwann muss ein Deckel drin sein.
const HistoryTrackerLimit = 2000

// Trennzeichen fuer `git log --format`. \x1e/\x1f (record/unit separator)
// kommen in Commit-Texten nicht vor — anders als \n, das in %b steht, und
// anders als jedes druckbare Zeichen, das jemand im Subject verwenden koennte.
const (
	gitRecordSep = "\x1e"
	gitFieldSep  = "\x1f"
)

// BeadCommit ist ein Commit, der mindestens eine bekannte Bead-Id nennt.
type BeadCommit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	At      string `json:"at"` // RFC3339 UTC, damit ein String-Vergleich chronologisch ist
	Author  string `json:"author"`
	Subject string `json:"subject"`
	// Beads sind alle im Commit genannten Ids, nicht nur die erste: ein Commit
	// wie "orbit-61z, orbit-7ys.1: TUI verdrahten" gehoert unter beide.
	Beads []string `json:"beads"`
}

// TrackerChange ist ein Statuswechsel aus dem bd-Audit-Log.
type TrackerChange struct {
	At     string `json:"at"` // RFC3339 UTC
	Actor  string `json:"actor"`
	Bead   string `json:"bead"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// isBeadIDChar sagt, welche Zeichen noch zu einer Bead-Id gehoeren koennten.
// Der Punkt ist dabei, dass '.' dazugehoert: sonst wuerde "gkp-ar7" in
// "gkp-ar7.14" als Treffer gelten und der Commit unter dem Elternbead landen
// statt unter dem Kind.
func isBeadIDChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '-' || b == '_' || b == '.'
}

// containsBeadRef prueft, ob id in text als eigenstaendiges Token vorkommt.
func containsBeadRef(text, id string) bool {
	for from := 0; from+len(id) <= len(text); {
		i := strings.Index(text[from:], id)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(id)
		linksFrei := i == 0 || !isBeadIDChar(text[i-1])
		rechtsFrei := end == len(text) || !isBeadIDChar(text[end])
		if linksFrei && rechtsFrei {
			return true
		}
		from = i + 1
	}
	return false
}

// BeadRefsIn gibt die Ids aus ids zurueck, die in text vorkommen — in der
// Reihenfolge von ids, damit das Ergebnis nicht von der Textstelle abhaengt.
//
// Bewusst ein Abgleich gegen die BEKANNTEN Ids und kein Regex auf das
// Id-Muster. Ein Regex `\b[a-z]+-[a-z0-9]{3,}\b` liest sich harmlos, liefert
// aber Fehltreffer: in orbit trifft er "orbit-eigene" in einem Commit-Subject und
// haengt den Commit an einen Bead, den es nicht gibt. Das faellt nicht auf,
// weil ein erfundener Bead in der Ansicht genauso aussieht wie ein echter. Mit
// dem exakten Abgleich kann das nicht passieren — die Kosten sind ein
// strings.Index je (Commit, Id), also nichts.
func BeadRefsIn(text string, ids []string) []string {
	if text == "" || len(ids) == 0 {
		return nil
	}
	var out []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if containsBeadRef(text, id) {
			out = append(out, id)
		}
	}
	return out
}

// CollectBeadCommits sucht in den letzten HistoryCommitLimit Commits nach
// bekannten Bead-Ids. Zweiter Rueckgabewert ist die Zahl der durchsuchten
// Commits — ohne die kann die Ansicht nicht zwischen "kein Commit nennt einen
// Bead" und "git war nicht erreichbar" unterscheiden, und genau das ist der
// haeufige Fall: dieses Repo selbst hatte am 2026-09-05 keinen einzigen Commit
// mit Bead-Id, orbit dagegen fast nur solche.
func CollectBeadCommits(cwd string, ids []string) ([]BeadCommit, int) {
	if len(ids) == 0 {
		return nil, 0
	}
	repo := RepoRoot(cwd)
	if repo == "" {
		return nil, 0
	}
	format := gitRecordSep + "%H" + gitFieldSep + "%aI" + gitFieldSep + "%an" +
		gitFieldSep + "%s" + gitFieldSep + "%b"
	out, ok := gitRun(repo, "log", "-n", strconv.Itoa(HistoryCommitLimit), "--format="+format)
	if !ok {
		return nil, 0
	}
	return parseBeadCommits(out, ids)
}

// parseBeadCommits ist der reine Teil: von der git-Ausgabe zu den Treffern.
// Getrennt, damit die Zuordnung ohne git-Repo testbar ist.
func parseBeadCommits(raw string, ids []string) ([]BeadCommit, int) {
	var out []BeadCommit
	scanned := 0
	for _, rec := range strings.Split(raw, gitRecordSep) {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, gitFieldSep, 5)
		if len(f) < 4 {
			continue
		}
		scanned++
		hash, at, author, subject := f[0], f[1], f[2], f[3]
		body := ""
		if len(f) == 5 {
			body = f[4]
		}
		// Subject UND Body durchsuchen: die Konvention setzt die Id ins
		// Subject, aber ein Trailer wie "Refs: gkp-uc7.8" ist genauso eine
		// Zuordnung und darf nicht verloren gehen.
		refs := BeadRefsIn(subject+"\n"+body, ids)
		if len(refs) == 0 {
			continue
		}
		short := hash
		if len(short) > 7 {
			short = short[:7]
		}
		out = append(out, BeadCommit{
			Hash:    hash,
			Short:   short,
			At:      toUTC(at),
			Author:  strings.TrimSpace(author),
			Subject: strings.TrimSpace(subject),
			Beads:   refs,
		})
	}
	return out, scanned
}

// toUTC normalisiert einen Zeitstempel auf RFC3339 in UTC.
//
// Das ist keine Kosmetik: die Historie wird ueber den At-STRING sortiert
// (buildTimeline verlaesst sich schon darauf), und git liefert mit %aI die
// lokale Zone — "2026-09-04T19:11:22+02:00" sortiert gegen ein
// "2026-09-04T18:00:00Z" aus dem Report-Store falsch, obwohl es spaeter liegt.
// Ein unparsbarer Wert kommt unveraendert zurueck; falsch einsortiert ist
// besser als weggeworfen.
func toUTC(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

// trackerEntry ist die Zeile des bd-Audit-Logs, soweit sie hier gebraucht wird.
type trackerEntry struct {
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	Actor     string `json:"actor"`
	IssueID   string `json:"issue_id"`
	Extra     struct {
		Field    string `json:"field"`
		NewValue string `json:"new_value"`
		OldValue string `json:"old_value"`
		Reason   string `json:"reason"`
	} `json:"extra"`
}

// CollectTrackerChanges liest die Statuswechsel aus <repo>/.beads/interactions.jsonl,
// neueste zuerst. Zweiter Rueckgabewert ist die Zahl aller gefundenen
// Statuswechsel, auch der ueber HistoryTrackerLimit hinaus verworfenen.
//
// Gefiltert wird auf field=="status". Das Log enthaelt grundsaetzlich jede
// Feldaenderung; alle mitzunehmen wuerde die Zeitleiste mit Titel- und
// Notiz-Edits fluten, waehrend die Frage, die diese Spur beantwortet, eine
// einzige ist: wer hat den Bead bewegt, und mit welcher Begruendung.
func CollectTrackerChanges(cwd string) ([]TrackerChange, int) {
	repo := RepoRoot(cwd)
	if repo == "" {
		return nil, 0
	}
	f, err := os.Open(filepath.Join(repo, ".beads", "interactions.jsonl"))
	if err != nil {
		// Kein Audit-Log ist kein Fehler: ein Repo ohne bd hat keins.
		return nil, 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Eine Begruendung kann lang sein (gemessen: 300+ Zeichen), aber nicht
	// beliebig — dieselbe Groessenordnung wie im Report-Store.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var alle []TrackerChange
	total := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var te trackerEntry
		if json.Unmarshal([]byte(line), &te) != nil {
			continue // eine kaputte Zeile kostet diese Zeile, nicht die Spur
		}
		if te.Kind != "field_change" || te.Extra.Field != "status" || te.IssueID == "" {
			continue
		}
		total++
		alle = append(alle, TrackerChange{
			At:     toUTC(te.CreatedAt),
			Actor:  strings.TrimSpace(te.Actor),
			Bead:   te.IssueID,
			From:   te.Extra.OldValue,
			To:     te.Extra.NewValue,
			Reason: strings.TrimSpace(te.Extra.Reason),
		})
	}
	// Die Datei ist append-only, also steht das Neueste am Ende. Umdrehen statt
	// sortieren: die Reihenfolge in der Datei IST die Chronologie, und ein
	// gleicher Zeitstempel soll die Schreibreihenfolge behalten.
	for i, j := 0, len(alle)-1; i < j; i, j = i+1, j-1 {
		alle[i], alle[j] = alle[j], alle[i]
	}
	if len(alle) > HistoryTrackerLimit {
		alle = alle[:HistoryTrackerLimit]
	}
	return alle, total
}

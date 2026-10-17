package appctx

// Vorschlag fuer Branch und PR je Issue (pr-anlegen, #79): welche Aktion
// ansteht (erst der Branch, dann der PR), wie der Branch heisst, und mit
// welchem Titel und Text der PR vorbelegt wird. Alles HIER gerechnet; die
// Detailspalte zeigt nur den Knopf und das Formular.

import (
	"fmt"
	"strings"
	"unicode"
)

// Aktionen eines Vorschlags.
const (
	AnlegenBranch = "branch"
	AnlegenPR     = "pr"
)

// slugGrenze ist die hoechste Laenge des Titelteils im Branch-Namen.
const slugGrenze = 40

// Anlegen ist der Vorschlag fuer ein offenes Issue. Bei Aktion branch ist
// Branch der vorgeschlagene Name, bei pr der Head des PRs.
type Anlegen struct {
	Aktion string `json:"aktion"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Titel  string `json:"titel"`
	Text   string `json:"text"`
}

var umschrift = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "ae", "Ö", "oe", "Ü", "ue", "ß", "ss")

// TitelSlug macht aus einem Titel den Teil eines Branch-Namens:
// Kleinbuchstaben, Umlaute umschrieben, alles andere zu "-", hoechstens
// slugGrenze Zeichen, an einer Wortgrenze gekuerzt. Leer wird "issue".
func TitelSlug(titel string) string {
	var b strings.Builder
	strich := false
	for _, r := range umschrift.Replace(titel) {
		r = unicode.ToLower(r)
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if strich && b.Len() > 0 {
				b.WriteByte('-')
			}
			strich = false
			b.WriteRune(r)
			continue
		}
		strich = true
	}
	s := b.String()
	if len(s) > slugGrenze {
		s = s[:slugGrenze]
		if i := strings.LastIndexByte(s, '-'); i > 0 {
			s = s[:i]
		}
		s = strings.Trim(s, "-")
	}
	if s == "" {
		return "issue"
	}
	return s
}

// BranchVorschlag: change/KENNUNG mit Change, sonst NR-SLUG.
func BranchVorschlag(nr int, titel, kennung string) string {
	if kennung != "" {
		return "change/" + kennung
	}
	return fmt.Sprintf("%d-%s", nr, TitelSlug(titel))
}

// PRTitelVorschlag folgt der Konvention "kennung: … (#N)".
func PRTitelVorschlag(nr int, titel, kennung string) string {
	titel = strings.TrimSpace(titel)
	if kennung != "" {
		rest := titel
		if strings.HasPrefix(strings.ToLower(rest), strings.ToLower(kennung)+":") {
			rest = strings.TrimSpace(rest[len(kennung)+1:])
		}
		titel = kennung + ": " + rest
	}
	marke := fmt.Sprintf("(#%d)", nr)
	if !strings.Contains(titel, marke) {
		titel += " " + marke
	}
	return titel
}

// PRTextVorschlag beginnt mit "Closes #N", damit der Merge das Issue schliesst.
func PRTextVorschlag(nr int, kennung string) string {
	text := fmt.Sprintf("Closes #%d", nr)
	if kennung != "" {
		text += fmt.Sprintf("\n\nChange `%s` (openspec/changes/%s).", kennung, kennung)
	}
	return text
}

// GueltigerBranchName haelt die Regeln von git check-ref-format --branch,
// enger gefasst: nur Buchstaben, Ziffern, ".", "_", "-" und "/".
func GueltigerBranchName(s string) bool {
	if s == "" || len(s) > 200 || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") ||
		strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock") || strings.Contains(s, "..") || strings.Contains(s, "//") ||
		strings.Contains(s, "@{") {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/') {
			return false
		}
	}
	for _, teil := range strings.Split(s, "/") {
		if strings.HasPrefix(teil, ".") || strings.HasSuffix(teil, ".lock") {
			return false
		}
	}
	return true
}

// AnlegenVorschlag rechnet den Vorschlag fuer ein offenes Issue mit seinen
// verknuepften Branches; nil, wenn einer davon schon einen offenen PR hat.
func AnlegenVorschlag(e ArbeitsEintrag, standard string) *Anlegen {
	if e.Art != "issue" || e.Nummer <= 0 {
		return nil
	}
	if standard == "" {
		standard = "main"
	}
	kennung := ""
	if e.Change != nil && !e.Change.Archiviert {
		kennung = e.Change.Kennung
	}
	v := &Anlegen{Aktion: AnlegenBranch, Branch: BranchVorschlag(e.Nummer, e.Titel, kennung), Base: standard,
		Titel: PRTitelVorschlag(e.Nummer, e.Titel, kennung), Text: PRTextVorschlag(e.Nummer, kennung)}
	if len(e.Branches) == 0 {
		return v
	}
	for _, b := range e.Branches {
		if b.PR != nil {
			return nil
		}
	}
	v.Aktion = AnlegenPR
	v.Branch = e.Branches[0].Name
	return v
}

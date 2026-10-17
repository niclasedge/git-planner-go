// HTML-Dateien eines Repos (beleg-bilder, html-dateien, #112).
//
// Aus dem Baum der Spitze des Standard-Branches die .html/.htm-Dateien
// (html-dateien-baum, #114; vorher nur aus den letzten 30 Commits), je Pfad
// einmal mit seiner letzten Aenderung, die neuesten 10, neueste zuerst. Dazu das Erstellt-Datum (Commit mit status added, sonst der
// aelteste Commit des Pfad-Verlaufs) und der Bildzaehler: welche Bilder die
// Datei zeigt und ob sie im Baum des Commits liegen. Die Oberflaeche zeichnet
// die Liste und den fertigen Zaehlertext, sie rechnet nichts nach.
package appctx

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// HTMLDateienTiefe ist die Zahl der HTML-Dateien, die die Liste hoechstens zeigt.
const HTMLDateienTiefe = 10

// HTMLBilder ist der Bildzaehler einer HTML-Datei.
type HTMLBilder struct {
	Gesamt      int `json:"gesamt"`
	Vorhanden   int `json:"vorhanden"`
	Fehlt       int `json:"fehlt"`
	Extern      int `json:"extern"`
	Eingebettet int `json:"eingebettet"`
	// Fehlende sind die Repo-Pfade der Bilder, die nicht im Baum liegen.
	Fehlende []string `json:"fehlende"`
	Text     string   `json:"text"`
	// Grund steht da, wenn Datei oder Baum nicht zu lesen waren.
	Grund string `json:"grund,omitempty"`
}

// HTMLDatei ist eine Zeile unter "HTML-Dateien".
type HTMLDatei struct {
	Pfad string `json:"pfad"`
	Name string `json:"name"`
	// SHA ist der Blob-SHA der Datei nach dem Commit.
	SHA     string `json:"sha"`
	Zeit    string `json:"zeit"`
	Commit  string `json:"commit"`
	Betreff string `json:"betreff"`
	// Erstellt ist leer, wenn der anlegende Commit nicht zu finden war.
	Erstellt       string     `json:"erstellt"`
	ErstelltCommit string     `json:"erstellt_commit,omitempty"`
	Adresse        string     `json:"adresse"`
	Link           string     `json:"link"`
	Bilder         HTMLBilder `json:"bilder"`
	// ErstelltText/-Voll und GeaendertText/-Voll sind Erstellt und Zeit nach
	// ZeitpunktText in der Anzeigezone (letzte-aktionen, #126).
	ErstelltText  string `json:"erstellt_text"`
	ErstelltVoll  string `json:"erstellt_voll"`
	GeaendertText string `json:"geaendert_text"`
	GeaendertVoll string `json:"geaendert_voll"`
}

// HTMLZeittexte traegt an jeder Datei die Texte von Erstellt und Zeit gegen
// jetzt ein; ohne lesbare Zeit bleiben sie leer.
func HTMLZeittexte(dateien []HTMLDatei, jetzt time.Time) {
	for i := range dateien {
		d := &dateien[i]
		d.ErstelltText, d.ErstelltVoll = ZeitpunktText(d.Erstellt, jetzt)
		d.GeaendertText, d.GeaendertVoll = ZeitpunktText(d.Zeit, jetzt)
	}
}

// HTMLDateien ist die Antwort von /api/html-dateien.
type HTMLDateien struct {
	Repo    string      `json:"repo"`
	Ref     string      `json:"ref,omitempty"`
	Dateien []HTMLDatei `json:"dateien"`
	Grund   string      `json:"grund,omitempty"`
}

// IstHTMLPfad sagt, ob pfad nach seiner Endung eine HTML-Datei ist.
func IstHTMLPfad(pfad string) bool {
	e := strings.ToLower(path.Ext(pfad))
	return (e == ".html" || e == ".htm") && path.Base(pfad) != e
}

// DateiAdresse ist die pfadbasierte Adresse, unter der der Dienst die Datei
// pfad aus dem Commit gerendert ausliefert (/api/datei/…). Jedes Glied ist
// einzeln maskiert, damit relative Verweise gegen denselben Commit laufen.
func DateiAdresse(nwo, commit, pfad string) string {
	glieder := strings.Split(pfad, "/")
	for i, g := range glieder {
		glieder[i] = url.PathEscape(g)
	}
	return "/api/datei/" + nwo + "/" + url.PathEscape(commit) + "/" + strings.Join(glieder, "/")
}

// HTMLBaumDeckel ist die Zahl der HTML-Dateien aus dem Baum, die hoechstens
// betrachtet werden (je Datei kostet die letzte Aenderung einen Abruf).
const HTMLBaumDeckel = 200

// HTMLPfadeAusBaum liefert die Pfade der HTML-Dateien im Baum, nach Pfad
// sortiert und auf HTMLBaumDeckel begrenzt. Greift die Grenze, nennt grund
// das.
func HTMLPfadeAusBaum(baum []ghrest.BaumEintrag) (pfade []string, grund string) {
	pfade = []string{}
	for _, e := range baum {
		if e.Typ == "blob" && IstHTMLPfad(e.Pfad) {
			pfade = append(pfade, e.Pfad)
		}
	}
	sort.Strings(pfade)
	if len(pfade) > HTMLBaumDeckel {
		grund = fmt.Sprintf("%d HTML-Dateien im Baum, betrachtet sind die ersten %d nach Pfad", len(pfade), HTMLBaumDeckel)
		pfade = pfade[:HTMLBaumDeckel]
	}
	return pfade, grund
}

// HTMLDateienAusBaum waehlt aus dem Baum an der Spitze die neuesten
// HTML-Dateien nach ihrer letzten Aenderung (letzte, je Pfad) aus, gleich
// wie alt sie ist. Fehlt die letzte Aenderung eines Pfades, zeigt der
// Eintrag auf die Spitze und steht hinten. Erstellt setzt es, wo ein Commit
// aus commits (dem Fenster der juengsten Commits) den Pfad angelegt hat.
func HTMLDateienAusBaum(repo, spitze string, baum []ghrest.BaumEintrag, letzte map[string]ghrest.CommitKopf, commits []ghrest.CommitMitDateien) ([]HTMLDatei, string) {
	pfade, grund := HTMLPfadeAusBaum(baum)
	blob := map[string]string{}
	for _, e := range baum {
		if e.Typ == "blob" {
			blob[e.Pfad] = e.SHA
		}
	}
	out := make([]HTMLDatei, 0, len(pfade))
	for _, p := range pfade {
		e := HTMLDatei{Pfad: p, Name: path.Base(p), SHA: blob[p], Commit: spitze}
		if k, ok := letzte[p]; ok && k.SHA != "" {
			e.Zeit, e.Commit, e.Betreff = k.Zeit, k.SHA, k.Betreff
		}
		if repo != "" && e.Commit != "" {
			e.Adresse = DateiAdresse(repo, e.Commit, p)
			e.Link = "https://github.com/" + repo + "/blob/" + e.Commit + "/" + p
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Zeit != out[j].Zeit {
			return out[i].Zeit > out[j].Zeit
		}
		return out[i].Pfad < out[j].Pfad
	})
	if len(out) > HTMLDateienTiefe {
		out = out[:HTMLDateienTiefe]
	}
	erstelltAusCommits(out, commits)
	return out, grund
}

// erstelltAusCommits setzt Erstellt: der juengste Commit mit status added
// fuer den Pfad, nicht neuer als die letzte Aenderung.
func erstelltAusCommits(out []HTMLDatei, commits []ghrest.CommitMitDateien) {
	folge := append([]ghrest.CommitMitDateien(nil), commits...)
	sort.SliceStable(folge, func(i, j int) bool { return folge[i].Zeit > folge[j].Zeit })
	for i := range out {
		if out[i].Zeit == "" {
			continue
		}
		for _, c := range folge {
			if c.Zeit > out[i].Zeit {
				continue
			}
			for _, d := range c.Dateien {
				if d.Pfad == out[i].Pfad && d.Status == "added" {
					out[i].Erstellt, out[i].ErstelltCommit = c.Zeit, c.SHA
					break
				}
			}
			if out[i].Erstellt != "" {
				break
			}
		}
	}
}

// ErstelltAusVerlauf nimmt den aeltesten Commit aus dem Verlauf eines Pfades
// (neueste zuerst). Scheitert der Verlauf oder ist er leer, bleibt es leer.
func ErstelltAusVerlauf(verlauf []ghrest.CommitKopf, err error) (zeit, commit string) {
	if err != nil || len(verlauf) == 0 {
		return "", ""
	}
	a := verlauf[len(verlauf)-1]
	return a.Zeit, a.SHA
}

var (
	htmlKommentarRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlSkriptRe    = regexp.MustCompile(`(?is)<script\b.*?</script\s*>`)
	htmlStilRe      = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style\s*>`)
	htmlTagRe       = regexp.MustCompile(`(?is)<(img|source|image)\b([^>]*)>`)
	htmlStilAttrRe  = regexp.MustCompile(`(?is)<[a-z][a-z0-9:-]*\b([^>]*)>`)
	htmlAttrRe      = regexp.MustCompile(`(?is)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	cssURLRe        = regexp.MustCompile(`(?is)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)`)
	schemaRe        = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

func htmlAttribute(roh string) map[string]string {
	out := map[string]string{}
	for _, m := range htmlAttrRe.FindAllStringSubmatch(roh, -1) {
		name := strings.ToLower(m[1])
		if _, schon := out[name]; schon {
			continue
		}
		out[name] = html.UnescapeString(m[2] + m[3] + m[4])
	}
	return out
}

func cssURLs(css string) []string {
	var out []string
	for _, m := range cssURLRe.FindAllStringSubmatch(css, -1) {
		out = append(out, m[1]+m[2]+m[3])
	}
	return out
}

// htmlBildVerweise sammelt die Bildverweise einer HTML-Datei in der Folge,
// in der sie stehen.
func htmlBildVerweise(inhalt string) []string {
	inhalt = htmlKommentarRe.ReplaceAllString(inhalt, "")
	inhalt = htmlSkriptRe.ReplaceAllString(inhalt, "")
	var out []string
	for _, m := range htmlTagRe.FindAllStringSubmatch(inhalt, -1) {
		a := htmlAttribute(m[2])
		switch strings.ToLower(m[1]) {
		case "img":
			out = append(out, a["src"])
		case "source":
			for _, kandidat := range strings.Split(a["srcset"], ",") {
				if f := strings.Fields(kandidat); len(f) > 0 {
					out = append(out, f[0])
				}
			}
		case "image":
			if h, ok := a["href"]; ok {
				out = append(out, h)
			} else {
				out = append(out, a["xlink:href"])
			}
		}
	}
	for _, m := range htmlStilRe.FindAllStringSubmatch(inhalt, -1) {
		out = append(out, cssURLs(m[1])...)
	}
	for _, m := range htmlStilAttrRe.FindAllStringSubmatch(htmlStilRe.ReplaceAllString(inhalt, ""), -1) {
		if s, ok := htmlAttribute(m[1])["style"]; ok {
			out = append(out, cssURLs(s)...)
		}
	}
	return out
}

// HTMLBilderZaehlen zaehlt die Bilder, die die HTML-Datei pfad zeigt, und
// ordnet jedes ein; vorhanden sagt, ob ein Repo-Pfad im Baum des Commits
// liegt.
func HTMLBilderZaehlen(pfad string, inhalt []byte, vorhanden func(string) bool) HTMLBilder {
	z := HTMLBilder{Fehlende: []string{}}
	gezaehlt := map[string]bool{}
	for _, v := range htmlBildVerweise(string(inhalt)) {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		klein := strings.ToLower(v)
		switch {
		case strings.HasPrefix(klein, "data:"):
			if gezaehlt[v] {
				continue
			}
			gezaehlt[v] = true
			z.Gesamt++
			z.Eingebettet++
			z.Vorhanden++
		case strings.HasPrefix(v, "//") || schemaRe.MatchString(v):
			if gezaehlt[v] {
				continue
			}
			gezaehlt[v] = true
			z.Gesamt++
			z.Extern++
		default:
			ziel, ok := repoPfadAus(pfad, v)
			schluessel := "repo:" + ziel
			if !ok {
				schluessel = "raus:" + v
			}
			if gezaehlt[schluessel] {
				continue
			}
			gezaehlt[schluessel] = true
			z.Gesamt++
			if ok && vorhanden != nil && vorhanden(ziel) {
				z.Vorhanden++
			} else {
				z.Fehlt++
				if ok {
					z.Fehlende = append(z.Fehlende, ziel)
				} else {
					z.Fehlende = append(z.Fehlende, v)
				}
			}
		}
	}
	z.Text = BilderText(z)
	return z
}

// repoPfadAus loest einen relativen oder wurzelrelativen Verweis aus der
// Datei pfad zu einem Repo-Pfad auf; false, wenn er aus der Wurzel fuehrt.
func repoPfadAus(pfad, verweis string) (string, bool) {
	if i := strings.IndexAny(verweis, "?#"); i >= 0 {
		verweis = verweis[:i]
	}
	if u, err := url.PathUnescape(verweis); err == nil {
		verweis = u
	}
	var voll string
	if strings.HasPrefix(verweis, "/") {
		voll = strings.TrimLeft(verweis, "/")
	} else {
		voll = path.Join(path.Dir(pfad), verweis)
	}
	// Die Glieder vor dem Clean zaehlen: ein Ausbruch aus der Wurzel fehlt.
	tiefe := 0
	for _, g := range strings.Split(voll, "/") {
		switch g {
		case "", ".":
		case "..":
			tiefe--
			if tiefe < 0 {
				return "", false
			}
		default:
			tiefe++
		}
	}
	sauber := path.Clean(voll)
	if sauber == "." || sauber == "" {
		return "", false
	}
	return sauber, true
}

// BilderText ist der Text des Zaehlers.
func BilderText(z HTMLBilder) string {
	if z.Gesamt == 0 {
		return "keine Bilder"
	}
	wort := "Bilder"
	if z.Gesamt == 1 {
		wort = "Bild"
	}
	teile := []string{fmt.Sprintf("%d %s", z.Gesamt, wort)}
	if z.Fehlt == 0 && z.Extern == 0 {
		return teile[0] + " · alle vorhanden"
	}
	teile = append(teile, fmt.Sprintf("%d vorhanden", z.Vorhanden))
	if z.Extern > 0 {
		teile = append(teile, fmt.Sprintf("%d extern", z.Extern))
	}
	if z.Fehlt > 0 {
		teile = append(teile, fmt.Sprintf("%d fehlt", z.Fehlt))
	}
	return strings.Join(teile, " · ")
}

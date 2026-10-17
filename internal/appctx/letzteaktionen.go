package appctx

// "Letzte Aktionen" als Zeitleiste (letzte-aktionen, #126;
// aktionen-zeitleiste, #128): die juengsten Ereignisse eines Repos, neueste
// zuerst. Quellen sind die Repo-Aktivitaet ueber alle Branches (Pushes samt
// ihren Commits und Dateien aus dem Vergleich, Merges, Branch angelegt und
// geloescht), jeder CI-Run, die zuletzt geschlossenen PRs, jedes geaenderte
// Bild, jede HTML-Datei, jeder Beleg und die zuletzt geschlossenen Issues.
// Ausgewaehlt, zusammengefasst, sortiert und beschriftet wird hier, auch Tag
// und Uhrzeit; der Browser zeichnet. Liegt ein Speicher nicht vor, fehlen
// seine Zeilen — hier wird nichts geholt.

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/anzeigezeit"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// LetzteAktionenZuerst ist die Zahl der Zeilen, die der Zeitstrom in jedem
// Fall liefert, LetzteAktionenFenster die Spanne, aus der er darueber hinaus
// jede Zeile liefert, LetzteAktionenAnzahl die hoechste Zahl der Zeilen
// (aktionen-24h, #132); AktionCommitsMax die Zahl der Commits je Push-Zeile,
// AktionDateienMax die der Datei-Zeilen je Push-Zeile (aktionen-zeitleiste, #128).
const (
	LetzteAktionenZuerst  = 10
	LetzteAktionenFenster = 24 * time.Hour
	LetzteAktionenAnzahl  = 150
	AktionCommitsMax      = 5
	AktionDateienMax      = 4
)

// Die Arten einer Zeile.
const (
	AktionPush      = "push"
	AktionForcePush = "force_push"
	AktionMerge     = "merge"
	AktionBranchNeu = "branch_neu"
	AktionBranchWeg = "branch_weg"
	AktionCI        = "ci"
	AktionPR        = "pr"
	AktionBild      = "bild"
	AktionHTML      = "html"
	AktionBeleg     = "beleg"
	AktionIssue     = "issue"
)

// aktionRang ist die Folge der Quellen bei gleicher Zeit.
var aktionRang = map[string]int{
	AktionPush: 0, AktionForcePush: 0, AktionMerge: 0, AktionBranchNeu: 0, AktionBranchWeg: 0,
	AktionCI: 1, AktionPR: 2, AktionBild: 3, AktionHTML: 4, AktionBeleg: 5, AktionIssue: 6,
}

var mergePRRe = regexp.MustCompile(`^Merge pull request #(\d+)\b`)

// AktionCommit ist ein Commit unter einer Push-Zeile.
type AktionCommit struct {
	SHA     string `json:"sha"`
	Kurz    string `json:"kurz"`
	Betreff string `json:"betreff"`
	Autor   string `json:"autor,omitempty"`
	Zeit    string `json:"zeit,omitempty"`
}

// LetzteAktion ist eine Zeile des Zeitstroms. Welche Felder gesetzt sind,
// haengt an Art.
type LetzteAktion struct {
	Art  string `json:"art"`
	Wort string `json:"wort"`
	// Zeit ist der Zeitpunkt (RFC 3339), Text und Voll nach ZeitpunktText,
	// Titel der Tooltip; Tag und Uhr ordnen die Zeile in die Zeitleiste
	// (aktionen-zeitleiste, #128).
	Zeit  string `json:"zeit"`
	Text  string `json:"text"`
	Voll  string `json:"voll"`
	Titel string `json:"titel"`
	Tag   string `json:"tag"`
	Uhr   string `json:"uhr"`
	// Stunde ist der Beginn der Stunde der Zeile ("20:00"), Pause nennt die
	// leeren Stunden seit der Zeile davor (aktionen-24h, #132).
	Stunde   string `json:"stunde"`
	Pause    string `json:"pause,omitempty"`
	Juengste bool   `json:"juengste"`
	// Aktivitaet, CI-Run und PR; Branch bei Datei-Zeilen aus einem Vergleich.
	Branch      string     `json:"branch,omitempty"`
	Standard    bool       `json:"standard,omitempty"`
	Anzahl      int        `json:"anzahl,omitempty"`
	SHA         string     `json:"sha,omitempty"`
	Kurz        string     `json:"kurz,omitempty"`
	Betreff     string     `json:"betreff,omitempty"`
	BetreffKurz string     `json:"betreff_kurz,omitempty"`
	Autor       string     `json:"autor,omitempty"`
	PR          *LetzterPR `json:"pr,omitempty"`
	// Commits eines Pushes aus seinen Vergleichen, neueste zuerst;
	// CommitZahl zaehlt alle.
	CommitZahl int            `json:"commit_zahl,omitempty"`
	Commits    []AktionCommit `json:"commits,omitempty"`
	// CI-Run.
	Punkt    string `json:"punkt,omitempty"`
	Ergebnis string `json:"ergebnis,omitempty"`
	Workflow string `json:"workflow,omitempty"`
	// URL ist die des Runs, des PRs oder des Issues.
	URL string `json:"url,omitempty"`
	// Bild, HTML und Beleg.
	Name    string `json:"name,omitempty"`
	Pfad    string `json:"pfad,omitempty"`
	Bild    string `json:"bild,omitempty"`
	Link    string `json:"link,omitempty"`
	Adresse string `json:"adresse,omitempty"`
	Kennung string `json:"kennung,omitempty"`
	Stufe   string `json:"stufe,omitempty"`
	// Issue und PR.
	Nummer     int         `json:"nummer,omitempty"`
	IssueTitel string      `json:"issue_titel,omitempty"`
	PRTitel    string      `json:"pr_titel,omitempty"`
	GemergtMit *GemergtMit `json:"gemergt_mit,omitempty"`

	// blob ist der Blob-SHA einer Datei-Zeile, paare die Vergleiche
	// (before, after) einer Push-Zeile, juengster Push zuerst.
	blob  string
	paare [][2]string
}

// AktionenEingabe ist, was der Zeitstrom braucht, alles schon geholt.
type AktionenEingabe struct {
	Repo, Standard string
	// Aktivitaet ist die Repo-Aktivitaet, Commits je SHA Betreff und Autor,
	// Vergleiche je VergleichSchluessel Commits und Dateien eines Pushes.
	Aktivitaet []ghrest.Aktivitaet
	Commits    map[string]ghrest.CommitKopf
	Vergleiche map[string]ghrest.PushVergleich
	// OffenePRs sind die offenen PRs der Arbeitsliste, GeschlossenePRs die
	// zuletzt geschlossenen.
	OffenePRs       []ghrest.PR
	GeschlossenePRs []ghrest.PR
	// CIRuns, Bilder und HTML sind die Speicher der Abschnitte; nil heisst:
	// liegt nicht vor.
	CIRuns []ghrest.WorkflowRun
	Bilder *GeaenderteBilder
	HTML   *HTMLDateien
	// Belege sind die der Galerie "Belegt", Zuletzt die Eintraege von zuletzt.
	Belege  []GalerieBeleg
	Zuletzt []ArbeitsEintrag
	// BildURL ist die Adresse, unter der die Oberflaeche einen Blob laden
	// kann; nil heisst: Datei-Zeilen aus Vergleichen ohne Bild.
	BildURL func(sha, name string) string
	Jetzt   time.Time
}

// VergleichSchluessel ist der Schluessel eines Vergleichs in
// AktionenEingabe.Vergleiche.
func VergleichSchluessel(vorher, nachher string) string { return vorher + "..." + nachher }

// aktivitaetArt uebersetzt activity_type; "" zaehlt nicht.
func aktivitaetArt(a ghrest.Aktivitaet) string {
	if !strings.HasPrefix(a.Ref, "refs/heads/") {
		return ""
	}
	if a.Art == "branch_deletion" {
		return AktionBranchWeg
	}
	if a.Nachher == "" {
		return ""
	}
	switch a.Art {
	case "push":
		return AktionPush
	case "force_push":
		return AktionForcePush
	case "pr_merge", "merge_queue_merge":
		return AktionMerge
	case "branch_creation":
		return AktionBranchNeu
	}
	return ""
}

// echterVorher sagt, ob before ein Commit ist und nicht nur Nullen.
func echterVorher(sha string) bool {
	return strings.Trim(sha, "0") != ""
}

// zeitWert liest eine Zeit fuer die Folge; Unlesbares zaehlt als aeltest.
func zeitWert(s string) time.Time {
	t, _ := zeitAus(strings.TrimSpace(s))
	return t
}

// AktivitaetsZeilen fasst die Eintraege der Aktivitaet zu Zeilen zusammen,
// neueste zuerst: aufeinanderfolgende Pushes desselben Branches werden eine
// Zeile. Betreff, Autor, PR und Commits fehlen noch.
func AktivitaetsZeilen(akt []ghrest.Aktivitaet) []LetzteAktion {
	var ereignisse []ghrest.Aktivitaet
	for _, a := range akt {
		if aktivitaetArt(a) != "" {
			ereignisse = append(ereignisse, a)
		}
	}
	sort.SliceStable(ereignisse, func(i, j int) bool {
		return zeitWert(ereignisse[i].Zeit).After(zeitWert(ereignisse[j].Zeit))
	})
	var out []LetzteAktion
	for _, a := range ereignisse {
		art := aktivitaetArt(a)
		branch := strings.TrimPrefix(a.Ref, "refs/heads/")
		var paar [][2]string
		if (art == AktionPush || art == AktionForcePush) && echterVorher(a.Vorher) {
			paar = [][2]string{{a.Vorher, a.Nachher}}
		}
		if n := len(out); n > 0 && art == AktionPush && out[n-1].Art == AktionPush && out[n-1].Branch == branch {
			out[n-1].Anzahl++
			out[n-1].paare = append(out[n-1].paare, paar...)
			continue
		}
		sha := a.Nachher
		if art == AktionBranchWeg {
			sha = ""
		}
		out = append(out, LetzteAktion{Art: art, Branch: branch, SHA: sha, Zeit: a.Zeit, Autor: a.Akteur, Anzahl: 1, paare: paar})
	}
	return out
}

// AktivitaetsSHAs sind die SHAs, deren Commit die Zeilen der Aktivitaet
// fuer Betreff und Autor brauchen, ohne Doppelte.
func AktivitaetsSHAs(akt []ghrest.Aktivitaet) []string {
	var out []string
	gesehen := map[string]bool{}
	for _, z := range AktivitaetsZeilen(akt) {
		if z.SHA != "" && !gesehen[z.SHA] {
			gesehen[z.SHA] = true
			out = append(out, z.SHA)
		}
	}
	return out
}

// VergleichsPaare sind die Paare (before, after) der Pushes und Force-Pushes
// der Aktivitaet, deren Vergleich die Zeitleiste braucht, neueste zuerst und
// ohne Doppelte; ein before aus Nullen hat keinen Vergleich.
func VergleichsPaare(akt []ghrest.Aktivitaet) [][2]string {
	var out [][2]string
	gesehen := map[[2]string]bool{}
	for _, z := range AktivitaetsZeilen(akt) {
		for _, p := range z.paare {
			if !gesehen[p] {
				gesehen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func kurzSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// prTitel sucht den Titel eines PRs in den offenen und geschlossenen PRs.
func (e AktionenEingabe) prTitel(nummer int) string {
	for _, l := range [][]ghrest.PR{e.OffenePRs, e.GeschlossenePRs} {
		for _, p := range l {
			if p.Nummer == nummer {
				return p.Titel
			}
		}
	}
	return ""
}

// commitsDerZeile sind die Commits der Vergleiche einer Push-Zeile, neueste
// zuerst: der juengste Push zuerst, im Vergleich vom letzten zum ersten.
func (e AktionenEingabe) commitsDerZeile(z LetzteAktion) ([]AktionCommit, bool) {
	var out []AktionCommit
	da := false
	for _, p := range z.paare {
		v, ok := e.Vergleiche[VergleichSchluessel(p[0], p[1])]
		if !ok {
			continue
		}
		da = true
		for i := len(v.Commits) - 1; i >= 0; i-- {
			k := v.Commits[i]
			out = append(out, AktionCommit{SHA: k.SHA, Kurz: kurzSHA(k.SHA), Betreff: k.Betreff, Autor: k.Autor, Zeit: k.Zeit})
		}
	}
	return out, da
}

func (e AktionenEingabe) aktivitaetZeilen() []LetzteAktion {
	zeilen := AktivitaetsZeilen(e.Aktivitaet)
	var dateien []LetzteAktion
	for i := range zeilen {
		z := &zeilen[i]
		z.Standard = z.Branch == e.Standard
		z.Kurz = kurzSHA(z.SHA)
		if k, ok := e.Commits[z.SHA]; ok && z.SHA != "" {
			z.Betreff = k.Betreff
			if k.Autor != "" {
				z.Autor = k.Autor
			}
		}
		if commits, ok := e.commitsDerZeile(*z); ok {
			z.CommitZahl = len(commits)
			if len(commits) > 0 {
				if z.Betreff == "" {
					z.Betreff = commits[0].Betreff
					if commits[0].Autor != "" {
						z.Autor = commits[0].Autor
					}
				}
				if len(commits) > AktionCommitsMax {
					commits = commits[:AktionCommitsMax]
				}
				z.Commits = commits
			}
		}
		z.BetreffKurz = betreffKurz(z.Betreff)
		switch z.Art {
		case AktionMerge:
			z.Wort = "Merge"
			if m := mergePRRe.FindStringSubmatch(z.Betreff); m != nil {
				n, _ := strconv.Atoi(m[1])
				z.PR = &LetzterPR{Nummer: n, URL: "https://github.com/" + e.Repo + "/pull/" + m[1], Titel: e.prTitel(n)}
			}
		case AktionForcePush:
			z.Wort = "Force-Push"
		case AktionBranchNeu:
			z.Wort = "Branch angelegt"
		case AktionBranchWeg:
			z.Wort = "Branch gelöscht"
		default:
			z.Wort = "Push"
			if z.Anzahl > 1 {
				z.Wort = strconv.Itoa(z.Anzahl) + " Pushes"
			}
		}
		if z.Anzahl <= 1 {
			z.Anzahl = 0
		}
		if z.Art == AktionPush || z.Art == AktionForcePush {
			for _, p := range e.OffenePRs {
				if p.Head == z.Branch && (z.PR == nil || p.Nummer > z.PR.Nummer) {
					z.PR = &LetzterPR{Nummer: p.Nummer, URL: p.URL, Titel: p.Titel}
				}
			}
		}
		dateien = append(dateien, e.dateiZeilen(*z)...)
	}
	return append(zeilen, dateien...)
}

// belegKennung ist der Ordner eines Belegpfads unter openspec/changes/, unter
// archive/ ohne das Datum davor.
func belegKennung(pfad string) string {
	glieder := strings.Split(strings.TrimPrefix(pfad, "openspec/changes/"), "/")
	if len(glieder) > 1 && glieder[0] == "archive" {
		return archivDatumRe.ReplaceAllString(glieder[1], "")
	}
	return glieder[0]
}

// dateiZeilen sind die Belege, Bilder und HTML-Dateien aus den Vergleichen
// einer Push-Zeile, zu ihrer Zeit und auf ihrem Branch, je Pfad hoechstens
// eine und hoechstens AktionDateienMax.
func (e AktionenEingabe) dateiZeilen(z LetzteAktion) []LetzteAktion {
	var out []LetzteAktion
	gesehen := map[string]bool{}
	for _, p := range z.paare {
		v, ok := e.Vergleiche[VergleichSchluessel(p[0], p[1])]
		if !ok {
			continue
		}
		for _, f := range v.Dateien {
			if len(out) >= AktionDateienMax {
				return out
			}
			if f.Status == "removed" || f.Pfad == "" || gesehen[f.Pfad] {
				continue
			}
			d := LetzteAktion{Zeit: z.Zeit, Branch: z.Branch, Standard: z.Standard, Name: path.Base(f.Pfad), Pfad: f.Pfad,
				Betreff: z.Betreff, Link: "https://github.com/" + e.Repo + "/blob/" + z.Branch + "/" + f.Pfad, blob: f.SHA}
			endung := BelegTyp(f.Pfad)
			switch {
			case istBelegPfad(f.Pfad) && endung != "jsonl":
				d.Art, d.Wort, d.Kennung = AktionBeleg, "Beleg", belegKennung(f.Pfad)
			case IstBildPfad(f.Pfad):
				d.Art, d.Wort = AktionBild, "Bild"
			case endung == "html" || endung == "htm":
				d.Art, d.Wort, d.Adresse = AktionHTML, "HTML", DateiAdresse(e.Repo, p[1], f.Pfad)
			default:
				continue
			}
			if d.Art != AktionHTML && e.BildURL != nil && f.SHA != "" && IstBildPfad(f.Pfad) {
				d.Bild = e.BildURL(f.SHA, d.Name)
			}
			gesehen[f.Pfad] = true
			out = append(out, d)
		}
	}
	return out
}

func (e AktionenEingabe) ciZeilen() []LetzteAktion {
	var out []LetzteAktion
	for _, r := range e.CIRuns {
		wort, zeit, sonst := "CI läuft", r.Start, r.Aktualisiert
		if r.Status == "completed" {
			wort, zeit, sonst = "CI-Run", r.Aktualisiert, r.Start
		}
		if _, ok := zeitAus(zeit); !ok {
			zeit = sonst
		}
		out = append(out, LetzteAktion{Art: AktionCI, Wort: wort, Zeit: zeit, Punkt: CIPunkt(r.Status, r.Ergebnis),
			Ergebnis: r.Ergebnis, Workflow: r.Name, Branch: r.Branch, Standard: r.Branch == e.Standard, URL: r.URL,
			Kurz: kurzSHA(r.HeadSHA), BetreffKurz: betreffKurz(r.Betreff)})
	}
	return out
}

// prZeilen sind die geschlossenen PRs, die keine Merge-Zeile nennt.
func (e AktionenEingabe) prZeilen(genannt map[int]bool) []LetzteAktion {
	var out []LetzteAktion
	for _, p := range e.GeschlossenePRs {
		if genannt[p.Nummer] {
			continue
		}
		wort, zeit := "PR geschlossen", p.GeschlossenAm
		if p.Gemergt {
			wort = "PR gemergt"
			if p.GemergtAm != "" {
				zeit = p.GemergtAm
			}
		}
		out = append(out, LetzteAktion{Art: AktionPR, Wort: wort, Zeit: zeit, Nummer: p.Nummer, PRTitel: p.Titel,
			URL: p.URL, Autor: p.Autor, Branch: p.Head})
	}
	return out
}

func (e AktionenEingabe) bildZeilen() []LetzteAktion {
	if e.Bilder == nil {
		return nil
	}
	var out []LetzteAktion
	for _, b := range e.Bilder.Bilder {
		out = append(out, LetzteAktion{Art: AktionBild, Wort: "Bild", Zeit: b.Zeit, Name: b.Name, Pfad: b.Pfad,
			Bild: b.Bild, Link: b.Link, Betreff: b.Betreff, blob: b.SHA})
	}
	return out
}

func (e AktionenEingabe) htmlZeilen() []LetzteAktion {
	if e.HTML == nil {
		return nil
	}
	var out []LetzteAktion
	for _, d := range e.HTML.Dateien {
		out = append(out, LetzteAktion{Art: AktionHTML, Wort: "HTML", Zeit: d.Zeit, Name: d.Name, Pfad: d.Pfad,
			Adresse: d.Adresse, Betreff: d.Betreff, blob: d.SHA})
	}
	return out
}

func (e AktionenEingabe) belegZeilen() []LetzteAktion {
	var out []LetzteAktion
	for _, b := range e.Belege {
		z := LetzteAktion{Art: AktionBeleg, Wort: "Beleg", Zeit: b.Zeit, Kennung: b.Kennung, Stufe: b.Stufe,
			Name: b.Name, Pfad: b.Pfad, Link: b.Link, blob: b.SHA}
		if b.Zeigbar {
			z.Bild = b.Bild
		}
		out = append(out, z)
	}
	return out
}

func (e AktionenEingabe) issueZeilen() []LetzteAktion {
	var out []LetzteAktion
	for _, i := range e.Zuletzt {
		if _, ok := zeitAus(strings.TrimSpace(i.Geschlossen)); !ok {
			continue
		}
		out = append(out, LetzteAktion{Art: AktionIssue, Wort: "Issue geschlossen", Zeit: i.Geschlossen, Nummer: i.Nummer,
			IssueTitel: i.Titel, URL: i.URL, GemergtMit: i.GemergtMit})
	}
	return out
}

// dateiSchluessel ist, woran zwei Datei-Zeilen als dieselbe Datei gelten: die
// Bildadresse, sonst der Blob; "" heisst: nicht vergleichbar.
func dateiSchluessel(z LetzteAktion) string {
	switch z.Art {
	case AktionBild, AktionHTML, AktionBeleg:
	default:
		return ""
	}
	if z.Bild != "" {
		return "bild:" + z.Bild
	}
	if z.blob != "" {
		return "blob:" + z.blob
	}
	return ""
}

var wochentageKurz = [...]string{"So.", "Mo.", "Di.", "Mi.", "Do.", "Fr.", "Sa."}

// TagText ist der Tageskopf der Zeitleiste fuer t gegen jetzt, in der Zone
// von jetzt: "Heute", "Gestern", sonst "Di., 29. Sept.", in einem anderen
// Jahr mit Jahreszahl.
func TagText(t, jetzt time.Time) string {
	t = t.In(jetzt.Location())
	switch kalendertage(t, jetzt) {
	case 0:
		return "Heute"
	case 1:
		return "Gestern"
	}
	s := fmt.Sprintf("%s, %d. %s", wochentageKurz[t.Weekday()], t.Day(), monateKurz[t.Month()-1])
	if t.Year() != jetzt.Year() {
		s += fmt.Sprintf(" %d", t.Year())
	}
	return s
}

// LetzteAktionenBauen baut den Zeitstrom: aus jeder Quelle jede Zeile,
// neueste zuerst, die ersten LetzteAktionenZuerst in jedem Fall, danach jede
// aus LetzteAktionenFenster vor jetzt, hoechstens LetzteAktionenAnzahl; bei gleicher Zeit gilt
// die Folge der Quellen und innerhalb einer Quelle ihre Folge. Dieselbe Datei
// steht nur einmal, in ihrer juengsten Zeile. Die erste Zeile traegt Juengste.
func LetzteAktionenBauen(e AktionenEingabe) []LetzteAktion {
	jetzt := e.Jetzt
	if jetzt.IsZero() {
		jetzt = anzeigezeit.Jetzt()
	}
	alle := e.aktivitaetZeilen()
	genannt := map[int]bool{}
	for _, z := range alle {
		if z.Art == AktionMerge && z.PR != nil {
			genannt[z.PR.Nummer] = true
		}
	}
	alle = append(alle, e.ciZeilen()...)
	alle = append(alle, e.prZeilen(genannt)...)
	alle = append(alle, e.bildZeilen()...)
	alle = append(alle, e.htmlZeilen()...)
	alle = append(alle, e.belegZeilen()...)
	alle = append(alle, e.issueZeilen()...)
	sort.SliceStable(alle, func(i, j int) bool {
		ti, tj := zeitWert(alle[i].Zeit), zeitWert(alle[j].Zeit)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return aktionRang[alle[i].Art] < aktionRang[alle[j].Art]
	})
	out := make([]LetzteAktion, 0, LetzteAktionenZuerst)
	gesehen := map[string]bool{}
	grenze := jetzt.Add(-LetzteAktionenFenster)
	var vorige time.Time
	for _, z := range alle {
		if len(out) >= LetzteAktionenAnzahl {
			break
		}
		// Die Folge ist neueste zuerst: ab der ersten Zeile vor der Grenze
		// kommt nach den ersten zehn keine mehr.
		if len(out) >= LetzteAktionenZuerst && zeitWert(z.Zeit).Before(grenze) {
			break
		}
		if k := dateiSchluessel(z); k != "" {
			if gesehen[k] {
				continue
			}
			gesehen[k] = true
		}
		z.Text, z.Voll = ZeitpunktText(z.Zeit, jetzt)
		if t, ok := zeitAus(strings.TrimSpace(z.Zeit)); ok {
			lokal := t.In(jetzt.Location())
			z.Tag, z.Uhr = TagText(t, jetzt), lokal.Format("15:04")
			stunde := time.Date(lokal.Year(), lokal.Month(), lokal.Day(), lokal.Hour(), 0, 0, 0, lokal.Location())
			z.Stunde = stunde.Format("15:04")
			if !vorige.IsZero() {
				if leer := int(vorige.Sub(stunde)/time.Hour) - 1; leer > 0 {
					z.Pause = fmt.Sprintf("%d Std. ohne Aktion", leer)
				}
			}
			vorige = stunde
		}
		z.Titel = z.Voll
		switch z.Art {
		case AktionPush, AktionForcePush, AktionMerge, AktionBranchNeu:
			if z.SHA != "" {
				z.Titel = strings.TrimPrefix(z.Voll+" · "+z.SHA, " · ")
			}
		}
		z.Juengste = len(out) == 0
		z.blob, z.paare = "", nil
		out = append(out, z)
	}
	return out
}

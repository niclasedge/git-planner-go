package appctx

// Konflikt je Datei loesen (konflikte-loesen, #75): ein Drei-Wege-Merge auf
// Zeilenebene und die Vorschau der drei Optionen meine, extern und mergen.
// Hier, nicht im Browser: die Oberflaeche zeichnet, was hier gerechnet wird.
//
// Der Merge folgt diff3: je Seite die Hunks gegenueber der Merge-Basis
// (Myers-Diff), Hunks beider Seiten, deren Bereiche in der Basis sich
// ueberschneiden, bilden eine Gruppe. Eine Gruppe nur einer
// Seite oder mit gleicher Fassung auf beiden Seiten loest sich von selbst,
// jede andere ist ein Konflikt-Hunk, fuer den meine oder extern gewaehlt
// werden muss. Eine fehlende Datei (nil) ist die Loeschung.

import (
	"fmt"
	"strings"
)

// Die drei Optionen je Datei und die zwei je Konflikt-Hunk.
const (
	WahlMeine  = "meine"
	WahlExtern = "extern"
	WahlMergen = "mergen"
)

// GeneriertHinweis steht an jeder generierten Konfliktdatei.
const GeneriertHinweis = "generiert – nach dem Lösen neu bauen"

// vorschauKontext ist die Zahl unveraenderter Zeilen, die eine Vorschau um
// jede Aenderung stehen laesst.
const vorschauKontext = 3

// diffGrenze deckelt die Editierdistanz des Myers-Diffs; darueber gilt der
// ganze Mittelteil als ersetzt. Das haelt Speicher und Zeit bei riesigen,
// voellig verschiedenen Dateien (etwa einem neu gebauten Bundle) im Rahmen.
const diffGrenze = 2000

// DateiStaende sind die drei Staende einer Konfliktdatei; nil heisst, die
// Datei fehlt auf dieser Seite.
type DateiStaende struct {
	Pfad   string
	Basis  *string
	Meine  *string
	Extern *string
}

// DiffZeile ist eine Zeile eines Diffs: gleich, hinzu, weg oder ausgelassen.
type DiffZeile struct {
	Art  string `json:"art"`
	Text string `json:"text"`
}

// MergeAbschnitt ist ein Stueck des Merge-Ergebnisses: gleich (steht fest)
// oder konflikt (ein Hunk, fuer den gewaehlt werden muss).
type MergeAbschnitt struct {
	Art             string
	Zeilen          []string
	Hunk            int
	Basis           []string
	Meine           []string
	Extern          []string
	MeineGeloescht  bool
	ExternGeloescht bool
}

// DreiWege ist das Ergebnis von DreiWegeMerge.
type DreiWege struct {
	Abschnitte []MergeAbschnitt
	// Hunks ist die Zahl der Konflikt-Hunks.
	Hunks int
	// geloescht: das Ergebnis ohne Konflikt ist die Loeschung.
	geloescht bool
	umbruch   bool
	// ganzeDatei: ein Hunk ueber die ganze Datei (Loeschen gegen Aendern).
	ganzeDatei                  bool
	meineUmbruch, externUmbruch bool
}

// Zeilen zerlegt einen Text in Zeilen; ein Umbruch am Ende ergibt keine
// leere letzte Zeile.
func Zeilen(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func zeilenOder(s *string) []string {
	if s == nil {
		return []string{}
	}
	return Zeilen(*s)
}

func umbruchAmEnde(s *string) bool { return s != nil && strings.HasSuffix(*s, "\n") }

func zusammensetzen(zeilen []string, umbruch bool) *string {
	s := strings.Join(zeilen, "\n")
	if umbruch && len(zeilen) > 0 {
		s += "\n"
	}
	return &s
}

func gleicheZeilen(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------- Diff ----------

// Arten der Bearbeitungsschritte.
const (
	schrittGleich = 'g'
	schrittWeg    = 'w'
	schrittHinzu  = 'h'
)

// myers liefert die kuerzeste Folge von Schritten, die a in b ueberfuehrt;
// ok ist false, wenn die Editierdistanz ueber diffGrenze liegt.
func myers(a, b []string) (out []byte, ok bool) {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return ersetzen(n, m), true
	}
	max := n + m
	off := max
	v := make([]int, 2*max+2)
	var spur [][]int
	for d := 0; d <= max && d <= diffGrenze; d++ {
		schnapp := make([]int, 2*d+1)
		copy(schnapp, v[off-d:off+d+1])
		spur = append(spur, schnapp)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				return zurueckverfolgen(spur, n, m, d), true
			}
		}
	}
	return nil, false
}

// ersetzen: n Zeilen weg, dann m hinzu.
func ersetzen(n, m int) []byte {
	out := make([]byte, 0, n+m)
	for i := 0; i < n; i++ {
		out = append(out, schrittWeg)
	}
	for i := 0; i < m; i++ {
		out = append(out, schrittHinzu)
	}
	return out
}

func zurueckverfolgen(spur [][]int, n, m, dEnde int) []byte {
	var out []byte
	x, y := n, m
	for d := dEnde; d > 0; d-- {
		vv := spur[d]
		hol := func(k int) int { return vv[k+d] }
		k := x - y
		var vorK int
		if k == -d || (k != d && hol(k-1) < hol(k+1)) {
			vorK = k + 1
		} else {
			vorK = k - 1
		}
		vorX := hol(vorK)
		vorY := vorX - vorK
		for x > vorX && y > vorY {
			out = append(out, schrittGleich)
			x--
			y--
		}
		if x == vorX {
			out = append(out, schrittHinzu)
		} else {
			out = append(out, schrittWeg)
		}
		x, y = vorX, vorY
	}
	for x > 0 && y > 0 {
		out = append(out, schrittGleich)
		x--
		y--
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// schritte ist myers mit abgeschnittenem gemeinsamem Anfang und Ende und
// der Grenze: darueber wird der Mittelteil ersetzt.
func schritte(a, b []string) []byte {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	ma, mb := a[p:len(a)-s], b[p:len(b)-s]
	mitte := myersBegrenzt(ma, mb)
	out := make([]byte, 0, p+len(mitte)+s)
	for i := 0; i < p; i++ {
		out = append(out, schrittGleich)
	}
	out = append(out, mitte...)
	for i := 0; i < s; i++ {
		out = append(out, schrittGleich)
	}
	return out
}

func myersBegrenzt(a, b []string) []byte {
	if r, ok := myers(a, b); ok {
		return r
	}
	return ersetzen(len(a), len(b))
}

// ZeilenDiff ist der Diff von alt nach neu. Innerhalb einer geaenderten
// Strecke steht weg vor hinzu.
func ZeilenDiff(alt, neu []string) []DiffZeile {
	out := []DiffZeile{}
	i, j := 0, 0
	var weg, hinzu []DiffZeile
	leeren := func() {
		out = append(out, weg...)
		out = append(out, hinzu...)
		weg, hinzu = nil, nil
	}
	for _, s := range schritte(alt, neu) {
		switch s {
		case schrittGleich:
			leeren()
			out = append(out, DiffZeile{Art: "gleich", Text: alt[i]})
			i++
			j++
		case schrittWeg:
			weg = append(weg, DiffZeile{Art: "weg", Text: alt[i]})
			i++
		case schrittHinzu:
			hinzu = append(hinzu, DiffZeile{Art: "hinzu", Text: neu[j]})
			j++
		}
	}
	leeren()
	return out
}

func ausgelassen(n int) DiffZeile {
	return DiffZeile{Art: "ausgelassen", Text: fmt.Sprintf("… %d Zeilen unverändert", n)}
}

// kuerzeStrecke kuerzt eine Strecke unveraenderter Zeilen. vorne: vor ihr
// steht nichts (nur ihr Ende ist Kontext); hinten: nach ihr steht nichts.
func kuerzeStrecke(zeilen []DiffZeile, vorne, hinten bool, k int) []DiffZeile {
	n := len(zeilen)
	switch {
	case vorne && !hinten:
		if n > k {
			return append([]DiffZeile{ausgelassen(n - k)}, zeilen[n-k:]...)
		}
	case hinten && !vorne:
		if n > k {
			return append(append([]DiffZeile{}, zeilen[:k]...), ausgelassen(n-k))
		}
	default:
		if n > 2*k+1 {
			out := append([]DiffZeile{}, zeilen[:k]...)
			out = append(out, ausgelassen(n-2*k))
			return append(out, zeilen[n-k:]...)
		}
	}
	return zeilen
}

// DiffKuerzen laesst um jede Aenderung k unveraenderte Zeilen stehen und
// ersetzt den Rest jeder Strecke durch eine Zeile "ausgelassen".
func DiffKuerzen(d []DiffZeile, k int) []DiffZeile {
	out := []DiffZeile{}
	for i := 0; i < len(d); {
		if d[i].Art != "gleich" {
			out = append(out, d[i])
			i++
			continue
		}
		j := i
		for j < len(d) && d[j].Art == "gleich" {
			j++
		}
		out = append(out, kuerzeStrecke(d[i:j], i == 0, j == len(d), k)...)
		i = j
	}
	return out
}

// ---------- Drei-Wege-Merge ----------

type hunk struct {
	von, bis int // Bereich in der Basis
	zeilen   []string
}

func hunksVon(basis, seite []string) []hunk {
	var out []hunk
	i, j := 0, 0
	var akt *hunk
	for _, s := range schritte(basis, seite) {
		switch s {
		case schrittGleich:
			if akt != nil {
				out = append(out, *akt)
				akt = nil
			}
			i++
			j++
		case schrittWeg:
			if akt == nil {
				akt = &hunk{von: i, bis: i}
			}
			i++
			akt.bis = i
		case schrittHinzu:
			if akt == nil {
				akt = &hunk{von: i, bis: i}
			}
			akt.zeilen = append(akt.zeilen, seite[j])
			j++
		}
	}
	if akt != nil {
		out = append(out, *akt)
	}
	return out
}

// vorher ordnet Hunks nach Beginn in der Basis; bei gleichem Beginn kommt
// die reine Einfuegung zuerst.
func vorher(a, b hunk) bool {
	return a.von < b.von || (a.von == b.von && a.bis <= b.bis)
}

// ueberlappt sagt, ob h den Basisbereich [von, bis) einer Gruppe
// ueberschneidet. Eine Einfuegung (leerer Bereich) ueberschneidet einen
// Bereich nur echt innen, eine andere Einfuegung nur an derselben Stelle.
// Aenderungen, die sich nur beruehren, ueberschneiden sich nicht.
func ueberlappt(h hunk, von, bis int) bool {
	switch {
	case h.von == h.bis && von == bis:
		return h.von == von
	case h.von == h.bis:
		return von < h.von && h.von < bis
	case von == bis:
		return h.von < von && von < h.bis
	}
	return h.von < bis && von < h.bis
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// anwenden setzt die Hunks einer Seite in den Basisbereich [von, bis).
func anwenden(basis []string, hs []hunk, von, bis int) []string {
	out := []string{}
	p := von
	for _, h := range hs {
		out = append(out, basis[p:h.von]...)
		out = append(out, h.zeilen...)
		p = h.bis
	}
	return append(out, basis[p:bis]...)
}

// DreiWegeMerge fuehrt meine und extern gegen die Merge-Basis zusammen.
func DreiWegeMerge(basis, meine, extern *string) DreiWege {
	d := DreiWege{meineUmbruch: umbruchAmEnde(meine), externUmbruch: umbruchAmEnde(extern)}
	b, m, e := zeilenOder(basis), zeilenOder(meine), zeilenOder(extern)
	fb := umbruchAmEnde(basis)
	switch {
	case d.meineUmbruch == d.externUmbruch:
		d.umbruch = d.meineUmbruch
	case d.meineUmbruch != fb:
		d.umbruch = d.meineUmbruch
	default:
		d.umbruch = d.externUmbruch
	}

	// Loeschungen: eine fehlende Seite gegen eine unveraenderte ist die
	// Loeschung, gegen eine geaenderte ein Hunk ueber die ganze Datei.
	if meine == nil || extern == nil {
		unveraendert := func(s *string) bool {
			return s == nil || (basis != nil && *s == *basis)
		}
		if (meine == nil && unveraendert(extern)) || (extern == nil && unveraendert(meine)) {
			d.geloescht = true
			return d
		}
		d.ganzeDatei = true
		d.Hunks = 1
		d.Abschnitte = []MergeAbschnitt{{Art: "konflikt", Hunk: 0, Basis: b, Meine: m, Extern: e,
			MeineGeloescht: meine == nil, ExternGeloescht: extern == nil}}
		return d
	}

	hm, he := hunksVon(b, m), hunksVon(b, e)
	gleich := []string{}
	anhaengen := func(z []string) { gleich = append(gleich, z...) }
	abschliessen := func() {
		if len(gleich) > 0 {
			d.Abschnitte = append(d.Abschnitte, MergeAbschnitt{Art: "gleich", Zeilen: gleich})
			gleich = []string{}
		}
	}
	pos, i, j := 0, 0, 0
	for i < len(hm) || j < len(he) {
		var gm, ge []hunk
		var von, bis int
		if j >= len(he) || (i < len(hm) && vorher(hm[i], he[j])) {
			von, bis = hm[i].von, hm[i].bis
			gm = append(gm, hm[i])
			i++
		} else {
			von, bis = he[j].von, he[j].bis
			ge = append(ge, he[j])
			j++
		}
		for {
			weiter := false
			if i < len(hm) && ueberlappt(hm[i], von, bis) {
				gm = append(gm, hm[i])
				bis = maxInt(bis, hm[i].bis)
				i++
				weiter = true
			}
			if j < len(he) && ueberlappt(he[j], von, bis) {
				ge = append(ge, he[j])
				bis = maxInt(bis, he[j].bis)
				j++
				weiter = true
			}
			if !weiter {
				break
			}
		}
		anhaengen(b[pos:von])
		pos = bis
		switch {
		case len(ge) == 0:
			anhaengen(anwenden(b, gm, von, bis))
		case len(gm) == 0:
			anhaengen(anwenden(b, ge, von, bis))
		default:
			vm, ve := anwenden(b, gm, von, bis), anwenden(b, ge, von, bis)
			if gleicheZeilen(vm, ve) {
				anhaengen(vm)
				continue
			}
			abschliessen()
			d.Abschnitte = append(d.Abschnitte, MergeAbschnitt{Art: "konflikt", Hunk: d.Hunks,
				Basis: append([]string{}, b[von:bis]...), Meine: vm, Extern: ve})
			d.Hunks++
		}
	}
	anhaengen(b[pos:])
	abschliessen()
	return d
}

// Ergebnis bildet den Inhalt nach der Wahl je Konflikt-Hunk (nil = die
// Datei wird geloescht). Ohne vollstaendige Wahl gibt es kein Ergebnis.
func (d DreiWege) Ergebnis(wahl []string) (*string, error) {
	if len(wahl) != d.Hunks {
		return nil, fmt.Errorf("%d Konflikt-Hunks, aber %d gewaehlt", d.Hunks, len(wahl))
	}
	for i, w := range wahl {
		if w != WahlMeine && w != WahlExtern {
			return nil, fmt.Errorf("Hunk %d: Wahl %q (meine oder extern)", i, w)
		}
	}
	if d.geloescht {
		return nil, nil
	}
	if d.ganzeDatei {
		a := d.Abschnitte[0]
		if wahl[0] == WahlMeine {
			if a.MeineGeloescht {
				return nil, nil
			}
			return zusammensetzen(a.Meine, d.meineUmbruch), nil
		}
		if a.ExternGeloescht {
			return nil, nil
		}
		return zusammensetzen(a.Extern, d.externUmbruch), nil
	}
	out := []string{}
	for _, a := range d.Abschnitte {
		switch {
		case a.Art == "gleich":
			out = append(out, a.Zeilen...)
		case wahl[a.Hunk] == WahlMeine:
			out = append(out, a.Meine...)
		default:
			out = append(out, a.Extern...)
		}
	}
	return zusammensetzen(out, d.umbruch), nil
}

// ---------- Vorschau ----------

// OptionVorschau ist die Vorschau von meine oder extern: das Ergebnis ist
// die eine Seite, der Diff zeigt, was von der anderen (Gegen) verloren ginge.
type OptionVorschau struct {
	Geloescht bool        `json:"geloescht"`
	Gegen     string      `json:"gegen"`
	Diff      []DiffZeile `json:"diff"`
}

// VorschauAbschnitt ist ein Abschnitt der Merge-Vorschau: gleich (gekuerzte
// Zeilen) oder konflikt (ein Hunk mit beiden Fassungen).
type VorschauAbschnitt struct {
	Art             string      `json:"art"`
	Zeilen          []DiffZeile `json:"zeilen,omitempty"`
	Hunk            int         `json:"hunk"`
	Basis           []string    `json:"basis,omitempty"`
	Meine           []string    `json:"meine,omitempty"`
	Extern          []string    `json:"extern,omitempty"`
	MeineGeloescht  bool        `json:"meine_geloescht,omitempty"`
	ExternGeloescht bool        `json:"extern_geloescht,omitempty"`
}

// MergeVorschau ist die Vorschau von mergen.
type MergeVorschau struct {
	Konfliktfrei bool                `json:"konfliktfrei"`
	Geloescht    bool                `json:"geloescht"`
	Hunks        int                 `json:"hunks"`
	Abschnitte   []VorschauAbschnitt `json:"abschnitte"`
	GegenMeine   []DiffZeile         `json:"gegen_meine,omitempty"`
	GegenExtern  []DiffZeile         `json:"gegen_extern,omitempty"`
}

// KonfliktVorschau ist die Vorschau aller drei Optionen einer Datei.
type KonfliktVorschau struct {
	Pfad      string         `json:"pfad"`
	Generiert bool           `json:"generiert"`
	Hinweis   string         `json:"hinweis,omitempty"`
	Meine     OptionVorschau `json:"meine"`
	Extern    OptionVorschau `json:"extern"`
	Mergen    MergeVorschau  `json:"mergen"`
}

// GenerierteDatei sagt, ob pfad beim Bauen entsteht (das Svelte-Bundle).
func GenerierteDatei(pfad string) bool {
	return strings.HasPrefix(pfad, "internal/assets/static/app/")
}

func alsGleich(zeilen []string) []DiffZeile {
	out := make([]DiffZeile, 0, len(zeilen))
	for _, z := range zeilen {
		out = append(out, DiffZeile{Art: "gleich", Text: z})
	}
	return out
}

// KonfliktVorschauRechnen rechnet die Vorschau der drei Optionen.
func KonfliktVorschauRechnen(st DateiStaende) KonfliktVorschau {
	m, e := zeilenOder(st.Meine), zeilenOder(st.Extern)
	v := KonfliktVorschau{
		Pfad:   st.Pfad,
		Meine:  OptionVorschau{Geloescht: st.Meine == nil, Gegen: WahlExtern, Diff: DiffKuerzen(ZeilenDiff(e, m), vorschauKontext)},
		Extern: OptionVorschau{Geloescht: st.Extern == nil, Gegen: WahlMeine, Diff: DiffKuerzen(ZeilenDiff(m, e), vorschauKontext)},
	}
	if GenerierteDatei(st.Pfad) {
		v.Generiert, v.Hinweis = true, GeneriertHinweis
	}
	d := DreiWegeMerge(st.Basis, st.Meine, st.Extern)
	mv := MergeVorschau{Konfliktfrei: d.Hunks == 0, Hunks: d.Hunks, Abschnitte: []VorschauAbschnitt{}}
	for i, a := range d.Abschnitte {
		if a.Art == "gleich" {
			mv.Abschnitte = append(mv.Abschnitte, VorschauAbschnitt{Art: "gleich",
				Zeilen: kuerzeStrecke(alsGleich(a.Zeilen), i == 0, i == len(d.Abschnitte)-1, vorschauKontext)})
			continue
		}
		mv.Abschnitte = append(mv.Abschnitte, VorschauAbschnitt{Art: "konflikt", Hunk: a.Hunk, Basis: a.Basis,
			Meine: a.Meine, Extern: a.Extern, MeineGeloescht: a.MeineGeloescht, ExternGeloescht: a.ExternGeloescht})
	}
	if mv.Konfliktfrei {
		erg, _ := d.Ergebnis(nil)
		mv.Geloescht = erg == nil
		r := zeilenOder(erg)
		mv.GegenMeine = DiffKuerzen(ZeilenDiff(m, r), vorschauKontext)
		mv.GegenExtern = DiffKuerzen(ZeilenDiff(e, r), vorschauKontext)
	}
	v.Mergen = mv
	return v
}

// KonfliktLoesung ist der Inhalt einer Datei nach der Entscheidung (nil =
// loeschen). Bei mergen muss hunks je Konflikt-Hunk meine oder extern sein.
func KonfliktLoesung(st DateiStaende, wahl string, hunks []string) (*string, error) {
	switch wahl {
	case WahlMeine:
		return st.Meine, nil
	case WahlExtern:
		return st.Extern, nil
	case WahlMergen:
		return DreiWegeMerge(st.Basis, st.Meine, st.Extern).Ergebnis(hunks)
	}
	return nil, fmt.Errorf("unbekannte Wahl %q (meine, extern oder mergen)", wahl)
}

package ghrest

// Konflikt je Datei loesen (konflikte-loesen, #75): die drei Staende einer
// Konfliktdatei ueber die Contents-API, je Seite der letzte Commit auf die
// Datei, und der Merge-Commit ueber die Git-Data-API (Blobs, Baum mit
// base_tree, Commit mit zwei Eltern, Ref vorschieben ohne force). Kein
// lokaler Klon, kein GraphQL.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ErrKopfVeraltet heisst: der Branch steht nicht mehr auf dem Kopf, den der
// Aufrufer gesehen hat, oder GitHub lehnt das Vorschieben ab.
var ErrKopfVeraltet = errors.New("der Branch steht nicht mehr auf dem gesehenen Kopf")

// GeaenderteDatei ist eine Datei aus compare/…: Pfad, Status (added,
// modified, removed, renamed …), Blob-sha auf der Kopfseite und bei renamed
// der alte Pfad.
type GeaenderteDatei struct {
	Pfad   string
	Status string
	SHA    string
	Vorher string
}

// KonfliktLage ist, was das Loesen eines PR-Konflikts braucht.
type KonfliktLage struct {
	Nummer     int
	Branch     string
	Base       string
	HeadSHA    string
	BaseSHA    string
	MergeBasis string
	// Dateien sind die Konfliktdateien (beide Seiten geaendert).
	Dateien []string
	// PRDateien sind die Dateien, die der PR seit der Merge-Basis geaendert hat.
	PRDateien []GeaenderteDatei
}

// DreiStaende sind die Inhalte einer Datei an Merge-Basis, PR-Kopf und
// Base-Kopf (nil = fehlt) samt dem letzten Commit auf die Datei je Seite.
type DreiStaende struct {
	Pfad                                   string
	Basis, Meine, Extern                   *string
	BasisCommit, MeineCommit, ExternCommit CommitKopf
}

func nichtGefunden(err error) bool {
	return HTTPStatus(err) == http.StatusNotFound
}

// gueltigerPfad laesst nur relative Pfade ohne ".." zu.
func gueltigerPfad(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, t := range strings.Split(p, "/") {
		if t == "" || t == "." || t == ".." {
			return false
		}
	}
	return true
}

func pfadEscape(p string) string {
	teile := strings.Split(p, "/")
	for i, t := range teile {
		teile[i] = url.PathEscape(t)
	}
	return strings.Join(teile, "/")
}

// Inhalt liefert den Inhalt einer Datei an ref ueber die Contents-API; nil
// ohne Fehler, wenn es die Datei dort nicht gibt.
func (c Client) Inhalt(ctx context.Context, nwo, pfad, ref string) (*string, error) {
	if !GueltigesNWO(nwo) || !gueltigerPfad(pfad) || !shaRe.MatchString(ref) {
		return nil, fmt.Errorf("ungueltige Datei %q@%q in %q", pfad, ref, nwo)
	}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/contents/"+pfadEscape(pfad)+"?ref="+url.QueryEscape(ref))
	if nichtGefunden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var roh struct {
		Typ      string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Size     int64  `json:"size"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbarer Inhalt %s: %v", pfad, err)
	}
	if roh.Typ != "" && roh.Typ != "file" {
		return nil, fmt.Errorf("%s ist keine Datei (%s)", pfad, roh.Typ)
	}
	var inhalt []byte
	switch {
	case roh.Encoding == "base64" && roh.Content != "":
		inhalt, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(roh.Content, "\n", ""))
		if err != nil {
			return nil, fmt.Errorf("GitHub: Inhalt %s nicht dekodierbar: %v", pfad, err)
		}
	case roh.Size > 0 && roh.SHA != "":
		// Ueber 1 MB liefert die Contents-API keinen Inhalt, nur den Blob.
		if inhalt, err = c.Blob(ctx, nwo, roh.SHA); err != nil {
			return nil, err
		}
	default:
		inhalt = []byte(roh.Content)
	}
	s := string(inhalt)
	return &s, nil
}

// LetzterCommit liefert den letzten Commit auf pfad, von ref aus gesehen
// (commits?path=&sha=&per_page=1). Ohne Commit ist das Ergebnis leer.
func (c Client) LetzterCommit(ctx context.Context, nwo, pfad, ref string) (CommitKopf, error) {
	if !GueltigesNWO(nwo) || !gueltigerPfad(pfad) || !shaRe.MatchString(ref) {
		return CommitKopf{}, fmt.Errorf("ungueltige Datei %q@%q in %q", pfad, ref, nwo)
	}
	q := url.Values{"path": {pfad}, "sha": {ref}, "per_page": {"1"}}
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/commits?"+q.Encode())
	if err != nil {
		return CommitKopf{}, err
	}
	var roh []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Date string `json:"date"`
			} `json:"author"`
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return CommitKopf{}, fmt.Errorf("GitHub: unlesbare Commits: %v", err)
	}
	if len(roh) == 0 {
		return CommitKopf{}, nil
	}
	r := roh[0]
	k := CommitKopf{SHA: r.SHA, Zeit: r.Commit.Committer.Date, Betreff: ErsteZeile(r.Commit.Message)}
	if k.Zeit == "" {
		k.Zeit = r.Commit.Author.Date
	}
	return k, nil
}

// refSHA liefert den Commit, auf dem ein Branch steht.
func (c Client) refSHA(ctx context.Context, nwo, branch string) (string, error) {
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/git/ref/heads/"+pfadEscape(branch))
	if err != nil {
		return "", err
	}
	var roh struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(b, &roh); err != nil {
		return "", fmt.Errorf("GitHub: unlesbarer Ref: %v", err)
	}
	return roh.Object.SHA, nil
}

// KonfliktLageHolen rechnet den Zustand des PRs (wie PRZustandHolen) und
// sammelt Koepfe, Merge-Basis, Konfliktdateien und die Dateien des PRs.
func (c Client) KonfliktLageHolen(ctx context.Context, nwo string, p *PR) (KonfliktLage, error) {
	p.Abgleich = PRZustand(p.Mergebar, p.MergeStatus, 0)
	kopf := p.HeadSHA
	if kopf == "" {
		kopf = p.Head
	}
	v, err := c.vergleichMitDateien(ctx, nwo, p.Base, kopf)
	if err != nil {
		return KonfliktLage{}, err
	}
	p.Voraus, p.Zurueck = v.Voraus, v.Zurueck
	p.Abgleich = PRZustand(p.Mergebar, p.MergeStatus, v.Zurueck)
	lage := KonfliktLage{Nummer: p.Nummer, Branch: p.Head, Base: p.Base, HeadSHA: p.HeadSHA, BaseSHA: v.BaseSHA,
		MergeBasis: v.MergeBasis, Dateien: []string{}, PRDateien: v.Details}
	if lage.BaseSHA == "" && p.Base != "" {
		if lage.BaseSHA, err = c.refSHA(ctx, nwo, p.Base); err != nil {
			return KonfliktLage{}, err
		}
	}
	if p.Abgleich != PRKonflikt || v.MergeBasis == "" {
		return lage, nil
	}
	gegen, err := c.vergleichMitDateien(ctx, nwo, v.MergeBasis, p.Base)
	if err != nil {
		return KonfliktLage{}, err
	}
	lage.Dateien = KonfliktDateien(v.Dateien, gegen.Dateien)
	p.KonfliktDateien = lage.Dateien
	return lage, nil
}

// DreiStaendeHolen liest eine Datei an Merge-Basis, PR-Kopf und Base-Kopf.
func (c Client) DreiStaendeHolen(ctx context.Context, nwo string, lage KonfliktLage, pfad string) (DreiStaende, error) {
	st := DreiStaende{Pfad: pfad}
	var err error
	for _, s := range []struct {
		ref    string
		inhalt **string
		commit *CommitKopf
	}{
		{lage.MergeBasis, &st.Basis, &st.BasisCommit},
		{lage.HeadSHA, &st.Meine, &st.MeineCommit},
		{lage.BaseSHA, &st.Extern, &st.ExternCommit},
	} {
		if *s.inhalt, err = c.Inhalt(ctx, nwo, pfad, s.ref); err != nil {
			return DreiStaende{}, err
		}
		if *s.commit, err = c.LetzterCommit(ctx, nwo, pfad, s.ref); err != nil {
			return DreiStaende{}, err
		}
	}
	return st, nil
}

type baumZeile struct {
	Path string  `json:"path"`
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	SHA  *string `json:"sha"`
}

func (c Client) shaAntwort(ctx context.Context, methode, adresse string, rumpf any) (string, error) {
	b, _, err := c.anfrage(ctx, methode, adresse, rumpf)
	if err != nil {
		return "", err
	}
	var roh struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(b, &roh); err != nil || roh.SHA == "" {
		return "", fmt.Errorf("GitHub: Antwort ohne sha von %s", strings.TrimPrefix(adresse, Basis()))
	}
	return roh.SHA, nil
}

// MergeCommitSchreiben legt einen Merge-Commit mit den Eltern PR-Kopf und
// Base-Kopf an und schiebt den Branch ohne force darauf. Der Baum ist der
// Base-Baum mit den Aenderungen des PRs seit der Merge-Basis und den
// geloesten Dateien (nil = loeschen). Steht der Branch nicht mehr auf
// lage.HeadSHA, ist der Fehler ErrKopfVeraltet.
func (c Client) MergeCommitSchreiben(ctx context.Context, nwo string, lage KonfliktLage, geloest map[string]*string, nachricht string) (string, error) {
	if !GueltigesNWO(nwo) || !shaRe.MatchString(lage.HeadSHA) || !shaRe.MatchString(lage.BaseSHA) || !gueltigerPfad(lage.Branch) {
		return "", fmt.Errorf("unvollstaendige Konfliktlage fuer %s", nwo)
	}
	for p := range geloest {
		if !gueltigerPfad(p) {
			return "", fmt.Errorf("ungueltiger Pfad %q", p)
		}
	}
	basis := Basis() + "/repos/" + nwo
	b, _, err := c.holen(ctx, basis+"/git/commits/"+url.PathEscape(lage.BaseSHA))
	if err != nil {
		return "", err
	}
	var baseCommit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(b, &baseCommit); err != nil || baseCommit.Tree.SHA == "" {
		return "", fmt.Errorf("GitHub: Base-Commit %s ohne Baum", lage.BaseSHA)
	}

	// Modi aus dem Kopf-Baum; fehlt er, gilt 100644.
	modus := map[string]string{}
	if baum, err := c.Baum(ctx, nwo, lage.HeadSHA); err == nil {
		for _, e := range baum {
			if e.Typ == "blob" && e.Modus != "" {
				modus[e.Pfad] = e.Modus
			}
		}
	}
	modusVon := func(p string) string {
		if m := modus[p]; m != "" {
			return m
		}
		return "100644"
	}
	zeilen := map[string]baumZeile{}
	loeschen := func(p string) { zeilen[p] = baumZeile{Path: p, Mode: modusVon(p), Type: "blob"} }
	setzen := func(p, sha string) { zeilen[p] = baumZeile{Path: p, Mode: modusVon(p), Type: "blob", SHA: &sha} }

	for _, f := range lage.PRDateien {
		if _, ok := geloest[f.Pfad]; ok {
			continue
		}
		switch f.Status {
		case "removed":
			loeschen(f.Pfad)
			continue
		case "renamed":
			if _, ok := geloest[f.Vorher]; f.Vorher != "" && !ok {
				loeschen(f.Vorher)
			}
		}
		if f.SHA == "" {
			return "", fmt.Errorf("GitHub: %s ohne Blob im Vergleich", f.Pfad)
		}
		setzen(f.Pfad, f.SHA)
	}
	pfade := make([]string, 0, len(geloest))
	for p := range geloest {
		pfade = append(pfade, p)
	}
	sort.Strings(pfade)
	// Eine geloeste Loeschung, die es im Base-Baum gar nicht gibt, ist dort
	// schon erledigt. Laesst sich der Base-Baum nicht lesen, wird geloescht.
	var imBase map[string]bool
	for _, p := range pfade {
		if geloest[p] != nil || imBase != nil {
			continue
		}
		imBase = map[string]bool{}
		if baum, err := c.Baum(ctx, nwo, baseCommit.Tree.SHA); err == nil {
			for _, e := range baum {
				imBase[e.Pfad] = true
			}
		} else {
			imBase = nil
			break
		}
	}
	for _, p := range pfade {
		inhalt := geloest[p]
		if inhalt == nil {
			if imBase == nil || imBase[p] {
				loeschen(p)
			} else {
				delete(zeilen, p)
			}
			continue
		}
		sha, err := c.shaAntwort(ctx, http.MethodPost, basis+"/git/blobs",
			map[string]any{"content": base64.StdEncoding.EncodeToString([]byte(*inhalt)), "encoding": "base64"})
		if err != nil {
			return "", err
		}
		setzen(p, sha)
	}
	baum := make([]baumZeile, 0, len(zeilen))
	for _, z := range zeilen {
		baum = append(baum, z)
	}
	sort.Slice(baum, func(i, j int) bool { return baum[i].Path < baum[j].Path })

	baumSHA, err := c.shaAntwort(ctx, http.MethodPost, basis+"/git/trees", map[string]any{"base_tree": baseCommit.Tree.SHA, "tree": baum})
	if err != nil {
		return "", err
	}
	commit, err := c.shaAntwort(ctx, http.MethodPost, basis+"/git/commits",
		map[string]any{"message": nachricht, "tree": baumSHA, "parents": []string{lage.HeadSHA, lage.BaseSHA}})
	if err != nil {
		return "", err
	}
	jetzt, err := c.refSHA(ctx, nwo, lage.Branch)
	if err != nil {
		return "", err
	}
	if jetzt != lage.HeadSHA {
		return "", fmt.Errorf("%w (%s steht auf %s, gesehen %s)", ErrKopfVeraltet, lage.Branch, kurz(jetzt), kurz(lage.HeadSHA))
	}
	_, _, err = c.anfrage(ctx, http.MethodPatch, basis+"/git/refs/heads/"+pfadEscape(lage.Branch), map[string]any{"sha": commit, "force": false})
	if err != nil {
		if st := HTTPStatus(err); st == http.StatusUnprocessableEntity || st == http.StatusConflict {
			return "", fmt.Errorf("%w: %v", ErrKopfVeraltet, err)
		}
		return "", err
	}
	return commit, nil
}

func kurz(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

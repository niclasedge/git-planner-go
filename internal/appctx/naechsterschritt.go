package appctx

// Der naechste Schritt je Change (naechster-schritt, #52): was jetzt zu tun
// ist, als Lage, Satz und Befehl — nachgebildet nach naechster_schritt und
// ablaufstufe in ops.py. Die Ableitung ist rein und steht hier, nicht in der
// Oberflaeche; die zeichnet nur.

import (
	"fmt"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

// Die Lagen eines Schritts.
const (
	SchrittTun    = "tun"
	SchrittWarten = "warten"
	SchrittRuhe   = "ruhe"
)

// Schritt ist der naechste Schritt eines Changes. Befehl ist leer, wenn der
// Schritt keinen Befehl hat (offene Aufgabe, Warten auf Review, archiviert).
type Schritt struct {
	Lage   string `json:"lage"`
	Text   string `json:"text"`
	Befehl string `json:"befehl,omitempty"`
}

func hatGate(gates []string, name string) bool {
	for _, g := range gates {
		if g == name {
			return true
		}
	}
	return false
}

// NaechsterSchritt leitet den Schritt ab. pr ist der Pull Request auf
// change/KENNUNG (nil: keiner bekannt), issueZu sagt, ob das Issue des
// Changes geschlossen ist. Die erste zutreffende Regel gewinnt.
func NaechsterSchritt(c *ChangeKurz, pr *ghrest.PR, issueZu bool) *Schritt {
	if c == nil {
		return nil
	}
	k := c.Kennung
	tun := func(text, befehl string) *Schritt { return &Schritt{Lage: SchrittTun, Text: text, Befehl: befehl} }
	switch {
	case c.Archiviert:
		return &Schritt{Lage: SchrittRuhe, Text: k + " ist archiviert. Nichts mehr zu tun."}
	case !hatGate(c.Gates, "gate1"):
		return tun(k+" braucht den Plan. Lassen Sie Gate 1 laufen.", "just gate1 "+k)
	case !hatGate(c.Gates, "gate2"):
		return tun("Der Plan von "+k+" steht. Jetzt fehlt der rote Test.", "just gate2 "+k)
	}
	for _, a := range c.Aufgaben {
		if !a.Erledigt {
			return tun("Als Nächstes in "+k+": "+strings.TrimSpace(a.Text), "")
		}
	}
	gemergt := pr != nil && pr.Gemergt
	offen := pr != nil && !pr.Gemergt && pr.Zustand != "closed"
	switch {
	case !hatGate(c.Gates, "fertig"):
		return tun("Alle Aufgaben in "+k+" sind erledigt. Schließen Sie ab.", "just fertig "+k)
	case gemergt || issueZu:
		return tun(k+" ist gemergt. Überführen Sie den Change ins Archiv.", "just archiv "+k)
	case offen:
		return &Schritt{Lage: SchrittWarten,
			Text: fmt.Sprintf("%s steht im Pull Request #%d und wartet auf Review und Merge.", k, pr.Nummer)}
	}
	return tun(k+" ist abgeschlossen. Stellen Sie den Pull Request.", "just change-pr "+k)
}

// prFuerChange sucht den Pull Request auf change/KENNUNG; ein offener geht
// einem geschlossenen vor.
func prFuerChange(prs []ghrest.PR, kennung string) *ghrest.PR {
	var treffer *ghrest.PR
	for i := range prs {
		if prs[i].Head != "change/"+kennung {
			continue
		}
		if !prs[i].Gemergt && prs[i].Zustand != "closed" {
			return &prs[i]
		}
		if treffer == nil {
			treffer = &prs[i]
		}
	}
	return treffer
}

// schritteSetzen traegt an jedem Change der Liste den Schritt ein.
func schritteSetzen(es []ArbeitsEintrag, prs []ghrest.PR, issueZu bool) {
	for i := range es {
		if c := es[i].Change; c != nil {
			c.Schritt = NaechsterSchritt(c, prFuerChange(prs, c.Kennung), issueZu)
		}
	}
}

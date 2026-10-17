package server

// Der Abgleich fuer die Detailspalte eines Changes (dateien-abgleich, #53):
// die Dateien seines Ordners auf dem Branch und auf dem Default-Branch und
// der Diff DEFAULT...change/KENNUNG aus der Compare-API. Geurteilt wird in
// appctx.ChangeAbgleichBauen.

import (
	"context"
	"fmt"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func (d *repoDienst) changeAbgleich(ctx context.Context, c ghrest.Client, nwo, kennung string, ort changeOrt,
	l *baumLeser, proposal, tasks string, specs []specText) appctx.ChangeAbgleich {
	e := appctx.ChangeAbgleichEingabe{Proposal: proposal, Tasks: tasks, Standard: ort.standard, Ref: ort.ref,
		Ast: ordnerDateien(l.dateien, ort.ordner)}
	for _, s := range specs {
		e.Capabilities = append(e.Capabilities, s.Capability)
	}
	if ort.ref != ort.standard && ort.haupt != "" {
		if b, err := d.baum(ctx, c, nwo, ort.haupt); err == nil {
			e.Haupt = ordnerDateien(neuerBaumLeser(ctx, d, c, nwo, b).dateien, ort.ordner)
		}
	}
	if strings.HasPrefix(ort.ref, "change/") && ort.standard != "" {
		dateien, err := c.VergleichDateien(ctx, nwo, ort.standard, ort.baum)
		if err != nil {
			e.DiffGrund = fmt.Sprintf("Vergleich %s...%s: %v", ort.standard, ort.ref, err)
		} else {
			e.Angefasst = dateien
		}
	} else {
		e.DiffGrund = fmt.Sprintf("kein Branch change/%s — gelesen aus %s, kein Diff zum Vergleichen", kennung, ort.ref)
	}
	return appctx.ChangeAbgleichBauen(e)
}

// ordnerDateien sind die Dateien unter ordner, relativ zu ihm, mit ihrem
// Blob-sha; nil, wenn der Ordner keine hat.
func ordnerDateien(dateien map[string]ghrest.BaumEintrag, ordner string) map[string]string {
	var out map[string]string
	for p, e := range dateien {
		if rel, ok := strings.CutPrefix(p, ordner+"/"); ok {
			if out == nil {
				out = map[string]string{}
			}
			out[rel] = e.SHA
		}
	}
	return out
}

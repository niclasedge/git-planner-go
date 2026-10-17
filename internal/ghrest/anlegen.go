package ghrest

// Branch und PR anlegen (pr-anlegen, #79): ein Branch zweigt vom Kopf eines
// anderen ab (POST git/refs), ein PR entsteht mit POST pulls. Vorher fragt
// OffenerPR, ob es fuer den Head schon einen offenen gibt. Alles REST.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrVorhanden heisst: den Branch oder den PR gibt es schon.
var ErrVorhanden = errors.New("gibt es schon")

// NeuerPR ist der Inhalt eines anzulegenden PRs.
type NeuerPR struct {
	Titel string
	Text  string
	Head  string
	Base  string
	Draft bool
}

func branchPlausibel(name string) bool {
	return name != "" && !strings.Contains(name, "..") && !strings.ContainsAny(name, " \t\n~^:?*[\\") &&
		!strings.HasPrefix(name, "/") && !strings.HasSuffix(name, "/")
}

// BranchAnlegen legt den Branch name auf dem Kopf von von an und liefert
// dessen sha. Gibt es den Branch schon, ist der Fehler ErrVorhanden.
func (c Client) BranchAnlegen(ctx context.Context, nwo, name, von string) (string, error) {
	if !GueltigesNWO(nwo) || !branchPlausibel(name) || !branchPlausibel(von) {
		return "", fmt.Errorf("ungueltiger Branch %q (von %q) in %q", name, von, nwo)
	}
	sha, err := c.refSHA(ctx, nwo, von)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", fmt.Errorf("GitHub: %s hat keinen Kopf", von)
	}
	_, _, err = c.anfrage(ctx, http.MethodPost, Basis()+"/repos/"+nwo+"/git/refs",
		map[string]any{"ref": "refs/heads/" + name, "sha": sha})
	if err != nil {
		if HTTPStatus(err) == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return "", fmt.Errorf("%w: Branch %s (%v)", ErrVorhanden, name, err)
		}
		return "", err
	}
	return sha, nil
}

// OffenerPR liefert den offenen PR mit head als Head-Branch im selben Repo,
// nil, wenn es keinen gibt.
func (c Client) OffenerPR(ctx context.Context, nwo, head string) (*PR, error) {
	if !GueltigesNWO(nwo) || !branchPlausibel(head) {
		return nil, fmt.Errorf("ungueltiger Head %q in %q", head, nwo)
	}
	owner := strings.SplitN(nwo, "/", 2)[0]
	b, _, err := c.holen(ctx, Basis()+"/repos/"+nwo+"/pulls?state=open&head="+url.QueryEscape(owner+":"+head)+"&per_page=10")
	if err != nil {
		return nil, err
	}
	var roh []rohPR
	if err := json.Unmarshal(b, &roh); err != nil {
		return nil, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	for _, r := range roh {
		if r.Head.Ref == head {
			p := r.pr()
			return &p, nil
		}
	}
	return nil, nil
}

// PRAnlegen legt einen PR an. Gibt es fuer den Head schon einen, ist der
// Fehler ErrVorhanden.
func (c Client) PRAnlegen(ctx context.Context, nwo string, n NeuerPR) (PR, error) {
	if !GueltigesNWO(nwo) || !branchPlausibel(n.Head) || !branchPlausibel(n.Base) || strings.TrimSpace(n.Titel) == "" {
		return PR{}, fmt.Errorf("ungueltiger PR %q -> %q in %q", n.Head, n.Base, nwo)
	}
	rumpf := map[string]any{"title": n.Titel, "body": n.Text, "head": n.Head, "base": n.Base, "draft": n.Draft}
	b, _, err := c.anfrage(ctx, http.MethodPost, Basis()+"/repos/"+nwo+"/pulls", rumpf)
	if err != nil {
		if HTTPStatus(err) == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return PR{}, fmt.Errorf("%w: PR fuer %s (%v)", ErrVorhanden, n.Head, err)
		}
		return PR{}, err
	}
	var r rohPR
	if err := json.Unmarshal(b, &r); err != nil {
		return PR{}, fmt.Errorf("GitHub: unlesbare Antwort: %v", err)
	}
	return r.pr(), nil
}

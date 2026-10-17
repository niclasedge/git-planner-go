package ghrest_test

// Branch und PR anlegen gegen eine Fake-GitHub-API (pr-anlegen, #79).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type anlegenAufruf struct {
	Methode, Pfad, Query string
	Rumpf                map[string]any
}

func anlegenGitHub(t *testing.T, antwort func(a anlegenAufruf, w http.ResponseWriter)) func() []anlegenAufruf {
	t.Helper()
	var mu sync.Mutex
	var alle []anlegenAufruf
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a := anlegenAufruf{Methode: r.Method, Pfad: r.URL.Path, Query: r.URL.RawQuery}
		if len(b) > 0 {
			_ = json.Unmarshal(b, &a.Rumpf)
		}
		mu.Lock()
		alle = append(alle, a)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		antwort(a, w)
	})
	return func() []anlegenAufruf {
		mu.Lock()
		defer mu.Unlock()
		return append([]anlegenAufruf{}, alle...)
	}
}

func TestBranchAnlegenVomStandard(t *testing.T) {
	alle := anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) {
		switch {
		case a.Methode == http.MethodGet && a.Pfad == "/repos/acme/alpha/git/ref/heads/main":
			fmt.Fprint(w, `{"ref":"refs/heads/main","object":{"sha":"shamain","type":"commit"}}`)
		case a.Methode == http.MethodPost && a.Pfad == "/repos/acme/alpha/git/refs":
			if a.Rumpf["ref"] == "refs/heads/change/vorhanden" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, `{"message":"Reference already exists"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"ref":%q,"object":{"sha":"shamain"}}`, a.Rumpf["ref"])
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "tok-anlegen"}
	sha, err := c.BranchAnlegen(context.Background(), "acme/alpha", "change/pr-anlegen", "main")
	if err != nil || sha != "shamain" {
		t.Fatalf("BranchAnlegen: %q %v", sha, err)
	}
	var post []anlegenAufruf
	for _, a := range alle() {
		if a.Methode == http.MethodPost {
			post = append(post, a)
		}
	}
	if len(post) != 1 || post[0].Rumpf["ref"] != "refs/heads/change/pr-anlegen" || post[0].Rumpf["sha"] != "shamain" {
		t.Errorf("POST git/refs %+v", post)
	}

	_, err = c.BranchAnlegen(context.Background(), "acme/alpha", "change/vorhanden", "main")
	if !errors.Is(err, ghrest.ErrVorhanden) {
		t.Errorf("vorhandener Branch: %v, erwartet ErrVorhanden", err)
	}
	if _, err := c.BranchAnlegen(context.Background(), "acme/alpha", "a..b", "main"); err == nil {
		t.Error("ungueltiger Name angenommen")
	}
}

func TestOffenerPRUndPRAnlegen(t *testing.T) {
	alle := anlegenGitHub(t, func(a anlegenAufruf, w http.ResponseWriter) {
		switch {
		case a.Methode == http.MethodGet && a.Pfad == "/repos/acme/alpha/pulls":
			if strings.Contains(a.Query, "head=acme%3Achange%2Fzehn") || strings.Contains(a.Query, "head=acme:change/zehn") {
				fmt.Fprint(w, `[{"number":10,"title":"Zehn","html_url":"u10","state":"open","head":{"ref":"change/zehn","sha":"s10","repo":{"full_name":"acme/alpha"}},"base":{"ref":"main"}}]`)
				return
			}
			fmt.Fprint(w, `[]`)
		case a.Methode == http.MethodPost && a.Pfad == "/repos/acme/alpha/pulls":
			if a.Rumpf["head"] == "change/doppelt" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, `{"message":"Validation Failed","errors":[{"message":"A pull request already exists for acme:change/doppelt."}]}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"number":90,"title":%q,"html_url":"https://github.com/acme/alpha/pull/90","draft":%v,"state":"open",
			  "head":{"ref":%q,"sha":"s90","repo":{"full_name":"acme/alpha"}},"base":{"ref":%q}}`, a.Rumpf["title"], a.Rumpf["draft"], a.Rumpf["head"], a.Rumpf["base"])
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	c := ghrest.Client{Token: "tok-anlegen"}
	ctx := context.Background()

	p, err := c.OffenerPR(ctx, "acme/alpha", "change/zehn")
	if err != nil || p == nil || p.Nummer != 10 {
		t.Fatalf("OffenerPR change/zehn: %+v %v", p, err)
	}
	if p, err := c.OffenerPR(ctx, "acme/alpha", "change/pr-anlegen"); err != nil || p != nil {
		t.Fatalf("OffenerPR ohne PR: %+v %v", p, err)
	}
	var abfrage string
	for _, a := range alle() {
		if a.Methode == http.MethodGet && a.Pfad == "/repos/acme/alpha/pulls" {
			abfrage = a.Query
		}
	}
	if !strings.Contains(abfrage, "state=open") || !strings.Contains(abfrage, "head=acme%3Achange%2Fpr-anlegen") {
		t.Errorf("Abfrage %q, erwartet state=open und head=acme:change/pr-anlegen", abfrage)
	}

	neu, err := c.PRAnlegen(ctx, "acme/alpha", ghrest.NeuerPR{Titel: "pr-anlegen: Anlegen (#79)", Text: "Closes #79",
		Head: "change/pr-anlegen", Base: "main", Draft: true})
	if err != nil || neu.Nummer != 90 || !neu.Draft || neu.URL == "" {
		t.Fatalf("PRAnlegen: %+v %v", neu, err)
	}
	var post []anlegenAufruf
	for _, a := range alle() {
		if a.Methode == http.MethodPost {
			post = append(post, a)
		}
	}
	if len(post) != 1 {
		t.Fatalf("POST %+v", post)
	}
	r := post[0].Rumpf
	if r["title"] != "pr-anlegen: Anlegen (#79)" || r["body"] != "Closes #79" || r["head"] != "change/pr-anlegen" || r["base"] != "main" || r["draft"] != true {
		t.Errorf("Rumpf %+v", r)
	}

	_, err = c.PRAnlegen(ctx, "acme/alpha", ghrest.NeuerPR{Titel: "x", Head: "change/doppelt", Base: "main"})
	if !errors.Is(err, ghrest.ErrVorhanden) {
		t.Errorf("doppelter PR: %v, erwartet ErrVorhanden", err)
	}
}

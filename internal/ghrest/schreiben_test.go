package ghrest_test

// Schreibaufrufe gegen eine Fake-GitHub-API (issue-verwaltung, #26).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

type aufruf struct {
	Methode, Pfad string
	Rumpf         map[string]any
}

func protokollGitHub(t *testing.T, antwort func(a aufruf, w http.ResponseWriter)) *[]aufruf {
	t.Helper()
	var alle []aufruf
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a := aufruf{Methode: r.Method, Pfad: r.URL.Path}
		if len(b) > 0 {
			_ = json.Unmarshal(b, &a.Rumpf)
		}
		alle = append(alle, a)
		w.Header().Set("Content-Type", "application/json")
		antwort(a, w)
	})
	return &alle
}

func TestIssueAnlegen(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"number":31,"title":"Neu","html_url":"https://github.com/acme/alpha/issues/31","id":3131}`)
	})
	i, err := ghrest.Client{Token: "t"}.IssueAnlegen(context.Background(), "acme/alpha",
		ghrest.NeuesIssue{Titel: "Neu", Text: "Text", Labels: []string{"bug"}, Zustaendig: []string{"bo"}})
	if err != nil || i.Nummer != 31 || i.URL == "" {
		t.Fatalf("IssueAnlegen %+v %v", i, err)
	}
	a := (*alle)[0]
	if a.Methode != http.MethodPost || a.Pfad != "/repos/acme/alpha/issues" || a.Rumpf["title"] != "Neu" || a.Rumpf["body"] != "Text" {
		t.Errorf("Aufruf %+v", a)
	}
	if fmt.Sprint(a.Rumpf["labels"]) != "[bug]" || fmt.Sprint(a.Rumpf["assignees"]) != "[bo]" {
		t.Errorf("labels/assignees %v %v", a.Rumpf["labels"], a.Rumpf["assignees"])
	}
}

func TestIssueAendernNurGesendeteFelder(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		fmt.Fprint(w, `{"number":4,"title":"vier","state":"closed"}`)
	})
	zu := "closed"
	if _, err := (ghrest.Client{Token: "t"}).IssueAendern(context.Background(), "acme/alpha", 4, ghrest.IssueAenderung{Zustand: &zu}); err != nil {
		t.Fatal(err)
	}
	labels := []string{"bug", "ui"}
	if _, err := (ghrest.Client{Token: "t"}).IssueAendern(context.Background(), "acme/alpha", 4, ghrest.IssueAenderung{Labels: &labels}); err != nil {
		t.Fatal(err)
	}
	a, b := (*alle)[0], (*alle)[1]
	if a.Methode != http.MethodPatch || a.Pfad != "/repos/acme/alpha/issues/4" {
		t.Errorf("Aufruf %+v", a)
	}
	if len(a.Rumpf) != 2 || a.Rumpf["state"] != "closed" || a.Rumpf["state_reason"] != "completed" {
		t.Errorf("Schliessen-Rumpf %v, erwartet genau state und state_reason", a.Rumpf)
	}
	if len(b.Rumpf) != 1 || fmt.Sprint(b.Rumpf["labels"]) != "[bug ui]" {
		t.Errorf("Labels-Rumpf %v", b.Rumpf)
	}
}

func TestKommentareUndKommentieren(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		if a.Methode == http.MethodGet {
			fmt.Fprint(w, `[{"user":{"login":"ana"},"body":"Erster","created_at":"2026-09-20T00:00:00Z","html_url":"u1"},
			 {"user":{"login":"bo"},"body":"Zweiter","created_at":"2026-09-21T00:00:00Z","html_url":"u2"}]`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"user":{"login":"ich"},"body":"Erledigt?","created_at":"2026-09-24T00:00:00Z"}`)
	})
	c := ghrest.Client{Token: "t"}
	ks, err := c.Kommentare(context.Background(), "acme/alpha", 4)
	if err != nil || len(ks) != 2 || ks[0].Autor != "ana" || ks[0].Text != "Erster" || ks[1].Zeit != "2026-09-21T00:00:00Z" {
		t.Fatalf("Kommentare %+v %v", ks, err)
	}
	k, err := c.Kommentieren(context.Background(), "acme/alpha", 4, "Erledigt?")
	if err != nil || k.Text != "Erledigt?" {
		t.Fatalf("Kommentieren %+v %v", k, err)
	}
	p := (*alle)[1]
	if p.Methode != http.MethodPost || p.Pfad != "/repos/acme/alpha/issues/4/comments" || p.Rumpf["body"] != "Erledigt?" {
		t.Errorf("Aufruf %+v", p)
	}
}

func TestBlockerSetzenUndLoesen(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch {
		case a.Methode == http.MethodGet && a.Pfad == "/repos/acme/alpha/issues/3":
			fmt.Fprint(w, `{"number":3,"id":303}`)
		case a.Methode == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		case a.Methode == http.MethodDelete:
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := ghrest.Client{Token: "t"}
	if err := c.BlockerSetzen(context.Background(), "acme/alpha", 2, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.BlockerLoesen(context.Background(), "acme/alpha", 2, 3); err != nil {
		t.Fatal(err)
	}
	var post, del *aufruf
	for i := range *alle {
		a := &(*alle)[i]
		if a.Methode == http.MethodPost {
			post = a
		}
		if a.Methode == http.MethodDelete {
			del = a
		}
	}
	if post == nil || post.Pfad != "/repos/acme/alpha/issues/2/dependencies/blocked_by" || fmt.Sprint(post.Rumpf["issue_id"]) != "303" {
		t.Errorf("POST %+v", post)
	}
	if del == nil || del.Pfad != "/repos/acme/alpha/issues/2/dependencies/blocked_by/303" {
		t.Errorf("DELETE %+v", del)
	}
	if err := c.BlockerSetzen(context.Background(), "acme/alpha", 2, 2); err == nil {
		t.Error("ein Issue blockiert sich selbst ohne Fehler")
	}
}

func TestLabelsUndZustaendige(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/labels":
			fmt.Fprint(w, `[{"name":"bug","color":"d73a4a"},{"name":"ui","color":"0e8a16"}]`)
		case "/repos/acme/alpha/assignees":
			fmt.Fprint(w, `[{"login":"ana"},{"login":"bo"}]`)
		}
	})
	c := ghrest.Client{Token: "t"}
	ls, err := c.Labels(context.Background(), "acme/alpha")
	if err != nil || len(ls) != 2 || ls[1].Name != "ui" || ls[1].Farbe != "0e8a16" {
		t.Errorf("Labels %+v %v", ls, err)
	}
	zs, err := c.MoeglicheZustaendige(context.Background(), "acme/alpha")
	if err != nil || strings.Join(zs, ",") != "ana,bo" {
		t.Errorf("Zustaendige %v %v", zs, err)
	}
}

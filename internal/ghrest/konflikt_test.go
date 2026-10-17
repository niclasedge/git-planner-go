package ghrest_test

// Konfliktzustand je PR und "Base in den Branch holen" (konflikte-anzeigen, #62).

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func wahr(b bool) *bool { return &b }

func TestPRZustandRegel(t *testing.T) {
	for _, fall := range []struct {
		mergebar *bool
		status   string
		zurueck  int
		soll     string
	}{
		{wahr(false), "dirty", 0, "konflikt"},
		{nil, "dirty", 4, "konflikt"},
		{wahr(false), "", 0, "konflikt"},
		{wahr(true), "behind", 3, "veraltet"},
		{wahr(true), "behind", 0, "veraltet"},
		{wahr(true), "clean", 2, "veraltet"},
		{nil, "unknown", 0, "unbekannt"},
		{nil, "", 0, "unbekannt"},
		{wahr(true), "clean", 0, "sauber"},
		{wahr(true), "blocked", 0, "sauber"},
		{wahr(true), "unstable", 0, "sauber"},
		{wahr(true), "draft", 0, "sauber"},
	} {
		if ist := ghrest.PRZustand(fall.mergebar, fall.status, fall.zurueck); ist != fall.soll {
			t.Errorf("PRZustand(%v, %q, %d) = %q, erwartet %q", fall.mergebar, fall.status, fall.zurueck, ist, fall.soll)
		}
	}
}

func TestKonfliktDateienSchnittmenge(t *testing.T) {
	ist := ghrest.KonfliktDateien([]string{"z.go", "a.go", "b.go", "b.go"}, []string{"c.go", "b.go", "z.go"})
	if soll := []string{"b.go", "z.go"}; !reflect.DeepEqual(ist, soll) {
		t.Errorf("KonfliktDateien = %v, erwartet %v", ist, soll)
	}
	if ist := ghrest.KonfliktDateien([]string{"a.go"}, []string{"b.go"}); len(ist) != 0 {
		t.Errorf("ohne Ueberschneidung %v", ist)
	}
}

func TestPRZustandHolenMitDateien(t *testing.T) {
	protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		switch a.Pfad {
		case "/repos/acme/alpha/compare/main...sha21":
			fmt.Fprint(w, `{"ahead_by":2,"behind_by":5,"merge_base_commit":{"sha":"basis21"},"files":[{"filename":"a.go"},{"filename":"b.go"}]}`)
		case "/repos/acme/alpha/compare/basis21...main":
			fmt.Fprint(w, `{"ahead_by":5,"behind_by":0,"merge_base_commit":{"sha":"basis21"},"files":[{"filename":"c.go"},{"filename":"b.go"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	p := ghrest.PR{Nummer: 21, HeadSHA: "sha21", Base: "main", Mergebar: wahr(false), MergeStatus: "dirty"}
	if err := (ghrest.Client{Token: "t"}).PRZustandHolen(context.Background(), "acme/alpha", &p); err != nil {
		t.Fatal(err)
	}
	if p.Abgleich != "konflikt" || p.Voraus != 2 || p.Zurueck != 5 || !reflect.DeepEqual(p.KonfliktDateien, []string{"b.go"}) {
		t.Errorf("PR %+v", p)
	}
}

func TestPRBaseHolen(t *testing.T) {
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"message":"Updating pull request branch."}`)
	})
	if err := (ghrest.Client{Token: "t"}).PRBaseHolen(context.Background(), "acme/alpha", 20, "sha20"); err != nil {
		t.Fatal(err)
	}
	if len(*alle) != 1 {
		t.Fatalf("Aufrufe %+v", *alle)
	}
	a := (*alle)[0]
	if a.Methode != http.MethodPut || a.Pfad != "/repos/acme/alpha/pulls/20/update-branch" || a.Rumpf["expected_head_sha"] != "sha20" {
		t.Errorf("Aufruf %+v", a)
	}
}

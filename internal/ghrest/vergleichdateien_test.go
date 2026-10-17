package ghrest_test

// Die Dateien eines Vergleichs BASIS...KOPF (dateien-abgleich, #53).

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func TestVergleichDateien(t *testing.T) {
	sha := "abcdef1234567890abcdef1234567890abcdef12"
	alle := protokollGitHub(t, func(a aufruf, w http.ResponseWriter) {
		if a.Pfad == "/repos/acme/alpha/compare/main..."+sha {
			fmt.Fprint(w, `{"ahead_by":2,"behind_by":0,"files":[
			  {"filename":"web/b.js","status":"modified"},{"filename":"a.go","status":"added"},{"filename":"a.go","status":"added"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	c := ghrest.Client{Token: "t"}
	d, err := c.VergleichDateien(context.Background(), "acme/alpha", "main", sha)
	if err != nil {
		t.Fatalf("VergleichDateien: %v (Aufrufe %+v)", err, *alle)
	}
	if !reflect.DeepEqual(d, []string{"a.go", "web/b.js"}) {
		t.Errorf("Dateien %q, erwartet sortiert und ohne Dopplung", d)
	}
	if _, err := c.VergleichDateien(context.Background(), "kein-repo", "main", sha); err == nil {
		t.Errorf("ungueltiges Repo ohne Fehler")
	}
	if _, err := c.VergleichDateien(context.Background(), "acme/alpha", "main", "fehlt"); err == nil {
		t.Errorf("404 ohne Fehler")
	}
}

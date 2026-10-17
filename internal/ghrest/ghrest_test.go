package ghrest_test

// REST-Abruf gegen eine Fake-GitHub-API (repo-leiste, #25). GKP_GITHUB_API
// lenkt den Client auf den Testserver; ohne Netz, ohne gh.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/ghrest"
)

func fakeGitHub(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("GKP_GITHUB_API", srv.URL)
	return srv
}

func TestReposFolgtDenSeiten(t *testing.T) {
	var srv *httptest.Server
	var auth []string
	srv = fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.URL.Path != "/user/repos" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[{"full_name":"acme/beta","name":"beta","owner":{"login":"acme"},"private":false,"open_issues_count":0,"html_url":"https://github.com/acme/beta"}]`)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?per_page=100&page=2>; rel="next", <%s/user/repos?per_page=100&page=2>; rel="last"`, srv.URL, srv.URL))
		fmt.Fprint(w, `[{"full_name":"acme/alpha","name":"alpha","owner":{"login":"acme"},"private":true,"archived":false,
		  "description":"Alpha-Dienst","language":"Go","default_branch":"main","pushed_at":"2026-09-23T10:00:00Z",
		  "open_issues_count":4,"html_url":"https://github.com/acme/alpha"}]`)
	})

	repos, err := ghrest.Client{Token: "tok-1"}.Repos(context.Background())
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(repos) != 2 || repos[0].NWO != "acme/alpha" || repos[1].NWO != "acme/beta" {
		t.Fatalf("Repos %+v, erwartet acme/alpha und acme/beta ueber zwei Seiten", repos)
	}
	a := repos[0]
	if a.Name != "alpha" || a.Owner != "acme" || !a.Privat || a.Beschreibung != "Alpha-Dienst" || a.Sprache != "Go" ||
		a.DefaultBranch != "main" || a.Gepusht != "2026-09-23T10:00:00Z" || a.OffeneIssues != 4 || a.URL != "https://github.com/acme/alpha" {
		t.Errorf("Felder von acme/alpha: %+v", a)
	}
	for _, h := range auth {
		if h != "Bearer tok-1" {
			t.Errorf("Authorization %q, erwartet Bearer tok-1", h)
		}
	}
}

func TestFehlerNenntStatusAberNieDenToken(t *testing.T) {
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials for geheim-123"}`)
	})
	_, err := ghrest.Client{Token: "geheim-123"}.Repos(context.Background())
	if err == nil {
		t.Fatal("kein Fehler bei 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Fehler %q nennt den Status nicht", err)
	}
	if strings.Contains(err.Error(), "geheim-123") {
		t.Errorf("Fehler %q enthaelt den Tokenwert", err)
	}
}

func TestOffeneIssuesMarkiertPRs(t *testing.T) {
	var pfad, state string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		pfad, state = r.URL.Path, r.URL.Query().Get("state")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
		 {"number":3,"title":"Drei","html_url":"https://github.com/acme/alpha/issues/3","user":{"login":"ana"},
		  "labels":[{"name":"bug","color":"d73a4a"}],"assignees":[{"login":"bo"}],"comments":2,"updated_at":"2026-09-20T08:00:00Z"},
		 {"number":5,"title":"Fuenf","html_url":"https://github.com/acme/alpha/pull/5","user":{"login":"cy"},
		  "labels":[],"assignees":[],"comments":0,"updated_at":"2026-09-21T08:00:00Z","pull_request":{"url":"x"}}
		]`)
	})
	alle, err := ghrest.Client{Token: "t"}.OffeneIssues(context.Background(), "acme/alpha")
	if err != nil {
		t.Fatalf("OffeneIssues: %v", err)
	}
	if pfad != "/repos/acme/alpha/issues" || state != "open" {
		t.Errorf("Anfrage %s state=%s, erwartet /repos/acme/alpha/issues state=open", pfad, state)
	}
	if len(alle) != 2 {
		t.Fatalf("%d Eintraege, erwartet 2", len(alle))
	}
	i := alle[0]
	if i.Nummer != 3 || i.Titel != "Drei" || i.Autor != "ana" || i.Kommentare != 2 || i.IstPR ||
		len(i.Labels) != 1 || i.Labels[0].Name != "bug" || i.Labels[0].Farbe != "d73a4a" ||
		len(i.Zustaendig) != 1 || i.Zustaendig[0] != "bo" || i.Aktualisiert != "2026-09-20T08:00:00Z" {
		t.Errorf("Issue 3: %+v", i)
	}
	if !alle[1].IstPR {
		t.Error("Eintrag 5 mit pull_request ist nicht als PR markiert")
	}
}

func TestUngueltigesRepoWirdNichtAngefragt(t *testing.T) {
	angefragt := false
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { angefragt = true })
	for _, nwo := range []string{"", "ohne-schraegstrich", "a/b/c", "../x"} {
		if _, err := (ghrest.Client{Token: "t"}).OffeneIssues(context.Background(), nwo); err == nil {
			t.Errorf("%q: kein Fehler", nwo)
		}
	}
	if angefragt {
		t.Error("ein ungueltiges owner/repo hat GitHub erreicht")
	}
}

func TestBranchesBaumUndBlob(t *testing.T) {
	blobAbrufe := 0
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/alpha/branches":
			fmt.Fprint(w, `[{"name":"main","commit":{"sha":"s-main"}},{"name":"change/foo","commit":{"sha":"s-foo"}}]`)
		case "/repos/acme/alpha/git/trees/s-main":
			if r.URL.Query().Get("recursive") != "1" {
				t.Errorf("Baum ohne recursive=1")
			}
			fmt.Fprint(w, `{"sha":"s-main","truncated":false,"tree":[
			 {"path":"openspec","type":"tree","sha":"t1"},
			 {"path":"openspec/changes/foo/tasks.md","type":"blob","sha":"b1","size":12}]}`)
		case "/repos/acme/alpha/git/blobs/b1":
			blobAbrufe++
			fmt.Fprint(w, `{"sha":"b1","encoding":"base64","content":"LSBbeF0gZWlucwo="}`)
		default:
			http.NotFound(w, r)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	bs, err := c.Branches(ctx, "acme/alpha")
	if err != nil || len(bs) != 2 || bs[1].Name != "change/foo" || bs[1].SHA != "s-foo" {
		t.Fatalf("Branches %+v %v", bs, err)
	}
	baum, err := c.Baum(ctx, "acme/alpha", "s-main")
	if err != nil || len(baum) != 2 {
		t.Fatalf("Baum %+v %v", baum, err)
	}
	if b := baum[1]; b.Pfad != "openspec/changes/foo/tasks.md" || b.Typ != "blob" || b.SHA != "b1" || b.Groesse != 12 {
		t.Errorf("Baumeintrag %+v", b)
	}
	inhalt, err := c.Blob(ctx, "acme/alpha", "b1")
	if err != nil || string(inhalt) != "- [x] eins\n" {
		t.Fatalf("Blob %q %v", inhalt, err)
	}
	if blobAbrufe != 1 {
		t.Errorf("%d Blob-Abrufe", blobAbrufe)
	}
}

func TestGeschlosseneIssuesUndBlocker(t *testing.T) {
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/alpha/issues":
			if r.URL.Query().Get("state") != "closed" {
				t.Errorf("state=%s, erwartet closed", r.URL.Query().Get("state"))
			}
			fmt.Fprint(w, `[
			 {"number":10,"title":"alt","closed_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-22T00:00:00Z"},
			 {"number":11,"title":"PR","closed_at":"2026-09-20T00:00:00Z","pull_request":{"url":"x"}},
			 {"number":12,"title":"neu","closed_at":"2026-09-10T00:00:00Z","body":"Text",
			  "issue_dependencies_summary":{"blocked_by":0}}]`)
		case "/repos/acme/alpha/issues/2/dependencies/blocked_by":
			fmt.Fprint(w, `[{"number":3,"state":"open","repository_url":"https://api.github.com/repos/acme/alpha"},
			 {"number":5,"state":"closed","repository_url":"https://api.github.com/repos/acme/alpha"},
			 {"number":8,"state":"open","repository_url":"https://api.github.com/repos/andere/repo"}]`)
		case "/repos/acme/alpha/issues/12":
			fmt.Fprint(w, `{"number":12,"title":"neu","body":"Text **fett**","html_url":"u","labels":[{"name":"x","color":"fff"}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	c := ghrest.Client{Token: "t"}
	ctx := context.Background()
	zu, err := c.GeschlosseneIssues(ctx, "acme/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(zu) != 2 || zu[0].Nummer != 12 || zu[1].Nummer != 10 || zu[0].Geschlossen != "2026-09-10T00:00:00Z" {
		t.Errorf("geschlossen %+v, erwartet 12 vor 10 ohne den PR", zu)
	}
	blocker, err := c.BlockiertDurch(ctx, "acme/alpha", 2)
	if err != nil || len(blocker) != 1 || blocker[0] != 3 {
		t.Errorf("Blocker %v %v, erwartet nur den offenen Blocker 3 aus demselben Repo", blocker, err)
	}
	i, err := c.Issue(ctx, "acme/alpha", 12)
	if err != nil || i.Text != "Text **fett**" || i.Titel != "neu" || len(i.Labels) != 1 {
		t.Errorf("Issue %+v %v", i, err)
	}
}

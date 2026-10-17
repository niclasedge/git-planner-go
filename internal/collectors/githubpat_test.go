package collectors

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestMain nimmt dem ganzen Paket die beiden PAT-Variablen weg. CollectGitHub
// greift auf sie zu; ein Entwickler, der sie laut README in seiner Shell hat,
// bekaeme sonst Tests rot, die mit PATs nichts zu tun haben (Review zu PR #4).
// Wer sie braucht, setzt sie per t.Setenv.
//
// Ausserdem zeigt der Endpunkt auf einen geschlossenen Port: ein Test, der
// vergisst, ghAPI zu starten, scheitert dann lokal, statt echt ins Netz zu
// gehen.
func TestMain(m *testing.M) {
	os.Unsetenv("GITHUB_PAT_PERSONAL")
	os.Unsetenv("GITHUB_PAT_WORK")
	ghGraphQLURL = "http://127.0.0.1:1/graphql-nicht-gestartet"
	os.Exit(m.Run())
}

// ghTokens sammelt die Authorization-Header aller Aufrufe.
func ghTokens(f *ghAPIFake) string {
	var s []string
	for _, a := range f.alle() {
		s = append(s, a.Auth)
	}
	return strings.Join(s, "\n")
}

func TestGitHubKarteNimmtGITHUB_PAT_WORKFuerWorkOrg(t *testing.T) {
	t.Setenv("GKP_WORK_OWNERS", "example-org")
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-test")
	t.Setenv("GITHUB_PAT_WORK", "pat-acme-test")
	f := ghSeiten(t, `{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)

	out := CollectGitHub(ghRepoMitOrigin(t, "https://github.com/example-org/orbit.git"))

	tokens := ghTokens(f)
	if len(f.alle()) != 2 || strings.Count(tokens, "bearer pat-acme-test") != 2 {
		t.Errorf("die API bekam nicht zweimal GITHUB_PAT_WORK:\n%s", tokens)
	}
	if strings.Contains(tokens, "pat-privat-test") {
		t.Errorf("das private Token ging an ein ACME-Repo:\n%s", tokens)
	}
	if out["account"] != "GITHUB_PAT_WORK" {
		t.Errorf("account=%v, erwartet den Variablennamen GITHUB_PAT_WORK", out["account"])
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "pat-acme-test") {
		t.Error("der Token-Wert steht in der Antwort der Karte")
	}
}

func TestGitHubKarteNimmtGITHUB_PAT_PERSONALFuerAndereRepos(t *testing.T) {
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-test")
	t.Setenv("GITHUB_PAT_WORK", "pat-acme-test")
	f := ghSeiten(t, `{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)

	out := CollectGitHub(ghTestRepo(t)) // example-user/infra

	if tokens := ghTokens(f); strings.Count(tokens, "bearer pat-privat-test") != 2 {
		t.Errorf("die API bekam nicht GITHUB_PAT_PERSONAL:\n%s", tokens)
	}
	if out["account"] != "GITHUB_PAT_PERSONAL" {
		t.Errorf("account=%v, erwartet GITHUB_PAT_PERSONAL", out["account"])
	}
}

// Ohne Variable gibt es keinen Rueckfall mehr (kein gh im Container): die
// Karte nennt die fehlende Variable und fragt die API gar nicht erst.
func TestGitHubKarteNenntDieFehlendeVariable(t *testing.T) {
	t.Setenv("GKP_WORK_OWNERS", "example-org")
	t.Setenv("CC_GH_ACCOUNTS", "")
	t.Setenv("GITHUB_PAT_PERSONAL", "pat-privat-test")
	t.Setenv("GITHUB_PAT_WORK", "")
	f := ghSeiten(t, `{}`)

	out := CollectGitHub(ghRepoMitOrigin(t, "https://github.com/example-org/orbit.git"))

	if n := len(f.alle()); n != 0 {
		t.Errorf("%d API-Aufrufe ohne passendes Token, erwartet 0", n)
	}
	if out["available"] != false || out["reason"] != "GITHUB_PAT_WORK ist nicht gesetzt" {
		t.Errorf("available=%v reason=%v", out["available"], out["reason"])
	}
	if out["account"] != "GITHUB_PAT_WORK" {
		t.Errorf("account=%v, erwartet GITHUB_PAT_WORK", out["account"])
	}
}

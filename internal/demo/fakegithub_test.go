package demo

import (
	"encoding/json"
	"net/http"
	"testing"
)

func get(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestFakeGitHubRequiresToken(t *testing.T) {
	f := NewFakeGitHub()
	defer f.Close()
	if got := get(t, f.URL()+"/user/repos", "").StatusCode; got != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", got)
	}
	resp := get(t, f.URL()+"/user/repos", Token)
	var repos []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0]["full_name"] != "acme/infra" || repos[1]["full_name"] != "acme/webshop" {
		t.Errorf("repos %v, want acme/infra and acme/webshop", repos)
	}
}

func TestFakeGitHubIsReadOnly(t *testing.T) {
	f := NewFakeGitHub()
	defer f.Close()
	req, _ := http.NewRequest(http.MethodPatch, f.URL()+"/repos/acme/webshop/issues/2", nil)
	req.Header.Set("Authorization", "Bearer "+Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("PATCH status %d, want 403", resp.StatusCode)
	}
}

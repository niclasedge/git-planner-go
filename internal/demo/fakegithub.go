// Package demo provides a self-contained demo mode: a small in-memory GitHub
// REST API with fictional repositories plus a throw-away workspace with git
// repos and beads maps. Nothing here talks to the real GitHub.
package demo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Token is the only bearer token the fake API accepts.
const Token = "demo-token"

type issue struct {
	Nr          int
	Title, Body string
	Closed      bool
	Created     time.Time
	ClosedAt    time.Time
	Labels      []string
	Assignees   []string
	Comments    []string // "login|text"
	Blockers    []int
	Subs        []int
	Parent      int
}

type pull struct {
	Nr     int
	Title  string
	Draft  bool
	Head   string
	Author string
}

type repoData struct {
	desc     string
	issues   map[int]*issue
	prs      map[int]*pull
	branches map[string]string
}

// FakeGitHub serves the REST endpoints the planner uses, from memory.
type FakeGitHub struct {
	mu    sync.Mutex
	repos map[string]*repoData // "owner/name"
	srv   *httptest.Server
}

// URL is the base URL to put into GKP_GITHUB_API.
func (f *FakeGitHub) URL() string { return f.srv.URL }

// Close stops the server.
func (f *FakeGitHub) Close() { f.srv.Close() }

func ago(days int) time.Time { return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour) }

// NewFakeGitHub starts the fake API with two fictional repositories.
func NewFakeGitHub() *FakeGitHub {
	f := &FakeGitHub{repos: map[string]*repoData{}}

	shop := &repoData{desc: "Storefront for the Acme web shop", issues: map[int]*issue{}, prs: map[int]*pull{},
		branches: map[string]string{"main": "3f9a1c2b4d5e", "change/hosted-fields": "a7d4e19c0b3f", "fix/guest-redirect": "c15b8e2f7a90", "chore/old-ci": "09e6d3ab41c8"}}
	add := func(r *repoData, i issue) { k := i; r.issues[i.Nr] = &k }
	add(shop, issue{Nr: 1, Title: "Checkout redesign", Body: "Umbrella for the new checkout: hosted payment fields, guest checkout, confirmation mail.", Created: ago(21), Labels: []string{"enhancement"}, Subs: []int{2, 3, 4}})
	add(shop, issue{Nr: 2, Title: "Replace payment form with hosted fields", Body: "Card data must never touch our servers.\n\n- [x] spike\n- [ ] SDK upgrade\n- [ ] rollout behind a flag", Created: ago(14), Labels: []string{"enhancement", "security"}, Assignees: []string{"ana"}, Parent: 1, Blockers: []int{5}, Comments: []string{"ana|Spike is on the branch change/hosted-fields."}})
	add(shop, issue{Nr: 3, Title: "Guest checkout without account", Created: ago(14), Closed: true, ClosedAt: ago(3), Labels: []string{"enhancement"}, Parent: 1})
	add(shop, issue{Nr: 4, Title: "Order confirmation e-mail template", Created: ago(13), Labels: []string{"enhancement"}, Assignees: []string{"bo"}, Parent: 1})
	add(shop, issue{Nr: 5, Title: "Upgrade payment SDK to v4", Created: ago(12), Labels: []string{"dependencies"}, Assignees: []string{"ana"}})
	add(shop, issue{Nr: 6, Title: "Search results page is slow on mobile", Body: "p95 is 2.8 s on a mid-range phone.", Created: ago(9), Labels: []string{"bug", "performance"}, Comments: []string{"bo|Reproduced with throttled CPU.", "ana|Probably the unbounded facet query."}})
	add(shop, issue{Nr: 7, Title: "Product image zoom", Created: ago(8), Labels: []string{"enhancement"}})
	add(shop, issue{Nr: 8, Title: "Migrate CI to reusable workflows", Created: ago(20), Closed: true, ClosedAt: ago(6), Labels: []string{"chore"}})
	shop.prs[20] = &pull{Nr: 20, Title: "Checkout: hosted payment fields (spike)", Draft: true, Head: "change/hosted-fields", Author: "ana"}
	shop.prs[21] = &pull{Nr: 21, Title: "Fix guest checkout redirect loop", Head: "fix/guest-redirect", Author: "bo"}
	f.repos["acme/webshop"] = shop

	infra := &repoData{desc: "Terraform and deploy config for Acme", issues: map[int]*issue{}, prs: map[int]*pull{},
		branches: map[string]string{"main": "3f9a1c2b4d5e"}}
	add(infra, issue{Nr: 1, Title: "Rotate TLS certificates before expiry", Created: ago(5), Labels: []string{"ops"}, Assignees: []string{"bo"}})
	add(infra, issue{Nr: 2, Title: "Grafana dashboard for queue depth", Created: ago(4), Labels: []string{"observability"}})
	add(infra, issue{Nr: 3, Title: "Move staging to the new cluster", Created: ago(15), Closed: true, ClosedAt: ago(2), Labels: []string{"ops"}})
	f.repos["acme/infra"] = infra

	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *FakeGitHub) issueJSON(repo string, r *repoData, i *issue) map[string]any {
	state := "open"
	var closed any
	if i.Closed {
		state = "closed"
		closed = i.ClosedAt.Format(time.RFC3339)
	}
	labels := []map[string]any{}
	for _, l := range i.Labels {
		labels = append(labels, map[string]any{"name": l, "color": labelColor(l)})
	}
	people := []map[string]any{}
	for _, a := range i.Assignees {
		people = append(people, map[string]any{"login": a})
	}
	done := 0
	for _, s := range i.Subs {
		if r.issues[s] != nil && r.issues[s].Closed {
			done++
		}
	}
	open := 0
	for _, b := range i.Blockers {
		if r.issues[b] == nil || !r.issues[b].Closed {
			open++
		}
	}
	m := map[string]any{"number": i.Nr, "id": 1000 + i.Nr, "title": i.Title, "body": i.Body, "state": state,
		"html_url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, i.Nr), "user": map[string]any{"login": "ana"},
		"labels": labels, "assignees": people, "comments": len(i.Comments),
		"created_at": i.Created.Format(time.RFC3339), "updated_at": i.Created.Format(time.RFC3339), "closed_at": closed,
		"sub_issues_summary":         map[string]any{"total": len(i.Subs), "completed": done},
		"issue_dependencies_summary": map[string]any{"blocked_by": open, "total_blocked_by": len(i.Blockers)}}
	if i.Parent != 0 {
		m["parent_issue_url"] = fmt.Sprintf("%s/repos/%s/issues/%d", f.srv.URL, repo, i.Parent)
	}
	return m
}

func labelColor(l string) string {
	switch l {
	case "bug":
		return "d73a4a"
	case "enhancement":
		return "0e8a16"
	case "security":
		return "b60205"
	case "performance":
		return "fbca04"
	case "dependencies":
		return "0366d6"
	}
	return "6e7781"
}

func (f *FakeGitHub) prJSON(repo string, p *pull) map[string]any {
	return map[string]any{"number": p.Nr, "title": p.Title, "html_url": fmt.Sprintf("https://github.com/%s/pull/%d", repo, p.Nr),
		"draft": p.Draft, "node_id": fmt.Sprintf("PR_%d", p.Nr), "state": "open", "merged": false, "mergeable": true,
		"mergeable_state": "clean", "user": map[string]any{"login": p.Author}, "labels": []any{},
		"head": map[string]any{"ref": p.Head, "sha": "sha" + strconv.Itoa(p.Nr), "repo": map[string]any{"full_name": repo}},
		"base": map[string]any{"ref": "main"}}
}

func reply(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Remaining", "4999")
	w.Header().Set("X-RateLimit-Limit", "5000")
	if r.Method == http.MethodGet && status == http.StatusOK {
		h := sha256.Sum256(b)
		etag := `"` + hex.EncodeToString(h[:6]) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func (f *FakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	notFound := func() { reply(w, r, http.StatusNotFound, map[string]any{"message": "Not Found"}) }
	if r.Header.Get("Authorization") != "Bearer "+Token {
		reply(w, r, http.StatusUnauthorized, map[string]any{"message": "Bad credentials"})
		return
	}
	p := r.URL.Path
	if p == "/user/repos" {
		var names []string
		for n := range f.repos {
			names = append(names, n)
		}
		sort.Strings(names)
		out := []map[string]any{}
		for _, n := range names {
			owner, name, _ := strings.Cut(n, "/")
			rd := f.repos[n]
			open := 0
			for _, i := range rd.issues {
				if !i.Closed {
					open++
				}
			}
			out = append(out, map[string]any{"full_name": n, "name": name, "owner": map[string]any{"login": owner},
				"default_branch": "main", "description": rd.desc, "open_issues_count": open + len(rd.prs)})
		}
		reply(w, r, 200, out)
		return
	}
	if p == "/graphql" {
		reply(w, r, 200, map[string]any{"data": map[string]any{}})
		return
	}
	parts := strings.Split(strings.TrimPrefix(p, "/repos/"), "/")
	if !strings.HasPrefix(p, "/repos/") || len(parts) < 3 {
		notFound()
		return
	}
	repo := parts[0] + "/" + parts[1]
	rd := f.repos[repo]
	if rd == nil {
		notFound()
		return
	}
	rest := strings.Join(parts[2:], "/")
	seg := parts[2:]
	switch {
	case rest == "issues" && r.Method == http.MethodGet:
		closed := r.URL.Query().Get("state") == "closed"
		var nrs []int
		for n := range rd.issues {
			nrs = append(nrs, n)
		}
		sort.Ints(nrs)
		out := []map[string]any{}
		for _, n := range nrs {
			if i := rd.issues[n]; i.Closed == closed {
				out = append(out, f.issueJSON(repo, rd, i))
			}
		}
		if !closed {
			for _, n := range sortedPRs(rd) {
				out = append(out, map[string]any{"number": n, "title": rd.prs[n].Title, "state": "open", "pull_request": map[string]any{}})
			}
		}
		reply(w, r, 200, out)
	case len(seg) >= 2 && seg[0] == "issues":
		n, _ := strconv.Atoi(seg[1])
		i := rd.issues[n]
		if i == nil {
			notFound()
			return
		}
		switch {
		case len(seg) == 2 && r.Method == http.MethodGet:
			reply(w, r, 200, f.issueJSON(repo, rd, i))
		case len(seg) == 3 && seg[2] == "comments" && r.Method == http.MethodGet:
			out := []map[string]any{}
			for _, c := range i.Comments {
				a, t, _ := strings.Cut(c, "|")
				out = append(out, map[string]any{"user": map[string]any{"login": a}, "body": t, "created_at": i.Created.Format(time.RFC3339)})
			}
			reply(w, r, 200, out)
		case len(seg) == 3 && seg[2] == "timeline":
			reply(w, r, 200, []any{})
		case len(seg) == 3 && seg[2] == "sub_issues":
			out := []map[string]any{}
			for _, s := range i.Subs {
				out = append(out, f.issueJSON(repo, rd, rd.issues[s]))
			}
			reply(w, r, 200, out)
		case len(seg) >= 4 && seg[2] == "dependencies" && r.Method == http.MethodGet:
			out := []map[string]any{}
			for _, b := range i.Blockers {
				state := "open"
				if rd.issues[b] != nil && rd.issues[b].Closed {
					state = "closed"
				}
				out = append(out, map[string]any{"number": b, "state": state, "repository_url": f.srv.URL + "/repos/" + repo})
			}
			reply(w, r, 200, out)
		default:
			// The demo is read-only: writes are refused, nothing is stored.
			reply(w, r, http.StatusForbidden, map[string]any{"message": "demo mode is read-only"})
		}
	case rest == "labels":
		reply(w, r, 200, []map[string]any{{"name": "bug", "color": "d73a4a"}, {"name": "enhancement", "color": "0e8a16"}})
	case rest == "assignees":
		reply(w, r, 200, []map[string]any{{"login": "ana"}, {"login": "bo"}})
	case rest == "branches":
		var names []string
		for n := range rd.branches {
			names = append(names, n)
		}
		sort.Strings(names)
		out := []map[string]any{}
		for _, n := range names {
			out = append(out, map[string]any{"name": n, "commit": map[string]any{"sha": rd.branches[n]}})
		}
		reply(w, r, 200, out)
	case rest == "pulls":
		out := []map[string]any{}
		if r.URL.Query().Get("state") == "closed" {
			reply(w, r, 200, out)
			return
		}
		for _, n := range sortedPRs(rd) {
			out = append(out, f.prJSON(repo, rd.prs[n]))
		}
		reply(w, r, 200, out)
	case len(seg) == 2 && seg[0] == "pulls":
		n, _ := strconv.Atoi(seg[1])
		if pr := rd.prs[n]; pr != nil {
			reply(w, r, 200, f.prJSON(repo, pr))
			return
		}
		notFound()
	case rest == "activity":
		reply(w, r, 200, []map[string]any{
			{"id": 1, "ref": "refs/heads/main", "timestamp": ago(3).Format(time.RFC3339), "activity_type": "pr_merge",
				"before": "5d2e8f1a9c70", "after": "3f9a1c2b4d5e", "actor": map[string]any{"login": "ana"}},
			{"id": 2, "ref": "refs/heads/change/hosted-fields", "timestamp": ago(1).Format(time.RFC3339), "activity_type": "push",
				"before": "b84c0e6f2d13", "after": "a7d4e19c0b3f", "actor": map[string]any{"login": "ana"}},
		})
	case len(seg) == 2 && seg[0] == "commits":
		reply(w, r, 200, map[string]any{"sha": seg[1], "author": map[string]any{"login": "ana"},
			"commit": map[string]any{"message": "checkout: hosted payment fields spike",
				"author":    map[string]any{"name": "Ana Example", "date": ago(1).Format(time.RFC3339)},
				"committer": map[string]any{"date": ago(1).Format(time.RFC3339)}},
			"files": []any{}})
	case len(seg) == 2 && seg[0] == "compare":
		reply(w, r, 200, map[string]any{"status": "ahead", "ahead_by": 1, "behind_by": 0, "commits": []any{}, "files": []any{}})
	case len(seg) == 3 && seg[0] == "git" && seg[1] == "trees":
		reply(w, r, 200, map[string]any{"sha": seg[2], "tree": []any{}})
	default:
		if r.Method == http.MethodGet && (strings.HasSuffix(rest, "/runs") || strings.Contains(rest, "actions")) {
			reply(w, r, 200, map[string]any{"total_count": 0, "workflow_runs": []any{}})
			return
		}
		notFound()
	}
}

func sortedPRs(rd *repoData) []int {
	var n []int
	for k := range rd.prs {
		n = append(n, k)
	}
	sort.Ints(n)
	return n
}

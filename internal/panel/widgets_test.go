package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The flat form has to keep working: Init folds it into one untitled group, and
// everything downstream only sees groups.
func TestMonitorInit_FlatSitesBecomeAGroup(t *testing.T) {
	m := &Monitor{Sites: []*Site{{Title: "a", URL: "http://a"}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(m.Groups) != 1 || m.Groups[0].Title != "" || len(m.Groups[0].Sites) != 1 {
		t.Fatalf("expected one untitled group, got %+v", m.Groups)
	}
	if len(m.Sites) != 1 {
		t.Fatalf("the flat list has to stay probeable, got %d", len(m.Sites))
	}
}

// Groups and a flat list at once: the flat sites come first, and Sites ends up
// as the probe list for all of them.
func TestMonitorInit_GroupsAndSites(t *testing.T) {
	m := &Monitor{
		Sites: []*Site{{Title: "loose", URL: "http://loose"}},
		Groups: []*SiteGroup{
			{Title: "Lokal", Sites: []*Site{{Title: "a", URL: "http://a"}}},
			{Title: "Remote", Sites: []*Site{{Title: "b", URL: "http://b"}, {Title: "c", CheckURL: "http://c"}}},
		},
	}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(m.Groups) != 3 || m.Groups[0].Title != "" {
		t.Fatalf("expected the loose sites first, got %+v", m.Groups)
	}
	if len(m.Sites) != 4 {
		t.Fatalf("expected 4 probed sites, got %d", len(m.Sites))
	}

	// The probe list and the groups must be the same objects, or the rows would
	// render the results of nothing.
	m.Sites[1].Status = 200
	if !m.Groups[1].Sites[0].Up() {
		t.Fatal("probing must write through to the grouped site")
	}

	// Defaults are applied inside groups too.
	if m.Groups[2].Sites[1].Timeout.Std() <= 0 {
		t.Fatal("a grouped site must get the default timeout")
	}
}

func TestMonitorInit_NoSites(t *testing.T) {
	if err := (&Monitor{}).Init(); err == nil {
		t.Fatal("expected an error")
	}
	if err := (&Monitor{Groups: []*SiteGroup{{Title: "empty"}}}).Init(); err == nil {
		t.Fatal("a group without sites is still no sites")
	}
	if err := (&Monitor{Groups: []*SiteGroup{{Sites: []*Site{{Title: "a"}}}}}).Init(); err == nil {
		t.Fatal("a grouped site without a url must be rejected")
	}
}

// failures-only hides a whole group, heading included.
func TestSiteGroupVisible(t *testing.T) {
	down := &Site{Title: "down", URL: "http://d", Error: "boom"}
	up := &Site{Title: "up", URL: "http://u", Status: 200}
	g := &SiteGroup{Sites: []*Site{up, down}}

	if len(g.Visible(false)) != 2 {
		t.Fatal("without the filter every site shows")
	}
	if v := g.Visible(true); len(v) != 1 || v[0] != down {
		t.Fatalf("expected only the failing site, got %+v", v)
	}
	if len((&SiteGroup{Sites: []*Site{up}}).Visible(true)) != 0 {
		t.Fatal("a healthy group must render nothing")
	}
}

// The same service name in two groups must not collide: one file for both would
// show the wrong picture for one of them.
func TestSiteSlug_GroupScoped(t *testing.T) {
	m := &Monitor{Groups: []*SiteGroup{
		{Title: "Lokal · Docker", Sites: []*Site{{Title: "SearXNG", URL: "http://a"}}},
		{Title: "netcup3 · Docker", Sites: []*Site{{Title: "SearXNG", URL: "http://b"}}},
	}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	a, b := m.Sites[0].Slug(), m.Sites[1].Slug()
	if a == b {
		t.Fatalf("slugs collide: %q", a)
	}
	if a != "lokal-docker-searxng" || b != "netcup3-docker-searxng" {
		t.Fatalf("unexpected slugs: %q / %q", a, b)
	}
}

// An ungrouped site keeps a bare slug — there is no group to qualify it.
func TestSiteSlug_Flat(t *testing.T) {
	m := &Monitor{Sites: []*Site{{Title: "files-tauri", URL: "http://a"}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := m.Sites[0].Slug(); got != "files-tauri" {
		t.Fatalf("got %q", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Lokal · Docker":  "lokal-docker",
		"Größe & Anzahl":  "groesse-anzahl",
		"  leading":       "leading",
		"trailing  ":      "trailing",
		"Ölpreis":         "oelpreis",
		"a---b":           "a-b",
		"http://x:8080/y": "http-x-8080-y",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// Only reachable sites are worth photographing, and a site with no browsable URL
// has nothing to photograph.
func TestShotTargets_OnlyReachable(t *testing.T) {
	m := &Monitor{Groups: []*SiteGroup{{Title: "G", Sites: []*Site{
		{Title: "up", URL: "http://up", Status: 200},
		{Title: "down", URL: "http://down", Error: "boom"},
		{Title: "check-only", CheckURL: "http://c", Status: 200},
	}}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	got := m.ShotTargets()
	if len(got) != 1 {
		t.Fatalf("expected only the reachable site with a url, got %+v", got)
	}
	if got[0].Slug != "g-up" || got[0].URL != "http://up" {
		t.Fatalf("wrong target: %+v", got[0])
	}
}

// No screenshot directory, no thumbnails — and no crash.
func TestLookupShots_NoDir(t *testing.T) {
	m := &Monitor{Sites: []*Site{{Title: "a", URL: "http://a"}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, s := range m.lookupShots() {
		if s != "" {
			t.Fatalf("expected no shots, got %q", s)
		}
	}
	if m.Sites[0].Shot() != "" {
		t.Fatal("Shot must be empty without a file")
	}
}

// A file that exists gets a cache-busting stamp; an empty one is treated as
// absent, because a half-written PNG is worse than no thumbnail.
func TestLookupShots_FindsFileAndSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("not really a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Monitor{Sites: []*Site{{Title: "a", URL: "http://a"}, {Title: "b", URL: "http://b"}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	m.shotsDir = dir

	shots := m.lookupShots()
	if !strings.HasPrefix(shots[0], "a.png?v=") {
		t.Fatalf("expected a stamped name, got %q", shots[0])
	}
	if shots[1] != "" {
		t.Fatalf("an empty file must count as absent, got %q", shots[1])
	}

	m.Sites[0].shot = shots[0]
	if !strings.HasPrefix(m.Sites[0].Shot(), "/shots/a.png?v=") {
		t.Fatalf("bad url: %q", m.Sites[0].Shot())
	}
}

// expect-status flips the meaning of one code: behind Traefik BasicAuth the
// middleware's 401 is the healthy answer, and anything else — including the
// 200 that appears when the auth is removed — must read as down.
func TestSiteUpExpectStatus(t *testing.T) {
	cases := []struct {
		name string
		site Site
		want bool
	}{
		{"expected 401 is up", Site{Status: 401, ExpectStatus: 401}, true},
		{"unexpected 200 is down", Site{Status: 200, ExpectStatus: 401}, false},
		{"error wins over match", Site{Status: 401, ExpectStatus: 401, Error: "x"}, false},
		{"unset keeps 2xx-3xx rule", Site{Status: 303}, true},
		{"unset keeps 401 down", Site{Status: 401}, false},
	}
	for _, c := range cases {
		if got := c.site.Up(); got != c.want {
			t.Errorf("%s: Up() = %v, want %v", c.name, got, c.want)
		}
	}
}

// The four exposure states, because "the port is open" and "anyone can walk in"
// are different facts and the widget must not blur them.
func TestSiteExposure(t *testing.T) {
	cases := []struct {
		name string
		site Site
		want string
		lbl  string
	}{
		{"no public url", Site{Status: 200}, "internal", "intern"},
		{"configured but unprobed", Site{PublicURL: "https://x", Status: 200}, "internal", "intern"},
		{"open", Site{PublicURL: "https://x", PublicStatus: 200}, "public", "öffentlich"},
		{"redirect counts as reachable", Site{PublicURL: "https://x", PublicStatus: 303}, "public", "öffentlich"},
		{"basic auth", Site{PublicURL: "https://x", PublicStatus: 401}, "guarded", "öffentlich · Auth"},
		{"forbidden", Site{PublicURL: "https://x", PublicStatus: 403}, "guarded", "öffentlich · Auth"},
		{"route down", Site{PublicURL: "https://x", PublicError: "dial: refused"}, "broken", "Route defekt"},
		{"server error", Site{PublicURL: "https://x", PublicStatus: 502}, "broken", "Route defekt"},
	}
	for _, c := range cases {
		if got := c.site.Exposure(); got != c.want {
			t.Errorf("%s: Exposure() = %q, want %q", c.name, got, c.want)
		}
		if got := c.site.ExposureLabel(); got != c.lbl {
			t.Errorf("%s: ExposureLabel() = %q, want %q", c.name, got, c.lbl)
		}
	}
}

// The public probe leaves the machine, so it runs on its own slow clock rather
// than with every internal round.
func TestPublicDue(t *testing.T) {
	m := &Monitor{Sites: []*Site{{Title: "a", URL: "http://a"}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.PublicInterval.Std() != 15*time.Minute {
		t.Fatalf("default interval: %v", m.PublicInterval.Std())
	}

	now := time.Now()
	internalOnly := &Site{URL: "http://a"}
	if m.publicDue(internalOnly, now) {
		t.Fatal("a site without a public url is never due")
	}

	fresh := &Site{URL: "http://a", PublicURL: "https://a", publicAt: now.Add(-time.Minute)}
	if m.publicDue(fresh, now) {
		t.Fatal("checked a minute ago is not due at a 15m interval")
	}

	stale := &Site{URL: "http://a", PublicURL: "https://a", publicAt: now.Add(-16 * time.Minute)}
	if !m.publicDue(stale, now) {
		t.Fatal("checked 16 minutes ago is due")
	}

	never := &Site{URL: "http://a", PublicURL: "https://a"}
	if !m.publicDue(never, now) {
		t.Fatal("a never-checked site is due immediately")
	}
}

// A round that skips the public check must leave the previous verdict alone —
// otherwise every internal refresh would blank the badge for 15 minutes.
func TestUpdate_KeepsPublicVerdictBetweenChecks(t *testing.T) {
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer pub.Close()
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()

	m := &Monitor{Sites: []*Site{{Title: "a", URL: internal.URL, PublicURL: pub.URL}}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	m.Update(context.Background())
	if got := m.Sites[0].Exposure(); got != "guarded" {
		t.Fatalf("first round should have probed the public url, got %q", got)
	}

	// Second round, immediately: the public check is not due, and the verdict
	// has to survive it.
	pub.Close()
	m.Update(context.Background())
	if got := m.Sites[0].Exposure(); got != "guarded" {
		t.Fatalf("verdict lost on a round that skipped the check: %q", got)
	}
	if !m.Sites[0].Up() {
		t.Fatal("the internal probe should still be fine")
	}
}

// Someone else's service gets no exposure claim at all — labelling github.com
// "intern" would be a statement about a deployment that is not ours.
func TestSiteExposure_ExternalGroup(t *testing.T) {
	m := &Monitor{Groups: []*SiteGroup{
		{Title: "Extern", External: true, Sites: []*Site{{Title: "GitHub API", URL: "https://api.github.com", Status: 200}}},
		{Title: "Mine", Sites: []*Site{{Title: "svc", URL: "http://svc", Status: 200}}},
	}}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := m.Sites[0].Exposure(); got != "" {
		t.Fatalf("external site should make no claim, got %q", got)
	}
	if got := m.Sites[1].Exposure(); got != "internal" {
		t.Fatalf("our own site without a public url is internal, got %q", got)
	}

	// An external service with a public url still gets checked — the flag only
	// suppresses the guess, not an explicit configuration.
	m.Sites[0].PublicURL = "https://api.github.com"
	m.Sites[0].PublicStatus = 200
	if got := m.Sites[0].Exposure(); got != "public" {
		t.Fatalf("an explicit public url must still count, got %q", got)
	}
}

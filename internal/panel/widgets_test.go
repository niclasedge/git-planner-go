package panel

import "testing"

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

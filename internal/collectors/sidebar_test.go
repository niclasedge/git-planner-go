package collectors

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// mustGitRepo makes dir a git repo whose origin is remote. Nothing is fetched:
// `git init` plus `git remote add` is enough for GhOriginRemote, which only
// reads the configured URL.
func mustGitRepo(t *testing.T, dir, remote string) {
	t.Helper()
	mustMkdir(t, dir)
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
		}
	}
}

func discoveredNames(got []ProjectRef) map[string]bool {
	names := map[string]bool{}
	for _, p := range got {
		names[p.Name] = true
	}
	return names
}

func TestDiscoverProjectsIn(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "has-beads", ".beads"))
	mustMkdir(t, filepath.Join(root, "also-beads", ".beads"))
	mustMkdir(t, filepath.Join(root, "has-none"))

	got := DiscoverProjectsIn([]string{root}, filepath.Join(root, "has-beads"))

	names := map[string]bool{}
	for _, p := range got {
		names[p.Name] = true
	}
	for _, want := range []string{"has-beads", "also-beads"} {
		if !names[want] {
			t.Errorf("expected %q to be discovered, got %+v", want, got)
		}
	}
	if names["has-none"] {
		t.Errorf("has-none (no .beads/) should not be discovered: %+v", got)
	}
}

func TestDiscoverProjectsIn_ActiveRepoAlwaysIncluded(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "active-without-markers")
	mustMkdir(t, active)

	got := DiscoverProjectsIn([]string{root}, active)

	found := false
	for _, p := range got {
		if p.Path == active {
			found = true
		}
	}
	if !found {
		t.Errorf("active repo %q must always be included even without .beads/, got %+v", active, got)
	}
}

// gkp-y2d: after E1 (beads and GitHub run side by side, uncoupled) a repo
// without .beads/ must still show up when it has a resolvable origin remote —
// otherwise every GitHub task in a repo without beads stays invisible.
func TestDiscoverProjectsIn_BeadsOrGhRemote(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "beads-no-remote", ".beads"))
	mustGitRepo(t, filepath.Join(root, "remote-no-beads"), "git@github.com:owner/remote-no-beads.git")
	mustMkdir(t, filepath.Join(root, "neither"))

	got := DiscoverProjectsIn([]string{root}, filepath.Join(root, "beads-no-remote"))
	names := discoveredNames(got)

	for _, want := range []string{"beads-no-remote", "remote-no-beads"} {
		if !names[want] {
			t.Errorf("expected %q to be discovered, got %+v", want, got)
		}
	}
	if names["neither"] {
		t.Errorf("neither (no .beads/, no remote) should not be discovered: %+v", got)
	}
}

// A git repo without an origin remote has nothing to show either — the
// criterion is a *resolvable* remote, not the presence of .git/.
func TestDiscoverProjectsIn_GitRepoWithoutOriginIsSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "no-origin")
	mustMkdir(t, dir)
	if out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	got := DiscoverProjectsIn([]string{root}, filepath.Join(root, "somewhere-else"))

	if discoveredNames(got)["no-origin"] {
		t.Errorf("a repo without origin should not be discovered: %+v", got)
	}
}

// A plain directory *inside* a repo would inherit that repo's origin if the
// remote were read without checking for .git/ first — `git -C dir` walks up.
func TestDiscoverProjectsIn_PlainDirInsideRepoDoesNotInheritRemote(t *testing.T) {
	root := t.TempDir()
	mustGitRepo(t, root, "git@github.com:owner/outer.git")
	mustMkdir(t, filepath.Join(root, "plain-subdir"))

	got := DiscoverProjectsIn([]string{root}, filepath.Join(root, "somewhere-else"))

	if discoveredNames(got)["plain-subdir"] {
		t.Errorf("plain-subdir has neither .beads/ nor an own remote: %+v", got)
	}
}

// Dedupe by name: the first root that carries a name wins, later roots do not
// overwrite it and the name appears once.
func TestDiscoverProjectsIn_DedupesByName(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	mustMkdir(t, filepath.Join(first, "same-name", ".beads"))
	mustMkdir(t, filepath.Join(second, "same-name", ".beads"))

	got := DiscoverProjectsIn([]string{first, second}, filepath.Join(first, "same-name"))

	seen := 0
	for _, p := range got {
		if p.Name == "same-name" {
			seen++
			if p.Path != filepath.Join(first, "same-name") {
				t.Errorf("first root must win, got path %q", p.Path)
			}
		}
	}
	if seen != 1 {
		t.Errorf("expected 'same-name' exactly once, got %d: %+v", seen, got)
	}
}

func TestDiscoverProjectsIn_UnreadableRootIsSkipped(t *testing.T) {
	got := DiscoverProjectsIn([]string{"/does/not/exist"}, "/also/does/not/exist")
	if len(got) != 1 || got[0].Path != "/also/does/not/exist" {
		t.Errorf("expected only the active repo fallback entry, got %+v", got)
	}
}

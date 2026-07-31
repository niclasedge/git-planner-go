package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/panel"
)

// Screenshots are taken by something else — a browser is not a dependency this
// binary carries. The app's part is two endpoints: one saying what is worth
// photographing right now, one serving what came back.

// handleShotTargets lists the reachable services and the file name each shot
// should get. The caller needs no knowledge of config.yaml, and "only if
// reachable" is decided here, where the probe results already are.
func (s *Server) handleShotTargets(w http.ResponseWriter, r *http.Request) {
	targets := s.panels.ShotTargets()
	if targets == nil {
		// An empty array, not null: the caller iterates it.
		targets = []panel.ShotTarget{}
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(targets)
}

// handleShot serves one screenshot.
//
// The name is checked rather than cleaned: a path is only ever a slug plus
// ".png", so anything else is a mistake or an attempt, and both deserve a 404
// rather than a best-effort interpretation. Without this the handler would read
// any file the process can reach.
func (s *Server) handleShot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validShotName(name) {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(s.cfg.Server.ShotsDir, name)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// The URL carries the file's mtime as ?v=, so a cached copy is only ever the
	// picture that URL named. A week is therefore safe and keeps the thumbnails
	// off the wire on every page view.
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// validShotName accepts exactly what Site.Slug produces plus the extension:
// lowercase letters, digits and inner dashes.
func validShotName(name string) bool {
	slug, ok := strings.CutSuffix(name, ".png")
	if !ok || slug == "" || len(name) > 128 {
		return false
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

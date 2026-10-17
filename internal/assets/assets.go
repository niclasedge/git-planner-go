// Package assets embeds the static dashboard assets (browser.js/css, dash.css,
// and the built Svelte bundle) into the binary via go:embed, so the app ships
// as a single self-contained file (the Go-native replacement for the Deno app's
// on-disk assets/ + serveDir). Nothing here is read at runtime, and the service
// therefore needs neither a working directory nor Node in its PATH.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed static/browser.css
var BrowserCSS string

//go:embed static/browser.js
var BrowserJS string

//go:embed static/dash.css
var DashCSS string

// AppJS is the Svelte bundle (gkp-a1k). Unlike the three above it is not
// hand-written: `just frontend` compiles web/src via vite into
// static/app/app.js, and that artifact is committed.
//
// Why committed rather than generated on demand: a //go:embed on a missing
// path is a compile error, so a generate-only scheme would make plain
// `go build ./...` fail on a fresh clone without Node — and a placeholder that
// compiles but serves nothing is worse still, because the binary would start
// and only the new view would stay blank. The same trade-off is already
// settled elsewhere in this repo: docs/*.png are committed next to their .dot
// sources so readers need no graphviz (see the `diagramm` recipe). The cost is
// drift, and `just gate` pays it by rebuilding the bundle before every test
// run.
//
// The CSS of the Svelte components is inlined into this same file by vite —
// there is deliberately no second artifact to keep in sync.
//
//go:embed static/app/app.js
var AppJS string

//go:embed static
var staticFS embed.FS

// FS returns the static/ subtree rooted so files are served at their bare names
// (e.g. /assets/browser.js → static/browser.js).
func FS() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed is compile-time guaranteed; this can't happen
	}
	return sub
}

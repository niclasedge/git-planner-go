package web

import (
	"crypto/md5"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
)

func init() {
	// Go's built-in table has no woff2, so http.FileServer would fall back to
	// content sniffing and serve the fonts as application/octet-stream. It
	// happens to work in browsers, but only because font loading does not check
	// the type — and whether it lands right otherwise depends on the host's
	// /etc/mime.types. Register it and the answer stops being machine-specific.
	mime.AddExtensionType(".woff2", "font/woff2")
}

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// staticHash is a digest of every embedded asset. It goes into the URL as
// /static/<hash>/... so browsers can cache assets forever and still pick up a
// new build immediately — no cache-busting query strings, no stale CSS.
var staticHash = hashFS(staticFS)

func hashFS(f embed.FS) string {
	var names []string
	fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names) // WalkDir order is stable, but be explicit

	h := md5.New()
	for _, n := range names {
		file, err := f.Open(n)
		if err != nil {
			continue
		}
		io.WriteString(h, n)
		io.Copy(h, file)
		file.Close()
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// staticURL builds the hashed URL for an asset path like "css/app.css".
func staticURL(p string) string {
	return path.Join("/static", staticHash, p)
}

// staticHandler serves the embedded assets under the hashed prefix.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // impossible: the directory is embedded above
	}
	h := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Safe because the hash changes whenever any asset does.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.StripPrefix("/static/"+staticHash+"/", h).ServeHTTP(w, r)
	})
}

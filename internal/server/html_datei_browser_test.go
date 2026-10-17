package server_test

// Browser-Test der Datei-Route (html-dateien, #112): Chromium oeffnet
// /api/datei/acme/alpha/k2/docs/bericht.html vom echten Handler gegen die
// Fake-GitHub-API. Das relativ eingebundene bilder/a.png muss der Browser
// ueber dieselbe Route holen, trotz CSP sandbox; beide Bilder (relativ und
// data:) muessen im Bildschirmfoto zu sehen sein. Mit
// GKP_BELEG_PNG_HTML_GERENDERT bleibt das Foto dort liegen.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func hdChromium() string {
	ausfuehrbar := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
	}
	if p := os.Getenv("GKP_CHROMIUM"); p != "" && ausfuehrbar(p) {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "headless_shell"} {
		if p, err := exec.LookPath(name); err == nil && ausfuehrbar(p) {
			return p
		}
	}
	var wurzeln []string
	if p := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); p != "" {
		wurzeln = append(wurzeln, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		wurzeln = append(wurzeln, filepath.Join(home, ".cache", "ms-playwright"))
	}
	for _, w := range wurzeln {
		for _, muster := range []string{"chromium-*/chrome-linux/chrome", "chromium_headless_shell-*/chrome-linux/headless_shell"} {
			treffer, _ := filepath.Glob(filepath.Join(w, muster))
			sort.Sort(sort.Reverse(sort.StringSlice(treffer)))
			for _, p := range treffer {
				if ausfuehrbar(p) {
					return p
				}
			}
		}
	}
	return ""
}

// hdPNG ist ein einfarbiges Bild mit schmalem Rahmen.
func hdPNG(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 240, 150))
	for y := 0; y < 150; y++ {
		for x := 0; x < 240; x++ {
			if x < 4 || y < 4 || x >= 236 || y >= 146 {
				img.Set(x, y, color.RGBA{A: 255})
			} else {
				img.Set(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// hdFarbeDa zaehlt die Pixel im Foto, die (bis auf Rundung) die Farbe c tragen.
func hdFarbeDa(foto image.Image, c color.RGBA) int {
	nah := func(a uint32, b uint8) bool {
		d := int(a>>8) - int(b)
		return d > -6 && d < 6
	}
	n := 0
	r := foto.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pr, pg, pb, _ := foto.At(x, y).RGBA()
			if nah(pr, c.R) && nah(pg, c.G) && nah(pb, c.B) {
				n++
			}
		}
	}
	return n
}

type hdProtokoll struct {
	mu     sync.Mutex
	status map[string]int
}

type hdAntwort struct {
	http.ResponseWriter
	code int
}

func (a *hdAntwort) WriteHeader(code int) {
	a.code = code
	a.ResponseWriter.WriteHeader(code)
}

func TestDateiGerendertImBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: der Test startet einen Browser")
	}
	chrome := hdChromium()
	if chrome == "" {
		t.Skip("kein Chromium gefunden — das Rendern ist damit NICHT belegt")
	}
	rot := color.RGBA{R: 210, G: 60, B: 50, A: 255}
	gruen := color.RGBA{R: 40, G: 160, B: 90, A: 255}
	f := hdFakeGH(t, hdPNG(rot), hdPNG(gruen))
	h, _, _ := repoHandler(t, f)
	prot := &hdProtokoll{status: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := &hdAntwort{ResponseWriter: w, code: http.StatusOK}
		h.ServeHTTP(a, r)
		prot.mu.Lock()
		prot.status[r.URL.Path] = a.code
		prot.mu.Unlock()
	}))
	defer srv.Close()

	ziel := os.Getenv("GKP_BELEG_PNG_HTML_GERENDERT")
	if ziel == "" {
		ziel = filepath.Join(t.TempDir(), "gerendert.png")
	}
	ctx, abbrechen := context.WithTimeout(context.Background(), 90*time.Second)
	defer abbrechen()
	out, err := exec.CommandContext(ctx, chrome,
		"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--disable-extensions", "--disable-dev-shm-usage", "--hide-scrollbars",
		"--user-data-dir="+t.TempDir(), "--window-size=900,700", "--virtual-time-budget=10000",
		"--screenshot="+ziel, srv.URL+"/api/datei/acme/alpha/k2/docs/bericht.html").CombinedOutput()
	if err != nil {
		t.Fatalf("chromium (%s): %v\n%s", chrome, err, out)
	}

	prot.mu.Lock()
	code, geholt := prot.status["/api/datei/acme/alpha/k2/docs/bilder/a.png"]
	seite := prot.status["/api/datei/acme/alpha/k2/docs/bericht.html"]
	prot.mu.Unlock()
	if seite != http.StatusOK {
		t.Fatalf("Seite: Status %d", seite)
	}
	if !geholt || code != http.StatusOK {
		t.Errorf("relatives Bild: geholt %v, Status %d; erwartet 200 ueber /api/datei/…/docs/bilder/a.png", geholt, code)
	}

	datei, err := os.Open(ziel)
	if err != nil {
		t.Fatalf("Foto: %v", err)
	}
	defer datei.Close()
	foto, err := png.Decode(datei)
	if err != nil {
		t.Fatalf("Foto kein PNG: %v", err)
	}
	if n := hdFarbeDa(foto, rot); n < 20000 {
		t.Errorf("relativ eingebundenes Bild nicht zu sehen (%d Pixel in seiner Farbe)", n)
	}
	if n := hdFarbeDa(foto, gruen); n < 20000 {
		t.Errorf("data:-Bild nicht zu sehen (%d Pixel in seiner Farbe)", n)
	}
}

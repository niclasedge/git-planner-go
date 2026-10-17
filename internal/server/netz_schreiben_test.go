package server_test

// Tests fuer netz-schreiben (#46): ueber den Netz-Listener darf schreiben,
// wessen Peer-IP (r.RemoteAddr, vom Socket) exakt in --write-clients steht.
// Die Peer-IP wird hier echt gesetzt: der Client bindet seine Quelladresse
// auf 127.0.0.3 (freigegeben) oder 127.0.0.4 (fremd) — beide liegen unter
// Linux auf lo, der Netz-Listener steht wie in network_gate_test.go auf
// 127.0.0.2. Kein Header kann daran etwas drehen, und genau das prueft
// TestNetzSchreibenXForwardedForHilftNicht.

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niclasedge/git-planner-go/internal/assets"
	"github.com/niclasedge/git-planner-go/internal/render"
	"github.com/niclasedge/git-planner-go/internal/server"
)

const mcpRumpf = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

// clientVon baut einen HTTP-Client, dessen Verbindungen von quelle ausgehen.
// Laesst sich die Adresse nicht binden, ueberspringt sich der Test mit Grund.
func clientVon(t *testing.T, quelle string) *http.Client {
	t.Helper()
	probe, err := net.Listen("tcp", quelle+":0")
	if err != nil {
		t.Skipf("Quelladresse %s nicht bindbar: %v", quelle, err)
	}
	probe.Close()
	d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(quelle)}}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: tr}
}

// netzMitFreigabe startet einen Handler mit Freigabeliste auf Loopback und
// auf der Netz-Adresse 127.0.0.2; areas.json liegt in einem Temp-Ordner.
func netzMitFreigabe(t *testing.T, freigabe string) (loopbackURL, netzURL, areaDatei string) {
	t.Helper()
	ips, err := server.ParseWriteClients(freigabe)
	if err != nil {
		t.Fatalf("ParseWriteClients(%q): %v", freigabe, err)
	}
	areaDatei = filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei)
	h := server.NewHandler(server.AppOptions{
		Cwd:          t.TempDir(),
		Assets:       render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS:      assets.FS(),
		WriteClients: ips,
	})
	return startOn(t, "127.0.0.1", h), startOn(t, "127.0.0.2", h), areaDatei
}

func senden(t *testing.T, c *http.Client, basis, pfad, rumpf string, kopf map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, basis+pfad, strings.NewReader(rumpf))
	if err != nil {
		t.Fatalf("Anfrage bauen: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range kopf {
		if k == "Host" {
			req.Host = v // net/http ignores a Host entry in Header
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Anfrage schicken: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestParseWriteClientsGueltig(t *testing.T) {
	ips, err := server.ParseWriteClients("192.0.2.63, ::2")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(ips) != 2 || !ips[0].Equal(net.ParseIP("192.0.2.63")) || !ips[1].Equal(net.ParseIP("::2")) {
		t.Fatalf("Liste = %v, erwartet [192.0.2.63 ::2]", ips)
	}
	leer, err := server.ParseWriteClients("")
	if err != nil || len(leer) != 0 {
		t.Fatalf("leere Angabe: Liste %v, Fehler %v — erwartet leer, kein Fehler", leer, err)
	}
}

func TestParseWriteClientsLehntBereicheUndWildcardsAb(t *testing.T) {
	for _, eintrag := range []string{"192.0.2.0/24", "*", "0.0.0.0", "::", "rechner.local", "192.0.2.*", "fe80::/64"} {
		_, err := server.ParseWriteClients("192.0.2.63," + eintrag)
		if err == nil {
			t.Errorf("ParseWriteClients mit %q: kein Fehler, erwartet Abbruch", eintrag)
			continue
		}
		if !strings.Contains(err.Error(), eintrag) {
			t.Errorf("Fehler zu %q nennt den Eintrag nicht: %v", eintrag, err)
		}
	}
}

func TestNetzSchreibenFreigegebeneIPDarf(t *testing.T) {
	_, netzURL, _ := netzMitFreigabe(t, "127.0.0.3")
	status, rumpf := senden(t, clientVon(t, "127.0.0.3"), netzURL, "/mcp", mcpRumpf, nil)
	if status != http.StatusOK {
		t.Fatalf("POST /mcp von freigegebener IP: Status %d, erwartet 200 (%s)", status, rumpf)
	}
}

func TestNetzSchreibenFremdeIPBleibtDraussen(t *testing.T) {
	_, netzURL, _ := netzMitFreigabe(t, "127.0.0.3")
	status, rumpf := senden(t, clientVon(t, "127.0.0.4"), netzURL, "/mcp", mcpRumpf, nil)
	if status != http.StatusForbidden {
		t.Fatalf("POST /mcp von fremder IP: Status %d, erwartet 403 (%s)", status, rumpf)
	}
}

func TestNetzSchreibenXForwardedForHilftNicht(t *testing.T) {
	_, netzURL, _ := netzMitFreigabe(t, "127.0.0.3")
	kopf := map[string]string{"X-Forwarded-For": "127.0.0.3", "X-Real-IP": "127.0.0.3", "Forwarded": "for=127.0.0.3"}
	status, rumpf := senden(t, clientVon(t, "127.0.0.4"), netzURL, "/mcp", mcpRumpf, kopf)
	if status != http.StatusForbidden {
		t.Fatalf("POST /mcp von fremder IP mit X-Forwarded-For: Status %d, erwartet 403 (%s)", status, rumpf)
	}
}

func TestNetzSchreibenLoopbackUnveraendert(t *testing.T) {
	loopbackURL, netzURL, _ := netzMitFreigabe(t, "")
	status, rumpf := senden(t, http.DefaultClient, loopbackURL, "/mcp", mcpRumpf, nil)
	if status != http.StatusOK {
		t.Fatalf("POST /mcp ueber Loopback: Status %d, erwartet 200 (%s)", status, rumpf)
	}
	status, _ = senden(t, http.DefaultClient, loopbackURL, "/mcp", mcpRumpf, map[string]string{"Origin": "https://boese.example"})
	if status != http.StatusForbidden {
		t.Fatalf("POST /mcp ueber Loopback mit fremder Origin: Status %d, erwartet 403", status)
	}
	// Leere Freigabeliste: ueber die Netz-Adresse bleibt es beim alten 403.
	status, _ = senden(t, clientVon(t, "127.0.0.3"), netzURL, "/mcp", mcpRumpf, nil)
	if status != http.StatusForbidden {
		t.Fatalf("POST /mcp ueber Netz ohne Freigabe: Status %d, erwartet 403", status)
	}
}

func TestNetzSchreibenPassendeOrigin(t *testing.T) {
	_, netzURL, areaDatei := netzMitFreigabe(t, "127.0.0.3")
	c := clientVon(t, "127.0.0.3")
	kopf := map[string]string{"Origin": netzURL} // netzURL ist http://127.0.0.2:PORT
	status, rumpf := senden(t, c, netzURL, "/mcp", mcpRumpf, kopf)
	if status != http.StatusOK {
		t.Fatalf("POST /mcp mit passender Origin: Status %d, erwartet 200 (%s)", status, rumpf)
	}
	status, rumpf = senden(t, c, netzURL, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`, kopf)
	if status != http.StatusOK {
		t.Fatalf("POST /action/task/bereich mit passender Origin: Status %d, erwartet 200 (%s)", status, rumpf)
	}
	if _, err := os.Stat(areaDatei); err != nil {
		t.Fatalf("areas.json fehlt nach erlaubtem Schreiben: %v", err)
	}
}

func TestNetzSchreibenFremdeOriginVonFreigegebenerIP(t *testing.T) {
	_, netzURL, areaDatei := netzMitFreigabe(t, "127.0.0.3")
	c := clientVon(t, "127.0.0.3")
	fremd := []string{
		"http://evil.example",
		"http://127.0.0.1" + netzURL[len("http://127.0.0.2"):], // Loopback-Origin gilt nicht fuer Netz-Requests
		strings.Replace(netzURL, "http://", "https://", 1),
	}
	for _, origin := range fremd {
		kopf := map[string]string{"Origin": origin}
		if status, rumpf := senden(t, c, netzURL, "/mcp", mcpRumpf, kopf); status != http.StatusForbidden {
			t.Errorf("POST /mcp mit Origin %q: Status %d, erwartet 403 (%s)", origin, status, rumpf)
		}
		if status, rumpf := senden(t, c, netzURL, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`, kopf); status != http.StatusForbidden {
			t.Errorf("POST /action/task/bereich mit Origin %q: Status %d, erwartet 403 (%s)", origin, status, rumpf)
		}
	}
	if _, err := os.Stat(areaDatei); !os.IsNotExist(err) {
		t.Errorf("areas.json existiert trotz fremder Origin (%v)", err)
	}
}

// TestNetzSchreibenDNSRebinding: stimmen Origin und Host-Header ueberein,
// nennen aber nicht die Adresse, auf der die Verbindung ankam, ist das der
// Rebinding-Fall (fremder Name zeigt auf die Netz-IP) — abgewiesen.
func TestNetzSchreibenDNSRebinding(t *testing.T) {
	_, netzURL, _ := netzMitFreigabe(t, "127.0.0.3")
	port := netzURL[strings.LastIndex(netzURL, ":")+1:]
	req, err := http.NewRequest(http.MethodPost, netzURL+"/mcp", strings.NewReader(mcpRumpf))
	if err != nil {
		t.Fatalf("Anfrage bauen: %v", err)
	}
	req.Host = "evil.example:" + port
	req.Header.Set("Origin", "http://evil.example:"+port)
	req.Header.Set("Content-Type", "application/json")
	resp, err := clientVon(t, "127.0.0.3").Do(req)
	if err != nil {
		t.Fatalf("Anfrage schicken: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Rebinding-Fall: Status %d, erwartet 403", resp.StatusCode)
	}
}

// Nutzerentscheidung 2026-09-25 (Nachtrag zu #46): "erstmal duerfen alle
// verbundenen Geraete editieren". Ausdruecklich per Schluesselwort "alle",
// nicht per Wildcard — "*", "0.0.0.0" und Bereiche bleiben abgelehnt.
func TestParseWriteFreigabeAlle(t *testing.T) {
	alle, ips, err := server.ParseWriteFreigabe(" alle ")
	if err != nil || !alle || len(ips) != 0 {
		t.Fatalf("ParseWriteFreigabe(alle) = %v, %v, %v — erwartet alle=true, keine IPs, kein Fehler", alle, ips, err)
	}
	alle, ips, err = server.ParseWriteFreigabe("192.0.2.63")
	if err != nil || alle || len(ips) != 1 {
		t.Fatalf("ParseWriteFreigabe(IP) = %v, %v, %v — erwartet eine IP, alle=false", alle, ips, err)
	}
	if _, _, err := server.ParseWriteFreigabe("alle,192.0.2.63"); err == nil {
		t.Fatalf("alle gemischt mit IPs: kein Fehler, erwartet Abbruch")
	}
	if _, _, err := server.ParseWriteFreigabe("*"); err == nil {
		t.Fatalf("Wildcard: kein Fehler, erwartet Abbruch")
	}
}

func TestNetzSchreibenAlleGeraete(t *testing.T) {
	areaDatei := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei)
	h := server.NewHandler(server.AppOptions{
		Cwd:       t.TempDir(),
		Assets:    render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS:   assets.FS(),
		WriteAlle: true,
	})
	netzURL := startOn(t, "127.0.0.2", h)
	c := clientVon(t, "127.0.0.4")
	if status, rumpf := senden(t, c, netzURL, "/mcp", mcpRumpf, map[string]string{"Origin": netzURL}); status != http.StatusOK {
		t.Fatalf("POST /mcp von beliebiger IP bei alle: Status %d, erwartet 200 (%s)", status, rumpf)
	}
	if status, rumpf := senden(t, c, netzURL, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`, map[string]string{"Origin": "http://evil.example"}); status != http.StatusForbidden {
		t.Fatalf("fremde Origin bei alle: Status %d, erwartet 403 (%s)", status, rumpf)
	}
	if _, err := os.Stat(areaDatei); !os.IsNotExist(err) {
		t.Errorf("areas.json existiert trotz fremder Origin (%v)", err)
	}
}

// Hinter einem Reverse-Proxy (Traefik mit Basic Auth, Docker-Portmapping)
// nennen Host und Origin den oeffentlichen Namen, nie die Socket-Adresse des
// Containers. --public-origin gibt genau diesen einen Namen frei.
func TestNetzSchreibenHinterProxy(t *testing.T) {
	areaDatei := filepath.Join(t.TempDir(), "areas.json")
	t.Setenv("CC_AREAS_FILE", areaDatei)
	h := server.NewHandler(server.AppOptions{
		Cwd:          t.TempDir(),
		Assets:       render.Assets{DashCSS: assets.DashCSS, BrowserCSS: assets.BrowserCSS, BrowserJS: assets.BrowserJS},
		AssetFS:      assets.FS(),
		WriteAlle:    true,
		PublicOrigin: "https://git.example.com",
	})
	netzURL := startOn(t, "127.0.0.2", h)
	c := clientVon(t, "127.0.0.4")
	oeffentlich := map[string]string{"Host": "git.example.com", "Origin": "https://git.example.com"}
	if status, rumpf := senden(t, c, netzURL, "/mcp", mcpRumpf, oeffentlich); status != http.StatusOK {
		t.Fatalf("POST /mcp ueber den Proxy-Namen: Status %d, erwartet 200 (%s)", status, rumpf)
	}
	for name, kopf := range map[string]map[string]string{
		"fremde Origin":         {"Host": "git.example.com", "Origin": "https://evil.example"},
		"http statt https":      {"Host": "git.example.com", "Origin": "http://git.example.com"},
		"fremder Host (Rebind)": {"Host": "evil.example", "Origin": "https://git.example.com"},
	} {
		if status, rumpf := senden(t, c, netzURL, "/action/task/bereich", `{"projekt":"demo","bereich":"Arbeit"}`, kopf); status != http.StatusForbidden {
			t.Errorf("%s: Status %d, erwartet 403 (%s)", name, status, rumpf)
		}
	}
	if _, err := os.Stat(areaDatei); !os.IsNotExist(err) {
		t.Errorf("areas.json existiert trotz abgewiesener Anfragen (%v)", err)
	}
}

func TestParsePublicOrigin(t *testing.T) {
	for roh, want := range map[string]string{
		"":                         "",
		"https://git.example.com":  "https://git.example.com",
		"https://git.example.com/": "https://git.example.com",
		"http://127.0.0.1:8765":    "http://127.0.0.1:8765",
	} {
		got, err := server.ParsePublicOrigin(roh)
		if err != nil || got != want {
			t.Errorf("ParsePublicOrigin(%q) = %q, %v — erwartet %q", roh, got, err, want)
		}
	}
	for _, roh := range []string{"git.example.com", "ftp://git.example.com", "https://git.example.com/pfad", "https://"} {
		if _, err := server.ParsePublicOrigin(roh); err == nil {
			t.Errorf("ParsePublicOrigin(%q): kein Fehler, erwartet Abbruch", roh)
		}
	}
}

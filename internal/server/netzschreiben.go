package server

// Schreiben ueber die Netz-Adresse fuer freigegebene Client-IPs (#46).
//
// Das Netz-Gate in NewHandler bleibt die erste Schicht: ueber den
// Netz-Listener geht ein Nicht-GET-Request nur durch, wenn die Peer-IP der
// TCP-Verbindung exakt in --write-clients steht. Die Peer-IP kommt aus
// r.RemoteAddr — net/http setzt sie aus dem akzeptierten Socket, bevor ein
// Byte des Requests gelesen ist. X-Forwarded-For, X-Real-IP und Forwarded
// werden nirgends gelesen: dieser Dienst laeuft nicht hinter einem Proxy,
// und jeder dieser Header ist vom Client frei waehlbar.
//
// Die zweite Schicht ist csrfOkFuer: fuer einen so durchgelassenen Request
// muss eine gesetzte Origin exakt http://<Host> sein, und Host muss die
// lokale Adresse der Verbindung sein. Die zweite Bedingung schliesst
// DNS-Rebinding aus — ein fremder Name, der auf die Netz-IP zeigt, bringt
// Origin und Host in Deckung, aber nicht mit der Socket-Adresse.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ParseWriteClients liest die komma-getrennte Liste aus --write-clients.
// Erlaubt sind nur einzelne IPv4-/IPv6-Adressen; ein Bereich (CIDR), eine
// Wildcard oder ein Name ist ein Fehler, der den Eintrag nennt. Leere
// Eintraege (auch eine ganz leere Angabe) werden uebergangen.
func ParseWriteClients(s string) ([]net.IP, error) {
	var out []net.IP
	for _, roh := range strings.Split(s, ",") {
		e := strings.TrimSpace(roh)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			return nil, fmt.Errorf("--write-clients: %q ist ein Netzbereich — nur einzelne IPs sind erlaubt", e)
		}
		if strings.Contains(e, "*") {
			return nil, fmt.Errorf("--write-clients: %q ist eine Wildcard — nur einzelne IPs sind erlaubt", e)
		}
		ip := net.ParseIP(e)
		if ip == nil {
			return nil, fmt.Errorf("--write-clients: %q ist keine IP-Adresse — nur einzelne IPv4-/IPv6-Adressen sind erlaubt", e)
		}
		if ip.IsUnspecified() {
			return nil, fmt.Errorf("--write-clients: %q steht fuer alle Adressen (Wildcard) — nur einzelne IPs sind erlaubt", e)
		}
		out = append(out, ip)
	}
	return out, nil
}

// WriteAlleWort ist das Schluesselwort, mit dem --write-clients das Schreiben
// fuer JEDES Geraet freigibt, das den Netz-Listener erreicht
// (Nutzerentscheidung 2026-09-25, Nachtrag zu #46). Bewusst ein Wort und
// keine Wildcard: "*", "0.0.0.0", "::" und Bereiche bleiben Fehler, damit
// eine Freigabe fuers ganze Netz nie aus Versehen in einer IP-Liste steht.
const WriteAlleWort = "alle"

// ParseWriteFreigabe liest --write-clients: entweder genau "alle" oder eine
// Liste einzelner IPs (ParseWriteClients). "alle" gemischt mit IPs ist ein
// Fehler.
func ParseWriteFreigabe(s string) (alle bool, ips []net.IP, err error) {
	if strings.TrimSpace(s) == WriteAlleWort {
		return true, nil, nil
	}
	for _, e := range strings.Split(s, ",") {
		if strings.TrimSpace(e) == WriteAlleWort {
			return false, nil, fmt.Errorf("--write-clients: %q steht allein oder gar nicht — nicht gemischt mit IPs", WriteAlleWort)
		}
	}
	ips, err = ParseWriteClients(s)
	return false, ips, err
}

// peerFreigegeben meldet, ob die Peer-IP der Verbindung exakt in liste steht.
func peerFreigegeben(liste []net.IP, r *http.Request) bool {
	if len(liste) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false // unbekannte Peer-Adresse: geschlossen bleiben.
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, f := range liste {
		if ip.Equal(f) {
			return true
		}
	}
	return false
}

// ParsePublicOrigin liest --public-origin: die eine Origin, unter der ein
// Reverse-Proxy (Traefik mit Basic Auth, Docker-Portmapping) den Dienst
// ausliefert, etwa https://git.example.com. Hinter dem Proxy nennen Host und
// Origin diesen Namen und nie die Socket-Adresse des Containers; ohne die
// Angabe waere jede Schreibroute dort 403. Leer heisst aus. Erlaubt ist nur
// scheme://host[:port], ohne Pfad.
func ParsePublicOrigin(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("--public-origin: %q ist keine Origin der Form https://host[:port]", s)
	}
	return u.Scheme + "://" + u.Host, nil
}

// netzSchreiberKey markiert im Request-Kontext einen Nicht-GET-Request, den
// das Netz-Gate wegen einer freigegebenen Peer-IP durchgelassen hat.
// Der Wert ist die --public-origin ("" wenn aus).
type netzSchreiberKey struct{}

func alsNetzSchreiber(r *http.Request, publicOrigin string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), netzSchreiberKey{}, publicOrigin))
}

func netzSchreiber(r *http.Request) (publicOrigin string, ok bool) {
	publicOrigin, ok = r.Context().Value(netzSchreiberKey{}).(string)
	return publicOrigin, ok
}

// csrfOkFuer ist die Origin-Pruefung aller Schreibrouten. Fuer Loopback (und
// jeden Request ohne Netz-Freigabe) gilt csrfOk unveraendert; fuer einen vom
// Netz-Gate freigegebenen Request muss eine gesetzte Origin exakt die Adresse
// nennen, auf der die Verbindung ankam.
func csrfOkFuer(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	public, ok := netzSchreiber(r)
	if !ok {
		return csrfOk(origin)
	}
	if origin == "" {
		return true
	}
	// Hinter dem Proxy: Origin und Host muessen beide den freigegebenen
	// oeffentlichen Namen tragen. Host allein schliesst DNS-Rebinding aus,
	// denn ein fremder Name kommt mit seinem eigenen Host-Header an.
	if public != "" && origin == public {
		if u, err := url.Parse(public); err == nil && r.Host == u.Host {
			return true
		}
	}
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return false
	}
	lokal := addr.String()
	return r.Host == lokal && origin == "http://"+lokal
}

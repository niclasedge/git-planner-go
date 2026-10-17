package ghrest

// Bedingte Abrufe (sync-status, #38): jede GET-Antwort mit ETag wird je Token
// und URL gemerkt; der naechste GET derselben URL schickt If-None-Match. Ein
// 304 liefert die gemerkte Antwort — GitHub rechnet ihn nicht gegen das
// Rate-Limit, und der Aufrufer merkt keinen Unterschied ausser der Zeit.
//
// Der Speicher lebt im Prozess. Der Schluessel enthaelt einen Fingerabdruck
// des Tokens, nicht den Token: zwei Konten teilen sich nie eine Antwort, und
// kein Speicherinhalt verraet einen Token.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// speicherDeckel begrenzt die Zahl der gemerkten Antworten; darueber wird der
// aelteste Teil verworfen. Ein Verwerfen kostet nur einen vollen Abruf.
const speicherDeckel = 4000

type gemerkt struct {
	etag string
	body []byte
	kopf http.Header
	zeit time.Time
}

var (
	speicherMu sync.Mutex
	speicher   = map[string]gemerkt{}

	zaehlerAufrufe      atomic.Int64
	zaehlerUnveraendert atomic.Int64
	zaehlerFehler       atomic.Int64

	rateMu          sync.Mutex
	rateVerbleibend = -1
	rateLimit       = -1
	rateReset       string
)

// Statistik sind die Zaehler seit dem Start (oder dem letzten SpeicherLeeren).
type Statistik struct {
	Aufrufe         int64  `json:"aufrufe"`
	Unveraendert    int64  `json:"unveraendert"`
	Fehler          int64  `json:"fehler"`
	RateVerbleibend int    `json:"rate_verbleibend"`
	RateLimit       int    `json:"rate_limit"`
	RateReset       string `json:"rate_reset,omitempty"`
	Gemerkt         int    `json:"gemerkt"`
}

// StatistikLesen liefert die Zaehler.
func StatistikLesen() Statistik {
	speicherMu.Lock()
	n := len(speicher)
	speicherMu.Unlock()
	rateMu.Lock()
	defer rateMu.Unlock()
	return Statistik{Aufrufe: zaehlerAufrufe.Load(), Unveraendert: zaehlerUnveraendert.Load(), Fehler: zaehlerFehler.Load(),
		RateVerbleibend: rateVerbleibend, RateLimit: rateLimit, RateReset: rateReset, Gemerkt: n}
}

// SpeicherLeeren verwirft alle gemerkten Antworten und setzt die Zaehler
// zurueck — fuer Tests und fuer ein vollstaendiges Neuladen.
func SpeicherLeeren() {
	speicherMu.Lock()
	speicher = map[string]gemerkt{}
	speicherMu.Unlock()
	zaehlerAufrufe.Store(0)
	zaehlerUnveraendert.Store(0)
	zaehlerFehler.Store(0)
	rateMu.Lock()
	rateVerbleibend, rateLimit, rateReset = -1, -1, ""
	rateMu.Unlock()
}

func speicherSchluessel(token, adresse string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:6]) + " " + adresse
}

func speicherLesen(schluessel string) (gemerkt, bool) {
	speicherMu.Lock()
	defer speicherMu.Unlock()
	g, ok := speicher[schluessel]
	return g, ok
}

func speicherSchreiben(schluessel string, g gemerkt) {
	speicherMu.Lock()
	defer speicherMu.Unlock()
	if len(speicher) >= speicherDeckel {
		// Die aeltere Haelfte verwerfen.
		grenze := time.Now()
		var summe time.Duration
		for _, e := range speicher {
			summe += grenze.Sub(e.zeit)
		}
		mittel := summe / time.Duration(len(speicher))
		for k, e := range speicher {
			if grenze.Sub(e.zeit) >= mittel {
				delete(speicher, k)
			}
		}
	}
	speicher[schluessel] = g
}

// rateMerken liest die Rate-Limit-Koepfe einer Antwort.
func rateMerken(kopf http.Header) {
	v, errV := strconv.Atoi(kopf.Get("X-RateLimit-Remaining"))
	l, errL := strconv.Atoi(kopf.Get("X-RateLimit-Limit"))
	if errV != nil && errL != nil {
		return
	}
	rateMu.Lock()
	defer rateMu.Unlock()
	if errV == nil {
		rateVerbleibend = v
	}
	if errL == nil {
		rateLimit = l
	}
	if r, err := strconv.ParseInt(kopf.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		rateReset = time.Unix(r, 0).UTC().Format(time.RFC3339)
	}
}

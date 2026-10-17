package appctx

// Sync-Stand und verstaendliche Fehler (sync-status, #38).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SyncStand beschreibt, woher eine Antwort kommt und wie frisch sie ist.
type SyncStand struct {
	// Zeit ist der letzte abgeschlossene Abruf bei GitHub (RFC3339); ob er
	// scheiterte, sagt Fehler.
	Zeit    string `json:"zeit"`
	DauerMS int64  `json:"dauer_ms"`
	// Aufrufe und Unveraendert zaehlen die GitHub-Aufrufe dieses Abrufs;
	// unveraendert sind die mit 304 beantworteten.
	Aufrufe      int64 `json:"aufrufe"`
	Unveraendert int64 `json:"unveraendert"`
	// Geaendert: der Inhalt unterscheidet sich vom vorigen Abruf.
	Geaendert bool `json:"geaendert"`
	// Laeuft: eine Erneuerung im Hintergrund ist unterwegs.
	Laeuft bool   `json:"laeuft"`
	Fehler string `json:"fehler,omitempty"`
}

// KontoGrundKurz macht aus einer Fehlermeldung eines Kontos einen kurzen,
// verstaendlichen Grund. Der volle Text bleibt als Detail erhalten.
func KontoGrundKurz(roh string) string {
	r := strings.TrimSpace(roh)
	switch {
	case r == "":
		return ""
	case strings.Contains(r, "nicht gesetzt"):
		return "nicht gesetzt"
	case strings.HasPrefix(r, "GitHub 401"):
		return "Token ungültig (401)"
	case strings.HasPrefix(r, "GitHub 403"):
		return "Repo-Liste nicht verfügbar (403)"
	case strings.HasPrefix(r, "GitHub 404"):
		return "Repo-Liste nicht gefunden (404)"
	case strings.Contains(r, "dial tcp"), strings.Contains(r, "no such host"), strings.Contains(r, "timeout"),
		strings.Contains(r, "connection refused"), strings.Contains(r, "deadline exceeded"):
		return "GitHub nicht erreichbar"
	}
	if len(r) > 60 {
		return r[:59] + "…"
	}
	return r
}

// InhaltFingerabdruck ist ein kurzer Hash des JSON-Inhalts. encoding/json
// sortiert Map-Schluessel, gleicher Inhalt ergibt also denselben Abdruck.
func InhaltFingerabdruck(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

package render

// Die Huelle der Things-Oberflaeche (things-oberflaeche, #12) unter /things.
//
// Sie traegt nur das Design-System (dash.css, damit Farben und Schrift aus
// derselben Quelle kommen wie in der alten Ansicht), den Knoten, in den das
// Svelte-Bundle montiert, und das Bundle selbst. Keine Daten im Rumpf: die
// Oberflaeche holt sich Leiste und Aufgaben ueber /api/leiste und
// /api/aufgaben, sortiert und gezaehlt von Go. browser.js gehoert NICHT
// hierher — die alte Ansicht unter / bleibt, wie sie ist.

// ThingsPage rendert die Huelle der Things-Oberflaeche.
func ThingsPage(assets Assets) string {
	// Das wirksame Thema steht vor dem ersten Zeichnen fest
	// (things-farben-fortschritt, #41): light oder dark aus kn-theme, sonst
	// (auto oder keine Wahl) nach prefers-color-scheme.
	themeInit := "<script>(function(){var t='light';try{t=localStorage.getItem('kn-theme')||'auto';}catch(e){}" +
		"if(t!=='light'&&t!=='dark')t=matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';" +
		"document.documentElement.dataset.theme=t;})();</script>"
	return "<!DOCTYPE html>\n" +
		`<html lang="de"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>knowledge — Aufgaben</title>` +
		faviconLink +
		themeInit +
		`<style>` + assets.DashCSS + `</style></head><body>` +
		`<div id="kn-svelte-root"></div>` +
		`<script src="/assets/app/app.js"></script>` +
		`</body></html>`
}

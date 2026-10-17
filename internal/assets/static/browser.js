/* ============================================================
   knowledge browser — controller (vanilla, no deps)
   Adapted from the Claude Design handoff "knowledge Browser.html":
   instead of hardcoded constants it reads window.DATA, which
   knowledge.py emits from its collectors. Extra sections vs. the
   original mock: Git (status + gated POST action forms) and Cost.
   ============================================================ */
(function () {
  "use strict";

  var DATA = window.DATA || {};
  var COLL = DATA.coll || {};
  var NAV = DATA.nav || [];
  var OV = DATA.overview || {};
  var GIT = DATA.git || null;
  var COST = DATA.cost || null;
  var REPORTS = DATA.reports || null;  // demo-axk.4: beads maps + report-store entries
  var ACTIVE = DATA.active_project || "";  // current project — threaded into all sub-requests

  /* ---------- helpers ---------- */
  function el(html) { var t = document.createElement("template"); t.innerHTML = String(html).trim(); return t.content.firstChild; }
  function de(n) { try { return Number(n).toLocaleString("de-DE"); } catch (e) { return String(n); } }
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
  function dotStyle(d) { return d === "m" ? ' style="background:var(--mag)"' : ""; }
  function dotCls(d) { return d && d !== "m" ? " " + d : ""; }
  var STATUS = {
    "accepted": ["g", "accepted"], "in progress": ["c", "in progress"],
    "in-progress": ["c", "in progress"], "to do": ["a", "to do"], "todo": ["a", "to do"],
    "done": ["g", "done"], "superseded": ["", "superseded"], "blocked": ["r", "blocked"]
  };
  function statusPair(s) { return STATUS[String(s || "").toLowerCase()] || ["", String(s || "")]; }
  function ktok(n) { n = Number(n) || 0; return n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(Math.round(n)); }
  function extractScript(cmd) {
    var m = String(cmd || "").match(/(\/?[^\s'"]+\.(?:py|sh|bash|zsh|js|mjs|cjs|ts|rb|pl|lua))/);
    return m ? m[1] : null;
  }
  function statusFromXY(xy) {
    var c = String(xy || "");
    if (c.indexOf("D") >= 0) return ["r", "gelöscht"];
    if (c.indexOf("R") >= 0) return ["c", "umbenannt"];
    if (c.indexOf("A") >= 0) return ["g", "staged"];
    if (c.indexOf("M") >= 0) return ["c", "geändert"];
    return ["", c.trim() || "?"];
  }

  function qs(params) {
    return Object.keys(params || {}).filter(function (k) { return params[k] != null && params[k] !== ""; })
      .map(function (k) { return encodeURIComponent(k) + "=" + encodeURIComponent(params[k]); }).join("&");
  }

  /* fetch a whitelisted file (/read) and inject it escaped into `host` */
  function loadFile(kind, params, host) {
    host.innerHTML = '<div class="filebody loading">lädt…</div>';
    fetch("/read?" + qs(Object.assign({ kind: kind, project: ACTIVE }, params || {}))).then(function (r) { return r.json(); }).then(function (d) {
      if (!d || !d.ok) { host.innerHTML = '<div class="dt-empty">konnte nicht laden' + (d && d.error ? ": " + esc(d.error) : "") + "</div>"; return; }
      var mtimeSeg = d.mtime ? " · geändert " + esc(d.mtime) : "";
      var head = '<div class="fmeta">~' + ktok(d.tokens) + " tok · " + de(d.size) + " B" + mtimeSeg + "</div>";
      var note = d.truncated ? '<div class="ftrunc">… gekürzt auf 60k Zeichen</div>' : "";
      // md: render content as clean markdown (kn-md) instead of raw <pre>
      var bodyHtml = (params && params.md)
        ? '<div class="kn-md filebody-md">' + mdToHtml(d.content) + "</div>"
        : '<pre class="filebody">' + esc(d.content) + "</pre>";
      host.innerHTML = head + bodyHtml + note;
    }).catch(function (e) { host.innerHTML = '<div class="dt-empty">Fehler: ' + esc(String(e)) + "</div>"; });
  }

  /* parse a unified diff into aligned side-by-side rows (left = HEAD, right = working) */
  function splitDiffRows(diff) {
    var rows = [], rem = [], add = [], ln = 0, rn = 0;
    function flush() {
      var n = Math.max(rem.length, add.length);
      for (var k = 0; k < n; k++) {
        var L = rem[k], R = add[k];
        rows.push({
          ltype: L ? "dm" : "empty", ln: L ? L.n : "", ltext: L ? L.t : "",
          rtype: R ? "dp" : "empty", rn: R ? R.n : "", rtext: R ? R.t : ""
        });
      }
      rem = []; add = [];
    }
    String(diff).split("\n").forEach(function (line) {
      if (/^@@/.test(line)) {
        flush();
        var m = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
        if (m) { ln = parseInt(m[1], 10); rn = parseInt(m[2], 10); }
        rows.push({ hunk: line });
        return;
      }
      if (/^(diff |index |--- |\+\+\+ |new file|deleted file|similarity|rename |old mode|new mode|Binary )/.test(line)) return;
      var c = line.charAt(0);
      if (c === "-") rem.push({ n: ln++, t: line.slice(1) });
      else if (c === "+") add.push({ n: rn++, t: line.slice(1) });
      else { flush(); var t = c === " " ? line.slice(1) : line; rows.push({ ltype: "ctx", ln: ln++, ltext: t, rtype: "ctx", rn: rn++, rtext: t }); }
    });
    flush();
    return rows;
  }
  function renderSplitDiff(diff) {
    var rows = splitDiffRows(diff);
    if (!rows.length || (rows.length === 1 && !rows[0].hunk && !rows[0].ltext && !rows[0].rtext))
      return '<div class="dt-empty">(keine Änderungen)</div>';
    var body = rows.map(function (r) {
      if (r.hunk) return '<div class="sd-hunk">' + esc(r.hunk) + "</div>";
      return '<div class="sd-row">' +
        '<div class="sd-ln">' + (r.ln || "") + "</div>" +
        '<div class="sd-code ' + r.ltype + '">' + esc(r.ltext) + "</div>" +
        '<div class="sd-ln">' + (r.rn || "") + "</div>" +
        '<div class="sd-code ' + r.rtype + '">' + esc(r.rtext) + "</div></div>";
    }).join("");
    return '<div class="splitdiff">' +
      '<div class="sd-headrow"><div class="sd-h">aktuell · HEAD</div><div class="sd-h">Änderungen · Arbeitsbaum</div></div>' +
      body + "</div>";
  }

  /* fetch a read-only git diff and render it side-by-side into `host` */
  function loadDiff(path, host) {
    host.innerHTML = '<div class="filebody loading">lädt…</div>';
    fetch("/gitdiff?" + qs({ path: path, project: ACTIVE })).then(function (r) { return r.json(); }).then(function (d) {
      if (!d || !d.ok) { host.innerHTML = '<div class="dt-empty">Diff fehlgeschlagen' + (d && d.error ? ": " + esc(d.error) : "") + "</div>"; return; }
      var note = d.truncated ? '<div class="ftrunc">… gekürzt auf 200k Zeichen</div>' : "";
      host.innerHTML = renderSplitDiff(d.diff) + note;
    }).catch(function (e) { host.innerHTML = '<div class="dt-empty">Fehler: ' + esc(String(e)) + "</div>"; });
  }

  var nav = document.getElementById("mp-nav");
  var list = document.getElementById("mp-list");
  var detail = document.getElementById("mp-detail");
  var mp = document.getElementById("mp");
  var state = { coll: null, idx: 0 };

  // suppressPush: set to true during boot replaceState and popstate handler so
  // select() does not push an extra history entry in those cases (CCS-034).
  var suppressPush = false;

  /* RP_EPIC ist das Epic, das die Reports-Ansicht gerade zeigt — "" heisst
     Liste (gkp-ar7.28). Der Zustand steht hier oben statt im Reports-Block,
     weil viewToPath ihn braucht: die Auswahl innerhalb der Ansicht ist Teil
     der URL, nicht bloss eine Anzeigeoption. */
  var RP_EPIC = "";

  /* Eine Ansicht ohne Projektsegment (gkp-92i: GET /<view> ueber ALLE
     Projekte) — die sechs Aufgaben-Ansichten (AV_REG) und Bereiche, dieselbe
     Menge, die appctx.ansichtSegmenteBauen serverseitig kennt. Jede andere
     Ansicht (Skills, Reports, …) hat dort keine eigene Route. */
  function istAnsichtOhneProjekt(id) {
    return !!AV_REG[id] || id === "bereiche";
  }

  /** Map a view ID to the canonical path for history.pushState (CCS-034).
   *  - With active project: /<project>[/<view>] (omit /ov as that is the default)
   *  - Without active project: /<view> fuer die sechs Aufgaben-Ansichten und
   *    Bereiche (gkp-92i/gkp-s9n), sonst "/ueberblick" (die uebrigen Ueberblick-Ansichten;
   *    "/" ist seit repo-leiste #25 die Things-Oberflaeche
   *    kennen keine eigene Route ohne Projekt)
   *  - Reports mit geoeffnetem Epic: /<project>/reports/<epic-id> (gkp-ar7.28)
   */
  function viewToPath(id) {
    if (!ACTIVE) return (id && istAnsichtOhneProjekt(id)) ? "/" + encodeURIComponent(id) : "/ueberblick";
    var base = "/" + encodeURIComponent(ACTIVE);
    if (!id || id === "ov") return base;
    var p = base + "/" + encodeURIComponent(id);
    if (id === "reports" && RP_EPIC) p += "/" + encodeURIComponent(RP_EPIC);
    return p;
  }

  /* Ein Wechsel der Ansicht schliesst das Epic. Ohne das bliebe RP_EPIC gesetzt
     und die naechste Rueckkehr zu /reports oeffnete wieder das alte Epic — die
     URL sagt dann etwas anderes als der Verlauf. */
  function rpVergissEpic(id) {
    if (id !== "reports") RP_EPIC = "";
  }

  /* Reports hat zwei Spalten, nicht drei (gkp-tl2). Die mittlere — .proj-col
     mit Projektkopf und Abschnittsnavigation — wandert dafuer in den Kopf der
     Listenspalte.

     VERSCHOBEN, nicht nachgebaut: dieselben DOM-Knoten wechseln den Platz.
     Damit bleiben Zaehler, Token-Angaben, is-empty/is-active und die in
     navRow gebundenen Klick-Handler exakt das, was sie in den anderen
     Ansichten sind. Ein Nachbau haette all das dupliziert und beim naechsten
     Datenstand still auseinanderlaufen lassen — genau der Datenverlust, der
     hier ausgeschlossen werden sollte.

     Die Projektauswahl ist davon nicht betroffen: die liegt in der 30px
     schmalen .proj-rail, bleibt stehen und bleibt anklickbar. */
  function rpNavHeimat() { return document.querySelector("#kn-sidebar .proj-col"); }

  function rpNavEinhaengen(ziel) {
    var mpnav = document.getElementById("mp-nav");
    var kopf = document.querySelector(".proj-colhd");
    if (!ziel || !mpnav) return;
    // Reihenfolge wie in .proj-col: erst der Projektkopf, dann die Abschnitte.
    if (kopf) ziel.appendChild(kopf);
    ziel.appendChild(mpnav);
    document.body.classList.add("rp-2col");
  }

  function rpNavAushaengen() {
    document.body.classList.remove("rp-2col");
    var heim = rpNavHeimat();
    if (!heim) return;
    var kopf = document.querySelector(".proj-colhd");
    var mpnav = document.getElementById("mp-nav");
    if (kopf && kopf.parentNode !== heim) heim.appendChild(kopf);
    if (mpnav && mpnav.parentNode !== heim) heim.appendChild(mpnav);
  }

  // Breadcrumb elements (may be null in server_test minimal HTML)
  var crumbHere  = document.getElementById("crumb-here");
  var crumbScope = document.getElementById("crumb-scope");
  function setCrumb(here, scope) {
    if (crumbHere)  crumbHere.textContent  = here;
    if (crumbScope) crumbScope.textContent = scope;
  }

  var SPECIAL = { ov: 1, cost: 1, context: 1 };

  /* ---------- nav ---------- */
  function navCount(it) {
    if (it.id === "ov") return "grid";
    if (it.id === "git") return GIT && GIT.files_struct ? GIT.files_struct.length : "·";
    // github: die Zahl steht erst nach dem Netzaufruf fest (GET /github), also
    // hier bewusst keine 0 — die waere eine Behauptung, kein Messwert.
    if (it.id === "github") return "gh";
    if (it.id === "cost") return "$";
    if (it.id === "context") return COLL.context && COLL.context.categories ? COLL.context.categories.length : 0;
    if (it.cnt != null) return it.cnt;
    /* null heisst "es gibt keine Zahl" und ist NICHT die Null.
       appctx.bereicheNav laesst cnt absichtlich weg, sobald jedes Projekt
       zugeordnet ist ("eine Null waere eine Aufforderung ohne Anlass"), und
       "bereiche" hat in COLL keinen Eintrag — der alte Rueckfall erfand hier
       also genau die 0, die Go vermieden hatte, samt is-empty aus navRow.
       Gemessen vor der Reparatur: nc="0", is-empty=true. */
    if (!COLL[it.id]) return null;
    return COLL[it.id].items ? COLL[it.id].items.length : 0;
  }
  /* AKTIV ist die Ansicht, auf der die Navigation gerade steht — ein DATUM und
     kein DOM-Zustand. Der Unterschied ist seit gkp-d52 einer: die Navigation
     kann neu gezeichnet werden, und aus einer geloeschten Zeile ist ihre
     Markierung nicht zurueckzulesen. select() fuehrt den Wert. */
  var AKTIV = "";
  function navRow(it, extraCls, host) {
    var c = navCount(it);
    // Ohne Zahl bleibt die Zelle leer — esc(null) gaebe "" zwar auch her, aber
    // die Unterscheidung wird hier gebraucht und soll deshalb dastehen.
    var hatZahl = c != null;
    var ncInner = (hatZahl ? esc(c) : "") + (it.tok ? '<small>~' + esc(it.tok) + " tok</small>" : "");
    // is-empty: dim entries whose count is exactly the number 0 (not strings like "grid"/"$").
    // "keine Zahl" faellt NICHT darunter: ausgegraut heisst "hier ist nichts
    // drin", und das ist eine Behauptung ueber einen Inhalt, den niemand
    // gezaehlt hat.
    var isEmpty = hatZahl && ((typeof c === "number" && c === 0) || (typeof c === "string" && c === "0"));
    var row = el(
      '<div class="nav-i' + (extraCls ? " " + extraCls : "") + (isEmpty ? " is-empty" : "") +
        (it.id === AKTIV ? " is-active" : "") + '" data-id="' + esc(it.id) + '">' +
        '<span class="nn">' + esc(it.label) + "</span>" +
        '<span class="nc">' + ncInner + "</span>" +
      "</div>"
    );
    row.addEventListener("click", function () { select(it.id); });
    (host || nav).appendChild(row);
  }
  /* gkp-ar7.23: the group heading is a margin label, not a row of its own — it
     sits rotated in a gutter left of its items and costs no vertical space.
     .nav-gt carries the word; the text goes in via textContent, so no escaping
     question arises.

     Seit gkp-d52 eine FUNKTION statt einer Schleife am Rand: die Zahl neben
     einer Aufgaben-Ansicht kann sich nachtraeglich aendern (GitHub kommt per
     GET /ansichten nach), und dann muss die Navigation noch einmal durch
     denselben Weg gezeichnet werden. Eine zweite Stelle, die eine Zahl in eine
     Zelle schreibt, waere eine zweite Gelegenheit, sie anders zu formatieren. */
  function navZeichnen() {
    nav.innerHTML = "";
    NAV.forEach(function (grp) {
      var items = grp.items || [];
      // Single-item groups: the group heading is redundant → render the lone item
      // directly as a heading-styled link (no gutter label).
      if (items.length === 1) {
        navRow(items[0], "nav-solo", nav);
        return;
      }
      var gd = el(
        '<div class="nav-gd"><div class="nav-gl"><span class="nav-gt"></span></div>' +
          '<div class="nav-gb"></div></div>'
      );
      gd.querySelector(".nav-gt").textContent = grp.g;
      var body = gd.querySelector(".nav-gb");
      items.forEach(function (it) { navRow(it, "", body); });
      nav.appendChild(gd);
    });
  }
  navZeichnen();

  /* Die aufgefrischten Zahlen der Aufgaben-Ansichten einsetzen (gkp-d52).
     `items` ist das Feld `nav` aus GET /ansichten — fertig gezaehlt von
     appctx.ansichtenNav, derselben Funktion, die die Zahlen beim Seitenaufbau
     erzeugt hat.

     Hier wird nichts gezaehlt und nichts nachgerechnet. Das ist nicht
     Formsache: die alte Zahl war nicht falsch berechnet, sie war nur ALT — die
     Liste bekommt ihre zweite Quelle nachtraeglich (GET /ansichten, gkp-pf2),
     die Navigation bekam sie nie. Wer das im Browser reparieren wollte, muesste
     dort zaehlen, was Go bereits ausgewaehlt hat, und haette damit zwei
     Zaehlungen, die auseinanderlaufen koennen — genau der Zustand, den dieses
     Ticket beendet.

     Eingesetzt wird in NAV, also in die Daten, und danach wird neu gezeichnet:
     eine Zelle von Hand zu beschriften waere eine zweite Stelle, die eine
     Navigationszahl formatiert. Bleibt die Zahl gleich, bleibt auch das DOM
     stehen — der haeufige Fall ist, dass GitHub nichts beitraegt. */
  function navZahlenUebernehmen(items) {
    var neu = {};
    (items || []).forEach(function (it) { if (it && it.id) neu[it.id] = it; });
    var geaendert = false;
    NAV.forEach(function (grp) {
      (grp.items || []).forEach(function (it) {
        var n = neu[it.id];
        if (!n || (it.cnt === n.cnt && it.dot === n.dot)) return;
        it.cnt = n.cnt;
        it.dot = n.dot;
        geaendert = true;
      });
    });
    if (geaendert) navZeichnen();
    /* Die obere Navigation (viewnav, gkp-s9n) hat in NAV KEINE Entsprechung,
       solange kein Projekt aktiv ist — builddata.go traegt die Gruppe
       "Aufgaben" nur im Projekt-View ein (projectGroups). Ein zweiter,
       eigener Abgleich statt eines Verlasses auf den NAV-Durchlauf oben, der
       in der Sammelansicht sonst nichts faende. */
    VIEWNAV_ITEMS.forEach(function (it) {
      var n = neu[it.id];
      if (!n) return;
      it.cnt = n.cnt;
      it.dot = n.dot;
    });
    viewNavZeichnen();
  }

  /* ---------- obere Navigation der Aufgaben-Ansichten (gkp-s9n) ----------
     Bewusste Abweichung vom Vorbild git-planner-ios (dort sitzt "Agenda" in
     der Sidebar, s. Bead-Notiz) — Begruendung am .viewnav-Kommentar in
     render_page.go. Die Grundmenge ist entweder die fertig gezaehlte Gruppe
     "Aufgaben" aus NAV (Projekt-View: appctx.ansichtenNav hat sie schon
     gebaut) oder, ohne aktives Projekt, ein Platzhalter in appctx' eigener
     Reihenfolge (ansicht.go: Regel.Ordnung 10/20/30/40/50/60) OHNE Zahl, bis
     kopfNavLaden() unten die echte Antwort von GET /ansichten nachtraegt. */
  function viewNavGrundmenge() {
    var grp = null;
    NAV.forEach(function (g) { if (g.g === "Aufgaben") grp = g; });
    if (grp) return grp.items;
    var reihenfolge = ["heute", "demnaechst", "jederzeit", "irgendwann", "eingang", "logbuch"];
    var items = [];
    reihenfolge.forEach(function (id) {
      if (AV_REG[id]) items.push({ id: id, label: AV_REG[id].label, dot: "", cnt: null });
    });
    items.push({ id: "bereiche", label: "Bereiche", dot: "", cnt: null });
    return items;
  }
  /* Leer initialisiert, NICHT ueber viewNavGrundmenge() befuellt: AV_REG ist
     an dieser Stelle im Skript noch {} (es wird erst am Dateiende, nach den
     avAnmelden()-Aufrufen, befuellt). Die echte Erstbefuellung steht deshalb
     im Boot-Block ganz unten, NACH avAnmelden — hier waere sie ein Zugriff auf
     eine Tabelle, die noch gar keine Ansicht kennt. */
  var VIEWNAV_ITEMS = [];
  function viewNavZeichnen() {
    // Kein DOM im node-Testgeschirr (navzahl_browser_test.go & Co. stubben nav/
    // list/detail, aber nicht document) — dieselbe Abwehr wie beim history-API-
    // Zugriff im Boot-Block ("history API not available in test env").
    if (typeof document === "undefined") return;
    var host = document.getElementById("kn-viewnav");
    if (!host) return;
    host.innerHTML = "";
    VIEWNAV_ITEMS.forEach(function (it) {
      var hatZahl = it.cnt != null;
      var btn = el(
        '<a class="vn-btn' + (it.id === AKTIV ? " is-active" : "") + '" href="' +
          esc(viewToPath(it.id)) + '" data-id="' + esc(it.id) + '">' +
          '<span class="vn-l">' + esc(it.label) + "</span>" +
          (it.dot === "r" ? '<span class="vn-dot" title="überfällige Arbeit"></span>' : "") +
          (hatZahl ? '<span class="vn-c">' + esc(it.cnt) + "</span>" : "") +
        "</a>"
      );
      btn.addEventListener("click", function (ev) {
        ev.preventDefault();
        select(it.id);
      });
      host.appendChild(btn);
    });
  }

  /* ---------- Projektliste mit Stats (gkp-s9n) ----------
     Woher die Zahlen kommen, war die offene Frage dieses Beads: MapCounts
     (Total/Open/Closed/Proven/Gaps) gibt es nur je KARTE und nur fuer das
     GEOEFFNETE Projekt (reports_view.go baut sie aus dem bereits geladenen
     Baum) — fuer die UEBRIGEN Projekte in dieser Liste steht das nirgends,
     ausser man riefe bd fuer jedes einzeln auf (das kostet Sekunden, Spec §5
     E6: 14 951 ms fuer neun Projekte seriell). Die einzige Quelle, die
     TATSAECHLICH ueber ALLE Projekte reicht, ist die Ansichten-Aggregation
     selbst (GET /ansichten ohne project, appctx.AnsichtenAlleProjekte) — sie
     ist ohnehin schon unterwegs (kopfNavLaden/avGithubNachziehen holen sie
     fuer die obere Navigation).

     Gezaehlt wird deshalb NICHT MapCounts nach, sondern die bereits
     GEWAEHLTEN Zeilen der fuenf offenen Ansichten (Logbuch bewusst aussen vor:
     es ist gekappt, Regel.Grenze, und eine aus ihm abgeleitete
     "geschlossen"-Zahl unterschaetzte ein aktivitaetsreiches Projekt lautlos —
     das waere die geratene Zahl, vor der die Bead-Notiz warnt). Damit bleiben
     zwei ECHTE Segmente statt vier erfundener: offen (Cyan, wie
     "startklar"/avGrundKlasse) und in Arbeit (Gruen, wie GrundInArbeit dort) —
     dieselbe Farbsprache wie an jeder Zeile selbst, nicht die des Vorbilds
     (das accent/orange auf einer anderen Palette benennt, s. Bead-Notiz zum
     "Gluecksfall" bei den HEX-Werten, nicht den Variablennamen).

     Ist ein Projekt aktiv, deckt der EINE ohnehin laufende Netzaufruf
     (/ansichten?project=X) nur dieses eine Projekt ab — die uebrigen Zeilen
     bleiben dann ohne Balken. Ein zweiter Aufruf allein fuer die Sidebar waere
     hier die teurere Antwort auf dieselbe Frage; ein leerer Balken behauptet
     nichts, ein falscher schon (dieselbe Abwaegung wie bei .pli-bar in
     sidebarCSS). */
  /* Derselbe Wortlaut wie appctx.GrundInArbeit (heute.go) — eine Konstante und
     kein Literal an der Aufrufstelle, damit ein Vertipper hier nicht lautlos
     in "alles ist offen" umschlaegt. */
  var GrundInArbeitJs = "in Arbeit";
  function projStatsAus(ansichten) {
    var out = {};
    var views = ["heute", "demnaechst", "jederzeit", "irgendwann", "eingang"];
    views.forEach(function (vid) {
      var a = ansichten && ansichten[vid];
      if (!a || !a.eintraege) return;
      a.eintraege.forEach(function (e) {
        if (e.ueberschrift) return;
        var p = e.projekt || "";
        if (!out[p]) out[p] = { offen: 0, wip: 0 };
        if ((e.gruende || []).indexOf(GrundInArbeitJs) >= 0) out[p].wip++;
        else out[p].offen++;
      });
    });
    return out;
  }
  function projListZeichnen(stats) {
    // Dieselbe Abwehr wie in viewNavZeichnen — kein document im Testgeschirr.
    if (typeof document === "undefined") return;
    stats = stats || {};
    [].forEach.call(document.querySelectorAll(".pli[data-pli-projekt]"), function (row) {
      var name = row.getAttribute("data-pli-projekt");
      var s = stats[name];
      var statEl = row.querySelector(".pli-stat");
      var barEl = row.querySelector(".pli-bar");
      if (!s || (s.offen + s.wip) === 0) {
        if (statEl) statEl.textContent = "";
        if (barEl) barEl.innerHTML = "";
        return;
      }
      if (statEl) statEl.textContent = s.offen + "/" + s.wip;
      if (barEl) {
        barEl.innerHTML =
          (s.offen > 0 ? '<span style="flex:' + s.offen + ';background:var(--cyan)"></span>' : "") +
          (s.wip > 0 ? '<span style="flex:' + s.wip + ';background:var(--green)"></span>' : "");
      }
    });
  }
  function projStatsLaden(j) {
    projListZeichnen(projStatsAus(j && j.ansichten));
  }

  /* ---------- select a nav entry ---------- */
  function select(id) {
    rpVergissEpic(id);
    /* is-reports zentral abraeumen, nicht in jedem Zweig einzeln: die Klasse
       setzt die Spaltenbreiten (1fr 2fr), und bliebe sie an einer anderen
       Ansicht haengen, waere deren Liste ploetzlich ein Drittel breit. */
    mp.classList.remove("is-reports");
    if (id !== "reports") rpNavAushaengen();
    AKTIV = id;
    [].forEach.call(nav.querySelectorAll(".nav-i"), function (n) {
      n.classList.toggle("is-active", n.dataset.id === id);
    });
    // Die obere Navigation (viewnav, gkp-s9n) baut sich bei jedem Wechsel neu
    // auf — dieselbe Notwendigkeit wie an navZeichnen()/AKTIV oben: die
    // Markierung der geoeffneten Ansicht ist ein DATUM, kein DOM-Zustand.
    viewNavZeichnen();
    if (AV_REG[id]) {
      /* Aufgaben-Ansichten (gkp-7gb): die Ansicht traegt ihren Namen, das
         Zeichnen holt sie ueber den Namen. Das ist der EINZIGE Zweig, den eine
         weitere Ansicht hier braucht — sie meldet sich am Ende der Datei an
         (avAnmelden) und faellt automatisch hier hinein. */
      mp.classList.add("is-overview"); state.coll = null;
      setCrumb(AV_REG[id].label || id, "· Projekt");
      avZeichne(id);
    } else if (id === "bereiche") {
      /* Bereiche (gkp-8ww) ist die EINZIGE Ansicht ueber alle Projekte — die
         Aufgabenlisten zeigen den Baum des geoeffneten Projekts. Deshalb
         "· alle Projekte" in der Brotkrume und deshalb kein AV_REG-Eintrag:
         sie zeichnet keine Aufgabenzeilen, sie zeichnet die Zuordnung. */
      mp.classList.add("is-overview"); state.coll = null;
      setCrumb("Bereiche", "· alle Projekte");
      bzZeichne();
    } else if (id === "git") {
      mp.classList.remove("is-overview"); state.coll = null;
      setCrumb("Git", "· Projekt");
      renderGit();
    } else if (id === "github") {
      // Wie Reports: eine scrollende Spalte, keine zweite Seitenleiste — es
      // gibt kein Detail zum Anklicken, die Titel verlinken nach GitHub.
      mp.classList.add("is-overview"); state.coll = null;
      setCrumb("GitHub", "· Projekt");
      renderGithub();
    } else if (id === "reports") {
      /* Reports ist seit gkp-o71 Master/Detail wie die Dateilisten, nur mit
         Beads: links die nach Map gruppierte Liste, rechts der Inhalt.
         Eigene Klasse statt is-overview — die Listenspalte wird hier
         gebraucht und ist breiter als die 280px der anderen Ansichten.
         state.coll bleibt null: die Auswahl haengt an RP_SEL, nicht an COLL,
         damit die allgemeine Tastatur-Navigation nicht mit hineinregiert.
         Seit gkp-tl2 sind es ZWEI Spalten statt drei: die Abschnittsnavigation
         zieht in den Listenkopf um (rpNavEinhaengen), rechts steht eine
         durchgehende Zeitleiste. */
      mp.classList.remove("is-overview"); mp.classList.add("is-reports");
      state.coll = null;
      setCrumb("Reports", "· Projekt");
      renderReports();
    } else if (id === "context") {
      // Context view: hide list column (full-width detail panel)
      mp.classList.add("is-overview"); state.coll = null;
      setCrumb("Kontext", "· Projekt");
      renderContext();
    } else if (id === "psections") {
      // CLAUDE.md: single file → no 2nd sidebar, full-width markdown detail.
      mp.classList.add("is-overview"); state.coll = "psections"; state.idx = 0;
      setCrumb("CLAUDE.md", "· Projekt");
      list.innerHTML = "";
      var pc = COLL["psections"];
      if (pc && pc.items && pc.items.length) renderDetail(pc, pc.items[0]);
      else detail.innerHTML = '<div class="dt dt-empty">Keine CLAUDE.md</div>';
    } else if (id === "changelog") {
      // CHANGELOG: like CLAUDE.md — no 2nd sidebar, full-width markdown document.
      mp.classList.add("is-overview"); state.coll = "changelog"; state.idx = 0;
      setCrumb("CHANGELOG", "· Projekt");
      list.innerHTML = "";
      renderChangelogDoc();
    } else if (SPECIAL[id]) {
      mp.classList.add("is-overview");
      if (id === "ov") {
        // Route: project view → renderProjectOverview; overview view → renderOverview
        if (ACTIVE) {
          setCrumb(ACTIVE, "· Projekt");
          renderProjectOverview();
        } else {
          setCrumb("Überblick", "· alle Projekte · global");
          renderOverview();
        }
      } else if (id === "cost") {
        setCrumb("Kosten", "· alle Projekte");
        renderCost();
      }
    } else {
      mp.classList.remove("is-overview");
      state.coll = id; state.idx = 0;
      // Update breadcrumb label from the collection title
      var coll = COLL[id];
      if (coll) {
        var scopeLabel = coll.scope === "global" ? "· alle Projekte · global" : "· Projekt";
        setCrumb(coll.title || id, scopeLabel);
      }
      renderList(""); selectItem(0);
    }

    // Push a new history entry for this view unless we are suppressing (boot or popstate).
    // Only push when the path would actually change — avoids duplicate entries.
    if (!suppressPush) {
      try {
        var newPath = viewToPath(id);
        if (newPath !== window.location.pathname) {
          history.pushState({ view: id }, "", newPath);
        }
      } catch (e) { /* history API not available in test env */ }
    }
  }

  /* Open a /read-backed file in the modal (memory/lektion/CLAUDE.md). */
  function openFileModal(title, kind, params) {
    showModal(title, "lädt…");
    fetch("/read?" + qs(Object.assign({ kind: kind, project: ACTIVE }, params || {})))
      .then(function (r) { return r.json(); })
      .then(function (x) { updateModal(title, (x && x.ok) ? x.content : ("Konnte nicht laden" + (x && x.error ? ": " + x.error : ""))); })
      .catch(function () { updateModal(title, "Fehler (Netzwerk)"); });
  }

  /* ---------- minimal, XSS-safe markdown → HTML (modal + report notes) ---------- */
  // demo-axk.4 review Welle 2, finding 1: mdInline builds an href="..."
  // attribute directly from markdown-link URLs; without escaping " (and, for
  // the same reason, ') a URL containing one breaks out of the attribute and
  // turns any subsequent text into an active attribute (e.g. onmouseover=).
  // Escaping both here — mirroring esc()'s five-character set above — closes
  // it for every mdToHtml call site, not just the notes field that made it
  // reachable from untrusted (MCP) input. Escaping " / ' in plain TEXT
  // content (not just attributes) is a no-op visually: &quot;/&#39; render as
  // the same characters " and ' once the browser parses the HTML, so this
  // does not change how any existing mdToHtml output looks.
  function mdEsc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function mdInline(s) {
    // input is already HTML-escaped; we only add safe tags + sanitized links
    s = s.replace(/`([^`]+)`/g, function (_m, c) { return "<code>" + c + "</code>"; });
    s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, function (_m, t, u) {
      return /^(https?:\/\/|\/)/.test(u)
        ? '<a href="' + u + '" target="_blank" rel="noopener">' + t + "</a>"
        : t;
    });
    return s;
  }
  function stripFrontmatter(src) {
    return String(src).replace(/^﻿?---\n[\s\S]*?\n---\n?/, "");
  }
  function mdToHtml(src) {
    var lines = String(src).split("\n"), out = [], para = [], listType = null, items = [];
    var inFence = false, fence = [];
    function flushPara() {
      if (para.length) { out.push("<p>" + mdInline(para.join(" ")) + "</p>"); para = []; }
    }
    function flushList() {
      if (listType) {
        out.push("<" + listType + ">" + items.join("") + "</" + listType + ">");
        items = [];
        listType = null;
      }
    }
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];
      if (/^\s*```/.test(line)) {
        if (inFence) {
          out.push("<pre><code>" + fence.join("\n") + "</code></pre>");
          fence = [];
          inFence = false;
        } else {
          flushPara();
          flushList();
          inFence = true;
        }
        continue;
      }
      if (inFence) { fence.push(mdEsc(line)); continue; }
      var h = line.match(/^(#{1,6})\s+(.*)$/);
      if (h) {
        flushPara();
        flushList();
        var lvl = Math.min(h[1].length + 1, 6);
        out.push("<h" + lvl + ">" + mdInline(mdEsc(h[2])) + "</h" + lvl + ">");
        continue;
      }
      if (/^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
        flushPara();
        flushList();
        out.push("<hr>");
        continue;
      }
      var ol = line.match(/^\s*\d+\.\s+(.*)$/);
      var ul = line.match(/^\s*[-*]\s+(.*)$/);
      if (ol || ul) {
        flushPara();
        var t = ol ? "ol" : "ul";
        if (listType && listType !== t) flushList();
        listType = t;
        var item = (ol ? ol[1] : ul[1]).replace(/^\[ \]\s?/, "☐ ").replace(/^\[[xX]\]\s?/, "☑ ");
        items.push("<li>" + mdInline(mdEsc(item)) + "</li>");
        continue;
      }
      flushList();
      var bq = line.match(/^\s*>\s?(.*)$/);
      if (bq) {
        flushPara();
        out.push("<blockquote>" + mdInline(mdEsc(bq[1])) + "</blockquote>");
        continue;
      }
      if (line.trim() === "") { flushPara(); continue; }
      para.push(mdEsc(line));
    }
    flushPara();
    flushList();
    if (inFence && fence.length) out.push("<pre><code>" + fence.join("\n") + "</code></pre>");
    return out.join("\n");
  }

  /* Shared shell for both showModal (markdown text) and showImageModal
     (demo-axk.8: proof-image full view) — one modal element, two ways
     to fill its body. */
  function modalShell(title, cls, href) {
    var prev = document.getElementById("kn-modal");
    if (prev) prev.remove();
    /* Der Link im Kopf ist die sichtbare Fassung dessen, was Strg-Klick auf
       das Vorschaubild schon konnte. Ohne ihn muss man die Geste kennen. */
    var link = href
      ? '<a class="kn-modal-raw" href="' + esc(href) + '" target="_blank" rel="noopener"' +
        ' title="das Bild allein, ohne die Seite drumherum">Original ↗</a>'
      : "";
    var ov = el(
      '<div class="kn-modal' + (cls ? " " + cls : "") + '" id="kn-modal"><div class="kn-modal-box">' +
        '<div class="kn-modal-h"><b></b>' + link +
        '<button class="kn-modal-x" type="button" aria-label="schliessen">✕</button></div>' +
        '<div class="kn-modal-body kn-md"></div></div></div>',
    );
    ov.querySelector("b").textContent = title;
    ov.addEventListener("click", function (e) { if (e.target === ov) ov.remove(); });
    ov.querySelector(".kn-modal-x").addEventListener("click", function () { ov.remove(); });
    document.body.appendChild(ov);
    return ov;
  }

  function showModal(title, content) {
    var ov = modalShell(title);
    // Content is markdown-rendered to a SAFE subset (all HTML escaped first).
    ov.querySelector(".kn-modal-body").innerHTML = mdToHtml(stripFrontmatter(content));
  }

  /* demo-axk.8: full-size proof-image view. Bypasses mdToHtml on
     purpose (src is a URL, not prose) — esc() still guards the attribute
     value since src is built from report-store data (repo + filename).
     is-image widens the shell: a proof screenshot is 1200-2400px wide and
     unreadable in the 820px box that fits prose. */
  function showImageModal(title, src) {
    var ov = modalShell(title, "is-image", src);
    ov.querySelector(".kn-modal-body").innerHTML =
      '<img class="rp-modal-img" src="' + esc(src) + '" alt="' + esc(title) + '">';
  }

  /* update an already-open modal (used after an async /read fetch) */
  function updateModal(title, content) {
    var ov = document.getElementById("kn-modal");
    if (!ov) { showModal(title, content); return; }
    ov.querySelector("b").textContent = title;
    ov.querySelector(".kn-modal-body").innerHTML = mdToHtml(stripFrontmatter(content));
  }

  /* ---------- list (col 2) ---------- */
  /* Die zuletzt GEZEICHNETE Zeilenfolge, als Index in c.items.
     Der Filter haengt an einem Textfeld, also an Browser-Zustand, den Go nicht
     kennt — die sichtbare Menge kann deshalb nur hier entstehen. Sie entsteht
     genau EINMAL, in renderList, und wird festgehalten: die Tastatur lief
     vorher ueber COLL[state.coll].items.length, also ueber die ungefilterte
     Menge. Gemessen mit Filter "alp" ueber alpha/beta/gamma/alpen: ArrowDown
     landete auf "beta" und "gamma", beide nicht in der Liste, und markActive
     markierte nichts.
     Der Name der Sammlung steht mit dabei: psections und changelog setzen
     state.coll, raeumen die Liste aber leer und rufen renderList gar nicht —
     ohne den Vergleich fuehrte die Tastatur dort durch eine unsichtbare
     Liste. */
  var LISTE_SICHTBAR = { coll: null, idx: [] };
  function renderList(q) {
    var c = COLL[state.coll];
    if (!c) { list.innerHTML = ""; LISTE_SICHTBAR = { coll: null, idx: [] }; return; }
    var rows = c.items.map(function (it, i) { return { it: it, i: i }; });
    // CLAUDE.md: don't list individual sections in the sidebar — one entry, full file below.
    if (c.type === "claude") rows = rows.slice(0, 1);
    if (q) {
      var ql = q.toLowerCase();
      rows = rows.filter(function (r) {
        return (r.it.name + " " + (r.it.title || "") + " " + (r.it.cat || r.it.role || r.it.type || "")).toLowerCase().indexOf(ql) > -1;
      });
    }
    LISTE_SICHTBAR = { coll: state.coll, idx: rows.map(function (r) { return r.i; }) };
    list.innerHTML = "";
    var head = el(
      '<div class="list-head">' +
        '<div class="lt"><b>' + esc(c.title) + '</b><span class="lc">' + c.items.length + "</span>" +
          '<span class="ls">scope: ' + esc(c.scope) + "</span></div>" +
        '<label class="filter"><span>/</span><input type="text" placeholder="filter…" autocomplete="off"></label>' +
      "</div>"
    );
    list.appendChild(head);
    var inp = head.querySelector("input");
    inp.value = q || "";
    inp.addEventListener("input", function () {
      renderList(this.value);
      var f = list.querySelector("input"); f.focus();
      f.setSelectionRange(this.value.length, this.value.length);
    });

    if (!rows.length) { list.appendChild(el('<div class="li-empty">keine Treffer</div>')); return; }

    // Types that show an init-load token count on the right (no status dot)
    var TOK_TYPES = { skill: true, agent: true };
    // Types with no status dot at all (clean list rows)
    var NO_DOT_TYPES = { skill: true, agent: true, know: true, lesson: true, memory: true, claude: true, changelog: true };

    function makeRow(r) {
      var sub;
      if (c.type === "session") {
        sub = (r.it.date ? r.it.date + " · " : "") + "~" + ktok(r.it.total || 0) + " tok · $" + (Number(r.it.cost || 0)).toFixed(2) +
          (r.it.error_count ? " · " + r.it.error_count + " Fehler" : "");
      } else {
        sub = r.it.cat || r.it.role || r.it.date ||
          (c.type === "skill" ? r.it.desc : "") ||
          r.it.type || r.it.scope || "";
      }
      var nm = c.type === "decision" ? String(r.it.name).replace(/^\d+\s—\s/, "")
        : c.type === "claude" ? "CLAUDE.md (ganze Datei)" : r.it.name;

      // Right side: init-load token for skill/agent; decision index for decisions; nothing otherwise
      var right = "";
      if (TOK_TYPES[c.type] && r.it.metaTokens) {
        right = '<div class="li-tok">~' + ktok(r.it.metaTokens) + '<small>tok · init</small></div>';
      } else if (c.type === "decision") {
        right = '<span class="li-ix">' + esc(r.it.id) + "</span>";
      }

      // Lead: no dot for NO_DOT_TYPES; decision uses right instead; others show dot
      var lead = "";
      if (!NO_DOT_TYPES[c.type] && c.type !== "decision") {
        lead = '<span class="dot li-dot' + dotCls(c.accent) + '"' + dotStyle(c.accent) + "></span>";
      }

      var row = el(
        '<div class="li" data-i="' + r.i + '">' + lead +
          '<div><div class="li-n">' + esc(nm) + "</div>" +
          (sub ? '<div class="li-s">' + esc(sub) + "</div>" : "") + "</div>" +
          right +
        "</div>"
      );
      row.addEventListener("click", function () { selectItem(r.i); });
      return row;
    }

    rows.forEach(function (r) { list.appendChild(makeRow(r)); });

    var has = rows.some(function (r) { return r.i === state.idx; });
    if (!has && rows.length) { selectItem(rows[0].i); } else { markActive(); }
  }

  function markActive() {
    [].forEach.call(list.querySelectorAll(".li"), function (n) {
      n.classList.toggle("is-active", +n.dataset.i === state.idx);
    });
  }

  function selectItem(i) { state.idx = i; markActive(); renderDetail(COLL[state.coll], COLL[state.coll].items[i]); }

  /* ---------- detail (col 3) ---------- */
  function badge(cls, txt) { return '<span class="badge ' + cls + '">' + esc(txt) + "</span>"; }
  function metaCell(l, v) { return '<div class="mc"><div class="ml">' + esc(l) + '</div><div class="mv">' + v + "</div></div>"; }

  function renderDetail(c, it) {
    if (!it) { detail.innerHTML = '<div class="dt dt-empty">nichts ausgewählt</div>'; return; }
    // desc = raw text (escaped at render); descHtml = trusted markup built here.
    var b = [], meta = [], secs = [], eyebrow = c.title, desc = it.desc || "", descHtml = "";

    if (c.type === "skill") {
      b.push(badge("g", "scope: " + c.scope));
      if (it.tokens) b.push(badge("c", "~" + ktok(it.tokens) + " tok"));
      meta.push(metaCell("Scope", esc(c.scope)));
      meta.push(metaCell("Token (≈)", "~" + ktok(it.tokens || 0)));
      meta.push(metaCell("Größe", de(it.size || 0) + " B"));
      meta.push(metaCell("Quelle", "<code>~/.claude/skills/" + esc(it.name) + "</code>"));
      var scripts = it.scripts || [];
      meta.push(metaCell("Scripts", scripts.length));
      if (it.desc) { descHtml = esc(it.desc); desc = ""; }
      // SKILL.md wrapped in collapsible <details> to avoid duplicating the description
      if (it.has_md) secs.push({ raw: true, h: "SKILL.md", html: '<div class="filehost" data-read="skill" data-name="' + esc(it.name) + '"></div>' });
      else secs.push({ raw: true, h: "SKILL.md", html: '<div class="dt-empty">keine SKILL.md gefunden</div>' });
      if (scripts.length) {
        var chips = scripts.map(function (sc) {
          return '<button class="fchip" data-read="skillfile" data-name="' + esc(it.name) +
            '" data-path="' + esc(sc.path) + '"><span class="fl">' + esc(sc.lang) + "</span>" +
            esc(sc.path) + ' <span class="fz">' + de(sc.size) + "B</span></button>";
        }).join("");
        secs.push({ h: "Scripts <span class=\"ct\">" + scripts.length + "</span>", html: '<div class="fchips">' + chips + '</div><div class="filehost2"></div>' });
      }
    } else if (c.type === "claude") {
      var doc = c.doc || {};
      b.push(badge("c", "CLAUDE.md")); b.push(badge("", c.scope));
      if (doc.tokens) b.push(badge("c", "~" + ktok(doc.tokens) + " tok"));
      if (doc.managed) b.push(badge("g", "managed-block"));
      meta.push(metaCell("Scope", esc(c.scope)));
      meta.push(metaCell("Token (≈)", "~" + ktok(doc.tokens || 0)));
      meta.push(metaCell("Größe", de(doc.size || 0) + " B"));
      desc = ""; descHtml = "";
      secs.push({ h: "CLAUDE.md (" + esc(c.scope) + ")", html: '<div class="filehost" data-read="' + esc(doc.kind || "") + '" data-md="1"></div>' });
    } else if (c.type === "agent") {
      b.push(badge("c", "agent")); b.push(badge("", c.scope));
      if (it.tokens) b.push(badge("c", "~" + ktok(it.tokens) + " tok"));
      meta.push(metaCell("Token (≈)", "~" + ktok(it.tokens || 0)));
      meta.push(metaCell("Größe", de(it.size || 0) + " B"));
      meta.push(metaCell("Quelle", "<code>~/.claude/agents/" + esc(it.name) + ".md</code>"));
      descHtml = it.role ? esc(it.role) : ""; desc = "";
      // Agent definition wrapped in collapsible <details>
      secs.push({ raw: true, h: "Agent-Definition", html: '<div class="filehost" data-read="agent" data-name="' + esc(it.name) + '"></div>' });
    } else if (c.type === "hook") {
      b.push(badge("", "event")); b.push(badge((it.count || 0) > 2 ? "a" : "g", (it.count || 0) + " hook(s)"));
      meta.push(metaCell("Event", "<code>" + esc(it.name) + "</code>")); meta.push(metaCell("Hooks", it.count || 0));
      var entries = it.entries || [];
      var hookScripts = [];
      if (entries.length) {
        var rows = entries.map(function (e) {
          var sp = extractScript(e.command);
          if (sp && hookScripts.indexOf(sp) < 0) hookScripts.push(sp);
          return '<div class="hookrow"><div class="hh">' +
            '<code>' + esc(e.matcher || "*") + "</code>" +
            '<span class="ht">' + esc(e.type || "command") + "</span></div>" +
            '<div class="hc">' + esc(e.command || "(kein Command)") + "</div></div>";
        }).join("");
        secs.push({ h: "Hooks", html: '<div class="hookrows">' + rows + "</div>" });
      }
      if (hookScripts.length) {
        var hchips = hookScripts.map(function (sp) {
          return '<button class="fchip" data-read="homefile" data-path="' + esc(sp) + '">' + esc(sp) + "</button>";
        }).join("");
        secs.push({ h: "Referenzierte Scripts <span class=\"ct\">unter ~/.claude lesbar</span>", html: '<div class="fchips">' + hchips + '</div><div class="filehost2"></div>' });
      }
      descHtml = "Hook-Event <code>" + esc(it.name) + "</code> mit " + (it.count || 0) + " registrierten Script(s)."; desc = "";
    } else if (c.type === "setting") {
      b.push(badge("c", it.scope || ""));
      meta.push(metaCell("Key", "<code>" + esc(it.name) + "</code>"));
      if (it.scope) meta.push(metaCell("Scope", esc(it.scope)));
    } else if (c.type === "know") {
      b.push(badge("c", "knowledge/")); if (it.type) b.push(badge("", it.type));
      if (it.type) meta.push(metaCell("Typ", esc(it.type)));
      meta.push(metaCell("Pfad", "<code>knowledge/" + esc(it.path || "") + "</code>"));
      if (it.desc) { descHtml = esc(it.desc); desc = ""; }
      if (it.path && /\.(md|txt|csv|json|ya?ml|html)$/i.test(it.path)) {
        secs.push({ h: "Inhalt", html: '<div class="filehost" data-read="knowfile" data-path="' + esc(it.path) + '"></div>' });
      }
    } else if (c.type === "memory") {
      b.push(badge("", "memory"));
      meta.push(metaCell("Datei", "<code>" + esc(it.name) + "</code>"));
      meta.push(metaCell("Pfad", "<code>knowledge/memory/</code>"));
      secs.push({ h: "Inhalt", html: '<div class="filehost" data-read="memory" data-name="' + esc(it.name) + '"></div>' });
      desc = "";
    } else if (c.type === "lesson") {
      b.push(badge("a", "Lektion"));
      meta.push(metaCell("Datei", "<code>" + esc(it.name) + "</code>"));
      meta.push(metaCell("Pfad", "<code>knowledge/</code>"));
      secs.push({ h: "Inhalt", html: '<div class="filehost" data-read="lektion" data-name="' + esc(it.name) + '"></div>' });
      desc = "";
    } else if (c.type === "changelog") {
      b.push(badge("", "CHANGELOG"));
      eyebrow = "CHANGELOG · " + c.scope; desc = "";
      var clBody = it.body ? String(it.body) : "";
      secs.push({ h: "Eintrag", html: clBody
        ? '<div class="kn-md">' + mdToHtml(clBody) + "</div>"
        : '<div class="dt-empty">(kein Inhalt)</div>' });
    } else if (c.type === "decision") {
      var st = statusPair(it.status); b.push(badge(st[0], st[1])); b.push(badge("", "ADR " + esc(it.id)));
      meta.push(metaCell("ID", esc(it.id))); meta.push(metaCell("Status", st[1]));
      if (it.ctx) secs.push({ h: "Kontext", html: '<div class="prose">' + esc(it.ctx) + "</div>" });
      if (it.dec) secs.push({ h: "Entscheidung", html: '<div class="prose">' + esc(it.dec) + "</div>" });
      if (it.body) secs.push({ h: "Inhalt", html: '<pre class="filebody">' + esc(it.body) + "</pre>" });
      desc = "";
    } else if (c.type === "session") {
      b.push(badge("", c.scope));
      if (it.date) b.push(badge("c", it.date));
      b.push(badge("", (it.turns || 0) + " turns"));
      if (it.error_count) b.push(badge("r", it.error_count + " Fehler"));
      if (it.repeat_count) b.push(badge("a", it.repeat_count + " Repeats"));
      var tot = it.total || ((it.input || 0) + (it.output || 0) + (it.cc || 0) + (it.cr || 0));
      var mx = Math.max(it.input || 0, it.output || 0, it.cc || 0, it.cr || 0, 1);
      function bar(l, v, col) {
        return '<div class="row"><span class="tl">' + l + '</span><span class="track"><i style="width:' +
          Math.max(0.4, (v || 0) / mx * 100) + "%;background:" + col + '"></i></span><span class="tv">' + de(v || 0) + "</span></div>";
      }
      secs.push({ h: "Token-Verteilung", html: '<div class="tbar">' +
        bar("Input", it.input, "var(--cyan)") + bar("Output", it.output, "var(--green)") +
        bar("Cache create", it.cc, "var(--amber)") + bar("Cache read", it.cr, "var(--mag)") + "</div>" });
      // clustering: failed commands + repeated commands (from session_analyze)
      if ((it.errors || []).length) {
        var erows = it.errors.map(function (e) {
          return '<div class="clrow cl-err"><div class="clh"><span class="clt r">' + esc(e.tool || "?") + "</span>" +
            (e.command ? '<code>' + esc(e.command) + "</code>" : "") + "</div>" +
            (e.preview ? '<div class="clp">' + esc(e.preview) + "</div>" : "") + "</div>";
        }).join("");
        secs.push({ h: "Fehler <span class=\"ct\">" + it.error_count + "</span>", html: '<div class="clrows">' + erows + "</div>" });
      }
      if ((it.repeats || []).length) {
        var rrows = it.repeats.map(function (r) {
          return '<div class="clrow"><div class="clh"><span class="clt a">' + (r.count || 0) + "×</span>" +
            '<code>' + esc(r.command || "") + "</code></div></div>";
        }).join("");
        secs.push({ h: "Gleiche Commands <span class=\"ct\">projektweit, diese Session beteiligt</span>", html: '<div class="clrows">' + rrows + "</div>" });
      }
      meta.push(metaCell("Session", "<code>" + esc(it.name) + "</code>"));
      if (it.date) meta.push(metaCell("Datum", esc(it.date)));
      meta.push(metaCell("Turns", it.turns || 0)); meta.push(metaCell("Total", de(tot)));
      meta.push(metaCell("Kosten (≈)", "$" + (Number(it.cost || 0)).toFixed(2)));
      desc = "";
    } else {
      b.push(badge(c.accent || "", c.title.toLowerCase()));
    }

    var title = c.type === "decision"
      ? String(it.name).replace(/^\d+\s—\s/, "")
      : c.type === "changelog"
        ? String(it.name).replace(/^#{1,3}\s+/, "")
        : c.type === "claude"
          ? "CLAUDE.md"
          : it.name;
    detail.innerHTML = '<div class="dt">' +
      '<div class="dt-eyebrow">' + esc(eyebrow) + "</div>" +
      '<h2><span class="mono">' + esc(title) + "</span></h2>" +
      (b.length ? '<div class="dt-badges">' + b.join("") + "</div>" : "") +
      (descHtml ? '<p class="dt-desc">' + descHtml + "</p>"
        : (desc ? '<p class="dt-desc">' + esc(desc) + "</p>" : "")) +
      (meta.length ? '<div class="dt-meta">' + meta.join("") + "</div>" : "") +
      // section headers: raw=true → collapsed <details>; otherwise normal dt-sec
      secs.map(function (s) {
        if (s.raw) {
          return '<details class="dt-raw">' +
            '<summary><span class="tw">▶</span><span class="rt">' + s.h + '</span></summary>' +
            s.html + "</details>";
        }
        return '<div class="dt-sec"><div class="sh">' + s.h + "</div>" + s.html + "</div>";
      }).join("") +
    "</div>";
    wireFileHosts(detail);
  }

  /* auto-load inline hosts + wire clickable file/script chips */
  function wireFileHosts(root) {
    var fhost = root.querySelector(".filehost[data-read]");
    if (fhost) loadFile(fhost.dataset.read, { name: fhost.dataset.name, path: fhost.dataset.path, md: fhost.dataset.md }, fhost);
    [].forEach.call(root.querySelectorAll(".fchip[data-read]"), function (chip) {
      chip.addEventListener("click", function () {
        var sec = chip.closest(".dt-sec") || root;
        var host = sec.querySelector(".filehost2") || root.querySelector(".filehost2");
        if (!host) return;
        [].forEach.call((sec.querySelectorAll(".fchip")), function (x) { x.classList.remove("is-active"); });
        chip.classList.add("is-active");
        loadFile(chip.dataset.read, { name: chip.dataset.name, path: chip.dataset.path, md: chip.dataset.md }, host);
      });
    });
  }

  /* ---------- Git: file-list column (col 2) + diff (col 3) ---------- */
  function gFileRow(f) {
    var st = f.untracked ? ["a", "U"] : statusFromXY(f.xy);
    var delta = "";
    if (f.untracked) delta = '<span class="gd-new">neu</span>';
    else {
      if (f.added != null) delta += '<span class="gd-add">+' + f.added + "</span>";
      if (f.deleted != null) delta += '<span class="gd-del">−' + f.deleted + "</span>";
    }
    return '<button class="gfile" data-path="' + esc(f.path) + '" title="' + esc(f.path) + '">' +
      '<span class="badge ' + st[0] + '">' + esc(st[1]) + "</span>" +
      '<span class="gfp">' + esc(f.path) + "</span>" +
      '<span class="gdelta">' + delta + "</span></button>";
  }

  function renderGit() {
    if (!GIT || GIT.available === false) {
      list.innerHTML = "";
      detail.innerHTML = '<div class="dt dt-empty">Git nicht verfügbar' + (GIT && GIT.reason ? ": " + esc(GIT.reason) : "") + "</div>";
      return;
    }
    var br = esc(GIT.branch || "?");
    var sync = [];
    if (GIT.ahead_origin != null) sync.push("↑" + GIT.ahead_origin + "/↓" + (GIT.behind_origin || 0) + " origin");
    if (GIT.ahead_main != null) sync.push("↑" + GIT.ahead_main + " main");

    var fstruct = GIT.files_struct || [];
    var tracked = fstruct.filter(function (f) { return !f.untracked; });
    var untrk = fstruct.filter(function (f) { return f.untracked; });

    var pj = '<input type="hidden" name="project" value="' + esc(ACTIVE) + '">';
    var forms =
      '<form class="gf" method="post" action="/action/add">' + pj +
        '<button type="submit">git add .</button></form>' +
      '<form class="gf" method="post" action="/action/commit">' + pj +
        '<input name="message" placeholder="commit message" required>' +
        '<button type="submit">Commit (lokal)</button></form>' +
      '<form class="gf" method="post" action="/action/push">' + pj +
        '<input type="hidden" name="branch" value="' + br + '">' +
        '<input name="confirm" placeholder="tippe PUSH" autocomplete="off">' +
        '<button type="submit" class="danger">Push → origin/' + br + "</button></form>" +
      '<form class="gf" method="post" action="/action/merge">' + pj +
        '<input name="branch" value="' + br + '">' +
        '<input name="confirm" placeholder="tippe MERGE" autocomplete="off">' +
        '<button type="submit" class="danger">Merge → main</button></form>' +
      '<form class="gf" method="post" action="/action/delete" onsubmit="return confirm(\'Branch wirklich löschen?\')">' + pj +
        '<input name="branch" placeholder="branch">' +
        '<input name="confirm" placeholder="Branch-Name" autocomplete="off">' +
        '<button type="submit">Delete branch</button></form>';

    list.innerHTML =
      '<details class="git-actions" open><summary>Git-Aktionen <span class="ct">localhost · push/merge: Tipp-Token</span></summary>' +
        '<div class="gitforms">' + forms + "</div></details>" +
      '<div class="list-head"><div class="lt"><b>Änderungen</b><span class="lc">' + fstruct.length + "</span>" +
        '<span class="ls">' + br + (sync.length ? " · " + esc(sync.join(" · ")) : "") + "</span></div>" +
        (GIT.shortstat ? '<div class="gss">' + esc(GIT.shortstat) + "</div>" : "") + "</div>" +
      (tracked.length ? '<div class="li-group">Tracked</div>' + tracked.map(gFileRow).join("") : "") +
      (untrk.length ? '<div class="li-group">Untracked</div>' + untrk.map(gFileRow).join("") : "") +
      (!fstruct.length ? '<div class="li-empty">clean — keine Änderungen</div>' : "");

    function showDiff(path, elBtn) {
      [].forEach.call(list.querySelectorAll(".gfile"), function (x) { x.classList.remove("is-active"); });
      if (elBtn) elBtn.classList.add("is-active");
      detail.innerHTML =
        '<div class="gitdiff"><div class="gitdiff-head"><code>' + (path ? esc(path) : "Gesamt-Diff (HEAD)") + "</code>" +
        (GIT.recommend ? '<span class="gdh-rec">' + esc(GIT.recommend) + "</span>" : "") +
        "</div><div class=\"diffhost\"></div></div>";
      loadDiff(path, detail.querySelector(".diffhost"));
    }

    [].forEach.call(list.querySelectorAll(".gfile[data-path]"), function (btn) {
      btn.addEventListener("click", function () { showDiff(btn.dataset.path, btn); });
    });

    var first = list.querySelector(".gfile");
    if (first) showDiff(first.dataset.path, first);
    else detail.innerHTML = '<div class="dt dt-empty">Arbeitsbaum sauber — kein Diff.</div>';
  }

  /* ---------- GitHub: offene Issues + PRs des origin-Remotes ----------
     Anders als jede andere Karte kommen diese Daten NICHT aus window.DATA:
     sie kosten zwei Netzaufrufe und haetten das Oeffnen eines Projekts weiter
     verlangsamt. Geholt wird beim Ansehen, ueber GET /github (Server-seitig
     2 min TTL, internal/collectors/github.go). Fail-open: available:false
     zeigt den Grund, die Seite bleibt heil. */
  function ghAgo(at) { return rpAgoBadge(at); }

  function ghLabels(labels) {
    var GRENZE = 6;
    var alle = labels || [];
    var chips = alle.slice(0, GRENZE).map(function (l) {
      // GitHub liefert die Label-Farbe als Hex ohne '#'. Nur echte
      // 6-stellige Hexwerte durchlassen — alles andere waere ein
      // Style-Injection-Vektor in einem Attribut.
      var col = /^[0-9a-fA-F]{6}$/.test(String(l.color || "")) ? "#" + l.color : "";
      var style = col ? ' style="border-color:' + col + ';color:' + col + '"' : "";
      return '<span class="gh-label"' + style + ">" + esc(l.name) + "</span>";
    }).join("");
    /* Dass gekappt wurde, muss dastehen — sonst sieht eine Zeile mit elf
       Labels aus wie eine mit sechs. Dasselbe Argument wie bei Regel.Grenze /
       a.gekappt in gkp-dop: eine Liste, die aufhoert, ohne zu sagen warum, ist
       von einer Liste, die zu Ende ist, nicht zu unterscheiden.
       Die Zahl bildet der Browser, und das ist hier kein Regelbruch: er kappt
       selbst und kennt beide Laengen. Gerechnet wuerde erst, wenn er
       entschiede, WELCHE sechs. */
    var rest = alle.length - GRENZE;
    if (rest > 0) {
      chips += '<span class="gh-label gh-label-mehr" title="' + esc(alle.length) +
        " Labels · " + esc(rest) + ' nicht gezeigt">+' + esc(rest) + "</span>";
    }
    return chips;
  }

  function ghRow(it, isPR) {
    var badges = "";
    if (isPR) {
      if (it.isDraft) badges += rpBadge("", "draft");
      if (it.reviewDecision === "APPROVED") badges += rpBadge("g", "approved");
      else if (it.reviewDecision === "CHANGES_REQUESTED") badges += rpBadge("r", "changes");
      else if (it.reviewDecision === "REVIEW_REQUIRED") badges += rpBadge("a", "review");
      if (it.headRefName) badges += '<span class="gh-branch">' + esc(it.headRefName) + "</span>";
    }
    return '<div class="gh-row">' +
      '<span class="gh-num">#' + esc(it.number) + "</span>" +
      '<a class="gh-title" href="' + esc(it.url) + '" target="_blank" rel="noopener noreferrer">' + esc(it.title) + "</a>" +
      '<span class="gh-meta">' + (it.author && it.author.login ? esc(it.author.login) : "") + "</span>" +
      '<span class="gh-labels">' + ghLabels(it.labels) + badges + "</span>" +
      ghAgo(it.updatedAt) +
    "</div>";
  }

  function ghSection(title, items, isPR, err) {
    var body;
    if (err) body = '<div class="rp-gap-warn">' + esc(err) + "</div>";
    else if (!items || !items.length) body = '<div class="dt-empty">nichts offen</div>';
    else body = items.map(function (it) { return ghRow(it, isPR); }).join("");
    return '<div class="gh-sec"><div class="sh">' + esc(title) +
      '<span class="lc">' + ((items && items.length) || 0) + "</span></div>" + body + "</div>";
  }

  function renderGithub() {
    list.innerHTML = "";
    detail.innerHTML = '<div class="gh-wrap"><div class="dt-empty">lädt GitHub…</div></div>';
    fetch("/github?" + qs({ project: ACTIVE }))
      .then(function (r) { return r.json(); })
      .then(function (G) {
        var head = '<div class="kn-board-head"><b>' + esc(G.nwo || "GitHub") + "</b>" +
          (G.account ? '<span class="lc">' + esc(G.account) + "</span>" : "") +
          (G.host ? '<span class="ls">' + esc(G.host) + (G.api_host && G.api_host !== G.host ? " → " + esc(G.api_host) : "") + "</span>" : "") +
          (G.cached_age_s != null ? '<span class="ls">gecacht · ' + esc(G.cached_age_s) + "s</span>" : "") +
        "</div>";
        var body;
        if (G.available === false) {
          body = '<div class="rp-gap-warn">GitHub nicht verfügbar' + (G.reason ? ": " + esc(G.reason) : "") + "</div>";
        } else {
          body =
            ghSection("Pull Requests", G.prs, true, G.prs_error) +
            ghSection("Issues", G.issues, false, G.issues_error) +
            '<div class="gh-foot">Nur offene Vorgänge, höchstens ' + esc(G.limit || 30) +
              " je Liste. Task-Tracking läuft über beads (Reports), GitHub zeigt hier nur, was von aussen kommt.</div>";
        }
        detail.innerHTML = '<div class="gh-wrap">' + head + body + "</div>";
      })
      .catch(function (e) {
        detail.innerHTML = '<div class="gh-wrap"><div class="rp-gap-warn">GitHub konnte nicht geladen werden: ' +
          esc(String(e)) + "</div></div>";
      });
  }

  /* ---------- Reports (demo-axk.4): Repo > Map > Sub-bead > Entries ----------
     All gap/supersede computation happened server-side (appctx.CollectReportsData,
     internal/appctx/reports_view.go) — this only renders what DATA.reports already
     decided. Reuses mdToHtml/.kn-md for notes and showModal/showImageModal for the
     proof-image click target — served for real since demo-axk.8 (GET
     /reports/<repo>/assets/<filename>, internal/reportassets). */
  function rpBadge(cls, txt) { return '<span class="badge' + (cls ? " " + cls : "") + '">' + esc(txt) + "</span>"; }
  function rpStatusCls(status) {
    var s = String(status || "").toLowerCase();
    if (s === "closed" || s === "done") return "g";
    if (s.indexOf("progress") >= 0) return "c";
    if (s === "blocked") return "r";
    return "a";
  }
  function rpFmtAt(at) { return String(at || "").replace("T", " ").replace(/\.\d+Z$/, "Z").slice(0, 19); }

  /* rpAgo turns an RFC3339 UTC stamp into "5m" / "2h" / "3d".
     Deliberately computed in the browser, not in Go: the server renders a page
     once and window.DATA is a snapshot, so a server-side "vor 5 Minuten" would
     freeze at render time and quietly lie in a tab left open — the very class
     of staleness that made gkp-ar7.18 hard to spot. Reading Date.now() here
     means the label is right whenever it is drawn. */
  function rpAgo(at) {
    if (!at) return "";
    var t = Date.parse(at);
    if (isNaN(t)) return "";
    var s = Math.floor((Date.now() - t) / 1000);
    if (s < 0) s = 0;                                  // Uhr-Schieflage
    if (s < 60) return s + "s";
    var m = Math.floor(s / 60);
    if (m < 60) return m + "m";
    var h = Math.floor(m / 60);
    if (h < 24) return h + "h";
    var d = Math.floor(h / 24);
    if (d < 365) return d + "d";
    return Math.floor(d / 365) + "y";
  }
  function rpAgoBadge(at) {
    var ago = rpAgo(at);
    return ago ? '<span class="rp-ago" title="' + esc(rpFmtAt(at)) + '">' + esc(ago) + "</span>" : "";
  }

  /* p.image is the server-side absolute path (reports.Entry.Proof[].image,
     e.g. .../.claude/reports/<repo>/assets/<id>-<n>.png) — repo comes from
     the entry itself (reports.Entry.Repo), not from that path, since the
     route takes repo/filename as two separate, independently sanitized
     segments (internal/reportassets). */
  function rpImageURL(repo, absPath) {
    var name = String(absPath || "").split("/").pop();
    return "/reports/" + encodeURIComponent(repo || "") + "/assets/" + encodeURIComponent(name);
  }

  /* Die Auszeichnung fuer einen kaputten Beleg. Sie stand dreimal woertlich in
     dieser Datei — zweimal als Urteil des Stores, einmal als Rueckfall zur
     Laufzeit —, und drei Kopien einer Zeichenkette sind drei Gelegenheiten,
     sie auseinanderlaufen zu lassen. Was die drei Faelle UNTERSCHEIDET, steht
     jetzt am Aufruf und nicht mehr im Markup. */
  function rpMissingHtml(etikett, text) {
    return '<div class="rp-proof rp-proof-missing">' + esc(etikett) +
      " · <code>" + esc(text) + "</code></div>";
  }

  function rpProofHtml(p, repo) {
    if (!p || typeof p !== "object") return "";
    if (typeof p.image === "string") {
      if (p.missing) return rpMissingHtml("Bild fehlt/ungültig", p.image);
      var name = String(p.image).split("/").pop();
      var url = rpImageURL(repo, p.image);
      /* Ein <a href> statt eines <button>, obwohl der normale Klick weiterhin
         das Modal oeffnet (rpBindImages ruft preventDefault). Der Unterschied
         ist alles, was der Browser von sich aus mit einem Link kann und mit
         einem Knopf nicht: Mittelklick und Strg-Klick in einen neuen Tab,
         "Grafik in neuem Tab oeffnen" im Kontextmenue, Ziel in der Statuszeile.
         Die Asset-Route setzt kein Content-Disposition, also zeigt der Browser
         das Bild an, statt es herunterzuladen. */
      return '<a class="rp-proof rp-proof-image" href="' + esc(url) +
        '" target="_blank" rel="noopener" data-rp-image="' + esc(url) +
        '" data-rp-name="' + esc(name) + '" title="Klick: Vollansicht · Strg-Klick: eigener Tab">' +
        '<img class="rp-thumb" src="' + esc(url) + '" alt="' + esc(name) + '" loading="lazy"></a>';
    }
    if (typeof p.file === "string") {
      /* Derselbe Marker, dieselbe Auszeichnung wie beim Bild (gkp-wiz). Seit
         gkp-2ln loest reports.resolveFileProof JEDEN Dateiverweis beim
         Schreiben auf und setzt missing:true, wenn er ins Leere zeigt, leer
         ist oder die Groessengrenze reisst — vorher gab es den Marker nur fuer
         Bilder, und dieser Zweig hat ihn nie angesehen.

         Das ist nicht Kosmetik: das Tor sperrt beim SCHLIESSEN, ein bereits
         geschriebener Bericht mit kaputtem Beleg las sich hier aber weiter wie
         ein Beweis — und diese Ansicht ist die einzige Stelle, an der ein
         Mensch ihn je ansieht.

         Warum hier NICHT steht, WARUM er kaputt ist: der Eintrag weiss es
         nicht. resolveFileProof schreibt "missing": true fuer "nicht
         gefunden" und fuer "leer/zu gross" gleichermassen, ohne Grund. Ein
         Grund, den der Browser dazuerfaende, waere genau die Sorte plausibel
         klingender Behauptung, gegen die der Marker gebaut wurde. Soll die
         Ansicht das sagen, muss ihn erst die Schreibseite festhalten. */
      if (p.missing) return rpMissingHtml("Datei fehlt/ungültig", p.file);
      /* p.copy ist der Vermerk, dass der Store eine Kopie unter
         <repo>/artefacts/ abgelegt hat — nur .dot/.svg bekommen sie. Er steht
         im title und nicht in der Zeile: dass ein Beleg heil ist, ist der
         Normalfall und braucht keine zweite Zeile, aber wer den Pfad sucht,
         findet ihn ohne Umweg ueber die JSONL. */
      var titel = typeof p.copy === "string" && p.copy ? ' title="Kopie: ' + esc(p.copy) + '"' : "";
      return '<div class="rp-proof rp-proof-file"><code' + titel + ">" + esc(p.file) + "</code></div>";
    }
    if (p.cmd != null || p.out != null) {
      var text = (p.cmd ? "$ " + p.cmd + "\n" : "") + (p.out != null ? String(p.out) : "");
      return '<pre class="rp-proof rp-proof-cmd">' + esc(text) + "</pre>";
    }
    return "";
  }

  function rpEntryHtml(e) {
    var wtParts = [];
    if (e.worktree) wtParts.push(e.worktree);
    if (e.branch) wtParts.push(e.branch);
    /* Bild-Belege stehen im Textfluss, nicht in einer Spalte daneben
       (gkp-o71). Bis dahin lagen sie rechts auf fester 40%-Breite — gebaut
       fuer die volle Fensterbreite. Im 2/3-Inhalt blieben davon rund 200px,
       und ein 2240px breiter Screenshot ist darin nur noch ein Fleck.

       Seit gkp-tl2 stehen sie UEBER dem Text statt darunter: in einer
       durchgehenden Zeitleiste scrollt man an einem Eintrag vorbei, und das
       Bild ist das, was einen Eintrag auf einen Blick wiedererkennbar macht —
       der Fliesstext ist es nicht.

       Die Trennung figs/proofs bleibt: ein fehlendes Bild (missing) ist eine
       Textzeile und keine Figur, gehoert also zum Text und nicht in den
       Bildblock. */
    var figs = [], proofs = [];
    (e.proof || []).forEach(function (p) {
      var html = rpProofHtml(p, e.repo);
      if (!html) return;
      if (p && typeof p.image === "string" && !p.missing) figs.push(html);
      else proofs.push(html);
    });
    var head =
      rpBadge(e.kind === "done" ? "g" : "c", e.kind || "?") +
      '<span class="rp-entry-date">' + esc(rpFmtAt(e.at)) + "</span>" +
      rpAgoBadge(e.at) +
      (wtParts.length ? rpBadge("", wtParts.join(" · ")) : "");
    /* Der Titel bleibt UEBER dem Bild, auch wenn das Bild "oben" steht: sonst
       sieht man zwei Screenshots und erfaehrt erst darunter, wozu sie gehoeren
       — am laufenden Stand nachgesehen und dort sofort als falsch erkennbar.
       "Oben" heisst also: vor dem Fliesstext, nicht vor der Ueberschrift. */
    var titel = '<div class="rp-entry-title">' + esc(e.title || "") + "</div>";
    var text =
      (e.impact ? '<div class="rp-entry-impact">' + esc(e.impact) + "</div>" : "") +
      (e.notes ? '<div class="kn-md rp-notes">' + mdToHtml(e.notes) + "</div>" : "") +
      (proofs.length ? '<div class="rp-proofs">' + proofs.join("") + "</div>" : "");
    /* Ohne Bild kein Bildblock — sonst steht unter jedem reinen Textbeleg ein
       leerer Kasten mit eigenem Abstand. */
    var body = figs.length
      ? '<div class="rp-entry-body">' + titel +
          '<div class="rp-entry-figs"><div class="rp-figs-inner">' + figs.join("") + "</div></div>" +
          '<div class="rp-entry-text">' + text + "</div>" +
        "</div>"
      : titel + text;

    /* A superseded entry stays reachable but collapsed: the correction above it
       is what counts, while the retracted version is still evidence of what was
       once claimed and must not disappear (that is the whole point of an
       append-only store). <details> gives that for free — no JS state, and the
       browser's own find-in-page can still open it. */
    if (e.superseded) {
      return '<details class="rp-entry rp-superseded">' +
        '<summary class="rp-entry-head">' + head + rpBadge("r", "ersetzt") + "</summary>" +
        body +
      "</details>";
    }
    return '<div class="rp-entry">' +
      '<div class="rp-entry-head">' + head + "</div>" +
      body +
    "</div>";
  }

  /* ---------- Sektionen (gkp-uc7) ----------
     Die Spalte hat vier Teile, in dieser Reihenfolge: was jetzt anfangbar ist,
     woran gearbeitet wird, was passiert ist, und der Rest. Alle Auswahl-
     Kriterien sind serverseitig entschieden (appctx: ready, in_progress,
     ready_count, timeline) — hier wird gefiltert, nicht gerechnet. */

  function rpSecHead(titel, zusatz) {
    return '<div class="rp-sec-head">' + esc(titel) +
      (zusatz ? '<span class="lc">' + esc(zusatz) + "</span>" : "") + "</div>";
  }

  /* rpPrio zeigt die Prioritaet so, wie bd sie im Terminal zeigt (P0..P4), und
     faerbt nur die oberen zwei ein. Alles einzufaerben macht die Farbe
     bedeutungslos. */
  function rpPrio(p) {
    var n = typeof p === "number" ? p : 2;
    return rpBadge(n <= 0 ? "r" : n === 1 ? "a" : "", "P" + n);
  }

  /* ---------- Epics-Liste (gkp-ar7.25, .29) ----------
     Eine Zeile je Eltern-Bead: Status, Prioritaet, Fortschritt, Belegquote.

     Das fehlte, und es fehlte auf die unauffaellige Art: die Spalte zeigte eine
     Karte nur dann als Block, wenn sie "in Arbeit" war. Stand keine Karte auf
     in_progress — der Normalfall in einem Repo, in dem gerade nichts laeuft —,
     war die Sektion leer und man sah zu KEINEM Epic einen Status. Die Liste
     steht deshalb ganz oben und zeigt jedes Epic: die offenen, und die zuletzt
     geschlossenen als Rueckschau (gkp-ar7.27).

     Gerechnet wird nichts davon hier: m.counts kommt fertig aus appctx
     (countBeads bzw. countOrphanEntries), weil die Belegregel Supersede- und
     Reopen-Faelle kennt, die eine zweite Fassung im Browser nicht haette. */

  /* Fortschritt ist geschlossen/gesamt, nicht belegt/gesamt (Entscheidung des
     Nutzers). Der Balken zeigt genau das eine; die Belegzahl steht als Text
     daneben, damit eine Luecke sichtbar bleibt, ohne den Balken mehrdeutig zu
     machen.

     Ohne Sub-beads gar kein Balken: ein leerer saehe aus wie "0 % erledigt",
     und das waere eine Behauptung ueber ein Epic ohne Inhalt.

     Seit gkp-wsk haengt derselbe Balken auch an einer AUFGABE (b.counts). Ob
     eine Aufgabe einen bekommt, entscheidet allein, OB DAS FELD DA IST: appctx
     fuehrt Counts als Zeiger und laesst ihn an einem Blatt weg (reports_view.go
     — "Der Zeiger ist die Regel ... im Typ ausgedrueckt statt in der Ansicht").
     Hier deshalb nur auf Existenz pruefen und kein 0/0 nachrechnen: sonst
     stuende dieselbe Regel zweimal da und die beiden drifteten auseinander.

     `was` benennt die gezaehlten Dinge, weil "Sub-beads" an einer Aufgabe die
     falsche Ebene benennt — dort sind es ihre Unteraufgaben. */
  function rpFortschritt(c, was) {
    if (!c || !c.total) return "";
    var pct = Math.round((c.closed / c.total) * 100);
    return '<span class="rp-ms-bar" title="' + c.closed + " von " + c.total + " " +
        (was || "Sub-beads") + ' geschlossen">' +
      '<span style="width:' + pct + '%"></span></span>' +
      '<span class="rp-ms-pct">' + pct + " %</span>";
  }

  /* Ein Klick oeffnet das Epic — eigene Ansicht, eigene URL (gkp-ar7.28). */
  function rpBindEpicOpen(root) {
    [].forEach.call(root.querySelectorAll("[data-rp-epic]"), function (row) {
      function go() { rpOeffneEpic(row.getAttribute("data-rp-epic")); }
      row.addEventListener("click", go);
      row.addEventListener("keydown", function (ev) {
        if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); go(); }
      });
    });
  }

  /* Oeffnen und Schliessen aendern NUR den Zustand und zeichnen neu — die
     Ansicht wird nicht nachgeladen, weil sie schon alles hat (window.DATA).
     Der Verlaufseintrag kommt von hier und nicht von select(), weil select()
     die Ansicht wechselt und das hier eine Auswahl INNERHALB der Ansicht ist. */
  function rpOeffneEpic(id) {
    RP_EPIC = String(id || "");
    /* Die Auswahl faellt mit dem Epic-Wechsel weg: sie zeigte auf eine Map,
       die gleich nicht mehr in der Liste steht. rpBaueListe waehlt danach den
       ersten sichtbaren Bead. */
    RP_SEL = null;
    renderReports();
    rpPushPfad();
  }
  function rpSchliesseEpic() {
    RP_EPIC = "";
    RP_SEL = null;
    renderReports();
    rpPushPfad();
  }
  function rpPushPfad() {
    try {
      var p = viewToPath("reports");
      if (p !== window.location.pathname) history.pushState({ view: "reports" }, "", p);
    } catch (e) { /* history API nicht verfuegbar (Testumgebung) */ }
  }

  /* ---------- Historie nach Herkunft (gkp-uc7.8) ----------
     Vier Spuren auf einem Zeitstrahl, je Eintrag beschriftet: report (der
     Report-Store), commit (git log), tracker (bd's Audit-Log — die einzige
     Quelle fuer "wer hat den Bead geschlossen") und pr.
     Drei davon kommen fertig aus appctx (REPORTS.history). Die PR-Spur nicht:
     sie kostet einen Netzaufruf und wird deshalb erst beim Ansehen aus
     GET /github geholt und hier eingemischt. */

  var HIST_LABEL = { report: "Report", commit: "Commit", pr: "PR", tracker: "Tracker" };
  var HIST_ORDER = ["report", "commit", "pr", "tracker"];

  /* Die PR-Spur wird EINMAL je Seitenaufbau geholt, nicht einmal je
     Historie-Abschnitt (gkp-o71). Vorher gab es genau einen Abschnitt pro
     Seite, also war der Unterschied keiner. Seit die Historie am gewaehlten
     Bead haengt, entsteht bei jedem Pfeiltastendruck ein neuer Abschnitt —
     ohne diese Memoisierung waere das ein Request je Tastendruck.
     Die 2-min-TTL serverseitig wuerde die Last abfangen, den Verbindungs-
     aufbau je Tastendruck aber nicht. */
  var RP_GH = null;
  function rpGithub() {
    if (!RP_GH) {
      RP_GH = fetch("/github?" + qs({ project: ACTIVE })).then(function (r) { return r.json(); });
    }
    return RP_GH;
  }

  function rpHistOrigin(it) {
    var cls = it.source === "commit" ? "c" : it.source === "pr" ? "a" : it.source === "tracker" ? "" : "g";
    return '<div class="rp-tl-origin">' +
      rpBadge(cls, HIST_LABEL[it.source] || it.source) +
      '<span class="rp-bead-id">' + esc(it.bead || "") + "</span>" +
      (it.actor ? '<span class="rp-hist-who">' + esc(it.actor) + "</span>" : "") +
    "</div>";
  }

  /* Ein Report-Eintrag: dieselbe Darstellung wie unter einem Bead, plus die
     Herkunft. Ohne die verliert die Zeitleiste genau die Zuordnung, die die
     Gruppierung oben leistet. */
  function rpHistReport(it) {
    var e = it.report || {};
    return rpHistOrigin(it) +
      '<div class="rp-hist-map">' + rpBadge("", e.map || "?") + "</div>" +
      rpEntryHtml(e);
  }

  /* Ein Commit ist eine Zeile, auch wenn er mehrere Beads nennt. Die Ids
     stehen alle in der Herkunftszeile, ab dem fuenften nur noch gezaehlt: ein
     Sammel-Commit mit achtzehn Refs (f1ec67b) macht die Zeile sonst laenger
     als den Betreff, den sie erklaeren soll. */
  var HIST_IDS_MAX = 4;

  function rpHistCommit(it) {
    var c = it.commit || {};
    var ids = c.beads || (it.bead ? [it.bead] : []);
    var sichtbar = ids.slice(0, HIST_IDS_MAX).map(function (b) {
      return '<span class="rp-bead-id">' + esc(b) + "</span>";
    }).join("");
    var rest = ids.length - HIST_IDS_MAX;
    return '<div class="rp-tl-origin">' +
        rpBadge("c", HIST_LABEL.commit) + sichtbar +
        (rest > 0 ? rpBadge("", "+" + rest + " weitere") : "") +
        (it.actor ? '<span class="rp-hist-who">' + esc(it.actor) + "</span>" : "") +
      "</div>" +
      '<div class="rp-hist-line">' +
        '<code class="rp-hash">' + esc(c.short || "") + "</code>" +
        '<span class="rp-hist-title">' + esc(c.subject || "") + "</span>" +
        rpAgoBadge(it.at) +
      "</div>";
  }

  function rpHistTracker(it) {
    var t = it.tracker || {};
    return rpHistOrigin(it) +
      '<div class="rp-hist-line">' +
        (t.from ? rpBadge(rpStatusCls(t.from), t.from) + '<span class="rp-hist-arrow">→</span>' : "") +
        rpBadge(rpStatusCls(t.to), t.to || "?") +
        rpAgoBadge(it.at) +
      "</div>" +
      /* Die Begruendung ist der Grund, warum diese Spur ueberhaupt da ist:
         `bd close --reason` ist die einzige Stelle, an der steht, warum ein
         Bead geschlossen wurde, wenn kein Report-Eintrag existiert. */
      (t.reason ? '<div class="rp-hist-reason">' + esc(t.reason) + "</div>" : "");
  }

  function rpHistPR(it) {
    var p = it.pr || {};
    var badges = "";
    if (p.isDraft) badges += rpBadge("", "draft");
    if (p.reviewDecision === "APPROVED") badges += rpBadge("g", "approved");
    else if (p.reviewDecision === "CHANGES_REQUESTED") badges += rpBadge("r", "changes");
    if (p.headRefName) badges += '<span class="gh-branch">' + esc(p.headRefName) + "</span>";
    return rpHistOrigin(it) +
      '<div class="rp-hist-line">' +
        '<span class="gh-num">#' + esc(p.number) + "</span>" +
        '<a class="rp-hist-title" href="' + esc(p.url) + '" target="_blank" rel="noopener noreferrer">' +
          esc(p.title || "") + "</a>" +
        badges + rpAgoBadge(it.at) +
      "</div>";
  }

  /* Tag und Uhrzeit stehen an jedem Eintrag, den appctx liefert (day/day_label/
     clock, in lokaler Zeit). Nur die PR-Spur kommt am Server vorbei — fuer sie
     wird dieselbe Ableitung hier nachgezogen, so wie beadRefsIn der Spiegel von
     collectors.BeadRefsIn ist. */
  var WOCHENTAGE = ["Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"];
  function histStamp(it) {
    if (it.day) return { day: it.day, label: it.day_label || it.day, clock: it.clock || "" };
    var d = new Date(it.at);
    if (isNaN(d.getTime())) return { day: "", label: "ohne Datum", clock: "" };
    function p2(n) { return (n < 10 ? "0" : "") + n; }
    return {
      day: d.getFullYear() + "-" + p2(d.getMonth() + 1) + "-" + p2(d.getDate()),
      label: WOCHENTAGE[d.getDay()] + ", " + p2(d.getDate()) + "." + p2(d.getMonth() + 1) + "." + d.getFullYear(),
      clock: p2(d.getHours()) + ":" + p2(d.getMinutes()),
    };
  }

  function rpHistItem(it, n) {
    var inner =
      it.source === "commit" ? rpHistCommit(it) :
      it.source === "tracker" ? rpHistTracker(it) :
      it.source === "pr" ? rpHistPR(it) :
      rpHistReport(it);
    return '<li class="rp-tl-node rp-src-' + esc(it.source) + (n % 2 ? " rp-tl-alt" : "") +
      '" data-src="' + esc(it.source) + '">' +
      '<span class="rp-tl-time">' + esc(histStamp(it).clock) + "</span>" +
      '<span class="rp-tl-dot" aria-hidden="true"></span>' +
      '<div class="rp-tl-item">' + inner + "</div>" +
    "</li>";
  }

  /* Tagesgruppen als Laeufe gleicher day-Werte in der schon sortierten Liste —
     keine zweite Gruppierung, die von der Sortierung abweichen koennte.
     Gezaehlt wird NACH dem Filter: sonst behauptet der Kopf eines Tages eine
     Zahl, die in der Liste darunter nicht steht. */
  function rpHistList(items) {
    if (!items.length) return '<div class="dt-empty">nichts in dieser Spur</div>';
    var tage = [], letzter = null;
    items.forEach(function (it) {
      var st = histStamp(it);
      if (!letzter || letzter.day !== st.day) {
        letzter = { day: st.day, label: st.label, items: [] };
        tage.push(letzter);
      }
      letzter.items.push(it);
    });
    /* Die Streifen zaehlen die EINTRAEGE, nicht die Kinder des <ol>. Mit
       :nth-child(odd) waeren die Tagestrenner mitgezaehlt, und die Abwechslung
       waere an jedem Tageswechsel gekippt — zwei helle Zeilen hintereinander,
       genau dort, wo man am ehesten hinsieht. Der Zaehler laeuft deshalb ueber
       alle Tage durch. */
    var n = 0;
    return '<ol class="rp-tl">' + tage.map(function (t) {
      return '<li class="rp-tl-day"><span class="rp-tl-dmark" aria-hidden="true"></span>' +
        '<span class="rp-tl-dd">' + esc(t.label) + "</span>" +
        '<span class="rp-tl-dn">' + t.items.length + " Schritt(e)</span></li>" +
        t.items.map(function (it) { return rpHistItem(it, n++); }).join("");
    }).join("") + "</ol>";
  }

  /* beadRefsIn ist die Zuordnung Text -> Bead-Id, und zwar dieselbe Regel wie
     collectors.BeadRefsIn in Go: Abgleich gegen die BEKANNTEN Ids
     (REPORTS.bead_ids) mit Grenzpruefung links und rechts, kein Regex auf das
     Id-Muster. Ein Muster wie prefix-[a-z0-9]{3,} liefert Fehltreffer
     (gemessen in orbit: "orbit-eigene"), und ein erfundener Bead sieht in der
     Ansicht genauso aus wie ein echter.
     Sie steht hier trotzdem doppelt, weil nur die PR-Spur sie im Browser
     braucht — deren Daten kommen absichtlich nie durch appctx. */
  function isBeadIdChar(ch) { return /[A-Za-z0-9._-]/.test(ch); }

  function beadRefsIn(text, ids) {
    text = String(text || "");
    if (!text || !ids || !ids.length) return [];
    var out = [];
    ids.forEach(function (id) {
      if (!id) return;
      var from = 0;
      while (from + id.length <= text.length) {
        var i = text.indexOf(id, from);
        if (i < 0) return;
        var links = i === 0 || !isBeadIdChar(text.charAt(i - 1));
        var end = i + id.length;
        var rechts = end === text.length || !isBeadIdChar(text.charAt(end));
        if (links && rechts) { out.push(id); return; }
        from = i + 1;
      }
    });
    return out;
  }

  function rpPrHistItems(G, ids) {
    var out = [];
    (G && G.prs || []).forEach(function (p) {
      /* Titel UND Branch: die Konvention setzt die Id in den Titel, aber ein
         Branch wie feature/gkp-uc7.8-historie ist genauso eine Zuordnung. */
      beadRefsIn([p.title, p.headRefName].join("\n"), ids).forEach(function (bead) {
        out.push({
          source: "pr", at: p.updatedAt, bead: bead,
          actor: p.author && p.author.login ? p.author.login : "", pr: p,
        });
      });
    });
    return out;
  }

  /* Dieselbe Ordnung wie appctx.buildHistory: neueste zuerst, bei gleicher
     Sekunde nach Herkunft und Bead — sonst tauschen ein Commit und der
     bd-Statuswechsel dazu (gleiche Sekunde, git hat keine feinere Aufloesung)
     bei jedem Neuzeichnen die Plaetze. */
  function rpHistSort(items) {
    return items.slice().sort(function (a, b) {
      if (a.at !== b.at) return a.at < b.at ? 1 : -1;
      if (a.source !== b.source) return a.source < b.source ? -1 : 1;
      return String(a.bead) < String(b.bead) ? -1 : String(a.bead) > String(b.bead) ? 1 : 0;
    });
  }

  /* Die Historie-Sektion. Der Filter ist Zustand dieser einen Sektion und
     lebt deshalb in ihrer Closure — kein Modul-Zustand, der einen Wechsel des
     Projekts ueberlebt und dann einen Filter zeigt, den niemand gesetzt hat.

     opts.beads schraenkt auf eine Bead-Menge ein (die Epic-Ansicht, gkp-ar7.30).
     Als Parameter statt als zweite Funktion: Spur-Chips, die Erklaerungen zu
     leeren Spuren und das Nachladen der PRs sind in beiden Faellen dieselben,
     und zwei Fassungen davon wuerden auseinanderlaufen. */
  function rpHistorieSec(opts) {
    var nur = opts && opts.beads;
    function gehoert(it) { return !nur || nur[String(it.bead || "")]; }

    var items = rpHistSort((REPORTS.history || []).filter(gehoert));
    var stats = REPORTS.history_stats || {};
    var aktiv = "alle";
    var titel = (opts && opts.titel) || "Historie";

    var sec = el('<div class="rp-sec"></div>');
    var kopf = el('<div class="rp-sec-head"></div>');
    var chips = el('<div class="rp-hist-chips"></div>');
    var notes = el('<div class="rp-hist-notes"></div>');
    var body = el('<div class="rp-timeline"></div>');
    sec.appendChild(kopf);
    sec.appendChild(chips);
    sec.appendChild(notes);
    sec.appendChild(body);

    function zaehlen(src) {
      var n = 0;
      items.forEach(function (it) { if (it.source === src) n++; });
      return n;
    }

    function chipHtml() {
      var out = '<button type="button" class="rp-chip' + (aktiv === "alle" ? " is-on" : "") +
        '" data-f="alle">alle<span class="ct">' + items.length + "</span></button>";
      HIST_ORDER.forEach(function (src) {
        out += '<button type="button" class="rp-chip' + (aktiv === src ? " is-on" : "") +
          '" data-f="' + src + '">' + esc(HIST_LABEL[src]) +
          '<span class="ct">' + zaehlen(src) + "</span></button>";
      });
      return out;
    }

    /* Eine leere Spur hat zwei sehr verschiedene Gruende — es gibt nichts, oder
       die Zuordnung greift nicht. Ohne den Unterschied sieht ein Repo, dessen
       Commits keine Bead-Id nennen, wie ein Defekt aus. */
    function notesHtml() {
      var out = "";
      HIST_ORDER.forEach(function (src) {
        var s = stats[src];
        if (!s || !s.note || zaehlen(src) > 0) return;
        if (aktiv !== "alle" && aktiv !== src) return;
        out += '<div class="rp-hist-note"><b>' + esc(HIST_LABEL[src]) + ":</b> " + esc(s.note) + "</div>";
      });
      var rep = stats.report;
      if (rep && rep.total > rep.shown && (aktiv === "alle" || aktiv === "report")) {
        out += '<div class="rp-hist-note">älter als die ' + rep.shown + " gezeigten Report-Einträge: " +
          (rep.total - rep.shown) + " weitere im Report-Store.</div>";
      }
      /* Ehrlich bleiben bei der Kappung (gkp-ar7.30): buildHistory kappt JE
         SPUR auf 100 Schritte ueber ALLE Karten. Eine auf ein Epic gefilterte
         Liste kann deshalb unvollstaendig sein, ohne dass ihr das anzusehen
         waere — sie endet einfach. Der Hinweis steht nur, wenn eine Spur
         tatsaechlich gekappt wurde. */
      if (nur) {
        var gekappt = HIST_ORDER.filter(function (src) {
          var s = stats[src];
          return s && s.total > s.shown;
        }).map(function (src) { return HIST_LABEL[src]; });
        if (gekappt.length) {
          out += '<div class="rp-hist-note">Die Spur ' + esc(gekappt.join(", ")) +
            " ist projektweit gekappt — zu diesem Epic kann Älteres fehlen.</div>";
        }
      }
      return out;
    }

    function paint() {
      var gezeigt = aktiv === "alle" ? items
        : items.filter(function (it) { return it.source === aktiv; });
      kopf.innerHTML = esc(titel) + '<span class="lc">' +
        (aktiv === "alle" ? items.length + " Schritt(e)" : gezeigt.length + " von " + items.length) +
        "</span>";
      chips.innerHTML = chipHtml();
      notes.innerHTML = notesHtml();
      body.innerHTML = rpHistList(gezeigt);
      rpBindImages(body);
      [].forEach.call(chips.querySelectorAll(".rp-chip"), function (b) {
        b.addEventListener("click", function () { aktiv = b.getAttribute("data-f"); paint(); });
      });
    }
    paint();

    /* Die PR-Spur nachladen, ueber rpGithub gebuendelt (ein Abruf je
       Seitenaufbau statt einer je Abschnitt). Dieselbe Route wie die
       GitHub-Karte, also zahlt wer beide ansieht ohnehin nur einmal.
       Fail-open: ohne gh, ohne Netz oder mit falschem Konto bleibt die
       Historie stehen und die Spur nennt den Grund. */
    rpGithub()
      .then(function (G) {
        if (!G || G.available === false) {
          stats.pr = { shown: 0, total: 0, note: "GitHub nicht verfügbar" + (G && G.reason ? ": " + G.reason : "") };
        } else if (G.prs_error) {
          stats.pr = { shown: 0, total: 0, note: String(G.prs_error) };
        } else {
          var neu = rpPrHistItems(G, REPORTS.bead_ids || []).filter(gehoert);
          items = rpHistSort(items.concat(neu));
          stats.pr = {
            shown: neu.length, total: (G.prs || []).length,
            note: neu.length ? "" : (G.prs || []).length +
              " offene(r) PR nennt keine Bead-Id — Zuordnung über Id im Titel oder Branch",
          };
        }
        paint();
      })
      .catch(function () {
        stats.pr = { shown: 0, total: 0, note: "GitHub-Abruf fehlgeschlagen" };
        paint();
      });

    return sec;
  }

  /* ---------- Reports: Liste + Inhalt (gkp-o71) ----------
     Links die Beads nach Map gruppiert, rechts der gewaehlte Bead. Davor war
     die Ansicht eine einzige scrollende Spalte mit fuenf Sektionen
     uebereinander (Epics · Startklar · In Arbeit · Historie · Weitere Karten);
     wer einen bestimmten Bead suchte, scrollte.

     Was aus den Sektionen wurde — sie sind umverteilt, nicht geloescht:
       Epics-Bilanz   -> Zahlen an der Map-Gruppenzeile und im Map-Inhalt
       Startklar      -> Abzeichen an der Bead-Zeile
       In Arbeit/Rest -> die Gruppierung selbst
       Historie       -> haengt am gewaehlten Bead bzw. an der gewaehlten Map,
                         statt einmal quer ueber alles zu laufen

     Die REIHENFOLGE kommt weiterhin aus Go (appctx.sortMaps und
     buildReportsMap, beide last_change absteigend) — hier wird gefiltert und
     gezeichnet, nicht gerechnet. Das ist dieselbe Arbeitsteilung, wegen der
     internal/appctx existiert. */

  var RP_SEL = null;    // {kind:"map"|"bead", map:<id>, bead:<id>}
  var RP_ROWS = [];     // nur die BEAD-Zeilen, in Anzeigereihenfolge
  var RP_MAPIDS = [];   // die tatsaechlich gezeichneten Map-Zeilen
  var RP_FILTER = "";

  /* Der Schalter "geschlossene ausblenden" (gkp-e58). Standard AN, weil die
     Ansicht die LAUFENDE Arbeit zeigen soll: in einer Karte mit 66 Beads sind
     die geschlossenen die Mehrheit, und das Offene steht zwischen ihnen.
     Ausgeblendet heisst nicht weg — ein Klick holt sie zurueck, und der Kopf
     nennt jederzeit die Zahl.

     Die Wahl haengt in localStorage und nicht in der URL: sie ist eine
     Gewohnheit des Nutzers, kein Teil des Ortes, auf den ein Link zeigt.
     Fehlt der Speicher (privater Modus, gesperrte Site-Daten), gilt der
     Standard — ein Wurf ins Leere darf die Ansicht nicht kosten. */
  var RP_ZU_SCHLUESSEL = "gkp.reports.nurOffen";
  var RP_NUR_OFFEN = (function () {
    try { return localStorage.getItem(RP_ZU_SCHLUESSEL) !== "0"; }
    catch (e) { return true; }
  })();

  function rpNurOffenSetzen(an) {
    RP_NUR_OFFEN = !!an;
    try { localStorage.setItem(RP_ZU_SCHLUESSEL, RP_NUR_OFFEN ? "1" : "0"); }
    catch (e) { /* ohne Speicher gilt die Wahl nur fuer diese Sitzung */ }
  }

  function rpAnzEintraege(n) { return n === 1 ? "1 Eintrag" : n + " Einträge"; }

  /* Ist ein Epic geoeffnet, zeigt die Liste nur dessen Beads (gkp-ar7.30) —
     dieselbe Regel wie vorher, nur wirkt sie jetzt auf die Liste statt auf
     eine Spalte. */
  function rpSichtbareMaps() {
    var maps = (REPORTS && REPORTS.maps) || [];
    if (!RP_EPIC) return maps;
    return maps.filter(function (m) { return m.id === RP_EPIC; });
  }

  function rpTrifft(text, q) {
    return !q || String(text || "").toLowerCase().indexOf(q) > -1;
  }

  /* Warum die Liste leer ist, ist drei verschiedene Auskuenfte wert. "Keine
     Epics in diesem Repo" war fuer alle drei da und in zweien davon schlicht
     falsch: bei einem geoeffneten, fertig gearbeiteten Epic sagte sie, es gebe
     keine Epics — waehrend eines offen VOR einem stand. Eine Ansicht, die den
     eigenen Filter nicht als Grund nennt, schickt den Leser das Repo suchen. */
  function rpLeerText(q, versteckt) {
    if (q) return "keine Treffer";
    if (RP_NUR_OFFEN && versteckt) {
      return "nichts Offenes — " + versteckt + " geschlossene(r) Bead(s) ausgeblendet";
    }
    return "Keine Epics in diesem Repo.";
  }

  function renderReports() {
    if (!REPORTS || REPORTS.available === false) {
      list.innerHTML = "";
      detail.innerHTML = "";
      detail.appendChild(el(
        '<div class="rp-wrap"><div class="kn-board-head"><b>Reports</b></div>' +
          '<div class="dt-empty" style="padding:16px 0">Beads/Reports nicht verfügbar' +
          (REPORTS && REPORTS.error ? ": " + esc(REPORTS.error) : "") + "</div></div>",
      ));
      RP_ROWS = []; RP_MAPIDS = []; RP_SEL = null;
      return;
    }
    rpBaueListe();
  }

  /* Eine Filterung, zwei Spalten: die Gruppen entstehen HIER einmal und gehen
     dann sowohl an die Liste als auch an die Zeitleiste. Zweimal filtern
     hiesse, dass links und rechts bei jeder kuenftigen Aenderung an genau einer
     der beiden Stellen auseinanderlaufen koennen — und dann zeigt der Sprung
     ins Leere. Aus demselben Grund sitzt auch der Status-Filter hier und nicht
     als zweiter Durchgang daneben.

     Rueckgabe traegt die Zahl der ausgeblendeten Beads mit: eine Liste, die
     Zeilen weglaesst, ohne zu sagen wieviele, behauptet Vollstaendigkeit. */
  function rpGruppen(maps, q) {
    var gruppen = [];
    var res = { ids: {}, versteckt: 0 };
    maps.forEach(function (m) {
      /* Trifft der Filter die MAP, bleiben alle ihre Beads stehen — sonst
         muesste man den Epic-Titel und den Bead-Titel gleichzeitig treffen,
         um eine Karte ganz zu sehen. */
      var mapTrifft = rpTrifft(m.id + " " + (m.title || "") + (m.unknown ? " unbekannte map" : ""), q);
      /* Eine unbekannte Map bleibt IMMER stehen: sie hat keine bd-Zustaende,
         die man ausblenden koennte, und sie ist eine Beanstandung (die
         Report-Datei zeigt auf kein Epic mehr) — kein erledigter Stand. */
      var zuAus = RP_NUR_OFFEN && !m.unknown;
      var einer = rpBaumAuswahl(m.beads, mapTrifft, q, zuAus, res);
      if (!mapTrifft && !einer) return;
      // Karte ohne offene Arbeit faellt mit heraus. Sonst stuenden
      // Gruppenzeile und Kartenzeile ueber einer leeren Gruppe — dreimal
      // Ueberschrift fuer nichts.
      if (zuAus && !einer) return;
      gruppen.push({ m: m, zeilen: rpFlach(m.beads, res.ids, 0, []) });
    });
    return { gruppen: gruppen, versteckt: res.versteckt };
  }

  /* ---------- Eine Schreibantwort einsetzen (gkp-8a0) ----------
     Jede Schreiboperation (POST /action/task/*) antwortet mit dem neu gebauten
     Ast der betroffenen Aufgabe, und die Ansicht ersetzt genau diesen Teil
     (Spec §7.2). Kein Vollreload, kein Polling, kein SSE — window.DATA war
     bis hierher ein Schnappschuss ohne Nachladeweg.

     Der Helfer taucht NICHT selbst, er tauscht nur: gezeichnet wird danach von
     dem, der ihn gerufen hat. Und er rechnet nichts nach. Die Zahlen kommen
     fertig aus appctx (counts je Vorfahr), die Reihenfolge steht in
     ast.kinder, so wie Go sie sortiert hat — eine zweite Sortierregel hier
     waere genau die Doppelung, die dieses Frontend vermeidet.

     Alles oder nichts: kann der Ast nicht EXAKT eingesetzt werden — die Karte
     fehlt, ein Vorfahr fehlt, die Kinderfolge nennt eine Aufgabe, die dieser
     Browser noch nie gesehen hat —, wird gar nichts angefasst und false
     zurueckgegeben. Der Aufrufer laedt dann nach. Eine halb aktualisierte Liste
     waere schlimmer als eine veraltete: an ihr ist nicht zu sehen, welcher Teil
     stimmt. */
  function rpTeilbaumEinsetzen(tb) {
    if (!REPORTS || !tb || !tb.aufgabe || !tb.aufgabe.id) return false;
    var weg = tb.weg || [];
    if (!weg.length || weg[0].id !== tb.karte) return false;

    var karte = null;
    (REPORTS.maps || []).forEach(function (m) { if (m.id === tb.karte) karte = m; });
    if (!karte) return false;

    /* Erst rechnen, dann schreiben: die neuen Kinderlisten entstehen
       vollstaendig, bevor die erste davon eingehaengt wird. Sonst stuende nach
       einem Fehlschlag auf halber Hoehe ein Baum, der weder alt noch neu ist. */
    var stufen = [];
    var knoten = karte;
    for (var i = 0; i < weg.length; i++) {
      if (!knoten || knoten.id !== weg[i].id) return false;
      var alt = i === 0 ? karte.beads : knoten.children;
      var neu = rpKinderFolge(alt, weg[i].kinder, tb.aufgabe);
      if (!neu) return false;
      stufen.push({ knoten: knoten, kinder: neu, wurzel: i === 0 });
      if (i + 1 < weg.length) knoten = rpKindMitId(neu, weg[i + 1].id);
    }
    stufen.forEach(function (s, i) {
      s.knoten.counts = weg[i].counts;
      if (s.wurzel) s.knoten.beads = s.kinder;
      else s.knoten.children = s.kinder;
    });
    return true;
  }

  /* Eine Ebene in der Reihenfolge, die Go geliefert hat: bekannte Aufgaben
     bleiben dieselben Objekte, die geschriebene wird ersetzt (oder ist neu und
     kommt hinzu). null heisst "nicht exakt moeglich" — dann nennt die Folge
     eine Aufgabe, die dieser Browser nicht hat, weil sie inzwischen woanders
     entstanden ist. Sie zu ueberspringen hiesse, eine Zeile zu verschlucken,
     von der Go gerade gesagt hat, dass es sie gibt. */
  function rpKinderFolge(alt, ids, neueAufgabe) {
    var raus = [];
    for (var i = 0; i < (ids || []).length; i++) {
      if (ids[i] === neueAufgabe.id) { raus.push(neueAufgabe); continue; }
      var b = rpKindMitId(alt, ids[i]);
      if (!b) return null;
      raus.push(b);
    }
    return raus;
  }

  function rpKindMitId(beads, id) {
    var raus = null;
    (beads || []).forEach(function (b) { if (b.id === id) raus = b; });
    return raus;
  }

  /* ---------- Der Aufgabenbaum als Zeilenfolge (gkp-wsk) ----------
     Eine Aufgabe kann Unteraufgaben haben, die wieder welche haben — beliebig
     tief, das Modell kennt keine Grenze (Spec §4.1). Geplaettet wird EINMAL,
     und Liste wie Zeitleiste bekommen dasselbe Ergebnis: die beiden laufen 1:1
     nebeneinander (siehe rpBaueZeitleiste), jede Zeile links hat rechts einen
     Block. Zwei Abstiege waeren zwei Gelegenheiten, das zu verlieren.

     Die Reihenfolge kommt aus Go — appctx.buildReportsBeads sortiert JEDE
     Ebene nach letzter Aenderung. Hier kommt nur der Abstieg dazu: Elternteil,
     dann seine Nachfahren. */
  function rpFlach(beads, sichtbar, tiefe, raus) {
    (beads || []).forEach(function (b) {
      /* Ein unsichtbarer Knoten nimmt seinen ganzen Ast mit. Das ist keine
         Abkuerzung, sondern die Umkehrung der Regel aus rpBaumAuswahl: dort
         bleibt ein Elternteil stehen, sobald IRGENDEIN Nachfahre stehen
         bleibt — faellt er trotzdem weg, ist unter ihm nichts mehr. */
      if (sichtbar && !sichtbar[b.id]) return;
      raus.push({ b: b, tiefe: tiefe });
      rpFlach(b.children, sichtbar, tiefe + 1, raus);
    });
    return raus;
  }

  /* Welche Beads eines Baums stehen bleiben — als Id-Menge, nicht als
     gefilterter Baum. Eine gefilterte KOPIE haette neue Objekte, und b.counts
     an einem davon saehe aus, als zaehle es den gezeigten Ausschnitt. Es zaehlt
     aber den ganzen Teilbaum (appctx.countBeads) und soll das auch: der
     Fortschritt einer Aufgabe haengt nicht daran, was der Filter gerade zeigt.

     Ein Elternteil bleibt stehen, sobald irgendein Nachfahre stehen bleibt.
     Sonst haengt eine eingerueckte Zeile unter nichts, und ihre Tiefe waere
     eine Aussage ueber einen Ast, den die Liste verschweigt. */
  function rpBaumAuswahl(beads, mapTrifft, q, zuAus, res) {
    var einer = false;
    (beads || []).forEach(function (b) {
      var kinder = rpBaumAuswahl(b.children, mapTrifft, q, zuAus, res);
      var selbst = mapTrifft || rpTrifft(b.id + " " + (b.title || ""), q);
      if (selbst && zuAus && b.status === "closed") {
        selbst = false;
        /* Gezaehlt wird nur, was der SCHALTER ausblendet, und nur wenn die
           Zeile wirklich verschwindet: ein geschlossener Elternteil mit offener
           Unteraufgabe steht weiter da und ist nicht "ausgeblendet". */
        if (!kinder) res.versteckt++;
      }
      if (selbst || kinder) {
        res.ids[b.id] = true;
        einer = true;
      }
    });
    return einer;
  }

  function rpBaueListe() {
    var maps = rpSichtbareMaps();
    var q = RP_FILTER.trim().toLowerCase();
    var ausw = rpGruppen(maps, q);
    var gruppen = ausw.gruppen;

    list.innerHTML = "";
    RP_ROWS = [];
    RP_MAPIDS = [];

    var worktrees = (REPORTS.worktrees || []).map(function (w) {
      return w.path ? String(w.path).split("/").pop() : "?";
    }).join(" · ");

    /* Die Abschnitte stehen VOR der klebenden Kopfzeile, nicht darin: sonst
       kleben 226 gemessene Pixel Navigation dauerhaft ueber der Liste und
       nehmen ein Viertel der Spalte mit, die gerade erst dazugewonnen wurde.
       So scrollt die Navigation weg, Filter und Map-Gruppe bleiben. */
    var kopfnav = el('<div class="rp-kopfnav"></div>');
    list.appendChild(kopfnav);
    rpNavEinhaengen(kopfnav);

    var kopf = el(
      '<div class="list-head rp-kopf">' +
        '<div class="lt"><b>' + esc(REPORTS.repo_name || "Reports") + "</b>" +
          '<span class="lc">' + (gruppen.length === maps.length
            ? maps.length + " Epic(s)"
            : gruppen.length + " von " + maps.length + " Epics") + "</span>" +
          (worktrees ? '<span class="ls">' + esc(worktrees) + "</span>" : "") +
        "</div>" +
        '<label class="filter"><span>/</span>' +
          '<input type="text" placeholder="bead oder titel…" autocomplete="off"></label>' +
        '<label class="rp-zu"><input type="checkbox"' + (RP_NUR_OFFEN ? " checked" : "") + ">" +
          "<span>geschlossene ausblenden</span>" +
          (ausw.versteckt ? "<em>" + ausw.versteckt + " Bead(s) aus</em>" : "") +
        "</label>" +
      "</div>",
    );
    list.appendChild(kopf);
    /* Die Map-Zeilen kleben (.li-group, position:sticky). Ohne Offset kleben
       sie am Fensterrand UNTER dieser Kopfzeile, die selbst klebt und hoeher
       liegt — die Gruppe und der erste Bead darunter waren dann unsichtbar.
       Die Hoehe haengt von Titel, Zaehler und Worktree-Zeile ab, wird also
       gemessen statt geraten; browser.css liest sie als --rp-headh. */
    list.style.setProperty("--rp-headh", kopf.offsetHeight + "px");
    // Ausdruecklich .filter input: seit dem Schalter daneben (gkp-e58) steht
    // im Kopf mehr als ein <input>, und ein blosses "input" traefe kuenftig
    // still das falsche.
    var inp = kopf.querySelector(".filter input");
    inp.value = RP_FILTER;
    inp.addEventListener("input", function () {
      RP_FILTER = this.value;
      rpBaueListe();
      var f = list.querySelector(".filter input");
      if (f) { f.focus(); f.setSelectionRange(RP_FILTER.length, RP_FILTER.length); }
    });
    kopf.querySelector(".rp-zu input").addEventListener("change", function () {
      rpNurOffenSetzen(this.checked);
      rpBaueListe();
    });

    gruppen.forEach(function (g) {
      RP_MAPIDS.push(g.m.id);
      list.appendChild(rpMapZeile(g.m));
      /* Die Karte selbst ist der erste EINTRAG der Gruppe, nicht nur ihre
         Ueberschrift. Vorher fuehrte nur ein Klick auf die Gruppenzeile zum
         Plan-Graphen, und die Pfeiltasten liefen an ihm vorbei — er war
         faktisch unauffindbar. Jetzt ist er der erste Halt jeder Gruppe. */
      RP_ROWS.push({ map: g.m.id, bead: null, karte: true });
      list.appendChild(rpKarteZeile(g.m));
      if (!g.zeilen.length) {
        list.appendChild(el('<div class="rp-li-empty">' +
          (g.m.unknown ? "keine bd-Kinder — Einträge stehen unter der Map" : "keine Sub-beads") + "</div>"));
        return;
      }
      /* Unteraufgaben sind eigene Zeilen, nicht ein Anhaengsel ihrer Aufgabe:
         die Pfeiltaste laeuft durch sie hindurch, und der Sprung nach rechts
         findet fuer jede einen Block. */
      g.zeilen.forEach(function (z) {
        RP_ROWS.push({ map: g.m.id, bead: z.b.id });
        list.appendChild(rpBeadZeile(g.m, z.b, z.tiefe));
      });
    });

    if (!RP_MAPIDS.length) {
      list.appendChild(el('<div class="li-empty">' + esc(rpLeerText(q, ausw.versteckt)) + "</div>"));
    }

    rpBaueZeitleiste(gruppen, q, ausw.versteckt);

    /* Faellt die Auswahl aus der gefilterten Liste, ruecken wir auf den ersten
       sichtbaren Eintrag — dieselbe Regel, die renderList fuer die Dateilisten
       schon anwendet. Sonst zeigt der Inhalt etwas, das links nicht mehr
       auffindbar ist. */
    /* Die erste Zeile ist seit gkp-ar7 die KARTE, nicht ein Bead — ohne diese
       Unterscheidung stand hier {kind:"bead", bead:null}, was auf nichts passt:
       beim Aufbau war dann gar nichts markiert. */
    if (rpSelSichtbar()) rpWaehle(RP_SEL, true);
    else if (RP_ROWS.length) rpWaehle(rpRowSel(RP_ROWS[0]), true);
    else if (RP_MAPIDS.length) rpWaehle({ kind: "map", map: RP_MAPIDS[0] }, true);
    else RP_SEL = null;
  }

  function rpMapZeile(m) {
    var c = m.counts || {};
    var zahlen = m.unknown
      ? c.total + " Bead(s) · " + c.proven + " belegt"
      : c.closed + "/" + c.total + (c.gaps ? " · " + c.gaps + " ohne Beleg" : "");
    var row = el(
      '<div class="li-group rp-li-map" data-rp-map="' + esc(m.id) + '" role="button" tabindex="0">' +
        '<span class="rp-li-mt">' + esc(m.unknown ? "Unbekannte Map" : (m.title || m.id)) +
          '<span class="ct">' + esc(m.id) + "</span></span>" +
        '<span class="rp-li-mc">' + esc(zahlen) + "</span>" +
      "</div>",
    );
    function go() { rpWaehle({ kind: "map", map: m.id }); }
    row.addEventListener("click", go);
    row.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); go(); }
    });
    return row;
  }

  function rpKarteZeile(m) {
    var c = m.counts || {};
    var zahlen = m.unknown
      ? c.total + " Bead(s) · " + c.proven + " belegt"
      : c.closed + "/" + c.total + (c.gaps ? " · " + c.gaps + " ohne Beleg" : "");
    var row = el(
      '<div class="li rp-li-karte" data-rp-map="' + esc(m.id) + '" role="button" tabindex="0">' +
        "<div>" +
          '<div class="rp-li-z1">' +
            '<span class="rp-li-id">Karte</span>' +
            '<span class="rp-li-t">' + (m.unknown
              ? "Einträge ohne bd-Epic"
              : "Plan-Graph, Fortschritt und Verlauf der ganzen Karte") + "</span>" +
          "</div>" +
          '<div class="rp-li-meta">' +
            (m.unknown || !c.total ? "" : rpFortschritt(c)) +
            '<span class="rp-ms-m">' + esc(zahlen) + "</span>" +
          "</div>" +
        "</div>" +
      "</div>",
    );
    function go() { rpWaehle({ kind: "map", map: m.id }); }
    row.addEventListener("click", go);
    row.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); go(); }
    });
    return row;
  }

  /* Ab dieser Ebene rueckt nichts mehr weiter ein (Spec §4.1: "die Einrueckung
     wird optisch gedeckelt, Tiefe nur noch ueber einen Zaehler sichtbar").
     Der Wert ist gemessen, nicht geraten — siehe die Begruendung an
     .rp-li-bead in browser.css. */
  var RP_TIEFE_MAX = 4;

  function rpBeadZeile(m, b, tiefe) {
    tiefe = tiefe || 0;
    var meta =
      /* Der Balken steht VORN, an derselben Stelle wie in der Kartenzeile —
         und nicht am Ende: .rp-li-meta laeuft nowrap mit overflow:hidden, ein
         letztes Element wird also bei vielen Abzeichen abgeschnitten. Genau
         das darf dem Fortschritt nicht passieren, er ist der Grund der Zeile.
         Kein Feld, kein Balken: die Regel steht im Typ (siehe rpFortschritt). */
      (b.counts ? rpFortschritt(b.counts, "Unteraufgaben") : "") +
      rpBadge(rpStatusCls(b.status), b.status || "?") +
      rpPrio(b.priority) +
      (b.ready ? rpBadge("c", "startklar") : "") +
      (b.gap ? rpBadge("r", "kein Beleg") : b.proven ? rpBadge("g", "belegt") : "") +
      ((b.entries || []).length ? rpBadge("", rpAnzEintraege(b.entries.length)) : "") +
      rpAgoBadge(b.last_change);
    /* Genau zwei Zeilen je Eintrag: oben Id und Titel, unten die Abzeichen.
       Vorher waren es vier (Id, Titel ueber zwei Zeilen, Abzeichen) — in einer
       Liste mit 66 Beads passten damit sieben auf den Schirm. Der Titel wird
       einzeilig gekuerzt statt umgebrochen; der volle Text haengt im title-
       Attribut, geht also nicht verloren. */
    /* Die Tiefe steht als Custom Property am Element, die Polsterung rechnet
       browser.css daraus. Ein fertig ausgerechnetes padding hier haette den
       Schrittweite-Wert im Skript und den Rest der Zeilenform im Stylesheet —
       zwei Stellen fuer eine Gestaltung. */
    var stufe = Math.min(tiefe, RP_TIEFE_MAX);
    var row = el(
      '<div class="li rp-li-bead' + (tiefe ? " is-sub" : "") + '" style="--rp-t:' + stufe + '"' +
        ' data-rp-map="' + esc(m.id) + '" data-rp-bead="' + esc(b.id) + '">' +
        "<div>" +
          '<div class="rp-li-z1">' +
            /* Jenseits des Deckels ist die Einrueckung keine Auskunft mehr —
               dann sagt die Ebene als Zahl, wie tief die Zeile haengt. */
            (tiefe > RP_TIEFE_MAX
              ? '<span class="rp-li-lv" title="Ebene ' + (tiefe + 1) +
                  ' — Einrückung gedeckelt">E' + (tiefe + 1) + "</span>"
              : "") +
            '<span class="rp-li-id">' + esc(b.id) + "</span>" +
            '<span class="rp-li-t" title="' + esc(b.title || "") + '">' + esc(b.title || "") + "</span>" +
          "</div>" +
          '<div class="rp-li-meta">' + meta + "</div>" +
        "</div>" +
      "</div>",
    );
    row.addEventListener("click", function () { rpWaehle({ kind: "bead", map: m.id, bead: b.id }); });
    return row;
  }

  function rpSelSichtbar() {
    if (!RP_SEL) return false;
    if (RP_SEL.kind === "bead") {
      return RP_ROWS.some(function (r) { return r.map === RP_SEL.map && r.bead === RP_SEL.bead; });
    }
    return RP_MAPIDS.indexOf(RP_SEL.map) > -1;
  }

  function rpMarkiere(hart) {
    var karte = !!RP_SEL && RP_SEL.kind === "map";
    [].forEach.call(list.querySelectorAll(".rp-li-map"), function (n) {
      n.classList.toggle("is-active", karte && n.getAttribute("data-rp-map") === RP_SEL.map);
    });
    [].forEach.call(list.querySelectorAll(".rp-li-karte"), function (n) {
      var an = karte && n.getAttribute("data-rp-map") === RP_SEL.map;
      n.classList.toggle("is-active", an);
      if (an) rpInsBild(n, hart);
    });
    [].forEach.call(list.querySelectorAll(".rp-li-bead"), function (n) {
      var an = !!RP_SEL && RP_SEL.kind === "bead" &&
        n.getAttribute("data-rp-map") === RP_SEL.map &&
        n.getAttribute("data-rp-bead") === RP_SEL.bead;
      n.classList.toggle("is-active", an);
      if (an) rpInsBild(n, hart);
    });
    /* Auch rechts markieren: in einer durchgehenden Leiste sagt die Liste
       allein nicht mehr, welcher der sichtbaren Bloecke der gewaehlte ist. */
    if (RP_TL) {
      [].forEach.call(RP_TL.querySelectorAll("[data-rp-tl-bead]"), function (n) {
        n.classList.toggle("is-hier", !!RP_SEL && RP_SEL.kind === "bead" &&
          n.getAttribute("data-rp-tl-map") === RP_SEL.map &&
          n.getAttribute("data-rp-tl-bead") === RP_SEL.bead);
      });
      [].forEach.call(RP_TL.querySelectorAll("[data-rp-tl-kopf]"), function (n) {
        n.classList.toggle("is-hier", karte && n.getAttribute("data-rp-tl-kopf") === RP_SEL.map);
      });
    }
  }

  /* Beim Springen per Tastatur muss die aktive Zeile im Bild bleiben — aber
     NUR die Liste darf sich bewegen. scrollIntoView klettert die Kette der
     Scroll-Container hoch und nahm die Seite mit (gemessen: window.scrollY
     sprang beim Wechsel ueber die Map-Grenze auf 48). Das sieht aus, als
     scrolle die Seite statt den Bead zu wechseln. Deshalb rechnen wir den
     Versatz selbst und setzen scrollTop der Liste.
     Oben ist nicht 0: dort kleben die Kopfzeile und die Map-Gruppenzeile, eine
     Zeile darunter waere zwar "im Container", aber verdeckt.

     hart=true heisst: es war eine ECHTE Auswahl (Klick, Pfeiltaste, oder der
     Spy beim Scrollen der Zeitleiste). Dann wird die Zeile nach OBEN gesetzt,
     nicht bloss ins Bild gerueckt — denn rechts steht der gewaehlte Block
     ebenfalls oben. Solange die Liste nur "irgendwo sichtbar" hielt, zeigten
     die beiden Spalten auf verschiedene Hoehen: rechts sprang der Block an den
     oberen Rand, links wanderte die Markierung langsam nach unten. Genau das
     sieht aus, als springe die rechte Spalte nicht mit.
     hart=false ist der Aufbau: da wird gar nicht gescrollt, sonst schoebe die
     erste Auswahl sofort die Abschnittsnavigation aus dem Bild. */
  function rpInsBild(n, hart) {
    if (!hart) return;
    var kopf = list.querySelector(".list-head");
    var gruppe = list.querySelector(".rp-li-map");
    var oben = (kopf ? kopf.offsetHeight : 78) + (gruppe ? gruppe.offsetHeight : 30);
    /* Ueber die Rechtecke statt offsetTop: das haengt am offsetParent, und ob
       .mp-list einer ist, steht in einer anderen Datei. */
    var lr = list.getBoundingClientRect();
    var nr = n.getBoundingClientRect();
    list.scrollTop += (nr.top - lr.top) - oben;
  }

  /* Auswahl heisst seit gkp-tl2 SPRINGEN, nicht neu zeichnen. Rechts steht eine
     durchgehende Zeitleiste ueber alle sichtbaren Karten; ein Klick oder eine
     Pfeiltaste scrollt sie an die Stelle des Beads.

     Der Unterschied ist nicht bloss technisch: vorher war jeder Bead eine
     eigene Seite und der Zusammenhang zwischen zwei Eintraegen unsichtbar.
     Jetzt ist die Nachbarschaft die Aussage — was davor und danach passiert
     ist, steht ohne einen weiteren Klick da. */
  function rpWaehle(sel, ohneSprung) {
    RP_SEL = sel;
    /* Beim Aufbau NICHT springen. Die Belegbilder tragen zwar loading="lazy",
       aber keine Masse — sie kommen nach dem ersten Layout an und schieben
       alles darunter nach unten. Ein Sprung davor zielt auf eine Position, die
       es Sekundenbruchteile spaeter nicht mehr gibt (gemessen: der gewaehlte
       Block lag anschliessend 340px ueber der Oberkante). Am Anfang steht die
       Leiste ohnehin richtig: oben, beim Kopf der ersten Karte. */
    rpMarkiere(!ohneSprung);
    if (!ohneSprung) rpZeitleisteAn(sel);
  }

  /* Bead-Ids sind bd-Ids (gkp-ar7.19) — Punkte und Bindestriche, kein
     Anfuehrungszeichen. Trotzdem maskiert, weil ein Attribut-Selektor mit
     einer fremden Zeichenkette sonst eine offene Flanke waere. */
  function rpAttrEsc(v) { return String(v == null ? "" : v).replace(/(["\\])/g, "\\$1"); }

  function rpTlKnoten(sel) {
    if (!sel || !RP_TL) return null;
    if (sel.kind === "bead") {
      return RP_TL.querySelector('[data-rp-tl-map="' + rpAttrEsc(sel.map) +
        '"][data-rp-tl-bead="' + rpAttrEsc(sel.bead) + '"]');
    }
    return RP_TL.querySelector('[data-rp-tl-kopf="' + rpAttrEsc(sel.map) + '"]') ||
      RP_TL.querySelector('[data-rp-tl-map="' + rpAttrEsc(sel.map) + '"]');
  }

  /* Hartes Setzen von scrollTop statt scrollIntoView: letzteres nimmt die
     ganze Seite mit (siehe rpInsBild) und laesst sich bei gedrueckter
     Pfeiltaste nicht einholen. RP_SPY_AUS haelt waehrenddessen den Scroll-Spy
     an, sonst korrigiert der die Auswahl waehrend des Sprungs zurueck. */
  var RP_SPY_AUS = false;
  var RP_SPY_TIMER = 0;
  var RP_SPY_RAF = 0;
  /* Gescrollt wird .rp-wrap, NICHT #mp-detail: die Polsterung und
     `height:100%; overflow:auto` sitzen am Wrapper, das Detail selbst hat
     keinen Ueberlauf. Ein scrollTop auf detail waere still wirkungslos
     gewesen — deshalb steht der Container hier ausdruecklich. */
  var RP_TL = null;

  function rpZeitleisteAn(sel) {
    var n = rpTlKnoten(sel);
    if (!n || !RP_TL) return;
    var dr = RP_TL.getBoundingClientRect();
    var nr = n.getBoundingClientRect();
    RP_SPY_AUS = true;
    RP_TL.scrollTop += nr.top - dr.top - 8;
    clearTimeout(RP_SPY_TIMER);
    RP_SPY_TIMER = setTimeout(function () { RP_SPY_AUS = false; }, 150);
  }

  /* Der Rueckweg: wer rechts scrollt, verschiebt links die Auswahl mit.
     Ohne das zeigt die Liste nach ein paar Umdrehungen am Rad etwas anderes
     an als der Bildschirm — und die Pfeiltaste springt dann von der alten
     Stelle weiter, nicht von der sichtbaren. */
  function rpSpy() {
    if (!RP_TL) return;
    /* Kartenkoepfe zaehlen mit: sonst zeigt die Liste den ersten Bead einer
       Karte an, waehrend rechts noch ihr Plan-Graph steht. */
    var bloecke = RP_TL.querySelectorAll("[data-rp-tl-bead], [data-rp-tl-kopf]");
    if (!bloecke.length) return;
    var oben = RP_TL.getBoundingClientRect().top;
    var treffer = bloecke[bloecke.length - 1];
    for (var i = 0; i < bloecke.length; i++) {
      // Der erste Block, dessen Unterkante noch unter der Oberkante liegt:
      // das ist der, den man gerade liest.
      if (bloecke[i].getBoundingClientRect().bottom - oben > 24) { treffer = bloecke[i]; break; }
    }
    var kopf = treffer.getAttribute("data-rp-tl-kopf");
    var neu = kopf
      ? { kind: "map", map: kopf }
      : {
          kind: "bead",
          map: treffer.getAttribute("data-rp-tl-map"),
          bead: treffer.getAttribute("data-rp-tl-bead"),
        };
    if (RP_SEL && RP_SEL.kind === neu.kind && RP_SEL.map === neu.map && RP_SEL.bead === neu.bead) return;
    RP_SEL = neu;
    rpMarkiere(true);
  }

  function rpSpyBinden(wrap) {
    wrap.addEventListener("scroll", function () {
      if (!mp.classList.contains("is-reports") || RP_SPY_AUS || RP_SPY_RAF) return;
      RP_SPY_RAF = requestAnimationFrame(function () { RP_SPY_RAF = 0; rpSpy(); });
    });
  }

  /* ---------- die Zeitleiste ---------- */

  /* Reihenfolge: nach Karte, darin zeitlich — dieselbe wie links. Links und
     rechts laufen damit 1:1 parallel, und Scrollen von oben nach unten heisst
     die Liste von oben nach unten. Eine karten-uebergreifende Chronologie
     waere zwar eine schoenere Erzaehlung, wuerde aber die Liste und die
     Zeitleiste entkoppeln — ein Klick auf den letzten Bead landete dann
     mitten im Block einer anderen Karte. Sortiert wird ohnehin in Go
     (appctx.sortMaps, buildReportsMap), hier wird nur gezeichnet. */
  function rpBaueZeitleiste(gruppen, q, versteckt) {
    detail.innerHTML = "";
    var wrap = el('<div class="rp-wrap rp-tl"></div>');
    RP_TL = wrap;
    rpSpyBinden(wrap);

    if (RP_EPIC) {
      var zurueck = el('<button type="button" class="rp-zurueck">← alle Epics</button>');
      zurueck.addEventListener("click", rpSchliesseEpic);
      wrap.appendChild(zurueck);
    }
    if (REPORTS.error) wrap.appendChild(el('<div class="rp-gap-warn">' + esc(REPORTS.error) + "</div>"));
    if (REPORTS.skipped > 0) {
      wrap.appendChild(el('<div class="rp-gap-warn">' + REPORTS.skipped +
        " Report-Zeile(n) beim Lesen übersprungen (kaputt oder zu lang).</div>"));
    }

    /* Der Ueberblick steht ganz oben und nur dort, wo er stimmt (gkp-3sq): in
       einem geoeffneten Epic zeigt die Ansicht EINE Karte, ein Bild aller
       Karten waere daneben eine zweite, widersprechende Aussage. Und bei
       aktivem Textfilter ebenfalls nicht: das SVG kommt fertig vom Server und
       kennt die Sucheingabe nicht — es wuerde Karten zeigen, die die Liste
       links gerade ausblendet. */
    if (!RP_EPIC && !q) {
      var ueber = rpUeberblick();
      if (ueber) wrap.appendChild(ueber);
    }

    if (!gruppen.length) {
      wrap.appendChild(el('<div class="dt-empty" style="padding:16px 0">' +
        esc(rpLeerText(q, versteckt)) + "</div>"));
      detail.appendChild(wrap);
      return;
    }

    gruppen.forEach(function (g) {
      var sec = el('<section class="rp-tl-map" data-rp-tl-map="' + esc(g.m.id) + '"></section>');
      rpTlMapKopf(sec, g.m);
      g.zeilen.forEach(function (z) { sec.appendChild(rpTlBead(g.m, z.b, z.tiefe)); });
      rpTlMapFuss(sec, g.m);
      wrap.appendChild(sec);
    });

    detail.appendChild(wrap);
    rpBindImages(wrap);
    rpLadePlanSVG(wrap);
    wrap.scrollTop = 0;
  }

  /* ---------- Der Ueberblick (gkp-3sq) ----------
     Ein Kaestchen je Karte mit offener Arbeit, mit Fortschrittsbalken. Das
     Bild kommt fertig aus appctx (buildMapsOverview, gerendert von graphviz)
     — hier wird es nur eingehaengt und anklickbar gemacht.

     Warum ueberhaupt ein Bild und keine Kachelreihe aus HTML: die Ansicht hat
     bereits zwei Graphen-Ebenen (Plan-Graph je Karte), und der Ueberblick ist
     dieselbe Sprache eine Ebene hoeher. Eine dritte Darstellungsform fuer
     dieselbe Sache waere eine Erklaerung mehr, die niemand liest. */
  function rpUeberblick() {
    var ov = (REPORTS && REPORTS.overview) || {};
    if (!ov.svg && !ov.note) return null;
    var sec = el('<section class="pane rp-ov"></section>');
    sec.appendChild(el(
      '<div class="p-head"><span class="pt">Karten mit offener Arbeit</span>' +
        (ov.shown ? rpBadge("c", ov.shown + (ov.shown === 1 ? " Karte" : " Karten")) : "") +
        /* Die ausgeblendeten werden GEZAEHLT, nicht verschwiegen: ein Bild,
           das Karten weglaesst, ohne das zu sagen, behauptet Vollstaendigkeit. */
        (ov.hidden ? rpBadge("g", ov.hidden + " gelöst · nicht im Bild") : "") +
        '<span class="sp"></span><span class="pc">Überblick</span></div>',
    ));
    if (ov.svg) {
      var box = el('<div class="rp-svg rp-ov-svg"></div>');
      box.innerHTML = ov.svg;
      rpOvKlick(box);
      sec.appendChild(box);
      sec.appendChild(el('<div class="rp-svg-note">Balken je Karte: ' +
        "geschlossen und belegt (grün), geschlossen ohne Beleg (rot), offen (grau). " +
        "Ein Klick auf eine Karte öffnet sie. Vollständig geschlossene Karten stehen " +
        "nicht im Bild — sie sind Historie, der Verlauf weiter unten zeigt sie.</div>"));
      /* NACH der Note statt davor: die Note erklaert die Farben des Bildes
         direkt darueber, das ist die Bildlegende. Der Pfad ist ein eigener,
         nachgelagerter Gedanke ("und wie geht es jetzt weiter") und keine
         Fortsetzung der Legende — er steht deshalb dahinter, nicht dazwischen. */
      var pfad = rpPfad(ov);
      if (pfad) sec.appendChild(pfad);
    } else {
      sec.appendChild(el('<div class="dt-empty">' + esc(ov.note) + "</div>"));
    }
    return sec;
  }

  /* Der Pfad (gkp-ar7.??): das SVG darueber zeigt jede Karte fuer sich, aber
     nicht, WELCHE Karten gerade zusammen anfangen duerfen — das liefert der
     Server bereits als topologische Wellen (bd swarm), hier wird nur
     gerendert. Rein textlich statt als zweiter Graph: ein weiteres Bild fuer
     denselben Datensatz waere eine zweite Sprache fuer dieselbe Frage, keine
     neue Antwort. Fehlt waves oder ist es leer, gibt es nichts zu sagen — nie
     einen leeren Kasten zeigen. */
  function rpPfad(ov) {
    var waves = ov.waves || [];
    if (!waves.length) return null;
    var box = el('<div class="rp-pfad"></div>');
    box.appendChild(el(
      '<div class="rp-pfad-bilanz">' +
        rpBadge("c", (ov.startable || 0) + " direkt startbar") +
        rpBadge("a", (ov.blocked || 0) + " warten") +
      "</div>",
    ));
    waves.forEach(function (w) {
      var maps = w.maps || [];
      if (!maps.length) return;
      var zeile = el(
        '<div class="rp-pfad-welle">' +
          '<span class="rp-pfad-wt">Welle ' + esc(w.n) + " · " +
            (w.n <= 1 ? "jetzt startbar" : "danach") + "</span>" +
          '<span class="rp-pfad-marken"></span>' +
        "</div>",
      );
      var marken = zeile.querySelector(".rp-pfad-marken");
      maps.forEach(function (m) { marken.appendChild(rpPfadMarke(m, w.n)); });
      box.appendChild(zeile);
    });
    return box;
  }

  /* Eine Marke je Karte in einer Welle. Die Gate-Karte (die Sammelkarte der
     offenen Menschen-/Kundenentscheidungen) ist rot — das ist der Knoten, an
     dem der Pfad auf einen Menschen wartet, nicht auf Code. Welle 1 ohne Gate
     ist cyan, dieselbe Farbe wie das Abzeichen "startklar" anderswo in dieser
     Ansicht; jede weitere Welle bleibt neutral. */
  function rpPfadMarke(m, welleNr) {
    var cls = m.gate ? "r" : welleNr <= 1 ? "c" : "";
    var titel = m.title || m.id || "";
    var offen = m.open == null ? 0 : m.open;
    var mk = el(
      '<span class="rp-pfad-marke' + (cls ? " " + cls : "") + '" role="button" tabindex="0" title="' +
        esc(titel) + '">' +
        '<span class="rp-pfad-mt">' + esc(titel) + "</span>" +
        '<span class="rp-pfad-mo">' + esc(offen) + "</span>" +
      "</span>",
    );
    function go() { rpOeffneEpic(m.id); }
    mk.addEventListener("click", go);
    mk.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); go(); }
    });
    return mk;
  }

  /* Die Knoten des Graphen tragen ein href="#epic:<id>" (graphviz macht daraus
     ein <a xlink:href>). Gebunden wird EIN Handler am Kasten statt einer je
     Knoten: das SVG wird als Ganzes ersetzt, und Handler an seinen inneren
     Knoten waeren beim naechsten Aufbau still weg. */
  function rpOvKlick(box) {
    box.addEventListener("click", function (e) {
      var t = e.target;
      var a = t && t.closest ? t.closest("a") : null;
      if (!a) return;
      // In SVG heisst das Attribut xlink:href; neuere graphviz-Fassungen
      // schreiben zusaetzlich href. Beide lesen, keines voraussetzen.
      var h = a.getAttribute("xlink:href") || a.getAttribute("href") || "";
      if (h.indexOf("#epic:") !== 0) return;
      e.preventDefault();
      rpOeffneEpic(h.slice(6));
    });
  }

  /* Aufgeschoben, nicht weggelassen: die Historie einer Karte und die eines
     Beads sind je ein eigener, teurer Block — 66 davon beim Aufbau zu bauen
     hiesse, die Zeitleiste fuer etwas zu bezahlen, das man in aller Regel
     nicht aufklappt. <details> traegt den Zustand, der Inhalt entsteht beim
     ersten Oeffnen und bleibt dann stehen. */
  function rpLazySec(titel, bauen) {
    var d = el('<details class="rp-lazy"><summary class="rp-lazy-sum">' + esc(titel) + "</summary></details>");
    d.addEventListener("toggle", function () {
      if (!d.open || d.getAttribute("data-gebaut")) return;
      d.setAttribute("data-gebaut", "1");
      var n = bauen();
      if (n) {
        d.appendChild(n);
        rpBindImages(d);
      }
    });
    return d;
  }

  function rpTlMapKopf(sec, m) {
    var c = m.counts || {};
    if (m.unknown) {
      sec.appendChild(el(
        '<div class="pane rp-map-unknown" data-rp-tl-kopf="' + esc(m.id) + '">' +
          '<div class="p-head"><span class="pt">Unbekannte Map</span>' +
            rpBadge("r", "kein bd-Epic mehr") +
            '<span class="sp"></span><span class="pc">' + esc(m.id) + "</span></div>" +
          '<div class="rp-gap-warn">Report-Datei <code>' + esc(m.id) + '.jsonl</code> verweist auf keine ' +
            "aktuelle bd-Map (Tippfehler im <code>map</code>-Feld beim Ingest oder die Epic-Id hat sich " +
            "geändert/wurde gelöscht).</div>" +
        "</div>",
      ));
      return;
    }
    var kopf = el(
      '<div class="pane rp-epic-kopf" data-rp-tl-kopf="' + esc(m.id) + '">' +
        '<div class="p-head"><span class="pt">' + esc(m.title || m.id) + "</span>" +
          rpBadge(rpStatusCls(m.status), m.status || "?") +
          /* Die Sammelkarte hat keine Prioritaet: sie ist kein Ticket. rpPrio
             wuerde die 0 als rotes "P0" zeichnen, also als kritisch — eine
             Behauptung ueber ein Feld, das niemand gesetzt hat. */
          (m.loose ? "" : rpPrio(m.priority)) +
          /* Die Sammelkarte ist kein Epic — "Epic ohne Karte" waere dort eine
             falsche Auskunft ueber etwas, das es im Tracker nicht gibt. */
          (m.loose ? rpBadge("", "kein Epic") : m.is_map ? "" : rpBadge("", "Epic ohne Karte")) +
          (m.ready_count ? rpBadge("c", m.ready_count + " startklar") : "") +
          (c.gaps ? rpBadge("r", c.gaps + " ohne Beleg") : "") +
          '<span class="sp"></span><span class="pc">' + esc(m.id) + "</span></div>" +
        '<div class="rp-epic-zahlen">' + rpFortschritt(c) +
          '<span class="rp-ms-m">' + esc(
            c.closed + " von " + c.total + (m.loose ? " Beads" : " Sub-beads") +
              " zu · " + c.proven + " belegt · " + c.open + " offen",
          ) + "</span>" +
          (m.closed_at ? '<span class="rp-ms-m">geschlossen ' + esc(rpFmtAt(m.closed_at)) + "</span>" : "") +
          (RP_EPIC ? "" : '<button type="button" class="rp-nur-epic" data-rp-epic="' + esc(m.id) +
            '">' + (m.loose ? "nur diese Beads ↗" : "nur dieses Epic ↗") + "</button>") +
        "</div>" +
        /* Der Plan-Graph steht OFFEN und in VOLLER HOEHE am Kopf des
           Kartenabschnitts, also genau da, wo die Karte anfaengt und vor
           ihrem ersten Bead. Kein Deckel und kein Umschalter mehr: ein
           gedeckelter Kasten bekommt einen eigenen Scrollbalken, und der
           faengt das Rad ab — wer ueber dem Graphen scrollt, kommt in der
           Zeitleiste nicht weiter. Nur zwei Karten je Ansicht, das traegt
           die volle Hoehe; bei 66 Verlaufs-Bloecken waere es anders (die
           bleiben deshalb lazy in <details>). */
        /* Die Karte "Ohne Epic" (m.loose) bekommt keinen Plan-Graphen: ihre Id
           steht in keinem Tracker, `bd graph` kann sie nicht zeichnen, und der
           Ladekasten wuerde nur in "kein plan.svg" enden. Stattdessen eine
           Zeile, die sagt, warum hier keiner steht. */
        (m.loose
          ? '<div class="rp-plan"><div class="rp-svg-note">Kein Plan-Graph: diese Beads ' +
              "hängen unter keinem Epic, es gibt also keine Karte, die sie ordnet. " +
              "Ein <code>bd create --parent &lt;epic&gt;</code> bzw. " +
              "<code>beads_create</code> mit <code>parent</code> hängt sie an eine.</div></div>"
          : '<div class="rp-plan">' +
              '<div class="rp-plan-kopf">Plan-Graph</div>' +
              (m.plan_svg
                ? '<div class="rp-svg rp-plan-svg">' + m.plan_svg + "</div>"
                : '<div class="rp-svg rp-plan-svg rp-svg-lade" data-plan-map="' + esc(m.id) + '">' +
                    '<div class="dt-empty">Plan-Graph lädt…</div></div>') +
              '<div class="rp-svg-note">Der Graph zeigt die zusammenhängende Komponente aus ' +
                "<code>bd graph</code>, also auch Beads anderer Epics, die über eine Kante hängen. " +
                "Zahlen und Verlauf zählen nur die " + (c.total || 0) + " Sub-beads dieses Epics.</div>" +
            "</div>") +
      "</div>",
    );
    // Ohne Sub-beads gibt es keinen Fortschritt zu zeigen; "0 %" waere eine
    // Behauptung ueber ein Epic ohne Inhalt.
    if (!c.total) {
      kopf.querySelector(".rp-epic-zahlen").innerHTML = '<span class="rp-ms-m">keine Sub-beads</span>';
    }
    sec.appendChild(kopf);
    rpBindEpicOpen(kopf);
  }

  function rpTlMapFuss(sec, m) {
    var others = m.other || [];
    if (others.length) {
      var os = el('<div class="rp-sec">' + rpSecHead(
        "Weitere Einträge", rpAnzEintraege(others.length) + " · Bead nicht (mehr) unter dieser Map",
      ) + "</div>");
      os.appendChild(el('<div class="rp-other">' + others.map(rpEntryHtml).join("") + "</div>"));
      sec.appendChild(os);
    }
    sec.appendChild(rpLazySec("Verlauf · " + m.id + " (ganze Karte)", function () {
      var nur = {};
      nur[m.id] = true;
      /* Ueber ALLE Nachfahren, nicht nur ueber die erste Ebene: der Verlauf
         "der ganzen Karte" liesse sonst den Beleg einer Unteraufgabe weg, und
         zwar lautlos. */
      rpFlach(m.beads, null, 0, []).forEach(function (z) { nur[z.b.id] = true; });
      return rpHistorieSec({ beads: nur, titel: "Verlauf · " + m.id });
    }));
  }

  function rpTlBead(m, b, tiefe) {
    tiefe = tiefe || 0;
    /* Dieselbe Tiefe wie links, mit demselben Deckel: die Zeitleiste laeuft
       1:1 mit der Liste, und eine Aufgabe, die links eingerueckt steht und
       rechts nicht, waere zweimal dieselbe Auskunft in zwei Fassungen. */
    var art = el('<article class="rp-tl-bead' + (tiefe ? " is-sub" : "") + '"' +
      ' style="--rp-t:' + Math.min(tiefe, RP_TIEFE_MAX) + '"' +
      ' data-rp-tl-map="' + esc(m.id) +
      '" data-rp-tl-bead="' + esc(b.id) + '"></article>');

    art.appendChild(el(
      '<div class="pane rp-bead-pane">' +
        '<div class="p-head"><span class="pt">' + esc(b.title || b.id) + "</span>" +
          rpBadge(rpStatusCls(b.status), b.status || "?") +
          rpPrio(b.priority) +
          (b.issue_type ? rpBadge("", b.issue_type) : "") +
          (b.ready ? rpBadge("c", "startklar") : "") +
          (b.gap ? rpBadge("r", "kein Beleg") : b.proven ? rpBadge("g", "belegt") : "") +
          '<span class="sp"></span><span class="pc">' + esc(b.id) + "</span></div>" +
        '<div class="rp-bead-meta">' +
          /* Dasselbe Zahlenpaar wie am Kartenkopf, eine Ebene tiefer: Balken
             plus Text. Der Belegstand steht NEBEN dem Fortschritt und nicht in
             ihm — "wie viel ist fertig" und "wie viel davon ist belegt" sind
             zwei Fragen, und ein Balken, der beide beantworten soll,
             beantwortet keine (dieselbe Regel wie bei MapCounts). */
          (b.counts ? rpFortschritt(b.counts, "Unteraufgaben") : "") +
          (b.counts
            ? '<span class="rp-ms-m">' + esc(
                b.counts.closed + " von " + b.counts.total + " Unteraufgaben zu · " +
                  b.counts.proven + " belegt · " + b.counts.open + " offen",
              ) + "</span>"
            : "") +
          (b.last_change ? '<span class="rp-ms-m">zuletzt ' + esc(rpFmtAt(b.last_change)) + "</span>" : "") +
          (b.closed_at ? '<span class="rp-ms-m">geschlossen ' + esc(rpFmtAt(b.closed_at)) + "</span>" : "") +
        "</div>" +
        (b.gap
          ? '<div class="rp-gap-warn">geschlossen ohne einen einzigen <code>done</code>-Eintrag — ' +
            "keine Verifikation im Report-Store.</div>"
          : "") +
      "</div>",
    ));

    /* Die Karte steht nicht mehr an jedem Bead: in der Zeitleiste ist sie die
       Ueberschrift des Abschnitts, in dem der Bead liegt. Eine Wiederholung
       je Bead waere 66-mal dieselbe Zeile. */
    var eintraege = b.entries || [];
    art.appendChild(el(eintraege.length
      ? '<div class="rp-bead">' + eintraege.map(rpEntryHtml).join("") + "</div>"
      : '<div class="dt-empty rp-tl-leer">keine Einträge im Report-Store</div>'));

    art.appendChild(rpLazySec("Verlauf · " + b.id, function () {
      var nur = {};
      nur[b.id] = true;
      return rpHistorieSec({ beads: nur, titel: "Verlauf · " + b.id });
    }));
    return art;
  }

  /* Pfeil hoch/runter laeuft die Eintraege der Liste ab, ueber Kartengrenzen
     hinweg. Die KARTE ist dabei der erste Eintrag ihrer Gruppe und ein
     regulaerer Halt — vorher wurde sie uebersprungen, und der Plan-Graph war
     mit der Tastatur nicht erreichbar. Die Gruppen-Ueberschrift selbst bleibt
     kein Halt: sie sagt dasselbe wie die Kartenzeile darunter. */
  function rpIndexAktuell() {
    if (!RP_SEL) return -1;
    for (var k = 0; k < RP_ROWS.length; k++) {
      if (RP_ROWS[k].map !== RP_SEL.map) continue;
      if (RP_SEL.kind === "map") { if (RP_ROWS[k].karte) return k; }
      else if (RP_ROWS[k].bead === RP_SEL.bead) return k;
    }
    return -1;
  }

  function rpSprung(delta) {
    if (!RP_ROWS.length) return;
    var i = rpIndexAktuell();
    var ziel = i < 0 ? (delta > 0 ? 0 : RP_ROWS.length - 1) : i + delta;
    if (ziel < 0) ziel = 0;
    if (ziel > RP_ROWS.length - 1) ziel = RP_ROWS.length - 1;
    rpWaehle(rpRowSel(RP_ROWS[ziel]));
  }

  /* Eine Zeile der Navigationsfolge in eine Auswahl uebersetzen. Eine Stelle
     dafuer, weil die Umwandlung an drei Orten gebraucht wird und ein
     vergessenes r.karte still eine Auswahl erzeugt, die auf nichts passt. */
  function rpRowSel(r) {
    return r.karte ? { kind: "map", map: r.map } : { kind: "bead", map: r.map, bead: r.bead };
  }

  document.addEventListener("keydown", function (e) {
    if (!mp.classList.contains("is-reports")) return;
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    // Ein offenes Beleg-Modal und das Filterfeld haben Vorrang: dort bedeuten
    // die Pfeiltasten scrollen bzw. Cursor, nicht "naechster Bead".
    if (document.getElementById("kn-modal")) return;
    var t = e.target;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
    e.preventDefault();
    rpSprung(e.key === "ArrowDown" ? 1 : -1);
  });

  /* Plan-Graphen nachladen (gkp-3k6). Einer je Karte, nacheinander statt
     gleichzeitig: die Antworten kommen aus demselben bd-Prozess, der seine
     Dolt-DB ohnehin serialisiert — parallele Anfragen wuerden sich nur
     gegenseitig anstehen und dabei alle Verbindungen des Browsers belegen.

     Faellt eine Anfrage aus, bleibt die Karte stehen und sagt es an ihrer
     Stelle; die Seite ist ohne Graph brauchbar, das war sie vorher auch,
     wenn bd oder dot fehlten. */
  function rpLadePlanSVG(root) {
    var offen = [].slice.call(root.querySelectorAll("[data-plan-map]"));
    (function naechste() {
      var box = offen.shift();
      if (!box) return;
      fetch("/plan-svg?" + qs({ project: ACTIVE, map: box.getAttribute("data-plan-map") }))
        .then(function (r) { return r.json(); })
        .then(function (d) {
          box.classList.remove("rp-svg-lade");
          box.removeAttribute("data-plan-map");
          box.innerHTML = d && d.svg
            ? d.svg
            : '<div class="dt-empty">kein plan.svg (bd/dot nicht verfügbar oder Map leer)</div>';
        })
        .catch(function (e) {
          box.classList.remove("rp-svg-lade");
          box.removeAttribute("data-plan-map");
          box.innerHTML = '<div class="dt-empty">Plan-Graph nicht ladbar: ' + esc(String(e)) + "</div>";
        })
        .then(naechste);
    })();
  }

  /* Bild-Beweise klickbar machen. Eigene Funktion, weil die Historie nach dem
     PR-Nachladen neu gezeichnet wird und die Handler ihrer alten Knoten
     mitnimmt — dann muss der neue Teilbaum erneut verdrahtet werden. */
  function rpBindImages(root) {
    [].forEach.call(root.querySelectorAll("[data-rp-image]"), function (btn) {
      btn.addEventListener("click", function (e) {
        /* Die Gesten, mit denen ein Nutzer "in einem neuen Tab" meint, in
           Ruhe lassen — sonst waere der <a> nur Fassade. Mittelklick kommt
           gar nicht als "click" an (button 1 ist "auxclick"), aber der
           Vollstaendigkeit halber steht er hier mit drin. */
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || e.button !== 0) return;
        e.preventDefault();
        showImageModal(btn.getAttribute("data-rp-name") || "Bild-Beweis", btn.getAttribute("data-rp-image"));
      });
      /* Fail open (demo-axk.8 req. 5): das Vorschaubild kann 404 liefern,
         ohne die Seite mitzunehmen.

         Und es sagt etwas ANDERES als der Marker aus dem Store. Dort heisst
         es: der Store hat beim SCHREIBEN festgestellt, dass der Beleg kaputt
         ist. Hier heisst es: der Store hielt ihn fuer heil, die Route liefert
         ihn JETZT nicht aus — Store und Route widersprechen sich. Den Grund
         hat der Browser selbst gemessen (er hat den 404 gesehen); ihn in den
         Satz des Server-Urteils zu kleiden, warf genau ihn weg. Im ersten Fall
         sucht man den Beleg, im zweiten die Asset-Route.

         Der Name ist der blosse Dateiname (data-rp-name), nicht der Pfad —
         der Pfad steht im Store und ist hier nicht der Gegenstand. */
      var thumb = btn.querySelector("img.rp-thumb");
      if (thumb) {
        thumb.addEventListener("error", function () {
          btn.outerHTML = rpMissingHtml("Bild nicht ausgeliefert", btn.getAttribute("data-rp-name") || "");
        }, { once: true });
      }
    });
  }

  /* ---------- Cost (account-wide $) ---------- */
  function renderCost() {
    if (!COST || COST.available === false) {
      detail.innerHTML = '<div class="dt dt-empty">Kosten nicht verfügbar' + (COST && COST.reason ? ": " + esc(COST.reason) : "") + "</div>";
      return;
    }
    function fmt(v) { v = Number(v || 0); return v >= 1000 ? "$" + (v / 1000).toFixed(2) + "k" : "$" + v.toFixed(2); }
    var meta = [
      metaCell("Heute", fmt(COST.today)), metaCell("Gestern", fmt(COST.yesterday)),
      metaCell("Woche (seit Fr)", fmt(COST.week)), metaCell("Monat", fmt(COST.month)),
      metaCell("Total", fmt(COST.total))
    ];
    var secs = "";
    [["5h-Limit", COST.five_hour], ["7d-Limit", COST.seven_day]].forEach(function (p) {
      var rl = p[1];
      var body = rl
        ? '<div class="kv"><span class="kk">Used → Prognose</span><span class="vv">' + esc(rl.used_pct) + "% → " + esc(rl.projected_pct) + '%</span></div>' +
          '<div class="prog"><i style="width:' + Math.max(0, Math.min(100, Number(rl.used_pct) || 0)) + '%"></i></div>' +
          '<div class="kv"><span class="kk">Reset in</span><span class="vv">' + esc(rl.resets_in) + " · " + esc(rl.elapsed_pct) + '% Zeit</span></div>'
        : '<div class="dt-empty">n/a</div>';
      secs += '<div class="dt-sec"><div class="sh">' + p[0] + "</div>" + body + "</div>";
    });
    detail.innerHTML = '<div class="dt">' +
      '<div class="dt-eyebrow">Usage · Kosten (account-weit)</div>' +
      '<h2><span class="mono">' + fmt(COST.total) + "</span></h2>" +
      '<p class="dt-desc">Echte $ über alle Maschinen (ccusage). Quelle wie claude-watch-tui.py — nicht repo-scoped.</p>' +
      '<div class="dt-meta">' + meta.join("") + "</div>" + secs +
    "</div>";
  }

  /* ---------- Kontext: /context-style initial-session token breakdown ----------
     Static categories come from window.DATA (coll.context). The hook-injection
     category is filled LIVE via /hook-inject (single-flight + TTL on the server).
     All numbers are estimates; the two fixed categories are flagged. */
  var CTX_COLORS = ["var(--cyan)", "var(--green)", "var(--amber)", "var(--mag)", "var(--cyan)", "var(--green)"];

  function ctxPct(tokens, window) {
    if (!window) return 0;
    return Math.max(0, Math.min(100, (Number(tokens) || 0) / window * 100));
  }

  /* ctxGruppiere buendelt die Eintraege einer Kategorie nach it.group und gibt
     die Gruppen in der Reihenfolge zurueck, in der sie ANKOMMEN — abgelesen,
     nicht geordnet.

     Hier stand bis gkp-7gb-Nachlese ein `orderedGroups.sort()` samt einer
     Vorgabeliste ["Project","User","Built-in"], also eine ZWEITE Ordnung ueber
     derselben Menge: collectors.readEnabledPlugins sortiert die
     Plugin-Schluessel schon (sort.Strings ueber "name@marketplace"), und
     builddata.go haengt Project-, User-, Plugin- und Built-in-Eintraege in
     genau der Folge aneinander, in der sie erscheinen sollen. Die zwei
     Schluessel laufen auseinander, sobald ein Plugin-Name Praefix eines anderen
     ist: "-" ist 0x2D, "@" ist 0x40, also ordnet Go "a-b@m" vor "a@z", waehrend
     das Etikett "Plugin · a" vor "Plugin · a-b" steht. Gemessen in
     TestKontextGruppenFolgeKommtAusGo.

     Object.create(null) und nicht {}: eine Gruppe namens "constructor" traefe
     sonst auf ein geerbtes Feld, und ihre Eintraege verschwaenden lautlos. */
  function ctxGruppiere(items) {
    var gruppen = Object.create(null);
    var folge = [];
    (items || []).forEach(function (it) {
      var g = it.group || "";
      if (!gruppen[g]) { gruppen[g] = []; folge.push(g); }
      gruppen[g].push(it);
    });
    return { gruppen: gruppen, folge: folge };
  }

  function renderContext() {
    list.innerHTML = "";
    var C = COLL["context"];
    if (!C) {
      detail.innerHTML = '<div class="dt dt-empty">Kontext nicht verfügbar — links ein Projekt wählen.</div>';
      return;
    }
    var win = Number(C.window) || 1000000;
    // Render shell (rebuilt on each hook update). `cats` is a working copy so the
    // live hook tokens can mutate the hooks category without touching DATA.
    var cats = (C.categories || []).map(function (c) {
      return { key: c.key, label: c.label, tokens: Number(c.tokens) || 0, fixed: c.fixed, live: c.live, items: c.items || [], output: c.output || "" };
    });

    function paint() {
      var measured = cats.reduce(function (s, c) { return s + (Number(c.tokens) || 0); }, 0);
      var pctTotal = (measured / win * 100);
      var free = Math.max(0, win - measured);

      // stacked bar segments (one per category with tokens > 0)
      var segs = cats.map(function (c, i) {
        if (!(c.tokens > 0)) return "";
        var w = ctxPct(c.tokens, win);
        return '<span class="ctx-seg" title="' + esc(c.label) + ": ~" + ktok(c.tokens) + ' tok" style="width:' +
          w + "%;background:" + CTX_COLORS[i % CTX_COLORS.length] + '"></span>';
      }).join("");

      var rows = cats.map(function (c, i) {
        var pct = ctxPct(c.tokens, win);
        var badge = c.fixed ? '<span class="ctx-tag fix">fix</span>'
          : (c.live ? '<span class="ctx-tag live">live</span>' : "");
        var swatch = '<span class="ctx-sw" style="background:' + CTX_COLORS[i % CTX_COLORS.length] + '"></span>';
        // hooks live category: body = raw output (escaped) once loaded
        var body;
        if (c.live) {
          body = c.output
            ? '<pre class="ctx-hookout">' + esc(c.output) + "</pre>"
            : '<div class="ctx-item ctx-loading">' + (c.loaded ? "(kein Output)" : "Hook wird live ausgeführt…") + "</div>";
        } else if (c.items.length) {
          // Items nach Gruppe rendern (Project → User → Plugin → Built-in).
          // Wenn kein item.group gesetzt → flache Liste wie bisher.
          var hasGroups = c.items.some(function(it) { return it.group; });
          if (hasGroups) {
            // Reihenfolge und Zuordnung kommen aus ctxGruppiere, also aus der
            // Folge, in der builddata.go die Eintraege aneinanderhaengt.
            var gr = ctxGruppiere(c.items);
            var grouped = gr.gruppen;

            body = gr.folge.map(function(grp) {
              var items = grouped[grp] || [];
              if (!items.length) return "";
              var header = '<div class="ctx-grp">' + esc(grp) + '</div>';
              var rows = items.map(function(it) {
                if (it.read) {
                  var detailId = "ctx-rd-" + esc(it.name).replace(/[^A-Za-z0-9]/g, "-");
                  return '<details class="ctx-readable" id="' + detailId + '">' +
                    '<summary><span class="ctx-in">' + esc(it.name) + "</span>" +
                    (it.desc ? '<span class="ctx-id">' + esc(it.desc) + "</span>" : "") +
                    '<span class="ctx-it">~' + ktok(it.tokens) + " tok</span></summary>" +
                    '<div class="ctx-readable-body" data-read="' + esc(it.read) + '"' +
                      (it.name ? ' data-name="' + esc(it.name) + '"' : '') +
                      (it.readPath ? ' data-path="' + esc(it.readPath) + '"' : '') +
                    '></div>' +
                    "</details>";
                }
                return '<div class="ctx-item"><span class="ctx-in">' + esc(it.name) + "</span>" +
                  (it.desc ? '<span class="ctx-id">' + esc(it.desc) + "</span>" : "") +
                  '<span class="ctx-it">~' + ktok(it.tokens) + " tok</span></div>";
              }).join("");
              return header + rows;
            }).join("");
          } else {
            body = c.items.map(function (it) {
              if (it.read) {
                var detailId = "ctx-rd-" + esc(it.name).replace(/[^A-Za-z0-9]/g, "-");
                return '<details class="ctx-readable" id="' + detailId + '">' +
                  '<summary><span class="ctx-in">' + esc(it.name) + "</span>" +
                  (it.desc ? '<span class="ctx-id">' + esc(it.desc) + "</span>" : "") +
                  '<span class="ctx-it">~' + ktok(it.tokens) + " tok</span></summary>" +
                  '<div class="ctx-readable-body" data-read="' + esc(it.read) + '"' +
                    (it.name ? ' data-name="' + esc(it.name) + '"' : '') +
                    (it.readPath ? ' data-path="' + esc(it.readPath) + '"' : '') +
                  '></div>' +
                  "</details>";
              }
              return '<div class="ctx-item"><span class="ctx-in">' + esc(it.name) + "</span>" +
                (it.desc ? '<span class="ctx-id">' + esc(it.desc) + "</span>" : "") +
                '<span class="ctx-it">~' + ktok(it.tokens) + " tok</span></div>";
            }).join("");
          }
        } else {
          body = '<div class="ctx-item ctx-empty">— keine Einzel-Items</div>';
        }
        // Alle Kategorien default eingeklappt — kein auto-open für live-Kategorie
        // (Live-Hook läuft trotzdem im Hintergrund, öffnet sich aber nicht automatisch).
        return '<details class="ctx-cat">' +
          '<summary>' + swatch + '<span class="ctx-l">' + esc(c.label) + "</span>" + badge +
            '<span class="ctx-bar-mini"><i style="width:' + pct + '%;background:' + CTX_COLORS[i % CTX_COLORS.length] + '"></i></span>' +
            '<span class="ctx-t">~' + ktok(c.tokens) + " tok</span>" +
            '<span class="ctx-p">' + pct.toFixed(1) + "%</span>" +
          "</summary><div class=\"ctx-body\">" + body + "</div></details>";
      }).join("");

      // free space row (non-collapsible)
      var freeRow = '<div class="ctx-free"><span class="ctx-sw ctx-sw-free"></span>' +
        '<span class="ctx-l">Free space</span>' +
        '<span class="ctx-t">~' + ktok(free) + " tok</span>" +
        '<span class="ctx-p">' + (free / win * 100).toFixed(1) + "%</span></div>";

      detail.innerHTML = '<div class="dt ctx-wrap">' +
        '<div class="dt-eyebrow">Session · statischer Initial-Kontext (≈ geschätzt)</div>' +
        '<h2><span class="mono">~' + ktok(measured) + " / " + ktok(win) + "</span>" +
          ' <span class="ctx-sum-p">(' + pctTotal.toFixed(1) + "%)</span></h2>" +
        '<p class="dt-desc">Was jede neue Session in diesem Projekt lädt. System prompt + tools sind fixe ' +
          'Näherungen (harness-abhängig); der Rest ist aus den Dateien geschätzt. Bei Skills/Agents zählt ' +
          'nur die je Skill/Agent geladene Metadaten (Name + Beschreibung), nicht die volle Datei ' +
          '(progressive disclosure). Hook-Injektion wird live ausgeführt.</p>' +
        '<div class="ctx-bar">' + segs + '<span class="ctx-seg ctx-seg-free" style="width:' +
          (free / win * 100) + '%"></span></div>' +
        '<div class="ctx-cats">' + rows + freeRow + "</div>" +
      "</div>";
      // Re-bind after every paint: detail.innerHTML was replaced, old listeners gone.
      // `toggle` does not bubble, so event delegation on `detail` would not work.
      // dataset.loaded guard prevents re-fetching already-loaded content.
      [].forEach.call(detail.querySelectorAll("details.ctx-readable"), function (det) {
        det.addEventListener("toggle", function () {
          if (!det.open) return;
          var host = det.querySelector(".ctx-readable-body");
          if (!host || host.dataset.loaded) return;
          host.dataset.loaded = "1";
          loadFile(host.dataset.read, { name: host.dataset.name || "", path: host.dataset.path || "" }, host);
        });
      });
    }

    paint();

    // Live hook injection — fetch once, then repaint with tokens + raw output.
    var hooksCat = null;
    for (var k = 0; k < cats.length; k++) { if (cats[k].live) hooksCat = cats[k]; }
    if (hooksCat) {
      fetch("/hook-inject?" + qs({ project: ACTIVE }))
        .then(function (r) { return r.json(); })
        .then(function (d) {
          hooksCat.loaded = true;
          if (d && d.ok) {
            hooksCat.tokens = Number(d.tokens) || 0;
            hooksCat.output = String(d.output || "");
          } else {
            hooksCat.output = "Hook-Ausführung fehlgeschlagen" + (d && d.error ? ": " + d.error : "") + ".";
          }
          paint();
        })
        .catch(function (e) {
          hooksCat.loaded = true;
          hooksCat.output = "Fehler: " + String(e);
          paint();
        });
    }
  }

  /* ---------- project overview page (landing for project view) ----------
     All data from window.DATA: COLL (coll), OV (overview), ACTIVE. */
  /* CHANGELOG as one full-width markdown document (no 2nd sidebar). */
  function renderChangelogDoc() {
    var items = (COLL["changelog"] || {}).items || [];
    var body = items.length
      ? '<div class="kn-md">' + mdToHtml(items.map(function (x) {
          return (x.name || "") + "\n\n" + (x.body || "");
        }).join("\n\n---\n\n")) + "</div>"
      : '<div class="dt-empty">Kein Changelog</div>';
    detail.innerHTML = '<div class="dt ctx-wrap">' +
      '<div class="dt-eyebrow">CHANGELOG · Projekt</div>' +
      '<h2><span class="mono">CHANGELOG</span> <span class="ctx-sum-p">(' + items.length + ")</span></h2>" +
      body + "</div>";
  }

  function renderProjectOverview() {
    var d = detail;
    d.innerHTML = "";
    d.className = "";

    // ---- helpers ----
    function fmtTok(n) { n = Number(n) || 0; return n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(Math.round(n)); }
    function fmtCostLocal(v) { v = Number(v || 0); return v >= 1000 ? "$" + (v / 1000).toFixed(2) + "k" : "$" + v.toFixed(2); }
    function tag(cls, txt) { return '<span class="tag' + (cls ? " " + cls : "") + '">' + esc(txt) + "</span>"; }
    function prow(l, r, cls) { return '<div class="prow"><span class="pl">' + l + '</span><span class="pr' + (cls ? " " + cls : "") + '">' + r + "</span></div>"; }

    // ---- project meta ----
    var projName = ACTIVE || "Projekt";
    var branch = String((DATA.meta || {}).branch || "?");
    var psections = COLL["psections"] || {};
    var pdoc = psections.doc || {};
    var claudeTok = Number(pdoc.tokens || OV.claude_tok_project || 0);

    // ---- KPI data ----
    var proj7d = 0;
    // find per-project cost from DATA.projects array
    var projects = DATA.projects || [];
    for (var pi = 0; pi < projects.length; pi++) {
      if (projects[pi].name === ACTIVE) { proj7d = Number(projects[pi].cost_7d || 0); break; }
    }
    var projInitTok = OV.proj_init_tok || "0";

    // ---- decisions/changelog/memory/lektionen from knowledge ----
    var decisionItems = (COLL["decisions"] || {}).items || [];
    var changelogItems = (COLL["changelog"] || {}).items || [];
    var memItems = (COLL["memory"] || {}).items || [];
    var lessonItems = (COLL["lessons"] || {}).items || [];

    // ---- CLAUDE.md sections as tags ----
    var sectionItems = psections.items || [];
    var sectionTags = sectionItems.map(function(s) { return tag("", s.name); }).join("");
    if (!sectionTags && claudeTok > 0) sectionTags = tag("", "(ganze Datei)");

    // ---- init-load token bar rows ----
    function tloadRows(items) {
      var max = 1;
      items.forEach(function(i) { if (Number(i.tok) > max) max = Number(i.tok); });
      return items.map(function(i) {
        return '<div class="row">' +
          '<div class="tl">' + esc(i.l) + "</div>" +
          '<div class="track"><i style="width:' + (Number(i.tok) / max * 100).toFixed(1) + '%;background:' + esc(i.color) + '"></i></div>' +
          '<div class="tv">' + fmtTok(i.tok) + '<small> tok</small></div>' +
          "</div>";
      }).join("");
    }
    var tloadItems = [];
    if (claudeTok) tloadItems.push({ l: "CLAUDE.md (Projekt)", tok: claudeTok, color: "var(--green)" });
    var ctxCats = (COLL["context"] || {}).categories || [];
    var memCat = null;
    ctxCats.forEach(function(c) { if (c.key === "memory") memCat = c; });
    var memTok = memCat ? Number(memCat.tokens || 0) : 0;
    if (memTok) tloadItems.push({ l: "Memory / CLAUDE.md", tok: memTok, color: "var(--mag)" });
    var initTotal = tloadItems.reduce(function(s, i) { return s + Number(i.tok); }, 0);

    // ---- assemble HTML ----
    var html =
      '<div class="ovwrap">' +
      // bar header
      '<div class="ovbar">' +
      '<h2><b>' + esc(projName) + "</b></h2>" +
      '<span class="branch">branch <code>' + esc(branch) + "</code></span>" +
      (claudeTok ? '<span class="hint">CLAUDE.md <b>~' + fmtTok(claudeTok) + " tok</b></span>" : "") +
      "</div>" +

      // KPI tiles
      '<div class="ov-kpis">' +
      '<div class="ov-kpi g"><div class="k-l">Kosten · 7 Tage</div><div class="k-v">' + fmtCostLocal(proj7d) + '</div>' +
        '<div class="k-s">dieses Projekt</div></div>' +
      '<div class="ov-kpi m"><div class="k-l">Projekt Init-Load</div><div class="k-v">' + fmtTok(projInitTok) + '<small> tok</small></div>' +
        '<div class="k-s">CLAUDE.md + memory</div></div>' +
      "</div>" +

      // Two columns: LEFT = Init-Load · RIGHT = Wissens-Index → neueste Memory
      '<div class="ov-cols">' +

      '<div class="ov-col ov-col-l">' +
        '<div class="pane">' +
          '<div class="p-head"><span class="pt">Projekt Init-Load</span>' +
            (claudeTok ? '<span class="pc">~' + fmtTok(claudeTok) + " tok CLAUDE.md</span>" : "") +
          '<span class="sp"></span></div>' +
          (tloadItems.length
            ? '<div class="tload">' + tloadRows(tloadItems) + "</div>" +
              '<div class="tload-foot"><span class="tf-l">Summe bei Projekt-Load</span><span class="tf-v">' + fmtTok(initTotal) + " tok</span></div>"
            : '<div style="color:var(--faint);font-size:11px;padding:6px 0">—</div>') +
        "</div>" +
      "</div>" +

      '<div class="ov-col ov-col-r">' +
        // CLAUDE.md card (top) — clickable → modal
        '<div class="pane ov-click" data-open="claude">' +
          '<div class="p-head"><span class="pt">CLAUDE.md</span>' +
            (claudeTok ? '<span class="pc">~' + fmtTok(claudeTok) + " tok</span>" : "") +
          '<span class="sp"></span><span class="pc">öffnen ›</span></div>' +
        "</div>" +
        // Decisions card
        (decisionItems.length
          ? '<div class="pane"><div class="p-head"><span class="pt">Decisions</span><span class="pc">' + decisionItems.length + '</span><span class="sp"></span></div>' +
            decisionItems.map(function(x, i) {
              return '<div class="prow ov-click" data-open="decision" data-idx="' + i + '"><span class="pl"><b>#' + esc(String(x.id || "")) + '</b> ' + esc(x.name ? String(x.name).replace(/^\d+\s—\s/, "") : "") + '</span><span class="pr g">' + esc(x.status || "") + "</span></div>";
            }).join("") + "</div>"
          : "") +
        // Lektionen card
        (lessonItems.length
          ? '<div class="pane"><div class="p-head"><span class="pt">Lektionen</span><span class="pc">' + lessonItems.length + '</span><span class="sp"></span></div>' +
            lessonItems.map(function(x) {
              return '<div class="prow ov-click" data-open="lektion" data-name="' + esc(x.name || "") + '"><span class="pl">' + esc(String(x.name || "").replace(/\.md$/, "")) + '</span><span class="pr">' + esc(x.date || "") + "</span></div>";
            }).join("") + "</div>"
          : "") +
        // Memory card — ALL files
        '<div class="pane"><div class="p-head"><span class="pt">Memory</span><span class="pc">' + memItems.length + '</span><span class="sp"></span></div>' +
          (memItems.length
            ? memItems.map(function(m) { return '<div class="prow ov-click" data-open="memory" data-name="' + esc(m.name || "") + '"><span class="pl">' + esc(m.name || "") + '</span><span class="pr c">' + esc(m.date || "") + "</span></div>"; }).join("")
            : '<div style="color:var(--faint);font-size:11px;padding:6px 0">Keine Memory-Dateien</div>') +
        "</div>" +
        // Changelog card — click opens the full changelog (sections like CLAUDE.md)
        (changelogItems.length
          ? '<div class="pane ov-click" data-open="changelog-all"><div class="p-head"><span class="pt">Changelog</span><span class="pc">' + changelogItems.length + '</span><span class="sp"></span><span class="pc">öffnen ›</span></div></div>'
          : "") +
      "</div>" +

      "</div></div>"; // ov-cols + ovwrap

    d.innerHTML = html;
    list.innerHTML = "";
    // Click-to-modal for knowledge entries (delegated; onclick avoids listener stacking).
    d.onclick = function (e) {
      var t = e.target.closest ? e.target.closest("[data-open]") : null;
      if (!t) return;
      var kind = t.getAttribute("data-open");
      if (kind === "decision") {
        var dd = decisionItems[+t.getAttribute("data-idx")];
        if (dd) showModal(String(dd.name || ""), String(dd.body || "(kein Inhalt)"));
      } else if (kind === "changelog") {
        var cc = changelogItems[+t.getAttribute("data-idx")];
        if (cc) showModal(String(cc.name || "").replace(/^#{1,3}\s+/, ""), String(cc.body || "(kein Inhalt)"));
      } else if (kind === "changelog-all") {
        showModal("CHANGELOG", changelogItems.map(function (c) { return (c.name || "") + "\n\n" + (c.body || ""); }).join("\n\n"));
      } else if (kind === "memory") {
        openFileModal(t.getAttribute("data-name"), "memory", { name: t.getAttribute("data-name") });
      } else if (kind === "lektion") {
        openFileModal(t.getAttribute("data-name"), "lektion", { name: t.getAttribute("data-name") });
      } else if (kind === "claude") {
        openFileModal("CLAUDE.md", "claude-project", {});
      }
    };
  }

  /* ---------- overview (tmux grid) ---------- */
  function ovTile(go, dot, title, body) {
    return '<div class="pane w3 link" data-go="' + go + '"><div class="p-head"><span class="dot' + dotCls(dot) + '"' +
      dotStyle(dot) + '></span><span class="pt">' + esc(title) + '</span><span class="sp"></span></div>' + body + "</div>";
  }
  function miniRL(label, rl) {
    if (!rl) return '<div class="rlm-row"><span class="rlm-l">' + label + '</span><span class="rlm-na">n/a</span></div>';
    var p = Math.max(0, Math.min(100, Number(rl.used_pct) || 0));
    return '<div><div class="rlm-row"><span class="rlm-l">' + label + '</span>' +
      '<span class="rlm-v">' + esc(rl.used_pct) + "% → " + esc(rl.projected_pct) + "% · " + esc(rl.resets_in) + "</span></div>" +
      '<div class="prog"><i style="width:' + p + '%"></i></div></div>';
  }
  function renderOverview() {
    var grid =
      ovTile("skills", "g", "Global",
        '<div class="duo"><div class="n" style="color:var(--green)">' + (OV.skills || 0) + '<small>Skills</small></div>' +
        '<div class="n" style="color:var(--cyan)">' + (OV.agents || 0) + '<small>Agents</small></div>' +
        '<div class="n">' + (OV.hooks || 0) + '<small>Hooks</small></div></div>' +
        '<div style="margin-top:9px;color:var(--dim);font-size:11px">Skills ~' + esc(OV.skills_tok || "0") +
        " tok · Agents ~" + esc(OV.agents_tok || "0") + " tok (SKILL.md/Definition gesamt)</div>") +
      ovTile("git", "g", "Git",
        '<div class="scan" style="color:var(--green)">' + esc(OV.branch || "?") +
        '<small> · ' + (OV.dirty ? "dirty" : "clean") + "</small></div>" +
        '<div style="margin-top:9px;color:var(--dim);font-size:11px">' + esc(OV.git_recommend || "") + "</div>") +
      ovTile("cost", "m", "Kosten · Limits",
        '<div class="scan" style="color:var(--mag)">' + esc(OV.cost_week || "$0") + '<small> Woche</small></div>' +
        '<div style="margin:7px 0 10px;color:var(--dim);font-size:11px">heute ' + esc(OV.cost_today || "$0") + " · total " + esc(OV.cost_total || "$0") + "</div>" +
        '<div class="rlwrap">' + miniRL("5h-Limit", OV.cost_5h) + miniRL("7d-Limit", OV.cost_7d) + "</div>") +
      ovTile("decisions", "g", "Decisions",
        '<ul class="list">' + (OV.decisions || []).map(function (d) {
          return '<li><span class="ix">' + esc(d.id) + '</span><span class="tx">' + esc(d.title) + "</span></li>";
        }).join("") + "</ul>") +
      ovTile("sessions", "m", "Tokens · Sessions",
        '<div class="scan" style="color:var(--mag)">' + esc(OV.tok_last || "—") + '<small> letzte (' + esc(OV.cost_last || "—") + " ≈)</small></div>" +
        '<div style="margin-top:9px;color:var(--dim);font-size:11px">7-Tage: ' + esc(OV.tok_week || "—") +
        " · alle Sessions: " + esc(OV.tok_sessions || "—") + " (" + esc(OV.cost_sessions || "—") + " ≈)</div>") +
      (function () {
        var kc = OV.know_counts || {};
        return ovTile("memory", "g", "Wissen",
          '<div class="duo"><div class="n" style="color:var(--green)">' + (kc.decisions || 0) + '<small>Decisions</small></div>' +
          '<div class="n">' + (kc.memory || 0) + '<small>Memory</small></div>' +
          '<div class="n" style="color:var(--amber)">' + (kc.lektionen || 0) + '<small>Lektionen</small></div></div>' +
          '<div style="margin-top:9px;color:var(--dim);font-size:11px">knowledge/ ' + (kc.pknow || 0) +
          " · CHANGELOG " + (kc.changelog || 0) + " · CLAUDE.md ~" + esc(OV.claude_tok_project || "0") + " tok</div>");
      })() +
      ovTile("sessions", "r", "Sessions · Health",
        '<div class="scan" style="color:' + ((OV.errors_total || 0) > 0 ? "var(--red)" : "var(--green)") + '">' + (OV.errors_total || 0) + '<small> Fehler</small></div>' +
        '<div style="margin-top:9px;color:var(--dim);font-size:11px">' + (OV.repeats_total || 0) + " wiederholte Commands</div>" +
        ((OV.top_tools || []).length ? '<div style="margin-top:6px;color:var(--faint);font-size:10.5px">Top: ' +
          (OV.top_tools || []).map(function (t) { return esc(t[0]) + " " + t[1]; }).join(" · ") + "</div>" : "")) +
      // "psections" has no SPECIAL handler — clicking falls through to renderList,
      // which shows the CLAUDE.md sections list. That is intentional.
      (ACTIVE ? ovTile("psections", "a", "Projekt-Settings",
        '<div class="duo"><div class="n" style="color:var(--green)">' + (OV.proj_skills || 0) + '<small>Skills</small></div>' +
        '<div class="n">' + (OV.proj_hooks || 0) + '<small>Hooks</small></div>' +
        '<div class="n" style="color:var(--cyan)">' + (OV.proj_agents || 0) + '<small>Agents</small></div></div>' +
        '<div style="margin-top:9px;color:var(--dim);font-size:11px">Init-Context ~' + esc(OV.proj_init_tok || "0") + ' tok/Session (global+projekt CLAUDE.md) · Projekt-CLAUDE.md ~' + esc(OV.claude_tok_project || "0") + ' tok</div>') : "");
    detail.innerHTML =
      '<div class="ov-wrap"><div class="ov-head"><h2>Übersicht</h2><span class="dc">' + esc(OV.subtitle || "") + "</span>" +
      '<span class="hint">Kachel anklicken → <b>Sektion</b></span></div>' +
      '<div class="panes" style="grid-auto-rows:min-content">' + grid + "</div></div>";
    [].forEach.call(detail.querySelectorAll("[data-go]"), function (t) {
      t.addEventListener("click", function () { select(t.dataset.go); });
    });
  }

  /* ---------- keyboard nav within list ---------- */
  document.addEventListener("keydown", function (e) {
    if (mp.classList.contains("is-overview")) return;
    if (e.target.tagName === "INPUT") return;
    if (!state.coll || !COLL[state.coll]) return;
    // Die gefilterte Zeilenfolge aus renderList, nicht c.items — siehe
    // LISTE_SICHTBAR. Passt sie nicht zur geoeffneten Sammlung, hat renderList
    // hier gar nicht gezeichnet, und es gibt nichts zu bewegen.
    if (LISTE_SICHTBAR.coll !== state.coll) return;
    var sicht = LISTE_SICHTBAR.idx;
    if (!sicht.length) return;
    // Steht die Auswahl ausserhalb der sichtbaren Menge, faengt die Bewegung
    // vorn an — irgendwohin springen muss sie, und der Anfang ist die einzige
    // Stelle, die nicht von der weggefilterten Zeile abhaengt.
    var pos = sicht.indexOf(state.idx);
    if (e.key === "ArrowDown") { e.preventDefault(); selectItem(sicht[pos < 0 ? 0 : Math.min(sicht.length - 1, pos + 1)]); }
    if (e.key === "ArrowUp") { e.preventDefault(); selectItem(sicht[pos < 0 ? 0 : Math.max(0, pos - 1)]); }
  });

  /* ---------- theme toggle (Catppuccin Mocha ⇄ Latte) ---------- */
  (function () {
    var btn = document.getElementById("kn-theme-toggle");
    if (!btn) return;
    function eff() {
      var e = document.documentElement.dataset.theme;
      if (e === "light" || e === "dark") return e;
      try { return matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark"; }
      catch (x) { return "dark"; }
    }
    function sync() {
      var cur = eff();
      // show the icon for the mode you'd switch TO
      btn.textContent = cur === "light" ? "☾" : "☀";
      btn.title = "Theme: " + cur + " · klick → " + (cur === "light" ? "dark" : "light");
    }
    btn.addEventListener("click", function () {
      var next = eff() === "light" ? "dark" : "light";
      document.documentElement.dataset.theme = next;
      try { localStorage.setItem("kn-theme", next); } catch (x) {}
      sync();
    });
    sync();
  })();

  /* popstate: user pressed Back/Forward — restore the view from the history state (CCS-034).
     If the popped URL belongs to a different project, reload the full page so the server
     can build DATA for that project. */
  window.addEventListener("popstate", function (e) {
    var state = e.state;
    var viewId = state && state.view ? String(state.view) : "";
    // Parse the current pathname to detect a project change
    var segs = window.location.pathname.slice(1).split("/").filter(function (s) { return s.length > 0; });
    var pathProject = segs.length >= 1 ? decodeURIComponent(segs[0]) : "";
    if (pathProject === "ueberblick") pathProject = "";
    // If the path project differs from the active project, reload (server builds DATA for that project)
    if (ACTIVE && pathProject && pathProject !== ACTIVE) {
      window.location.reload();
      return;
    }
    if (!ACTIVE && pathProject) {
      window.location.reload();
      return;
    }
    // Same project — restore view client-side without adding another history entry.
    // Das Epic kommt aus dem PFAD, nicht aus state: der Verlaufseintrag der
    // Liste und der eines Epics tragen beide view:"reports", und nur das dritte
    // Segment unterscheidet sie (gkp-ar7.28).
    var zielEpic = segs.length >= 3 ? decodeURIComponent(segs[2]) : "";
    suppressPush = true;
    try {
      RP_EPIC = zielEpic;
      select(viewId || ((NAV[0] && NAV[0].items && NAV[0].items[0] && NAV[0].items[0].id) || "ov"));
    } finally { suppressPush = false; }
  });

  /* ---------- Aufgaben-Ansichten (gkp-7gb, Spec §5) ----------
     Eine Ansicht ist ein NAME plus eine Zeichenfunktion, angemeldet am Ende
     dieser Datei. Das Zeichnen holt sie ueber den Namen, select() kennt keinen
     einzelnen Ansichtsnamen. Eine weitere Ansicht (gkp-1oj) ist damit ein
     weiterer avAnmelden-Aufruf am Ende, kein Eingriff in einen if-Zweig in der
     Mitte — drei Agenten arbeiten nach diesem Ticket gleichzeitig in dieser
     Datei.

     Was hier NICHT passiert: auswaehlen und sortieren. Beides kommt fertig aus
     Go (appctx/ansicht.go). Der Rumpf REPORTS.ansichten[id].eintraege nennt je
     Zeile nur die Bead-Id, die Karte, die Tiefe und den Grund; den ZEILENINHALT
     holt der Browser aus REPORTS.maps, wo die Aufgabe ohnehin schon steht. Das
     hat zwei Gruende: die Antwort wird nicht doppelt so gross, und nach einem
     Schreibvorgang frischt rpTeilbaumEinsetzen genau diesen Baum auf — die
     Zeilen sind dann aktuell, ohne dass hier etwas nachgezogen wuerde. */

  var AV_REG = {};
  function avAnmelden(id, def) { AV_REG[id] = def; }

  function avAnsicht(id) {
    return (REPORTS && REPORTS.ansichten && REPORTS.ansichten[id]) || null;
  }

  /* Die Aufgabe zu einer Id, irgendwo im Baum. Ein Nachschlagen, keine
     Rechnung: Go hat entschieden, DASS die Zeile da ist, hier wird nur ihr
     Inhalt geholt. */
  function avBeadSuchen(id) {
    var treffer = null;
    function ab(beads) {
      (beads || []).forEach(function (b) {
        if (treffer) return;
        if (b.id === id) { treffer = b; return; }
        ab(b.children);
      });
    }
    (REPORTS && REPORTS.maps || []).forEach(function (m) { if (!treffer) ab(m.beads); });
    return treffer;
  }

  function avKarteSuchen(id) {
    var k = null;
    (REPORTS && REPORTS.maps || []).forEach(function (m) { if (m.id === id) k = m; });
    return k;
  }

  /* Der PFAD eines Projekts, nicht sein Name: die Schreibrouten verlangen ihn
     so (siehe den Kommentarkopf von internal/server/taskroutes.go — ein Name
     wuerde dort ueber getTarget aufgeloest und fiele bei einem Tippfehler auf
     das Repo des DIENSTES zurueck). Er steht in DATA.projects.

     Der Name kommt NICHT mehr immer von ACTIVE (gkp-s9n): in der Sammelansicht
     (Heute & Co. ueber alle Projekte, gkp-92i) traegt jede Zeile ihr EIGENES
     Projekt (AnsichtEintrag.Projekt), und das kann ein anderes sein als das
     gerade geoeffnete. Ohne diese Uebersetzung schriebe "Schließen" aus der
     Sammelansicht immer ins aktive Repo — oder, ohne aktives Repo, gegen einen
     leeren Pfad, der beim Dienst zurueckfaellt (siehe oben).

     null statt "" bei einem unbekannten Namen, und das ist Absicht (gkp-uv0,
     Review-Hinweis zu gkp-s9n): AnsichtEintrag.Projekt ist beads.Name (der
     Hauptworktree), DATA.projects ist nach dem VERZEICHNISNAMEN geschluesselt
     (DiscoverProjectsIn) — bei einem verknuepften Worktree mit abweichendem
     Verzeichnisnamen faellt das auseinander. Gemessen 2026-09-17: keins der
     neun Repos unter ~/git ist ein verknuepfter Worktree, der Fall tritt heute
     nicht ein. Trotzdem: ein "" wuerde still durchgereicht und traefe entweder
     das Repo des Dienstes oder, schlimmer, zufaellig ein GLEICHNAMIGES anderes
     Projekt. Die Aufrufer muessen deshalb null als Abbruch lesen, nicht als
     leeren Pfad senden — der richtige Fix (die Zeile traegt den Pfad direkt)
     ist als gkp-uv0 abgelegt. */
  function avPfadFuerProjekt(name) {
    var ps = DATA.projects || [];
    for (var i = 0; i < ps.length; i++) if (ps[i].name === name) return ps[i].path || null;
    return null;
  }

  function avPostJSON(pfad, rumpf) {
    return fetch(pfad, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(rumpf),
    }).then(avAntwortLesen);
  }

  function avPostForm(pfad, form) {
    return fetch(pfad, { method: "POST", body: form }).then(avAntwortLesen);
  }

  /* Ein Fehlschlag ist hier kein geworfener Fehler, sondern ein Ergebnis mit
     ok:false — der Rumpf traegt bei jedem Fehlerfall eine Begruendung (fehler,
     grund, aufrufe, beads), und die gehoert in die Zeile, nicht in die
     Konsole. */
  function avAntwortLesen(r) {
    return r.json().then(
      function (j) { return { ok: r.ok, rumpf: j || {} }; },
      function () { return { ok: false, rumpf: { fehler: "Antwort von " + r.status + " ist kein JSON" } }; }
    );
  }

  function avFehlertext(rumpf) {
    var t = rumpf.fehler || "unbekannter Fehler";
    if (rumpf.grund) t += " (" + rumpf.grund + ")";
    if (rumpf.beads && rumpf.beads.length) t += " — betrifft: " + rumpf.beads.join(", ");
    return t;
  }

  /* Die Antwort einer Schreiboperation einarbeiten.

     ZWEI Dinge, und sie sind nicht dasselbe: der `teilbaum` sagt, wie die Zeile
     jetzt AUSSIEHT, `ansichten` sagt, ob und wo sie noch STEHT. Ein gesetztes
     Datum kann eine Aufgabe aus Heute herausnehmen — das ist eine Auswahl, und
     die trifft Go. Fehlt der Teilbaum (`nachladen` ist gesetzt), bleibt die
     Zeile auf dem alten Stand und die Ansicht sagt es; die Auswahl stimmt
     trotzdem. */
  function avAntwortAnwenden(rumpf, ansichtId) {
    if (rumpf.teilbaum) rpTeilbaumEinsetzen(rumpf.teilbaum);
    if (rumpf.ansichten && REPORTS) REPORTS.ansichten = rumpf.ansichten;
    /* Eine Schreibroute antwortet mit der beads-Haelfte (StandNach ohne
       GitHub) — sie kennt die zweite Quelle nicht und soll sie auch nicht
       holen muessen. Das memoisierte Ergebnis von GET /ansichten ist damit
       veraltet: es traegt einen Baum von VOR dem Schreibvorgang. Also
       verwerfen und neu holen, statt eine alte Zeile wieder einzusetzen. */
    AV_GH_ANS = null;
    avZeichne(ansichtId);
    return rumpf.nachladen || "";
  }

  /* Ein Auswahlgrund sagt eines von zwei Dingen, und dieser Unterschied ist der
     Punkt der Tabelle (gkp-2jc):

       EINORDNUNG   warum die Zeile in DIESER Liste steht. Vierzehn der fuenfzehn
                    Gruende sind das. Sie beschreiben, sie beanstanden nicht.
       BEANSTANDUNG an der Aufgabe stimmt etwas nicht. Bisher genau einer:
                    "ohne Beleg" heisst geschlossen ohne aktuellen done-Eintrag
                    — derselbe Befund, den beads_query op=gaps liefert und den
                    die Reports-Ansicht als Luecke malt (appctx/logbuch.go).

     Warum nicht einfach fuenfzehn Farben: dann sagt keine mehr etwas. Die
     Loesung ist deshalb umgekehrt gebaut — die Beanstandung bekommt Gewicht,
     indem den ruhigen Gruenden welches GENOMMEN wird. Die vier mit Termindruck
     oder Arbeitsstand behalten ihre alten Farben (.badge.r/.a/.c/.g), die zehn
     uebrigen werden mit av-ruhig leiser als ein blankes .badge, und nur
     av-mangel kommt an Farbe hinzu. Unterm Strich traegt eine Zeile hoechstens
     zwei farbige Gruende statt potenziell fuenfzehn.

     Ein Grund, den die Tabelle nicht kennt, faellt auf av-unbekannt und wird
     gestrichelt gezeichnet, statt still als Einordnung durchzugehen — eine neue
     Konstante in appctx soll man sehen. Geprueft wird das mit
     hasOwnProperty und nicht mit AV_GRUND[g], damit ein Grund namens
     "constructor" eine Klasse bekommt und keine Funktion.

     Die Reihenfolge der Gruende an der Zeile macht Go (Urteil.Gruende); hier
     wird nicht umsortiert, auch nicht, um die Beanstandung nach vorn zu ziehen
     — Auswahl und Sortierung gehoeren nach appctx. */
  var AV_GRUND = {
    /* Heute — appctx/heute.go */
    "überfällig": "r",
    "fällig heute": "a",
    "startklar": "c",
    "in Arbeit": "g",
    "geplant": "av-ruhig",
    /* Demnächst — appctx/demnaechst.go */
    "morgen": "av-ruhig",
    "diese Woche": "av-ruhig",
    "später": "av-ruhig",
    /* Irgendwann — appctx/irgendwann.go */
    "vertagt": "av-ruhig",
    /* Eingang — appctx/eingang.go */
    "ohne Karte": "av-ruhig",
    /* Logbuch — appctx/logbuch.go */
    "heute erledigt": "av-ruhig",
    "letzte 7 Tage": "av-ruhig",
    "früher": "av-ruhig",
    "ohne Abschlussdatum": "av-ruhig",
    "ohne Beleg": "r av-mangel"
  };

  function avGrundKlasse(g) {
    return Object.prototype.hasOwnProperty.call(AV_GRUND, g) ? AV_GRUND[g] : "av-ruhig av-unbekannt";
  }

  function avGrundMangel(g) { return avGrundKlasse(g).indexOf("av-mangel") >= 0; }

  /* Eine Beanstandung ist kein andersfarbiges Abzeichen, sondern ein anderes:
     sie traegt ein ▲ IM TEXT und sagt im Titel, was beanstandet wird. Beides
     ueberlebt, was Farbe nicht ueberlebt — einen Ausdruck, eine Rot-Gruen-
     Schwaeche, ein fremdes Farbschema. Die Farbe ist die Zugabe, nicht der
     Traeger der Aussage. */
  function avGrundAbzeichen(g) {
    var k = avGrundKlasse(g);
    if (k.indexOf("av-mangel") < 0) return rpBadge(k, g);
    return '<span class="badge ' + k + '" title="' +
      esc("Beanstandung: geschlossen, aber ohne aktuellen done-Eintrag — dieselbe Lücke, " +
          "die beads_query op=gaps meldet und die Reports-Ansicht als Lücke zeichnet.") +
      '">▲ ' + esc(g) + "</span>";
  }

  /* "vor 3 Tagen" / "heute" / "in 2 Tagen" aus einer Zahl, die Go geliefert
     hat. Gerechnet wird hier nichts — der Kalendertagabstand steht im Rumpf,
     weil er von der Zone abhaengt und eine zweite Rechnung im Browser genau
     die Doppelung waere, die dieses Frontend vermeidet. */
  function avTageText(n) {
    if (n == null) return "";
    if (n === 0) return "heute";
    if (n === -1) return "gestern";
    if (n === 1) return "morgen";
    return n < 0 ? "vor " + (-n) + " Tagen" : "in " + n + " Tagen";
  }

  function avZeichne(id) {
    var a = avAnsicht(id);
    var def = AV_REG[id] || {};
    list.innerHTML = "";
    if (!a) {
      /* Ohne aktives Projekt (gkp-92i: /heute & Co. ohne Projektsegment) bringt
         der Seitenaufbau REPORTS.ansichten noch gar nicht mit — das Aggregat
         kommt ausschliesslich ueber GET /ansichten nach. Ohne diesen Aufruf
         bliebe die Zeile hier stehen, weil avGithubNachziehen sonst nur nach
         einem ERFOLGREICHEN a.eintraege-Zugriff lief (return oben, davor). */
      detail.innerHTML = '<div class="av-wrap"><div class="dt-empty">' +
        esc(def.label || id) + (ACTIVE ? " steht in diesen Daten nicht — Projekt ohne beads?" : " lädt…") +
        "</div></div>";
      avGithubNachziehen(id);
      return;
    }

    /* Die Bilanz kommt als LISTE und wird in genau dieser Reihenfolge gemalt:
       dringend zuerst. Frueher stand hier ein Object.keys(...).sort() — das
       sortierte nach Code-Einheiten, "überfällig" faengt mit U+00FC an und
       stand damit hinter jedem ASCII-Wort, also ganz rechts. Sortiert wird in
       appctx/ansicht.go (ansichtAus), nicht hier. */
    var zaehler = (a.zaehler || []).map(function (z) {
      /* Auch hier oben wird die Beanstandung als solche gezeichnet, und das ist
         die wichtigere der beiden Stellen: im Logbuch ist genau diese Zahl die,
         die beads_query op=gaps beantwortet (appctx/logbuch.go). Grau neben
         "letzte 7 Tage" waere sie zu sehen und trotzdem unsichtbar. */
      var m = avGrundMangel(z.grund);
      return '<span class="av-z' + (m ? " av-mangel" : "") + '"><b>' +
        esc(String(z.anzahl)) + "</b>" + (m ? "▲ " : "") + esc(z.grund) + "</span>";
    }).join("");

    var kopf = '<div class="av-kopf"><b>' + esc(a.titel || id) + "</b>" +
      '<span class="lc">' + esc(a.tag) + " · " + a.eintraege.length + " Aufgabe(n)</span>" +
      '<span class="av-zs">' + zaehler + "</span></div>" +
      (a.hinweis ? '<div class="av-hinweis">' + esc(a.hinweis) + "</div>" : "") +
      /* Warum keine GitHub-Zeile dabei ist, sagt Go (github_leer). Ohne das
         ist "dieses Repo hat keine Issues" von "GitHub war nicht erreichbar"
         nicht zu unterscheiden, und die Liste saehe vollstaendig aus, ohne es
         zu sein — dieselbe Lücke, die der Hinweis darueber schliesst. */
      (AV_GH_GRUND ? '<div class="av-hinweis">Ohne GitHub: ' + esc(AV_GH_GRUND) + "</div>" : "") +
      /* projekt_fehler (gkp-92i/gkp-s9n) nennt Projekte, die im Sammellauf
         NICHT geprueft wurden — sei es, weil ihr Aufgabenbaum sich nicht
         aufbauen liess, oder weil das Zeitbudget des Laufs ausging, BEVOR sie
         an der Reihe waren (appctx.ansichtenUeberProjekte). Beides ist
         dieselbe Aussage an dieser Stelle: die Liste unten ist gerade
         UNVOLLSTAENDIG, nicht leer — ein stilles Weglassen saehe aus wie
         Vollstaendigkeit (dieselbe Begruendung wie am github_leer-Hinweis
         darueber, hier bloss ueber mehrere Projekte statt einer Quelle). */
      (AV_PROJEKT_FEHLER.length
        ? '<div class="av-hinweis">Unvollständig — nicht geprüft: ' +
            AV_PROJEKT_FEHLER.map(function (f) {
              return esc(f.projekt) + " (" + esc(f.grund) + ")";
            }).join(", ") +
            "</div>"
        : "");

    var wrap = el('<div class="av-wrap" data-av-ansicht="' + esc(id) + '">' + kopf +
      '<div class="av-liste"></div></div>');
    var liste = wrap.querySelector(".av-liste");

    /* Was eine leere Liste BEDEUTET, sagt die Regel (appctx: Regel.Leer), nicht
       dieser Block. Hier stand der Satz generisch — "Nichts offen" —, und in
       fuenf von sechs Ansichten stimmte er; im Logbuch ist dort nichts
       GESCHLOSSEN, nicht nichts offen. Ein Satz, der in der Mehrzahl der Faelle
       zufaellig passt, ist in den uebrigen eine Falschauskunft, und gerade eine
       leere Liste hat nichts ausser diesem Satz, woran man sie pruefen koennte.

       Der Ausweichtext sagt nur noch das, was fuer JEDE Liste gilt: dass leer
       nicht kaputt heisst — er behauptet nichts ueber eine BESTIMMTE Liste und
       kann deshalb in keiner Ansicht falsch sein. Genau darin unterscheidet er
       sich vom alten "Nichts offen", das im Logbuch die Umkehrung der Wahrheit
       war.

       Hier stand bis zuletzt, er greife bei einem Rumpf aus einem aelteren
       Stand, weil "der Projektkontext auf der Platte liegt und einen Neustart
       um bis zu cache.TTLMs ueberlebt". Das ist falsch, nachgemessen: der
       Plattencache haelt cache.Aggregate, und das hat drei Felder
       (GeneratedAt, Global, Projects) — kein ansichten. Die Ansichten liegen in
       cache.projectContextCache, einer prozesslokalen Go-Map, die mit dem
       Prozess stirbt; auch der "stale but present"-Pfad reicht nur Kontext
       DESSELBEN laufenden Binaries heraus. Und Leer kann dort nicht fehlen:
       ansichtAus ist die einzige Stelle, die eine Ansicht baut, sie kopiert
       r.Leer bedingungslos, und TestJedeAnsichtNenntIhrenEigenenLeersatz
       erzwingt fuer jede Regel einen nicht-leeren Satz.

       Nach heutiger Messung ist dieser Zweig also unerreichbar. Er bleibt als
       Netz gegen Versionsversatz stehen — ein Browser-Tab, dessen Seite von
       einem Binary vor gkp-3oh stammt, traegt eine Nutzlast ohne leer. Das ist
       eine andere und viel schwaechere Behauptung als die alte, und sie steht
       hier als das, was sie ist: ein Restrisiko, das niemand gemessen hat. */
    if (!a.eintraege.length) {
      liste.appendChild(el('<div class="li-empty">' +
        esc(a.leer || "Die Liste ist leer, nicht kaputt.") + "</div>"));
    }
    /* Gruppenkoepfe je Projekt (gkp-s9n), NUR in der Sammelansicht (ohne
       aktives Projekt, gkp-92i) — bei gewaehltem Einzelprojekt gaebe es genau
       eine Gruppe, und ihr Kopf waere die Wiederholung des Namens, der schon
       in der Brotkrume und im Sidebar-Eintrag steht (Abnahmekriterium dieses
       Beads). Die Zaehlung je Projekt laeuft VOR dem Zeichnen und getrennt
       davon, als schlichte Tally-Schleife (forEach, kein .filter/.reduce) —
       Auswahl und Sortierung bleiben bei appctx (ansichtAus/sammle): hier wird
       nur gezaehlt, was bereits als EIGENE Zeile dasteht (Ueberschriften
       zaehlen nicht mit, aus demselben Grund wie in appctx.ansichtAus). */
    var zeigeGruppen = !ACTIVE;
    var projektAnzahl = {};
    if (zeigeGruppen) {
      a.eintraege.forEach(function (e) {
        if (e.ueberschrift) return;
        projektAnzahl[e.projekt] = (projektAnzahl[e.projekt] || 0) + 1;
      });
    }
    var letztesProjekt = null;
    /* Stufe 1 (gkp-dnz): die Liste ist FLACH (e.tiefe traegt die Einrueckung,
       Spec §4.1), das Zuklappen einer Zeile mit Kindern muss also die
       NACHFOLGENDEN Zeilen mit groesserer Tiefe verstecken, bis die naechste
       Zeile auf gleicher oder kleinerer Tiefe kommt — derselbe Stapel wie in
       appctx.kappen (ansicht.go), nur hier ueber "versteckt" statt "behalten".
       afStapel traegt fuer jeden noch offenen Vorfahren dessen Tiefe und ob ER
       zugeklappt ist; eine Zeile ist versteckt, wenn IRGENDEIN Vorfahre es ist
       — ein zugeklappter Grossvater versteckt den Vater mit, egal ob der
       Vater selbst offen waere. */
    var afStapel = [];
    a.eintraege.forEach(function (e) {
      while (afStapel.length && afStapel[afStapel.length - 1].tiefe >= e.tiefe) afStapel.pop();
      var versteckt = afStapel.some(function (s) { return s.zu; });
      if (zeigeGruppen && e.projekt !== letztesProjekt) {
        letztesProjekt = e.projekt;
        /* Die Herkunftsmarke unterscheidet ein GitHub-Repo von einem
           beads-Projekt an der Gruppe (Bead-Notiz: beide koennen als eigene
           Gruppe erscheinen) — dieselbe Farbsprache wie an der Zeile selbst
           (rpBadge(e.herkunft === "github" ? "a" : "", ...) weiter unten). */
        var mark = e.herkunft === "github" ? "a" : "c";
        liste.appendChild(el(
          '<div class="li-group av-gk"><span class="av-gk-mark ' + mark + '" title="' +
            esc(e.herkunft === "github" ? "GitHub" : "beads") + '"></span>' +
            '<span class="av-gk-name">' + esc(e.projekt || "(ohne Projekt)") + "</span>" +
            '<span class="av-gk-c">' + esc(String(projektAnzahl[e.projekt] || 0)) + "</span></div>"
        ));
      }
      var b = avBeadSuchen(e.bead) || avFremdeAufgabe(e);
      if (!b) {
        /* Go nennt eine Aufgabe, die dieser Browser nicht hat. Das ist kein
           Grund, die Zeile zu verschlucken — verschluckt saehe die Liste
           vollstaendig aus. Ob SIE Kinder hat, ist von hier aus nicht zu
           sehen — sie bleibt also immer offen (zu:false), sonst verschwaende
           ein Ast, dessen einziger Beweis fuer seine Existenz diese eine
           Zeile ist. */
        var fehlt = el('<div class="li av-li av-fehlt">' + esc(e.bead) +
          " — Zeile nicht im geladenen Baum, Ansicht neu laden</div>");
        if (versteckt) fehlt.hidden = true;
        liste.appendChild(fehlt);
        afStapel.push({ tiefe: e.tiefe, zu: false });
        return;
      }
      var row = avZeile(id, e, b);
      if (versteckt) row.hidden = true;
      liste.appendChild(row);
      if (b.children && b.children.length) {
        afStapel.push({ tiefe: e.tiefe, zu: !avAufOffen(e) });
      }
    });

    /* Warum die Liste hier aufhoert, sagt Go (Regel.Grenze, appctx). Der Satz
       steht am ENDE und nicht im Kopf: oben faellt nicht auf, dass etwas fehlt,
       unten ist eine gekappte Liste von einer zu Ende gegangenen sonst gar
       nicht zu unterscheiden — dasselbe Argument wie bei a.leer darueber.
       Gerechnet wird auch hier nichts; die Zahlen im Satz kennt nur der Rahmen,
       der die Kappung vorgenommen hat. */
    if (a.gekappt) {
      liste.appendChild(el('<div class="av-gekappt">' + esc(a.gekappt) + "</div>"));
    }

    detail.innerHTML = "";
    detail.appendChild(wrap);
    avBinden(id, wrap);
    avGithubNachziehen(id);
  }

  /* Eine Aufgabe, die nicht aus beads kommt (gkp-pf2), steht nicht in
     REPORTS.maps — nach E1 laufen die beiden Quellen nebeneinander, und die
     GitHub-Seite wird bewusst NICHT in den beads-Baum gemischt (dort zaehlen
     Fortschritt, Belegstand und Historie mit). Go schickt den Zeileninhalt
     deshalb am Eintrag mit, und zwar nur fuer genau diese Zeilen.

     `extern` ist die eine Eigenschaft, die die Zeile braucht: sie ist lesend.
     Schreiben gegen GitHub ist nicht im Umfang (Spec §7, §10), also darf an so
     einer Zeile auch kein Knopf stehen, der es anbietet. */
  function avFremdeAufgabe(e) {
    if (!e.titel) return null;
    return { id: e.bead, title: e.titel, status: e.status || "", extern: true };
  }

  /* Die GitHub-Haelfte der Ansichten kommt nach, nicht mit: sie kostet einen
     Netzaufruf und haelt sonst den Seitenaufbau auf (gkp-isj) — dieselbe
     Begruendung, aus der GET /github und GET /plan-svg eigene Routen sind.
     Zusammengefuegt hat sie Go; hier wird nur die fertige Liste eingesetzt und
     neu gezeichnet.

     Die Wiederholung bricht an der Identitaet ab: nach dem ersten Einsetzen ist
     REPORTS.ansichten dasselbe Objekt, das die memoisierte Antwort traegt. Ohne
     diesen Vergleich riefe avZeichne sich selbst endlos auf. */
  var AV_GH_ANS = null;
  var AV_GH_GRUND = "";
  /* projekt_fehler (AnsichtenAggregatAntwort, nur ohne project) nennt
     Projekte, die der Sammellauf NICHT geprueft hat — Begruendung am
     av-hinweis in avZeichne oben. Neu gesetzt bei JEDER Antwort (auch mit
     leerem Array): sonst bliebe beim Wechsel von der Sammelansicht in ein
     einzelnes Projekt (das dieses Feld gar nicht kennt) die letzte Liste
     stehen. */
  var AV_PROJEKT_FEHLER = [];
  /* Der Netzaufruf selbst, geteilt zwischen kopfNavLaden (Kopf-Navigation +
     Projektliste, laeuft IMMER genau einmal beim Seitenaufbau) und
     avGithubNachziehen (die eine geoeffnete Ansicht danach). Eine Funktion und
     nicht zwei Kopien des fetch-Aufrufs, weil AV_GH_ANS das Single-Flight
     traegt: der zweite Aufrufer bekommt dasselbe Promise, keinen zweiten
     Request. */
  function avAnsichtenHolen() {
    if (!AV_GH_ANS) {
      AV_GH_ANS = fetch("/ansichten?" + qs({ project: ACTIVE }))
        .then(function (r) { return r.json(); })
        .catch(function (e) { return { github_leer: "GET /ansichten: " + e }; });
    }
    return AV_GH_ANS;
  }
  /* kopfNavLaden speist NUR die obere Navigation (viewnav, gkp-s9n) und
     stoesst die Projektliste-Stats an — sie zeichnet KEINE Ansicht neu. Das
     ist der Unterschied zu avGithubNachziehen darunter: kopfNavLaden laeuft
     EINMAL beim Seitenaufbau, unabhaengig davon, was der Nutzer gerade ansieht
     (Skills, Reports, …) — ein avZeichne(irgendeinId) waere dort in den
     meisten Faellen falsch, weil gar keine der sechs Ansichten offen ist. */
  function kopfNavLaden() {
    avAnsichtenHolen().then(function (j) {
      if (!j) return;
      AV_GH_GRUND = j.github_leer || "";
      AV_PROJEKT_FEHLER = j.projekt_fehler || [];
      navZahlenUebernehmen(j.nav);
      projStatsLaden(j);
    });
  }
  function avGithubNachziehen(ansichtId) {
    avAnsichtenHolen().then(function (j) {
      if (!j || !REPORTS) return;
      AV_GH_GRUND = j.github_leer || "";
      AV_PROJEKT_FEHLER = j.projekt_fehler || [];
      /* VOR dem Abbruch unten, und das ist Absicht: die Navigation zeigt SECHS
         Ansichten, die geoeffnete ist nur eine davon. Stuende das hinter dem
         Identitaetsvergleich, blieben die fuenf anderen Zahlen beim zweiten
         Zeichnen auf dem Seitenaufbau-Stand — unsichtbar falsch, weil niemand
         sie offen hat. */
      navZahlenUebernehmen(j.nav);
      projStatsLaden(j);
      if (!j.ansichten || REPORTS.ansichten === j.ansichten) return;
      REPORTS.ansichten = j.ansichten;
      avZeichne(ansichtId);
    });
  }

  /* --- Stufe 1 (gkp-dnz): Auf-/Zuklappen von Unteraufgaben ---
     Vorbild fuer die Persistenz ist RP_ZU_SCHLUESSEL/RP_NUR_OFFEN weiter oben
     — ein einzelnes Flag genuegt dort, weil es EINE Einstellung fuer die ganze
     Liste ist. Hier steht dagegen ein Zustand JE BEAD, deshalb ein Objekt statt
     eines Booleans, sonst aber dasselbe Muster: lesen mit try/catch (privates
     Fenster, Kontingent voll), schreiben nach jeder Aenderung.

     Es wird nur die ABWEICHUNG vom Standardzustand gespeichert (avAufOffen
     faellt auf e.ueberschrift zurueck), nicht der Zustand jeder je gesehenen
     Zeile — sonst wüchse der Speicher mit jeder geöffneten Aufgabe, obwohl die
     meisten nie umgeschaltet werden. */
  var AV_AUF_SCHLUESSEL = "gkp.av.aufgeklappt";
  var AV_AUF = (function () {
    try {
      var roh = localStorage.getItem(AV_AUF_SCHLUESSEL);
      return roh ? JSON.parse(roh) : {};
    } catch (e) { return {}; }
  })();
  function avAufSetzen(bead, offen) {
    AV_AUF[bead] = offen;
    try { localStorage.setItem(AV_AUF_SCHLUESSEL, JSON.stringify(AV_AUF)); }
    catch (e) { /* privates Fenster, Kontingent voll — der Klick wirkt trotzdem, nur diese Session */ }
  }
  /* Default ist e.ueberschrift: eine Zwischenueberschrift steht seit gkp-a1d
     aufgeklappt da (eine Ueberschrift ohne sichtbaren Inhalt waere sinnlos —
     Begruendung an AnsichtEintrag.Ueberschrift), eine normale Aufgabenzeile MIT
     eigenem Grund dagegen zu: sie hat bereits einen wahren Grund, hier zu
     stehen, ihre Unteraufgaben sind ein Extra und keine Voraussetzung dafuer.
     Ein Twisty an einer Ueberschrift SCHLIESST deshalb beim ersten Klick, einer
     an einer normalen Zeile OEFFNET. */
  function avAufOffen(e) {
    if (Object.prototype.hasOwnProperty.call(AV_AUF, e.bead)) return AV_AUF[e.bead];
    return !!e.ueberschrift;
  }

  /* --- Stufe 2 (gkp-dnz): Belege lazy je Zeile ---
     Der Zaehler (e.beleg_plan/e.beleg_done) kommt EAGER aus appctx.ansicht.go
     mit jeder Zeile — billig, weil er nur ueber eine bereits im Speicher
     stehende Liste zaehlt (reports.ListMaps hat die Datei ohnehin fuer den
     Baum gelesen). Die EINTRAEGE selbst (Beleg-Text, Bilder) bleiben aussen
     vor: ein einzelnes proof.out darf 256 KiB gross sein, und bei rund 250
     offenen Aufgaben tragen gemessen nur 11 ueberhaupt einen Eintrag — sie
     eager mitzuschicken waere Ballast in jeder Antwort fuer einen Fall, der in
     95 % der Zeilen gar nicht zutrifft. Geholt wird deshalb erst beim
     Aufklappen, ueber GET /belege (gkp-e9h) — dieselbe Aufschub-Idee wie
     rpLazySec weiter oben, nur mit einem Netzaufruf statt eines synchronen
     Aufbaus. */
  function avAnzBelege(plan, done) {
    var teile = [];
    if (plan) teile.push(plan + " plan");
    if (done) teile.push(done + " done");
    return teile.join(" · ");
  }

  function avBelegeSec(e, stufe) {
    var titel = avAnzBelege(e.beleg_plan || 0, e.beleg_done || 0);
    // --rp-t auf dem Knoten selbst: er ist ein GESCHWISTER von .li.av-li,
    // nicht sein Nachfahre, erbt die Custom Property also nicht von dort.
    var d = el('<details class="rp-lazy av-belege" style="--rp-t:' + stufe + '">' +
      '<summary class="rp-lazy-sum">' + esc(titel) + "</summary></details>");
    d.addEventListener("toggle", function () {
      if (!d.open || d.getAttribute("data-gebaut")) return;
      d.setAttribute("data-gebaut", "1");
      var lade = el('<div class="dt-empty">lädt…</div>');
      d.appendChild(lade);
      fetch("/belege?" + qs({ project: e.projekt || ACTIVE, bead: e.bead }))
        .then(function (r) { return r.json(); })
        .then(function (j) {
          d.removeChild(lade);
          var entries = (j && j.entries) || [];
          var n = el('<div class="rp-other">' + (entries.length
            ? entries.map(rpEntryHtml).join("")
            : '<div class="dt-empty">Keine Einträge (mehr) gefunden.</div>') + "</div>");
          d.appendChild(n);
          // ZWINGEND, sonst oeffnet kein Bild-Modal (rpEntryHtml zeichnet die
          // Vorschau, rpBindImages bindet erst den Klick darauf).
          rpBindImages(d);
        })
        .catch(function (err) {
          d.removeChild(lade);
          d.appendChild(el('<div class="dt-empty">Fehler: ' + esc(String(err && err.message || err)) + "</div>"));
        });
    });
    return d;
  }

  function avZeile(ansichtId, e, b) {
    var stufe = Math.min(e.tiefe || 0, RP_TIEFE_MAX);
    var gruende = (e.gruende || []).map(avGrundAbzeichen).join("");
    var termin = "";
    if (e.frist) {
      termin += '<span class="av-termin" title="Frist (due_at) ' + esc(e.frist) + '">⏱ ' +
        esc(e.frist) + " · " + esc(avTageText(e.frist_tage)) + "</span>";
    }
    if (e.wann) {
      termin += '<span class="av-termin av-wann" title="Wann (defer_until) ' + esc(e.wann) + '">▸ ' +
        esc(e.wann) + "</span>";
    }
    /* gkp-a1d/gkp-s9n: FristHerkunft nennt die Bead-Id, aus der eine
       ZWISCHENUEBERSCHRIFT ihre Frist/Gruende geliehen hat. Ohne diesen
       Vermerk waere die Zahl an DIESER Zeile eine erfundene Angabe — sie steht
       nicht an diesem Bead (Begruendung: AnsichtEintrag.FristHerkunft). */
    var herkunftVermerk = e.frist_herkunft
      ? '<span class="av-erbe" title="Frist und Gründe geerbt von ' + esc(e.frist_herkunft) +
          ' — diese Zeile hat selbst keinen eigenen Grund">aus ' + esc(e.frist_herkunft) + "</span>"
      : "";
    var karte = avKarteSuchen(e.karte);
    /* Die Id einer fremden Aufgabe ist ein Link: sie fuehrt an den einzigen
       Ort, an dem sich mit ihr arbeiten laesst. Zusammengesetzt wird der Link
       nicht — Go liefert ihn fertig (AnsichtEintrag.URL), weil die Nummer ohne
       das Repo nichts oeffnet. */
    var titelZelle = e.url
      ? '<a class="rp-li-t" href="' + esc(e.url) + '" target="_blank" rel="noopener" title="' +
          esc(b.title || "") + '">' + esc(b.title || "") + "</a>"
      : '<span class="rp-li-t" title="' + esc(b.title || "") + '">' + esc(b.title || "") + "</span>";
    // Stufe 1 (gkp-dnz): der Twisty erscheint NUR, wenn es etwas aufzuklappen
    // gibt — b.children kommt aus REPORTS.maps (avBeadSuchen), derselbe Baum,
    // den der Balken (b.counts) schon liest.
    var kinder = !!(b.children && b.children.length);
    var offen = kinder && avAufOffen(e);
    var twisty = kinder
      ? '<button type="button" class="av-tw' + (offen ? " is-offen" : "") + '" aria-expanded="' +
          (offen ? "true" : "false") + '" title="' +
          (offen ? "Unteraufgaben einklappen" : "Unteraufgaben aufklappen") + '"></button>'
      : "";
    var row = el(
      '<div class="av-row" data-av-bead="' + esc(e.bead) + '" data-av-karte="' + esc(e.karte) +
          '" data-av-projekt="' + esc(e.projekt || "") + '">' +
        '<div class="li av-li' + (e.tiefe ? " is-sub" : "") +
            (e.ueberschrift ? " av-ueberschrift" : "") + '" style="--rp-t:' + stufe + '">' +
          "<div>" +
            '<div class="rp-li-z1">' +
              twisty +
              (e.tiefe > RP_TIEFE_MAX
                ? '<span class="rp-li-lv" title="Ebene ' + (e.tiefe + 1) + '">E' + (e.tiefe + 1) + "</span>"
                : "") +
              '<span class="rp-li-id">' + esc(e.bead) + "</span>" +
              titelZelle +
            "</div>" +
            '<div class="rp-li-meta">' +
              (b.counts ? rpFortschritt(b.counts, "Unteraufgaben") : "") +
              rpBadge(e.herkunft === "github" ? "a" : "", e.herkunft) +
              rpBadge(rpStatusCls(b.status), b.status || "?") +
              /* Kein P-Abzeichen an einer fremden Zeile: GitHub kennt keine
                 Prioritaet, und eine gezeichnete Zahl waere eine erfundene. */
              (b.extern ? "" : rpPrio(b.priority)) +
              gruende +
              termin +
              herkunftVermerk +
              '<span class="av-karte">' + esc(karte ? (karte.title || karte.id) : e.karte) + "</span>" +
            "</div>" +
          "</div>" +
          /* Eine Zwischenueberschrift (gkp-a1d) traegt KEINE Aktionsknoepfe —
             auch nicht den "nur lesend"-Hinweis darunter: sie ist Struktur,
             keine Aufgabe, an der man etwas terminieren oder schliessen
             koennte (Begruendung: AnsichtEintrag.Ueberschrift). Termin und
             Schliessen schreiben sonst ueber bdops nach beads; eine
             GitHub-Aufgabe hat dort kein Gegenstueck — der Knopf waere ein
             Angebot, das die Route nur ablehnen koennte (Spec §7: GitHub ist
             in Ausbaustufe 1 lesend). */
          (e.ueberschrift
            ? ""
            : (b.extern
                ? '<div class="av-akt"><span class="lc" title="Schreiben gegen GitHub ist nicht im Umfang (Spec §7)">nur lesend</span></div>'
                : '<div class="av-akt">' +
                    '<button type="button" class="av-b" data-av-akt="termin">Termin</button>' +
                    '<button type="button" class="av-b" data-av-akt="schliessen">Schließen</button>' +
                  "</div>")) +
        "</div>" +
        '<div class="av-form" hidden></div>' +
      "</div>"
    );
    if (kinder) {
      row.querySelector(".av-tw").addEventListener("click", function (ev) {
        ev.stopPropagation();
        avAufSetzen(e.bead, !offen);
        avZeichne(ansichtId);
      });
    }
    // Stufe 2 (gkp-dnz): NUR an Blaettern (kein Bead mit Kindern traegt
    // gemessen eigene Eintraege) und NUR an schreibbaren Zeilen — eine
    // GitHub-Zeile hat keine Belege (Spec §10, kein Report-Store dafuer).
    var belegeSumme = (e.beleg_plan || 0) + (e.beleg_done || 0);
    if (!kinder && !b.extern && belegeSumme > 0) {
      row.appendChild(avBelegeSec(e, stufe));
    }
    return row;
  }

  function avBinden(ansichtId, wrap) {
    [].forEach.call(wrap.querySelectorAll("[data-av-akt]"), function (btn) {
      btn.addEventListener("click", function (ev) {
        ev.stopPropagation();
        var row = btn.closest(".av-row");
        var form = row.querySelector(".av-form");
        var was = btn.getAttribute("data-av-akt");
        if (!form.hidden && form.getAttribute("data-av-offen") === was) {
          form.hidden = true;
          return;
        }
        form.setAttribute("data-av-offen", was);
        form.hidden = false;
        if (was === "termin") avTerminForm(ansichtId, row, form);
        else avSchliessForm(ansichtId, row, form);
      });
    });
  }

  function avMeldung(form, text, schlecht) {
    var m = form.querySelector(".av-msg");
    if (!m) return;
    m.className = "av-msg" + (schlecht ? " is-bad" : "");
    m.textContent = text;
  }

  /* --- Datum setzen: POST /action/task/terminieren ---
     Ein LEERES Feld heisst "nicht erwaehnt", nicht "loeschen". Der Unterschied
     ist der ganze Grund, warum taskops.TerminWunsch Zeiger fuehrt: ein Termin,
     der sich setzen, aber nie entfernen laesst, waere schlimmer als keiner —
     und ein leeres Feld, das still loescht, waere die Falle daneben. Loeschen
     hat deshalb einen eigenen Knopf, der "" schickt.

     Der Wert selbst wird NICHT geprueft: bds Grammatik (+6h, tomorrow,
     next monday, 2026-01-15) hier nachzubauen hiesse, eine Kopie zu pflegen,
     die mit jeder bd-Version auseinanderlaeuft. bd lehnt ab, die Antwort sagt
     warum. */
  function avTerminForm(ansichtId, row, form) {
    var id = row.getAttribute("data-av-bead");
    form.innerHTML =
      '<div class="av-fz">Termine für <b>' + esc(id) + "</b> — bd-Grammatik: " +
        "<code>tomorrow</code>, <code>+2w</code>, <code>2026-01-15</code></div>" +
      '<label class="av-f">Wann <small>defer_until · versteckt bis</small>' +
        '<input type="text" name="wann" autocomplete="off" placeholder="leer = unverändert"></label>' +
      '<label class="av-f">Frist <small>due_at · Deadline</small>' +
        '<input type="text" name="frist" autocomplete="off" placeholder="leer = unverändert"></label>' +
      '<div class="av-btn">' +
        '<button type="button" class="av-b av-b-go" data-av="setzen">setzen</button>' +
        '<button type="button" class="av-b" data-av="wann-weg">Wann löschen</button>' +
        '<button type="button" class="av-b" data-av="frist-weg">Frist löschen</button>' +
      "</div>" +
      '<div class="av-msg"></div>';

    function senden(wunsch) {
      var projekt = row.getAttribute("data-av-projekt") || "";
      var pfad = avPfadFuerProjekt(projekt);
      if (!pfad) {
        avMeldung(form, "Pfad für Projekt „" + projekt + "“ nicht gefunden — nicht geschrieben.", true);
        return;
      }
      avMeldung(form, "schreibe…", false);
      wunsch.pfad = pfad;
      wunsch.id = id;
      avPostJSON("/action/task/terminieren", wunsch).then(function (a) {
        if (!a.ok) { avMeldung(form, avFehlertext(a.rumpf), true); return; }
        var nach = avAntwortAnwenden(a.rumpf, ansichtId);
        if (nach) alert("Geschrieben, aber die Zeile konnte nicht ersetzt werden: " + nach);
      });
    }

    form.querySelector('[data-av="setzen"]').addEventListener("click", function () {
      var wann = form.querySelector('[name="wann"]').value.trim();
      var frist = form.querySelector('[name="frist"]').value.trim();
      if (!wann && !frist) { avMeldung(form, "Beide Felder leer — nichts zu tun.", true); return; }
      var w = {};
      if (wann) w.wann = wann;
      if (frist) w.frist = frist;
      senden(w);
    });
    form.querySelector('[data-av="wann-weg"]').addEventListener("click", function () { senden({ wann: "" }); });
    form.querySelector('[data-av="frist-weg"]').addEventListener("click", function () { senden({ frist: "" }); });
  }

  /* --- Schliessen mit Beleg: POST /action/task/schliessen ---
     Das Bild ist kein Beiwerk. Das Tor in internal/closeops verlangt einen
     AKTUELLEN done-Eintrag mit einem sichtbaren Element; ein {cmd,out}-Beleg
     allein genuegt nicht (reports.HasValidatedDoneEntry). Der Dialog nimmt es
     deshalb per Drag-and-drop, per Dateiauswahl oder aus der Zwischenablage
     entgegen — der Browser hat keinen Dateipfad zu bieten, den der Dienst
     oeffnen koennte, also muessen die Bytes mit.

     Geprueft wird das Bild hier NICHT. Ob die Bytes ein Bild sind, entscheidet
     internal/reports an den Magic-Bytes; eine mildere Vorpruefung hier waere
     das eigentliche Tor. */
  function avSchliessForm(ansichtId, row, form) {
    var id = row.getAttribute("data-av-bead");
    var karte = row.getAttribute("data-av-karte");
    form.innerHTML =
      '<div class="av-fz">Schließen von <b>' + esc(id) + "</b> — ein done-Beleg mit " +
        "<b>sichtbarem</b> Element ist Pflicht (Bild oder .dot/.svg).</div>" +
      '<label class="av-f">Titel des Belegs<input type="text" name="titel" autocomplete="off"></label>' +
      '<label class="av-f">Wirkung<textarea name="wirkung" rows="2"></textarea></label>' +
      '<label class="av-f">Grund <small>bd close --reason, geht ins Audit-Log</small>' +
        '<input type="text" name="grund" autocomplete="off"></label>' +
      '<div class="av-drop" tabindex="0">Bild hierher ziehen, einfügen (Strg+V) oder ' +
        '<button type="button" class="av-b" data-av="waehlen">auswählen</button>' +
        '<input type="file" accept="image/*" hidden></div>' +
      '<div class="av-vorschau"></div>' +
      '<div class="av-btn"><button type="button" class="av-b av-b-go" data-av="zu">schließen</button></div>' +
      '<div class="av-msg"></div>';

    var bild = null;
    var drop = form.querySelector(".av-drop");
    var datei = form.querySelector('input[type="file"]');
    var vorschau = form.querySelector(".av-vorschau");

    function bildNehmen(f) {
      if (!f) return;
      bild = f;
      vorschau.innerHTML = "";
      var img = document.createElement("img");
      img.src = URL.createObjectURL(f);
      img.alt = f.name || "Beleg";
      vorschau.appendChild(img);
      vorschau.appendChild(el('<span class="av-fz">' + esc(f.name || "eingefügt") + " · " +
        Math.round((f.size || 0) / 1024) + " KiB</span>"));
      avMeldung(form, "Bild bereit.", false);
    }

    form.querySelector('[data-av="waehlen"]').addEventListener("click", function () { datei.click(); });
    datei.addEventListener("change", function () { bildNehmen(datei.files && datei.files[0]); });
    drop.addEventListener("dragover", function (ev) { ev.preventDefault(); drop.classList.add("is-over"); });
    drop.addEventListener("dragleave", function () { drop.classList.remove("is-over"); });
    drop.addEventListener("drop", function (ev) {
      ev.preventDefault();
      drop.classList.remove("is-over");
      bildNehmen(ev.dataTransfer && ev.dataTransfer.files && ev.dataTransfer.files[0]);
    });
    /* Der paste-Horcher haengt am Formular und nicht am document: sonst fuehre
       ein Strg+V irgendwo auf der Seite ein Bild in ein Formular ein, das der
       Benutzer gerade nicht ansieht. */
    form.addEventListener("paste", function (ev) {
      var items = (ev.clipboardData && ev.clipboardData.items) || [];
      for (var i = 0; i < items.length; i++) {
        if (items[i].kind === "file") { bildNehmen(items[i].getAsFile()); ev.preventDefault(); return; }
      }
    });

    form.querySelector('[data-av="zu"]').addEventListener("click", function () {
      var titel = form.querySelector('[name="titel"]').value.trim();
      if (!titel) { avMeldung(form, "Ohne Titel kein Bericht — und ohne Bericht kein Beleg.", true); return; }
      var projekt = row.getAttribute("data-av-projekt") || "";
      var pfad = avPfadFuerProjekt(projekt);
      if (!pfad) {
        avMeldung(form, "Pfad für Projekt „" + projekt + "“ nicht gefunden — nicht geschrieben.", true);
        return;
      }
      var auftrag = {
        pfad: pfad,
        karte: karte,
        wuensche: [{
          bead: id,
          grund: form.querySelector('[name="grund"]').value.trim(),
          bericht: { titel: titel, wirkung: form.querySelector('[name="wirkung"]').value.trim() },
        }],
      };
      var fd = new FormData();
      fd.append("auftrag", JSON.stringify(auftrag));
      if (bild) fd.append("bild", bild, bild.name || "beleg.png");
      avMeldung(form, "schreibe…", false);
      avPostForm("/action/task/schliessen", fd).then(function (a) {
        if (!a.ok) { avMeldung(form, avFehlertext(a.rumpf), true); return; }
        avAntwortAnwenden(a.rumpf, ansichtId);
      });
    });
  }

  /* ---------- Bereiche (gkp-8ww, Spec §4.3 / E2, §5) ----------

     Die Ansicht, die den SONDERFALL des Modells bedient: Area → Projekt ist
     die einzige Ebene, die es nur gibt, weil jemand sie festlegt. bd, GitHub
     und git liefern alles andere; hier wird es geschrieben.

     Was hier NICHT passiert — und das ist derselbe Punkt wie bei den
     Aufgaben-Ansichten: gruppieren und sortieren. Beides kommt fertig aus Go
     (appctx/bereiche.go). Der Browser laeuft ueber `gruppen` in der gegebenen
     Reihenfolge und zeichnet. Die Regel ist hier sogar noch strenger zu
     nehmen als sonst, weil die Reihenfolge der Areas die der DATEI ist: sie
     im Browser zu sortieren wuerfe genau die Ordnung weg, die jemand von Hand
     aufgeschrieben hat.

     Fuenf Operationen, alle ueber POST /action/task/bereich*: anlegen,
     umbenennen, loeschen (gkp-td3), zuordnen, Zuordnung loesen. Jede antwortet
     mit der NEU GEBAUTEN Gruppierung — das Gegenstueck zum `teilbaum` der
     Aufgabenrouten. Ein Teilbaum passt hier nicht: ein Umbenennen zieht jedes
     Projekt seiner Area mit, das waeren n Teile statt eines.

     Das Loeschen ist die Operation, die die Ansicht ueberhaupt erst
     AUSPROBIERBAR macht: ohne sie blieb jeder Versuch — ein Tippfehler, ein
     Antippen — dauerhaft in areas.json und dauerhaft in der Auswahlliste jedes
     Projekts. Es geht nur bei einer LEEREN Area; eine belegte lehnt die Route
     ab und nennt die Projekte, die im Weg stehen (areas.Store.Loeschen). */

  function bzBlock() { return (DATA && DATA.bereiche) || {}; }

  /* Die Antwort einarbeiten. Nur wenn ein `bereiche`-Block dabei ist — ein
     Fehlschlag traegt keinen, und ein stehengelassener alter Stand ist besser
     als ein leerer. */
  function bzAnwenden(rumpf) {
    if (rumpf && rumpf.bereiche && DATA) DATA.bereiche = rumpf.bereiche;
    bzZeichne();
  }

  function bzMeldung(text, schlecht) {
    var m = detail.querySelector(".bz-msg");
    if (!m) return;
    m.className = "bz-msg" + (schlecht ? " is-bad" : "");
    m.textContent = text;
  }

  function bzSchreiben(pfad, rumpf) {
    bzMeldung("schreibe…", false);
    return avPostJSON(pfad, rumpf).then(function (a) {
      if (!a.ok) { bzMeldung(avFehlertext(a.rumpf), true); return false; }
      bzAnwenden(a.rumpf);
      return true;
    });
  }

  /* Die Auswahlliste eines Projekts. Die Namen kommen aus dem Block
     (`bereiche`), den Go aus DENSELBEN Gruppen ableitet, die daneben gezeichnet
     werden — es kann also keine Area zur Wahl stehen, die die Ansicht nicht
     zeigt, und keine gezeigte fehlen.

     Der erste Eintrag ist das Loesen der Zuordnung und traegt den leeren Wert:
     "" heisst in taskops.BereichWunsch "Zuordnung loeschen" (areas.SetArea),
     nicht "Bereich namens leer". */
  function bzAuswahl(projekt, aktuell, istOhne) {
    var namen = bzBlock().bereiche || [];
    var ohneName = bzBlock().ohne_name || "Ohne Bereich";
    var opts = '<option value=""' + (istOhne ? " selected" : "") + ">— " + esc(ohneName) + " —</option>";
    namen.forEach(function (n) {
      opts += '<option value="' + esc(n) + '"' + (!istOhne && n === aktuell ? " selected" : "") +
        ">" + esc(n) + "</option>";
    });
    return '<select class="bz-sel" data-bz-projekt="' + esc(projekt) + '">' + opts + "</select>";
  }

  function bzZeichne() {
    var block = bzBlock();
    var gruppen = block.gruppen || [];
    list.innerHTML = "";

    var ohneAnzahl = 0;
    var projekte = 0;
    gruppen.forEach(function (g) {
      projekte += (g.projekte || []).length;
      if (g.ohne) ohneAnzahl = (g.projekte || []).length;
    });

    var kopf = '<div class="av-kopf"><b>Bereiche</b>' +
      '<span class="lc">' + gruppen.length + " Gruppe(n) · " + projekte + " Projekt(e)</span>" +
      '<span class="av-zs">' +
        (ohneAnzahl ? '<span class="av-z is-a"><b>' + ohneAnzahl + "</b>ohne Bereich</span>" : "") +
      "</span></div>" +
      /* Die Einschraenkung gehoert in den Kopf, nicht in eine Fussnote: diese
         Ansicht zeigt die ZUORDNUNG, nicht die Aufgaben einer Area. Spec §5
         nennt beides unter "Area-Ansicht"; was fehlt, muss dastehen
         (Spec §4.4). */
      '<div class="av-hinweis">Area → Projekt (Spec §4.3, E2). Die Zuordnung steht in ' +
        esc(block.pfad || "areas.json") + ' und wird von Hand gepflegt. ' +
        'Ein Projekt ohne Zuordnung steht unter „' + esc(block.ohne_name || "Ohne Bereich") +
        '“ — sichtbar, damit die Pflege auffällt. Die AUFGABEN einer Area zeigt diese Liste nicht; ' +
        'sie stehen je Projekt in Heute, Demnächst, Jederzeit.</div>' +
      /* Eine unlesbare areas.json wird GEMELDET. Stillschweigend als leerer
         Zustand gezeichnet, saehe sie aus wie „noch nichts zugeordnet“ — und
         der naechste Schreibvorgang machte genau diese Leere endgueltig. */
      (block.fehler ? '<div class="av-hinweis is-bad">areas.json: ' + esc(block.fehler) + "</div>" : "") +
      '<div class="bz-neu">' +
        '<input type="text" class="bz-in" data-bz-neu placeholder="neuer Bereich…">' +
        '<button type="button" class="av-b" data-bz-akt="anlegen">Anlegen</button>' +
        '<span class="bz-msg"></span>' +
      "</div>";

    var wrap = el('<div class="av-wrap bz-wrap">' + kopf + '<div class="av-liste"></div></div>');
    var liste = wrap.querySelector(".av-liste");

    if (!gruppen.length) {
      liste.appendChild(el('<div class="li-empty">Keine Projekte entdeckt — die Liste ist leer, nicht kaputt.</div>'));
    }

    gruppen.forEach(function (g) {
      var ps = g.projekte || [];
      liste.appendChild(el(
        '<div class="bz-gruppe' + (g.ohne ? " is-ohne" : "") + '" data-bz-gruppe="' + esc(g.bereich) + '">' +
          '<span class="bz-name">' + esc(g.bereich) + "</span>" +
          '<span class="lc">' + ps.length + "</span>" +
          /* Die Sammel-Area bekommt weder Umbenennen- noch Loeschen-Knopf: sie
             steht in keiner Datei, sondern entsteht beim Lesen
             (areas.OhneBereich). Ein Knopf waere ein Angebot, das die Route nur
             ablehnen koennte — beim Loeschen waere der einzige denkbare Sinn
             ausserdem, die unzugeordneten Projekte zu verstecken, also das
             Gegenteil von Spec §4.3. */
          /* Die Aktionen stehen in EINEM Behaelter. Ohne ihn traegt nur der
             letzte Knopf das `margin-left:auto` aus `.bz-gruppe > :last-child`
             — der zweite rutschte an den rechten Rand, der erste bliebe an der
             Zahl kleben. */
          '<span class="bz-akt">' +
          (g.ohne
            ? '<span class="lc" title="Die Sammel-Area entsteht beim Lesen und steht in keiner Datei">nicht umbenennbar · nicht löschbar</span>'
            : '<button type="button" class="av-b" data-bz-akt="umbenennen">Umbenennen</button>' +
              /* Kein Bestaetigungsdialog. Er gehoerte vor einen teuren Verlust,
                 und den gibt es hier nicht: loeschbar ist nur eine LEERE Area,
                 und die stellt das Eingabefeld eine Zeile weiter oben in einem
                 Anlauf wieder her. Ein Dialog vor einer billigen, umkehrbaren
                 Sache erzieht dazu, ihn wegzuklicken — und dann steht er auch
                 dort nicht mehr im Weg, wo er zaehlte. */
              '<button type="button" class="av-b" data-bz-akt="loeschen">Löschen</button>') +
          "</span>" +
        "</div>"
      ));
      if (!ps.length) {
        liste.appendChild(el('<div class="li-empty bz-leer">keine Projekte — als Ziel einer Zuweisung wählbar</div>'));
      }
      ps.forEach(function (pr) {
        liste.appendChild(el(
          '<div class="li av-li bz-proj">' +
            "<div>" +
              '<div class="rp-li-z1"><span class="rp-li-t" title="' + esc(pr.pfad || "") + '">' +
                esc(pr.name) + "</span></div>" +
              '<div class="rp-li-meta"><span class="lc">' + esc(pr.pfad || "") + "</span></div>" +
            "</div>" +
            '<div class="av-akt">' + bzAuswahl(pr.name, g.bereich, !!g.ohne) + "</div>" +
          "</div>"
        ));
      });
    });

    detail.innerHTML = "";
    detail.appendChild(wrap);
    bzBinden(wrap);
  }

  function bzBinden(wrap) {
    var neu = wrap.querySelector("[data-bz-neu]");

    wrap.querySelector('[data-bz-akt="anlegen"]').addEventListener("click", function () {
      var name = (neu.value || "").trim();
      /* Nicht "" abfangen und nichts tun: die Route lehnt den leeren Namen mit
         einer Begruendung ab, und die ist besser als ein Knopf, der ohne
         Erklaerung nicht reagiert. Der Browser prueft hier nichts nach —
         areas.pruefeName ist die eine Stelle, die entscheidet, was ein Name
         sein darf. */
      bzSchreiben("/action/task/bereich/anlegen", { bereich: name }).then(function (ok) {
        if (ok) neu.value = "";
      });
    });

    [].forEach.call(wrap.querySelectorAll('[data-bz-akt="umbenennen"]'), function (btn) {
      btn.addEventListener("click", function () {
        var zeile = btn.closest(".bz-gruppe");
        var alt = zeile.getAttribute("data-bz-gruppe");
        /* Der ALTE Name reist im Rumpf mit (taskops.BereichUmbenennenWunsch).
           Steht die Ansicht auf einem veralteten Stand, scheitert das sichtbar
           an „bereich gibt es nicht“, statt eine fremde Area umzubenennen. */
        var feld = el('<input type="text" class="bz-in">');
        feld.value = alt;
        var los = el('<button type="button" class="av-b">Übernehmen</button>');
        var form = el('<span class="bz-um"></span>');
        form.appendChild(feld);
        form.appendChild(los);
        btn.replaceWith(form);
        feld.focus();
        feld.select();
        los.addEventListener("click", function () {
          bzSchreiben("/action/task/bereich/umbenennen", { alt: alt, neu: (feld.value || "").trim() });
        });
        feld.addEventListener("keydown", function (ev) { if (ev.key === "Enter") los.click(); });
      });
    });

    [].forEach.call(wrap.querySelectorAll('[data-bz-akt="loeschen"]'), function (btn) {
      btn.addEventListener("click", function () {
        /* Der Name reist im Rumpf mit, genau wie beim Umbenennen: steht die
           Ansicht auf einem veralteten Stand, scheitert das sichtbar an
           „bereich gibt es nicht“, statt eine fremde Area zu treffen.

           Geprueft wird hier nichts — ob die Area leer ist, entscheidet
           areas.Store.Loeschen. Der Browser kennt zwar `g.projekte`, aber eine
           zweite Pruefung waere eine zweite Regel: sie koennte den Knopf
           sperren, waehrend die Datei laengst etwas anderes sagt, und dann
           bliebe die Ablehnung ohne Begruendung. So kommt stattdessen die
           Meldung der Route in die Zeile — samt der Projekte, die im Weg
           stehen. */
        var alt = btn.closest(".bz-gruppe").getAttribute("data-bz-gruppe");
        bzSchreiben("/action/task/bereich/loeschen", { bereich: alt });
      });
    });

    [].forEach.call(wrap.querySelectorAll(".bz-sel"), function (sel) {
      sel.addEventListener("change", function () {
        /* Ein leerer Wert LOEST die Zuordnung — das ist die vierte Operation
           des Beads und braucht keinen eigenen Knopf: "Ohne Bereich" ist die
           erste Wahl der Liste, und areas.SetArea liest "" als loeschen. */
        bzSchreiben("/action/task/bereich", { projekt: sel.getAttribute("data-bz-projekt"), bereich: sel.value });
      });
    });
  }

  /* Die Anmeldung. Genau hier, am Ende, kommt eine weitere Ansicht dazu. */
  avAnmelden("heute", { label: "Heute" });
  avAnmelden("demnaechst", { label: "Demnächst" });
  avAnmelden("eingang", { label: "Eingang" });
  avAnmelden("irgendwann", { label: "Irgendwann" });
  avAnmelden("jederzeit", { label: "Jederzeit" });
  avAnmelden("logbuch", { label: "Logbuch" });

  /* Jetzt, NACH der Anmeldung, kennt AV_REG jede der sechs Ansichten — erst
     ab hier darf die obere Navigation (gkp-s9n) ihre echte Grundmenge lesen
     (Begruendung an VIEWNAV_ITEMS' leerer Erstinitialisierung oben). Gezeichnet
     wird sofort (der Projekt-View hat die Zahlen schon synchron aus NAV), die
     Sammelansicht bekommt ihre echten Zahlen gleich danach ueber
     kopfNavLaden() nachgetragen — GENAU EINMAL pro Seitenaufbau, unabhaengig
     davon, welche Ansicht unten als erstes zu sehen ist. */
  VIEWNAV_ITEMS = viewNavGrundmenge();
  viewNavZeichnen();
  kopfNavLaden();

  /* boot — determine initial view:
   *  1. window.INITIAL_VIEW (set by server for path-URL loads, CCS-034)
   *  2. fallback to first nav entry (existing behaviour)
   * Use replaceState so the initial load doesn't add an extra history entry. */
  (function () {
    var firstNavId = (NAV[0] && NAV[0].items && NAV[0].items[0] && NAV[0].items[0].id) || "ov";
    // Collect all known nav IDs for validation
    var allNavIds = {};
    NAV.forEach(function (grp) {
      (grp.items || []).forEach(function (it) { if (it.id) allNavIds[it.id] = true; });
    });
    // SPECIAL IDs are also valid targets (cost, context, ov)
    var allSpecial = { ov: true, cost: true, context: true };

    var hint = (typeof window !== "undefined" && window.INITIAL_VIEW) ? String(window.INITIAL_VIEW) : "";
    // gkp-92i/gkp-s9n: /heute & Co. ohne Projektsegment setzen INITIAL_VIEW auf
    // eine der sechs Ansichten (oder "bereiche"), OHNE dass NAV sie kennt —
    // die Gruppe "Aufgaben" existiert serverseitig nur im Projekt-View
    // (builddata.go: projectGroups). Ohne istAnsichtOhneProjekt() fiele der
    // Hinweis hier durch und die Seite zeigte "ov" statt der verlangten
    // Ansicht.
    var bootId = (hint && (allNavIds[hint] || allSpecial[hint] || hint === "git" || istAnsichtOhneProjekt(hint)))
      ? hint : firstNavId;

    // Das dritte Pfadsegment gilt nur unter /reports (gkp-ar7.28). Sonst waere
    // /projekt/git/irgendwas eine URL, die etwas verspricht, was es nicht gibt.
    if (bootId === "reports" && typeof window !== "undefined" && window.INITIAL_SUB) {
      RP_EPIC = String(window.INITIAL_SUB);
    }

    suppressPush = true;
    try {
      select(bootId);
      // Replace the initial history entry with the canonical path for this view
      try {
        history.replaceState({ view: bootId }, "", viewToPath(bootId));
      } catch (e) { /* history API not available in test env */ }
    } finally {
      suppressPush = false;
    }
  })();
})();

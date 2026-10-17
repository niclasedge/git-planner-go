package render

import (
	"strconv"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/appctx"
	"github.com/niclasedge/git-planner-go/internal/collectors"
)

const extraCSS = `
.gitforms{ display:grid; gap:9px; }
.gf{ display:flex; flex-wrap:wrap; gap:7px; align-items:center; }
.gf input{ font:inherit; font-size:12px; padding:5px 9px; border:1px solid var(--line-2);
  border-radius:6px; background:var(--inset); color:var(--fg); flex:1; min-width:120px; }
.gf input::placeholder{ color:var(--faint); }
.gf button{ font:inherit; font-size:11.5px; font-weight:700; padding:6px 12px; border-radius:6px;
  border:1px solid color-mix(in oklch,var(--green) 40%,var(--line-2)); background:var(--green-d);
  color:var(--green); cursor:pointer; white-space:nowrap; }
.gf button.danger{ border-color:color-mix(in oklch,var(--red) 45%,var(--line-2));
  background:var(--red-d); color:var(--red); }
.gf .gn{ font-size:10px; color:var(--red); }
.filebody{ background:var(--inset); border:1px solid var(--line); border-radius:8px;
  padding:13px 15px; font-size:11.5px; line-height:1.6; color:var(--fg-2);
  white-space:pre-wrap; word-break:break-word; max-height:62vh; overflow:auto; margin:0; }
.filebody.loading{ color:var(--faint); font-style:italic; white-space:normal; }
.ftrunc{ color:var(--faint); font-size:10.5px; margin-top:6px; }
.rlwrap{ display:grid; gap:9px; margin-top:2px; }
.rlm-row{ display:flex; justify-content:space-between; gap:8px; margin-bottom:4px; align-items:baseline; }
.rlm-l{ font-size:10px; letter-spacing:.08em; text-transform:uppercase; color:var(--dim); }
.rlm-v{ font-size:10px; color:var(--fg-2); font-variant-numeric:tabular-nums; }
.rlm-na{ font-size:10px; color:var(--faint); }
.mp-list .li .li-s{ display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; }
.hookrows{ display:grid; gap:8px; }
.hookrow{ background:var(--panel); border:1px solid var(--line); border-radius:7px; padding:9px 11px; }
.hookrow .hh{ display:flex; gap:8px; align-items:baseline; margin-bottom:5px; }
.hookrow .hh code{ color:var(--cyan); font-size:11px; }
.hookrow .hh .ht{ color:var(--faint); font-size:10px; text-transform:uppercase; letter-spacing:.08em; }
.hookrow .hc{ font-size:11px; color:var(--green); white-space:pre-wrap; word-break:break-word; line-height:1.5; }
.nav-i .nc{ text-align:right; line-height:1.25; }
.nav-i .nc small{ display:block; font-size:8.5px; color:var(--faint); font-weight:500; letter-spacing:.02em; }
.fmeta{ font-size:10px; color:var(--faint); margin-bottom:5px; font-variant-numeric:tabular-nums; }
.fchips{ display:flex; flex-wrap:wrap; gap:6px; margin-bottom:10px; }
.fchip{ font:inherit; font-size:10.5px; color:var(--fg-2); background:var(--panel-3);
  border:1px solid var(--line-2); border-radius:6px; padding:4px 9px; cursor:pointer;
  display:inline-flex; align-items:center; gap:7px; white-space:nowrap; }
.fchip:hover{ background:var(--panel-2); border-color:var(--line-3); }
.fchip.is-active{ border-color:color-mix(in oklch,var(--cyan) 45%,var(--line-2)); color:var(--cyan); }
.fchip .fl{ font-size:8.5px; text-transform:uppercase; letter-spacing:.06em; color:var(--amber); }
.fchip .fz{ color:var(--faint); font-size:9.5px; }
.fchip.diffall{ color:var(--cyan); }
.filehost2{ margin-top:4px; }
.gfiles{ display:grid; gap:4px; margin-bottom:10px; }
.gfile{ font:inherit; font-size:11.5px; text-align:left; background:var(--panel);
  border:1px solid var(--line); border-radius:6px; padding:6px 10px; cursor:pointer;
  display:flex; align-items:center; gap:9px; color:var(--fg-2); }
.gfile:hover{ background:var(--panel-2); }
.gfile.is-active{ box-shadow:inset 2px 0 0 var(--cyan); background:var(--panel-3); }
.gfile .gfp{ word-break:break-all; }
.filebody.diff{ padding:0; }
.filebody.diff .dl{ display:block; padding:0 13px; }
.filebody.diff .dp{ color:var(--green); background:color-mix(in oklch,var(--green) 9%,transparent); }
.filebody.diff .dm{ color:var(--red); background:color-mix(in oklch,var(--red) 9%,transparent); }
.filebody.diff .dh{ color:var(--cyan); }
.filebody.diff .df{ color:var(--dim); }
.mp-list .li-group{ position:sticky; top:0; z-index:1; font-size:9.5px; letter-spacing:.12em;
  text-transform:uppercase; color:var(--amber); background:var(--panel);
  padding:9px 13px 5px; border-bottom:1px solid var(--line); }
.clrows{ display:grid; gap:7px; }
.clrow{ background:var(--panel); border:1px solid var(--line); border-radius:7px; padding:8px 11px; }
.clrow.cl-err{ border-color:color-mix(in oklch,var(--red) 30%,var(--line)); }
.clrow .clh{ display:flex; gap:8px; align-items:baseline; flex-wrap:wrap; }
.clrow .clh code{ color:var(--green); font-size:11px; word-break:break-all; }
.clrow .clt{ font-size:10px; font-weight:700; padding:1px 6px; border-radius:5px;
  background:var(--panel-3); color:var(--dim); white-space:nowrap; }
.clrow .clt.r{ color:var(--red); } .clrow .clt.a{ color:var(--amber); }
.clrow .clp{ margin-top:6px; font-size:10.5px; color:var(--fg-2); white-space:pre-wrap;
  word-break:break-word; max-height:120px; overflow:auto; }
.list-head .gss{ font-size:10px; color:var(--dim); margin-top:7px; font-variant-numeric:tabular-nums; }
.mp-list .gfile{ width:100%; font:inherit; font-size:12px; text-align:left; background:none;
  border:0; border-bottom:1px solid var(--line); padding:7px 13px; cursor:pointer;
  display:grid; grid-template-columns:auto 1fr auto; align-items:center; gap:9px; color:var(--fg-2); }
.mp-list .gfile:hover{ background:var(--panel-2); }
.mp-list .gfile.is-active{ background:var(--panel-3); box-shadow:inset 2px 0 0 var(--cyan); color:var(--fg-strong); }
.mp-list .gfile .gfp{ word-break:break-all; line-height:1.3; }
.mp-list .gfile .gdelta{ font-size:10px; font-variant-numeric:tabular-nums; white-space:nowrap; }
.gd-add{ color:var(--green); } .gd-del{ color:var(--red); margin-left:5px; } .gd-new{ color:var(--amber); }
.git-actions{ border-top:1px solid var(--line-2); margin-top:4px; }
.git-actions > summary{ list-style:none; cursor:pointer; font-size:9.5px; letter-spacing:.1em;
  text-transform:uppercase; color:var(--dim); padding:11px 13px; user-select:none; }
.git-actions > summary::-webkit-details-marker{ display:none; }
.git-actions > summary:hover{ color:var(--fg-2); }
.git-actions > summary .ct{ color:var(--faint); text-transform:none; letter-spacing:0; }
.git-actions .gitforms{ padding:2px 13px 14px; }
.gitdiff{ min-height:100%; }
.gitdiff-head{ display:flex; align-items:baseline; gap:12px; flex-wrap:wrap;
  padding:12px 16px; border-bottom:1px solid var(--line);
  position:sticky; top:0; background:var(--bg-grid); z-index:2; }
.gitdiff-head code{ color:var(--green); font-size:12.5px; word-break:break-all; }
.gitdiff-head .gdh-rec{ color:var(--dim); font-size:10.5px; }
.gitdiff .filebody.diff{ border:0; border-radius:0; max-height:none; overflow:visible; }
.splitdiff{ font-size:11.5px; line-height:1.55; font-variant-numeric:tabular-nums; }
.sd-headrow, .sd-row{ display:grid; grid-template-columns:46px minmax(0,1fr) 46px minmax(0,1fr); }
.sd-headrow{ position:sticky; top:0; z-index:1; background:var(--panel); border-bottom:1px solid var(--line-2); }
.sd-headrow .sd-h{ grid-column:span 2; padding:7px 12px; font-size:9.5px; letter-spacing:.1em;
  text-transform:uppercase; color:var(--dim); }
.sd-headrow .sd-h:nth-child(2){ border-left:1px solid var(--line-2); }
.sd-hunk{ padding:3px 12px; color:var(--cyan); background:var(--inset);
  border-top:1px solid var(--line); border-bottom:1px solid var(--line); font-size:10.5px; }
.sd-ln{ text-align:right; padding:1px 8px; color:var(--faint); font-size:10px; user-select:none;
  border-right:1px solid var(--line); background:var(--bg-grid); }
.sd-code{ padding:1px 10px; white-space:pre-wrap; word-break:break-word; color:var(--fg-2);
  border-right:1px solid var(--line); }
.sd-code.dm{ background:color-mix(in oklch,var(--red) 13%,transparent); color:var(--red); }
.sd-code.dp{ background:color-mix(in oklch,var(--green) 13%,transparent); color:var(--green); }
.sd-code.empty{ background:repeating-linear-gradient(45deg,transparent,transparent 6px,var(--inset) 6px,var(--inset) 12px); }
`

const headerCSS = `
.hd{
  display:flex; align-items:center; gap:14px;
  padding:9px 14px; border-bottom:1px solid var(--line-2);
  background:var(--panel-2);
  flex:none;
}
.crumb{ display:flex; align-items:baseline; gap:8px; font-size:12.5px; min-width:0; }
.crumb .root{ color:var(--green); font-weight:700; }
.crumb .sep{ color:var(--faint); }
.crumb .here{ color:var(--fg); font-weight:600; }
.crumb .scope{ color:var(--dim); }
.hd .sp{ flex:1; }
.stats{ display:flex; align-items:stretch; gap:0; }
.stat{ display:flex; flex-direction:column; gap:2px; padding:0 14px; border-left:1px solid var(--line); }
.stat:first-child{ border-left:0; }
.stat .sv{ font-size:13px; font-weight:700; color:var(--fg); line-height:1; font-variant-numeric:tabular-nums; }
.stat .sv.g{ color:var(--green); } .stat .sv.a{ color:var(--amber); } .stat .sv.c{ color:var(--cyan); }
.stat .sl{ font-size:9px; letter-spacing:.1em; text-transform:uppercase; color:var(--dim); }
.hd .asof{ font-size:10.5px; color:var(--faint); white-space:nowrap; }
.hd .tg{
  flex:none; font:inherit; font-size:13px; line-height:1;
  background:var(--panel-3); color:var(--fg-2); border:1px solid var(--line-2);
  border-radius:6px; padding:4px 9px; cursor:pointer;
}
.hd .tg:hover{ color:var(--fg); border-color:var(--line-3); }
/* gkp-s9n: obere Navigation der sechs Aufgaben-Ansichten + Bereiche — bewusste
   Abweichung vom Vorbild (~/git/git-planner-ios, wo "Agenda" in der Sidebar
   sitzt): unsere Ansichten sind seit E6 projektuebergreifend und gehoeren
   damit ueber alles, nicht in dieselbe Spalte wie die Projektauswahl. Optik
   vom Vorbild-Env-Switch (web/main.py :921-927): Segmented Pill, 34px hoch,
   3px Innenabstand, aktiv = Flaeche statt Farbe (derselbe Grundsatz wie an
   .prl.is-active weiter unten). */
.viewnav{
  display:inline-flex; align-items:center; gap:2px; flex:0 0 auto; height:34px; padding:3px;
  background:var(--inset); border:1px solid var(--line-2); border-radius:9px;
  overflow:auto; max-width:52vw;
}
.vn-btn{
  flex:none; display:inline-flex; align-items:center; gap:6px; height:100%;
  padding:0 11px; border:none; border-radius:6px; background:transparent;
  color:var(--dim); font:inherit; font-size:11.5px; font-weight:600;
  text-decoration:none; cursor:pointer; white-space:nowrap;
}
.vn-btn:hover{ color:var(--fg); }
.vn-btn.is-active{ background:var(--panel-3); color:var(--fg); }
.vn-c{ font-variant-numeric:tabular-nums; color:var(--faint); }
.vn-btn.is-active .vn-c{ color:var(--dim); }
/* Roter Punkt bei ueberfaelliger Arbeit — appctx.ansichtenNav liefert ihn
   bereits (dot:"r"), hier wird nur gezeichnet. */
.vn-dot{ width:6px; height:6px; border-radius:50%; background:var(--red); flex:none; }
`

const sidebarCSS = `
.kn-body{ display:flex; align-items:stretch; flex:1 1 auto; min-height:0; }
.pane-side{
  width:236px; flex:0 0 236px; overflow:auto;
  border-right:1px solid var(--line-2); background:var(--bg-grid);
}
.pane-side::-webkit-scrollbar{ width:9px; }
.pane-side::-webkit-scrollbar-thumb{ background:var(--line-2); border-radius:5px;
  border:2px solid transparent; background-clip:padding-box; }
.pane-side::-webkit-scrollbar-thumb:hover{ background:var(--line-3); }
.kn-body .mp{ flex:1 1 auto; min-width:0; }
/* gkp-s9n: die 30px-Schiene mit gedrehten Projektnamen weicht einer vollen
   Projektliste mit Stats (Vorbild ~/git/git-planner-ios web/main.py
   renderRepos, :1453-1490 / CSS :987-1004) — die sechs Aufgaben-Ansichten
   sitzen jetzt oben im Header (.viewnav), die Sidebar traegt nur noch die
   Projektliste und darunter die Abschnitte des aktiven Projekts. Gestapelt
   wie das Vorbild seine Sidebar baut (.pane-repos: Agenda oben, Repos darunter,
   beides mit eigenem Scroll) — bei uns oben die Projektliste, darunter
   proj-col. */
.pane-side{ display:flex; flex-direction:column; overflow:hidden; }
.projlist{
  flex:0 1 auto; max-height:46%; min-height:0; overflow:auto; display:flex;
  flex-direction:column; gap:3px; padding:8px 8px 10px; background:var(--inset);
  border-bottom:1px solid var(--line-2);
}
.pli{
  flex:none; display:flex; flex-direction:column; padding:8px 10px;
  border-radius:7px; border:1px solid transparent; text-decoration:none;
  color:var(--dim); cursor:pointer; transition:background .1s;
}
.pli:hover{ background:var(--panel-2); }
/* Aktiv ist Flaeche, nicht Farbe — derselbe Grundsatz wie am alten .prl. */
.pli.is-active{ background:var(--panel-3); border-color:var(--line-3); color:var(--fg); }
.pli.is-active .pli-name{ color:var(--fg-strong); font-weight:700; }
.pli.muted{ color:var(--faint); }
.pli-top{ display:flex; align-items:center; gap:7px; }
.pli-name{ flex:1 1 auto; min-width:0; overflow:hidden; text-overflow:ellipsis;
  white-space:nowrap; font-size:11.5px; font-weight:600; color:inherit; }
.pli-stat{ flex:none; font-size:10px; color:var(--faint); font-variant-numeric:tabular-nums; }
.pli-cost{ flex:none; font-size:9px; color:var(--faint); font-variant-numeric:tabular-nums; }
/* Der Balken bleibt LEER (kein Kind-Element), solange projStatsLaden() im
   Browser noch nicht geantwortet hat oder das Projekt keine offene Arbeit
   traegt — eine leere Flaeche behauptet nichts, ein Balken bei 0 Aufgaben
   waere eine erfundene Angabe. Gefuellt wird er von browser.js
   (projListZeichnen), nicht hier: die Zahlen kommen aus der
   projektuebergreifenden Ansichten-Aggregation (GET /ansichten ohne project,
   gkp-92i) und die kennt dieser Seitenaufbau nicht. */
.pli-bar{ display:flex; gap:2px; height:4px; margin-top:6px; border-radius:3px;
  overflow:hidden; background:var(--line); }
.pli-bar > span{ display:block; min-width:0; }
.pli-ov{ color:var(--faint); border-bottom:1px solid var(--line-2); padding-bottom:8px; margin-bottom:2px; }
.pli-more{
  flex:none; margin-top:2px; height:24px; cursor:pointer; background:transparent;
  border:0; border-top:1px solid var(--line-2); color:var(--faint); font-size:10px;
}
.pli-more:hover{ background:var(--panel-2); color:var(--fg-2); }
.proj-col{ flex:1 1 auto; display:flex; flex-direction:column; min-width:0; min-height:0; overflow:auto; }
.proj-colhd{ padding:12px 11px 9px; border-bottom:1px solid var(--line-2); }
.proj-colhd b{ display:block; font-size:12px; color:var(--fg);
  overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.proj-colhd .host{ display:block; margin-top:2px; font-size:9.5px; color:var(--faint);
  overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
#mp-nav{ padding-bottom:12px; }
#mp-nav .nav-i{ padding:5px 10px; margin:0; border-radius:0; }
#mp-nav .nav-i .nn{ font-size:11.5px; }
/* Ein-Element-Gruppen tragen keinen Rinnentitel (er waere eine Wiederholung des
   Eintrags), stehen dafuer als Ueberschrift gesetzt - und auf derselben
   Textkante wie die Eintraege in den Gruppen. */
#mp-nav .nav-i.nav-solo{ margin-top:6px; padding-left:calc(var(--navgut) + 10px); }
#mp-nav .nav-i.nav-solo .nn{ font-size:12.5px; letter-spacing:.04em; text-transform:uppercase;
  font-weight:700; color:var(--dim); }
#mp-nav .nav-i.nav-solo:hover .nn{ color:var(--fg-2); }
#mp-nav .nav-i.nav-solo.is-active .nn{ color:var(--cyan); }
`

const projectCSS = `
.ovwrap{ padding:0; }
/* Project overview two-column layout: left = init-load, right = wissen/memory */
.ov-cols{ display:flex; gap:14px; align-items:flex-start; flex-wrap:wrap; padding:14px 16px; }
.ov-col{ display:flex; flex-direction:column; gap:14px; min-width:300px; box-sizing:border-box; }
.ov-col-l{ flex:3 1 360px; }
.ov-col-r{ flex:2 1 280px; }
.ov-col .pane{ width:100%; box-sizing:border-box; }
/* Clickable overview rows (open detail modal) */
.ov-click{ cursor:pointer; border-radius:6px; transition:background .1s; }
.ov-click:hover{ background:var(--panel-2); }
.ovbar{
  display:flex; align-items:baseline; gap:12px; flex-wrap:wrap;
  padding:14px 18px; border-bottom:1px solid var(--line); background:var(--panel);
  position:sticky; top:0; z-index:2;
}
.ovbar h2{ font-size:14px; font-weight:700; margin:0; color:var(--fg); letter-spacing:.02em; }
.ovbar h2 b{ color:var(--green); }
.ovbar .dc{ color:var(--faint); font-size:11.5px; }
.ovbar .hint{ margin-left:auto; color:var(--dim); font-size:10.5px; }
.ovbar .hint b{ color:var(--cyan); font-weight:600; }
.ovbar .branch{ font-size:10.5px; color:var(--dim); }
.ovbar .branch code{ color:var(--green); }
.ov-kpis{ display:grid; grid-template-columns:repeat(auto-fit,minmax(130px,1fr));
  border-bottom:1px solid var(--line); background:var(--panel); }
.ov-kpi{ padding:14px 16px; border-right:1px solid var(--line); }
.ov-kpi:last-child{ border-right:0; }
.ov-kpi .k-l{ font-size:9px; letter-spacing:.13em; color:var(--dim); text-transform:uppercase;
  display:flex; gap:6px; align-items:center; margin-bottom:8px; }
.ov-kpi .k-v{ font-size:26px; font-weight:700; line-height:1; letter-spacing:-.01em;
  color:var(--fg); font-variant-numeric:tabular-nums; }
.ov-kpi .k-v small{ font-size:12px; font-weight:500; color:var(--dim); }
.ov-kpi .k-s{ font-size:10px; color:var(--fg-2); margin-top:7px; }
.ov-kpi.g .k-v{ color:var(--green); } .ov-kpi.a .k-v{ color:var(--amber); }
.ov-kpi.c .k-v{ color:var(--cyan); } .ov-kpi.m .k-v{ color:var(--mag); }
.tload{ display:grid; gap:10px; }
.tload .row{ display:grid; grid-template-columns:108px 1fr 70px; align-items:center; gap:12px; }
.tload .tl{ font-size:11px; color:var(--fg-2); }
.tload .tl small{ color:var(--faint); font-size:10px; }
.tload .track{ height:10px; background:var(--inset); border:1px solid var(--line); border-radius:3px; overflow:hidden; }
.tload .track > i{ display:block; height:100%; border-radius:2px; }
.tload .tv{ font-size:11.5px; color:var(--fg); font-variant-numeric:tabular-nums; text-align:right; }
.tload .tv small{ color:var(--faint); font-size:9px; }
.tload-foot{ margin-top:12px; padding-top:11px; border-top:1px dotted var(--line);
  display:flex; justify-content:space-between; font-size:11.5px; }
.tload-foot .tf-l{ color:var(--dim); }
.tload-foot .tf-v{ color:var(--cyan); font-weight:700; font-variant-numeric:tabular-nums; }
.prow{ display:grid; grid-template-columns:1fr auto; gap:10px; align-items:baseline;
  padding:6px 0; border-bottom:1px dotted var(--line); font-size:12px; }
.prow:last-child{ border-bottom:0; }
.prow .pl{ color:var(--fg-2); word-break:break-word; }
.prow .pl b{ color:var(--fg); font-weight:600; }
.prow .pr{ color:var(--dim); font-variant-numeric:tabular-nums; white-space:nowrap; font-size:11px; }
.prow .pr.g{ color:var(--green); } .prow .pr.a{ color:var(--amber); } .prow .pr.c{ color:var(--cyan); }
/* Changelog entries in the project overview: heading on its own full-width line above the body */
.cl-ov{ padding:7px 0; border-bottom:1px dotted var(--line); }
.cl-ov:last-child{ border-bottom:0; }
.cl-ov-h{ font-size:12px; color:var(--fg); font-weight:600; line-height:1.4; word-break:break-word; }
.cl-ov-b{ font-size:11px; color:var(--dim); margin-top:3px; line-height:1.45; word-break:break-word; }
`

// modalCSS holds the section-header bar used by the Reports view plus the
// shared modal (showModal / showImageModal) and its markdown body styles.
const modalCSS = `
.kn-board-head{ padding:11px 16px; border-bottom:1px solid var(--line,#1c2229); display:flex;
  align-items:baseline; gap:10px; background:var(--panel,#0b0d10); font-size:13px; }
.kn-board-head .lc{ color:var(--faint,#6c7682); }
.kn-modal{ position:fixed; inset:0; background:rgba(0,0,0,.55); display:flex; align-items:center;
  justify-content:center; z-index:1000; }
.kn-modal-box{ background:var(--panel,#0b0d10); border:1px solid var(--line-2,#1c2229);
  border-radius:10px; width:min(820px,92vw); max-height:86vh; display:flex; flex-direction:column; }
/* 820px passt zu Fliesstext, nicht zu einem Beleg-Screenshot von 1200-2400px
   Breite: der war im Prosa-Kasten unlesbar. Bilder bekommen fast das Fenster. */
.kn-modal.is-image .kn-modal-box{ width:min(2000px,97vw); max-height:96vh; }
.kn-modal.is-image .kn-modal-body{ padding:8px; }
.kn-modal-h{ display:flex; align-items:center; gap:10px; padding:11px 14px;
  border-bottom:1px solid var(--line-2,#1c2229); font-size:13px; }
.kn-modal-h b{ flex:1; color:oklch(0.83 0.11 215); }
.kn-modal-x{ background:none; border:0; color:var(--dim,#8a929c); cursor:pointer; font-size:14px; }
/* Nebenweg, kein zweiter Hauptweg: leiser als der Titel, laut genug zum Finden. */
.kn-modal-raw{ flex:none; font-size:11px; color:var(--dim,#8a929c);
  border:1px solid var(--line-2,#1c2229); border-radius:5px; padding:2px 8px; }
.kn-modal-raw:hover{ color:var(--fg-2,#c2cad2); border-color:var(--line-3,#2a3138); }
.kn-modal-body{ margin:0; padding:14px 18px; overflow:auto; font-size:12.5px; line-height:1.55;
  color:var(--fg,#d7dee5); word-break:break-word; }
.kn-md h2,.kn-md h3,.kn-md h4{ margin:14px 0 6px; font-weight:700; line-height:1.3; }
.kn-md h2{ font-size:15px; } .kn-md h3{ font-size:13.5px; }
.kn-md h4{ font-size:12.5px; color:var(--dim,#8a929c); }
.kn-md p{ margin:7px 0; }
.kn-md ul,.kn-md ol{ margin:7px 0; padding-left:20px; } .kn-md li{ margin:2px 0; }
.kn-md code{ background:var(--bg-grid,#0a0b0d); border:1px solid var(--line-2,#1c2229); border-radius:4px;
  padding:1px 5px; font-size:11.5px; }
.kn-md pre{ background:var(--bg-grid,#0a0b0d); border:1px solid var(--line-2,#1c2229); border-radius:7px;
  padding:10px 12px; overflow:auto; }
.kn-md pre code{ background:none; border:0; padding:0; }
.kn-md blockquote{ margin:7px 0; padding:2px 12px; border-left:3px solid var(--line-3,#2a313a);
  color:var(--dim,#8a929c); }
.kn-md hr{ border:0; border-top:1px solid var(--line-2,#1c2229); margin:12px 0; }
.kn-md a{ color:oklch(0.83 0.11 215); }
`

const contextCSS = `
.ctx-wrap{ max-width:920px; }
.ctx-sum-p{ color:var(--dim); font-size:14px; font-weight:500; }
.ctx-bar{ display:flex; width:100%; height:14px; border-radius:7px; overflow:hidden;
  background:var(--inset); border:1px solid var(--line); margin:12px 0 18px; }
.ctx-seg{ height:100%; display:block; min-width:1px; }
.ctx-seg-free{ background:repeating-linear-gradient(45deg,transparent,transparent 5px,var(--line) 5px,var(--line) 10px); flex:1 1 auto; }
.ctx-cats{ display:grid; gap:6px; }
.ctx-cat{ border:1px solid var(--line); border-radius:8px; background:var(--panel); overflow:hidden; }
.ctx-cat > summary{ list-style:none; cursor:pointer; display:flex; align-items:center; gap:9px;
  padding:9px 12px; user-select:none; font-size:12.5px; }
.ctx-cat > summary::-webkit-details-marker{ display:none; }
.ctx-cat > summary:hover{ background:var(--panel-2); }
.ctx-sw{ width:10px; height:10px; border-radius:3px; flex:0 0 auto; }
.ctx-sw-free{ background:var(--line-3); }
.ctx-l{ font-weight:600; color:var(--fg); }
.ctx-tag{ font-size:9px; text-transform:uppercase; letter-spacing:.06em; padding:1px 6px;
  border-radius:5px; font-weight:700; }
.ctx-tag.fix{ background:var(--panel-3); color:var(--dim); }
.ctx-tag.live{ background:color-mix(in oklch,var(--mag) 18%,transparent); color:var(--mag); }
.ctx-bar-mini{ flex:1 1 auto; height:5px; min-width:40px; background:var(--inset);
  border-radius:3px; overflow:hidden; margin:0 4px; }
.ctx-bar-mini i{ display:block; height:100%; }
.ctx-t{ font-size:11px; color:var(--fg-2); font-variant-numeric:tabular-nums; white-space:nowrap; }
.ctx-p{ font-size:10.5px; color:var(--faint); font-variant-numeric:tabular-nums; width:46px;
  text-align:right; white-space:nowrap; }
.ctx-body{ border-top:1px solid var(--line); padding:6px 12px 9px; display:grid; gap:3px; }
.ctx-item{ display:flex; align-items:baseline; gap:9px; font-size:11.5px; color:var(--fg-2);
  padding:2px 0; }
.ctx-in{ flex:0 0 auto; color:var(--fg); font-weight:500; }
.ctx-id{ flex:1 1 auto; color:var(--faint); font-size:10.5px; overflow:hidden;
  text-overflow:ellipsis; white-space:nowrap; }
.ctx-it{ flex:0 0 auto; color:var(--cyan); font-variant-numeric:tabular-nums; }
.ctx-item.ctx-empty, .ctx-item.ctx-loading{ color:var(--faint); font-style:italic; }
.ctx-hookout{ margin:0; font-size:10.5px; line-height:1.55; color:var(--green); white-space:pre-wrap;
  word-break:break-word; max-height:340px; overflow:auto; background:var(--inset);
  border:1px solid var(--line-2); border-radius:6px; padding:9px 11px; }
.ctx-free{ display:flex; align-items:center; gap:9px; padding:9px 12px; border:1px dashed var(--line-2);
  border-radius:8px; font-size:12.5px; color:var(--dim); }
.ctx-free .ctx-l{ color:var(--dim); }
/* Readable memory items: collapsible file viewer inside context category */
details.ctx-readable{ border-top:1px solid var(--line-2); }
details.ctx-readable:first-child{ border-top:0; }
details.ctx-readable > summary{
  list-style:none; cursor:pointer; display:flex; align-items:baseline; gap:9px;
  font-size:11.5px; color:var(--fg-2); padding:2px 0; user-select:none;
}
details.ctx-readable > summary::-webkit-details-marker{ display:none; }
details.ctx-readable > summary:hover{ color:var(--fg); }
details.ctx-readable[open] > summary .ctx-in{ color:var(--cyan); }
.ctx-readable-body{ padding:6px 0 2px; }
/* Quell-Gruppen-Header innerhalb Skills/Agents (Project/User/Plugin/Built-in) */
.ctx-grp{ font-size:9.5px; letter-spacing:.1em; text-transform:uppercase; color:var(--amber);
  padding:7px 0 3px; margin-top:4px; border-top:1px solid var(--line-2); user-select:none; }
.ctx-grp:first-child{ border-top:0; margin-top:0; padding-top:2px; }
`

// RenderOpts holds the inputs to RenderPage.
type RenderOpts struct {
	Cwd         string
	Context     map[string]any
	Sidebar     []collectors.SidebarProject
	Active      string
	View        string // "overview" | "project"
	InitialView string
	// InitialSub ist das dritte Pfadsegment: die Auswahl INNERHALB der Ansicht,
	// heute die Epic-Id unter /reports (gkp-ar7.28). Getrennt von InitialView,
	// weil beides verschiedene Fragen beantwortet — welche Ansicht, und was
	// darin offen ist.
	InitialSub  string
	GeneratedAt string
	Loading     bool
}

// RenderPage renders the full single-page dashboard HTML (browser.js-compatible).
func RenderPage(opts RenderOpts, assets Assets) string {
	ctx := opts.Context
	if ctx == nil {
		ctx = map[string]any{"generated_at": "", "cwd": opts.Cwd, "projects": []any{}, "active_project": "", "cards": map[string]any{}}
	}
	view := opts.View
	if view == "" {
		if opts.Active != "" {
			view = "project"
		} else {
			view = "overview"
		}
	}
	data := appctx.BuildData(ctx, view)

	css := assets.DashCSS + "\n" + assets.BrowserCSS + "\n" + extraCSS + "\n" + headerCSS + "\n" +
		sidebarCSS + "\n" + projectCSS + "\n" + modalCSS + "\n" + contextCSS
	dataJson := SafeScriptJson(data)

	active := ""
	if view == "project" {
		active = opts.Active
	}
	sb := opts.Sidebar

	projChips := func(cost float64) string {
		if cost > 0 {
			return `<span class="pli-cost">` + escape(appctx.FmtCostRund(cost)) + `</span>`
		}
		return ""
	}

	// Projektliste mit Stats statt der 30px-Schiene (gkp-s9n): Name, dann eine
	// leere Zahlengruppe und ein leerer Balken — beide traegt browser.js nach
	// (projListZeichnen), gespeist aus der projektuebergreifenden
	// Ansichten-Aggregation, die dieser Seitenaufbau nicht kennt (Begruendung
	// an .pli-bar in sidebarCSS). Der Ueberblick bleibt der erste Eintrag - im
	// Projekt-View der Rueckweg, im Ueberblick selbst der aktive.
	var projRows strings.Builder
	ovCls := ""
	if view != "project" {
		ovCls = " is-active"
	}
	projRows.WriteString(`<a class="pli pli-ov` + ovCls + `" href="/ueberblick" title="Überblick · alle Projekte">` +
		`<span class="pli-name">Überblick</span></a>`)
	for _, p := range sb {
		isActive := view == "project" && p.Name == active
		hasActivity := p.Cost7d > 0 || isActive
		cls := ""
		if isActive {
			cls = " is-active"
		} else if !hasActivity {
			cls = " muted"
		}
		q := encodeURIComponent(p.Name)
		projRows.WriteString(`<a class="pli` + cls + `" href="/` + q +
			`" data-pli-projekt="` + escape(p.Name) + `" title="` + escape(p.Path) + `"`)
		if isActive {
			projRows.WriteString(` aria-current="page"`)
		}
		if !hasActivity {
			projRows.WriteString(" hidden")
		}
		projRows.WriteString(`>` +
			`<div class="pli-top">` +
			`<span class="pli-name">` + escape(p.Name) + `</span>` +
			`<span class="pli-stat"></span>` +
			projChips(p.Cost7d) +
			`</div>` +
			`<div class="pli-bar"></div>` +
			`</a>`)
	}

	quietCount := 0
	for _, p := range sb {
		isActive := view == "project" && p.Name == active
		if !isActive && p.Cost7d == 0 {
			quietCount++
		}
	}
	quietToggle := ""
	if quietCount > 0 {
		quietToggle = `<button type="button" class="pli-more" id="kn-show-quiet" title="` +
			itoa(quietCount) + ` Projekte ohne Aktivität einblenden">+` + itoa(quietCount) + `</button>`
	}

	// Der Spaltenkopf nennt, worauf sich die Abschnitte beziehen. Die
	// Projektliste traegt den Namen waagerecht, der Kopf ebenso - zusammen mit
	// dem Pfad, den die Liste nur im title-Attribut traegt.
	colTitle, colSub := "Überblick", "alle Projekte"
	if view == "project" {
		colTitle = active
		colSub = ""
		for _, p := range sb {
			if p.Name == active {
				colSub = p.Path
			}
		}
	}

	sidebarHtml := `<aside class="pane-side" id="kn-sidebar">` +
		`<div class="projlist" role="navigation" aria-label="Projekte">` +
		projRows.String() + quietToggle +
		`</div>` +
		`<div class="proj-col">` +
		`<div class="proj-colhd"><b>` + escape(colTitle) + `</b>` +
		`<span class="host">` + escape(colSub) + `</span></div>` +
		`<nav id="mp-nav"></nav>` +
		`</div>` +
		`</aside>`

	var sumCost float64
	for _, p := range sb {
		sumCost += p.Cost7d
	}

	standSrc := opts.GeneratedAt
	if standSrc == "" {
		if meta, ok := data["meta"].(map[string]any); ok {
			standSrc = asString(meta["generated_at"])
		}
	}
	stand := escape(standSrc)

	crumbHere := "Überblick"
	crumbScope := "· alle Projekte · global"
	if view != "overview" {
		crumbHere = escape(active)
		crumbScope = "· Projekt"
	}

	header := `<header class="hd">` +
		`<nav class="crumb">` +
		`<span class="root">knowledge</span>` +
		`<span class="sep">/</span>` +
		`<span class="here" id="crumb-here">` + crumbHere + `</span>` +
		`<span class="scope" id="crumb-scope">` + crumbScope + `</span>` +
		`</nav>` +
		// Obere Navigation der sechs Aufgaben-Ansichten + Bereiche (gkp-s9n) —
		// leer im HTML, browser.js (viewNavZeichnen) fuellt sie: die Zahlen
		// haengen an appctx.ansichtenNav bzw. der Ansichten-Aggregation
		// (GET /ansichten), die dieser Seitenaufbau fuer den Ueberblick nicht
		// mitbringt (Begruendung: builddata.go, projectGroups nur im Projekt-View).
		`<nav class="viewnav" id="kn-viewnav" aria-label="Aufgaben-Ansichten"></nav>` +
		`<span class="sp"></span>` +
		`<div class="stats">` +
		`<div class="stat"><span class="sv">` + itoa(len(sb)) + `</span><span class="sl">Projekte</span></div>` +
		`<div class="stat"><span class="sv g">≈` + escape(appctx.FmtCost(sumCost)) + `</span><span class="sl">7 Tage</span></div>` +
		`</div>` +
		`<span class="asof">Stand ` + stand + `</span>` +
		`<button class="tg" id="kn-theme-toggle" type="button" aria-label="Theme wechseln">◐</button>` +
		`</header>`

	themeInit := "<script>(function(){try{var t=localStorage.getItem('kn-theme');" +
		"if(t==='light'||t==='dark')document.documentElement.dataset.theme=t;}" +
		"catch(e){}})();</script>"

	quietScript := ""
	if quietCount > 0 {
		quietScript = "<script>(function(){var b=document.getElementById('kn-show-quiet');" +
			"if(!b)return;" +
			"b.addEventListener('click',function(){" +
			"var rows=document.querySelectorAll('.pli[hidden]');" +
			"[].forEach.call(rows,function(r){r.removeAttribute('hidden');r.classList.add('muted');});" +
			"b.remove();});})();</script>"
	}

	initialViewScript := ""
	if opts.InitialView != "" {
		initialViewScript = "<script>window.INITIAL_VIEW = " + SafeScriptJson(opts.InitialView) + ";</script>"
	}
	if opts.InitialSub != "" {
		initialViewScript += "<script>window.INITIAL_SUB = " + SafeScriptJson(opts.InitialSub) + ";</script>"
	}

	return "<!DOCTYPE html>\n" +
		`<html lang="de"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>knowledge — Browser</title>` +
		faviconLink +
		themeInit +
		`<style>` + css + `</style></head><body>` +
		`<div class="kn-shell">` +
		header +
		`<div class="kn-body">` +
		sidebarHtml +
		`<div class='mp' id='mp'>` +
		`<div class='mp-list' id='mp-list'></div>` +
		`<div class='mp-detail' id='mp-detail'></div></div>` +
		`</div>` +
		`</div>` +
		`<script>window.DATA = ` + dataJson + `;</script>` +
		initialViewScript +
		`<script>` + assets.BrowserJS + `</script>` +
		quietScript +
		`</body></html>`
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

<script>
  // Die Karte eines Repos (repo-karte, #36) als Folge-Linienplan
  // (repo-karte-folge, #99, Entwurf G): von links nach rechts die Zonen
  // erledigt, jetzt und naechste, je Map eine waagerechte Linie, die Stationen
  // an ihrem Slot, die offenen Fragen als Karten in der Jetzt-Zone, von denen
  // rot gestrichelte Pfeile mit Schranke zu den gesperrten Stationen laufen.
  // Welche Station in welcher Zone an welchem Slot auf welcher Linie steht,
  // was gesperrt, bereit oder kritisch ist, und welche Kante welche Art hat,
  // kommt fertig aus Go (internal/appctx/karte.go). Hier wird nur in Pixel
  // umgerechnet und gezeichnet. Eingeklappt, bis jemand sie oeffnet; erst
  // dann geht die Anfrage raus. Darunter steht die Liste "Issues nach
  // Parent" (karte-gruppen, #121): ein Klick auf eine Station springt zu
  // ihrer Zeile und markiert beide, ein Klick auf eine Zeile markiert ihre
  // Station; die Detailspalte oeffnet der Doppelklick. Eingeklappt ist die
  // Karte eine Zeile (karte-einzeilig, #124): ihre Zahlen kommen fertig aus
  // Go, vor dem Laden aus karte_kopf der Arbeitsliste, danach aus kopf der
  // Karte; ein Klick auf eine Nummer oeffnet die Detailspalte.
  import { tick } from 'svelte';
  import { holen, mitQuery } from './api.js';
  import KarteGruppen from './KarteGruppen.svelte';

  let { repo, oeffnen, kopf = null } = $props();

  // Neuer Schluessel (karte-einzeilig, #124): nach dem Deploy beginnen alle
  // eingeklappt, auch wer unter kn-karte-offen "offen" gemerkt hatte.
  const SCHLUESSEL = 'kn-karte-offen-2';
  const LABEL = 172, LUECKE = 14, KOPF = 26;
  const FRAGE_H = 58, FRAGE_ABSTAND = 8;
  const ZEILE = 78, KARTE_H = 98, ACHSE = 46;
  const FARBEN = ['#8e4ec6', '#e07b00', '#138a8a', '#1c7cf4', '#c2366f', '#6b8e23', '#a0522d'];
  const STUFEN = ['gate1', 'gate2', 'im_pr'];

  function gemerkt() {
    try {
      return JSON.parse(localStorage.getItem(SCHLUESSEL) || '[]');
    } catch {
      return [];
    }
  }

  let offen = $state(false);
  let karte = $state(null);
  let fehler = $state('');
  let weite = $state(1000);
  let gewaehlt = $state(null);
  let geladenFuer = '';

  $effect(() => {
    const r = repo;
    karte = null;
    fehler = '';
    gewaehlt = null;
    geladenFuer = '';
    // Lokal lesen, nicht ueber offen: sonst haengt der Effekt an offen und
    // laeuft bei jedem Umschalten neu (doppelte Anfrage).
    const warOffen = gemerkt().includes(r);
    offen = warOffen;
    if (warOffen) laden(r);
  });

  async function laden(r) {
    if (geladenFuer === r) return;
    geladenFuer = r;
    try {
      const k = await holen(mitQuery('/api/karte', { repo: r }));
      if (repo === r) karte = k;
      // Erneuert der Dienst gerade im Hintergrund, einmal nachfragen.
      if (k.sync?.laeuft && repo === r) {
        setTimeout(async () => {
          try {
            const neu = await holen(mitQuery('/api/karte', { repo: r }));
            if (repo === r) karte = neu;
          } catch {
            /* der alte Stand bleibt */
          }
        }, 2500);
      }
    } catch (e) {
      if (repo === r) fehler = e.message;
    }
  }

  function umschalten() {
    offen = !offen;
    try {
      const liste = gemerkt().filter((x) => x !== repo);
      if (offen) liste.push(repo);
      localStorage.setItem(SCHLUESSEL, JSON.stringify(liste));
    } catch {
      /* ohne Speicher gilt der Zustand nur bis zum Neuladen */
    }
    if (offen) laden(repo);
  }

  // Station -> Zeile: markieren und die Zeile in den sichtbaren Bereich
  // holen. Zeile -> Station: nur markieren.
  async function waehlen(nr, springen) {
    gewaehlt = nr;
    if (!springen) return;
    await tick();
    document.querySelector(`[data-zeile="${nr}"]`)?.scrollIntoView({ block: 'center' });
  }

  function farbe(nr) {
    return plan?.linien.get(nr)?.farbe ?? 'var(--t-grau)';
  }

  function stationTaste(ev, s) {
    if (ev.key === 'Enter' || ev.key === ' ') {
      ev.preventDefault();
      waehlen(s.nummer, true);
    }
  }

  function taste(ev, z) {
    if (ev.key === 'Enter' || ev.key === ' ') {
      ev.preventDefault();
      oeffnen(z);
    }
  }

  // Die Zeile: Zahlen aus Go, hier nur zusammengesetzt.
  let k = $derived(karte?.kopf || kopf);

  function nummer(ev, nr) {
    ev.stopPropagation();
    oeffnen({ nummer: nr, titel: '#' + nr });
  }

  const kurz = (t, n) => (t && t.length > n ? t.slice(0, Math.max(1, n - 1)) + '…' : t || '');
  const zahl = (name) => karte?.zonen?.find((z) => z.name === name)?.anzahl ?? 0;

  // Die Geometrie: nur Pixel aus dem, was Go liefert.
  let plan = $derived.by(() => {
    if (!karte) return null;
    const nE = zahl('erledigt'), nJ = zahl('jetzt'), nN = zahl('naechste');
    const nF = karte.fragen.length;
    const mE = Math.max(150, nE * 44), mJ = Math.max(nF ? 360 : 0, nJ * 170, 190), mN = Math.max(170, nN * 64);
    const x0 = LABEL + LUECKE;
    let frei = weite - x0 - 40 - (mE + mJ + mN);
    if (frei < 0) frei = 0;
    const wE = mE + frei * 0.24, wJ = mJ + frei * 0.3, wN = mN + frei * 0.46;
    const breite = x0 + wE + wJ + wN + 40;
    const zonen = {
      erledigt: { x: x0, w: wE, n: nE },
      jetzt: { x: x0 + wE, w: wJ, n: nJ },
      naechste: { x: x0 + wE + wJ, w: wN, n: nN },
    };
    const fragenOben = KOPF + 10;
    const fragenHoehe = nF ? nF * (FRAGE_H + FRAGE_ABSTAND) : 0;
    let y = fragenOben + fragenHoehe + 12;
    const linien = new Map();
    karte.linien.forEach((l, i) => {
      const mitJetzt = karte.stationen.some((s) => s.linie === l.nummer && s.zone === 'jetzt');
      const h = ZEILE + (mitJetzt ? KARTE_H : 0);
      const farbe = l.nummer === 0 ? 'var(--t-grau)' : FARBEN[i % FARBEN.length];
      linien.set(l.nummer, { oben: y, h, spur: y + 36, farbe, index: i });
      y += h;
    });
    const achse = y + 8;
    const hoehe = achse + ACHSE;
    const stationen = new Map();
    for (const s of karte.stationen) {
      const z = zonen[s.zone];
      if (!z) continue;
      const abstand = z.w / Math.max(1, z.n);
      stationen.set(s.nummer, { x: z.x + (s.slot + 0.5) * abstand, y: linien.get(s.linie)?.spur ?? 0, abstand, s });
    }
    const fragen = new Map();
    karte.fragen.forEach((f, i) => {
      const oben = fragenOben + i * (FRAGE_H + FRAGE_ABSTAND);
      fragen.set(f.nummer, { x: zonen.jetzt.x + 10, w: zonen.jetzt.w - 20, oben, mitte: oben + FRAGE_H / 2, i });
    });
    return { breite, hoehe, zonen, linien, stationen, fragen, fragenOben, fragenHoehe, achse };
  });

  // Anfang und Ende einer Linie: vom linken Rand bis hinter die letzte
  // Station; bis zur ersten Station gepunktet, wenn noch nichts erledigt ist.
  function spur(l) {
    const xs = karte.stationen.filter((s) => s.linie === l.nummer).map((s) => plan.stationen.get(s.nummer)?.x ?? 0);
    const erste = Math.min(...xs), letzte = Math.max(...xs);
    const mitErledigt = karte.stationen.some((s) => s.linie === l.nummer && s.zone === 'erledigt');
    return { von: mitErledigt ? plan.zonen.erledigt.x - 8 : erste, bis: letzte + 18, frueh: mitErledigt ? null : plan.zonen.erledigt.x - 8, erste };
  }

  // Der Weg einer Kante. Frage-Kanten laufen aus der Karte nach rechts in
  // eine eigene Gasse, senkrecht auf die Linie und waagerecht bis kurz vor die
  // Station; die anderen laufen waagerecht und knicken in 45 Grad.
  function weg(k) {
    const nach = plan.stationen.get(k.nach);
    if (!nach) return null;
    if (k.art === 'frage') {
      const f = plan.fragen.get(k.von);
      if (!f) return null;
      const gasse = plan.zonen.naechste.x + 6 + f.i * 7;
      const ende = nach.x - 22;
      return { d: `M${f.x + f.w} ${f.mitte} H${gasse} V${nach.y} H${ende}`, schranke: { x: nach.x - 17, y: nach.y } };
    }
    const von = plan.stationen.get(k.von);
    if (!von) return null;
    const x1 = von.x + 10, y1 = von.y, x2 = nach.x - 13, y2 = nach.y;
    const dy = Math.abs(y2 - y1);
    if (dy < 1) return { d: `M${x1} ${y1} H${x2}` };
    if (x2 - x1 > dy + 16) {
      return { d: `M${x1} ${y1} H${x2 - dy - 8} L${x2 - 8} ${y2} H${x2}` };
    }
    const mx = (x1 + x2) / 2;
    return { d: `M${x1} ${y1} C${mx} ${y1} ${mx} ${y2} ${x2} ${y2}` };
  }

  function stufeAn(s, st) {
    const i = STUFEN.indexOf(s.stufe);
    const j = STUFEN.indexOf(st);
    if (j < i) return 'kn-karte-stufe-vor';
    if (j === i) return 'kn-karte-stufe-jetzt';
    return '';
  }

  function wartetText(s) {
    if (!s.wartet_auf?.length) return s.bereit ? 'bereit – frei startbar' : '';
    return 'wartet auf ' + s.wartet_auf.map((n) => '#' + n).join(', ');
  }

  function wartetArt(s) {
    if (s.gesperrt) return 'kn-karte-wartet-gesperrt';
    if (s.wartet_auf?.some((n) => karte.stationen.find((x) => x.nummer === n)?.zone === 'jetzt')) return 'kn-karte-wartet-aktiv';
    return '';
  }
</script>

<section class="kn-karte">
  <!-- Die ganze Zeile klappt; der Knopf traegt den Zustand fuer Tastatur und
       Vorleser, sein Klick steigt zur Zeile auf. -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="kn-karte-kopf" data-offen={offen} onclick={umschalten}>
    <button type="button" class="kn-karte-klapp" aria-expanded={offen} aria-label={offen ? 'Karte zuklappen' : 'Karte aufklappen'}>
      <svg class="kn-karte-pfeil" class:kn-zu={!offen} width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 3.5 5 6.5 8 3.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
    </button>
    <span class="kn-karte-titel">Karte</span>
    {#if k}<span class="kn-karte-zahl">{'· '}{k.erledigt} erledigt · {k.aktiv} aktiv · {k.naechste} als nächstes{#if k.fragen}{' · '}{k.fragen} {k.fragen === 1 ? 'Frage' : 'Fragen'}{#if k.p1?.length}{' ('}{k.p1.length}× P1:{' '}{#each k.p1 as f, i (f.nummer)}{#if i}{', '}{/if}<button type="button" class="kn-karte-nr kn-karte-p1" data-kopf-nummer={f.nummer} title="Frage #{f.nummer} öffnen" onclick={(ev) => nummer(ev, f.nummer)}>#{f.nummer}</button>{' hält '}{f.haelt}{/each}{')'}{/if}{/if}{#if k.bereit}{' · nächste bereit: '}<button type="button" class="kn-karte-nr" data-kopf-nummer={k.bereit} title="#{k.bereit} öffnen" onclick={(ev) => nummer(ev, k.bereit)}>#{k.bereit}</button>{/if}</span>{/if}
  </div>

  {#if offen}
    {#if fehler}
      <p class="kn-karte-leer" title={fehler}>Karte nicht geladen: {fehler}</p>
    {:else if !karte}
      <p class="kn-karte-leer">Lade Issues, Maps und die Reihe …</p>
    {:else if !karte.stationen.length && !karte.fragen.length}
      <p class="kn-karte-leer">Keine Issues: nichts erledigt, nichts offen.</p>
    {:else if plan}
      {#if karte.ziel}<p class="kn-karte-ziel" data-ziel>Ziel: {karte.ziel}</p>{/if}
      <div class="kn-karte-rahmen" bind:clientWidth={weite}>
      <svg class="kn-karte-svg" width={plan.breite} height={plan.hoehe} viewBox="0 0 {plan.breite} {plan.hoehe}" role="img" aria-label="Folge-Linienplan: erledigt, jetzt, als Nächstes">
        <defs>
          <pattern id="kn-schraffur" width="7" height="7" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width="7" height="7" fill="color-mix(in srgb, var(--t-rot) 6%, transparent)" />
            <line x1="0" y1="0" x2="0" y2="7" stroke="color-mix(in srgb, var(--t-rot) 35%, transparent)" stroke-width="2" />
          </pattern>
          <marker id="kn-spitze-aktiv" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 0 8 4 0 8z" fill="var(--t-blau)" />
          </marker>
          <marker id="kn-spitze-folge" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 0 8 4 0 8z" fill="var(--t-dim)" />
          </marker>
        </defs>

        <!-- Innerhalb des svg, damit die Elemente im SVG-Namensraum entstehen. -->
        {#snippet linienkarte(l, g)}
          <rect x="2" y={g.oben + 6} width={LABEL - 4} height="54" rx="6" stroke={g.farbe} class="kn-karte-linienrahmen" />
          <circle cx="18" cy={g.oben + 20} r="9" fill={g.farbe} />
          <text x="18" y={g.oben + 23.5} text-anchor="middle" class="kn-karte-linien-nr">{l.nummer > 0 ? l.nummer : '–'}</text>
          <text x="32" y={g.oben + 24} class="kn-karte-linien-titel" fill={l.nummer > 0 ? g.farbe : 'var(--t-dim)'}>{kurz(l.titel, 18)}</text>
          <rect x="10" y={g.oben + 33} width={LABEL - 20} height="4" rx="2" class="kn-karte-fortschritt" />
          <rect x="10" y={g.oben + 33} width={(LABEL - 20) * (l.gesamt ? l.erledigt / l.gesamt : 0)} height="4" rx="2" fill={g.farbe} />
          <text x="10" y={g.oben + 53} class="kn-karte-linien-stand"><tspan class="kn-karte-linien-zahl">{l.erledigt}/{l.gesamt} erledigt</tspan> · {l.aktiv} aktiv · {l.gesperrt} gesperrt</text>
          <title>{l.nummer > 0 ? '#' + l.nummer + ' ' : ''}{l.titel} · {l.erledigt}/{l.gesamt} erledigt · {l.aktiv} aktiv · {l.gesperrt} gesperrt</title>
        {/snippet}

        <!-- Zonenbaender -->
        {#each [['erledigt', 'ERLEDIGT · letzte ' + zahl('erledigt') + ', ältestes links'], ['jetzt', 'JETZT · ' + karte.stand + ' · aktiv + zu klären'], ['naechste', 'ALS NÄCHSTES · Rang 1..' + zahl('naechste')]] as [name, titel] (name)}
          {@const z = plan.zonen[name]}
          <g data-zone-band={name}>
            <rect x={z.x} y="0" width={z.w} height={plan.hoehe} class="kn-karte-band kn-karte-band-{name}" />
            <rect x={z.x + 2} y="2" width={z.w - 4} height={KOPF - 4} rx="3" class="kn-karte-bandkopf kn-karte-bandkopf-{name}" />
            <text x={z.x + z.w / 2} y={KOPF / 2 + 5} text-anchor="middle" class="kn-karte-bandtitel kn-karte-bandtitel-{name}">{titel}</text>
          </g>
        {/each}
        {#if karte.fragen.length}
          <rect x={plan.zonen.erledigt.x} y={plan.fragenOben - 4} width={plan.zonen.erledigt.w + plan.zonen.jetzt.w} height={plan.fragenHoehe} class="kn-karte-klaeren" />
          <text x={plan.zonen.erledigt.x + 12} y={plan.fragenOben + 18} class="kn-karte-klaeren-titel">ZU KLÄREN</text>
          <text x={plan.zonen.erledigt.x + 12} y={plan.fragenOben + 36} class="kn-karte-klaeren-text">Offene Fragen gehören zu keinem Slot.</text>
          <text x={plan.zonen.erledigt.x + 12} y={plan.fragenOben + 51} class="kn-karte-klaeren-text">Ihre Pfeile sperren nach rechts.</text>
        {/if}

        <!-- Linien je Map -->
        {#each karte.linien as l (l.nummer)}
          {@const g = plan.linien.get(l.nummer)}
          {@const sp = spur(l)}
          <g class="kn-karte-linie" data-linie={l.nummer}>
            <line x1="0" x2={plan.breite} y1={g.oben + g.h} y2={g.oben + g.h} class="kn-karte-trenner" />
            {#if sp.frueh !== null}
              <line x1={sp.frueh} x2={sp.erste} y1={g.spur} y2={g.spur} stroke={g.farbe} class="kn-karte-spur-frueh" />
            {/if}
            <line x1={sp.von} x2={sp.bis} y1={g.spur} y2={g.spur} stroke={g.farbe} class="kn-karte-spur" />
            <line x1={sp.bis} x2={sp.bis} y1={g.spur - 8} y2={g.spur + 8} stroke={g.farbe} class="kn-karte-prellbock" />
            {#if l.nummer > 0}
              <g class="kn-karte-linienkarte kn-karte-klickbar" data-linie-titel={l.nummer} role="button" tabindex="0"
                 onclick={() => oeffnen(l)} onkeydown={(ev) => taste(ev, l)}>
                {@render linienkarte(l, g)}
              </g>
            {:else}
              <g class="kn-karte-linienkarte">{@render linienkarte(l, g)}</g>
            {/if}
          </g>
        {/each}

        <!-- Kanten: erst der Leuchtrand des kritischen Pfads, dann die Pfeile -->
        {#each karte.kanten as k (k.von + '-' + k.nach)}
          {@const w = weg(k)}
          {#if w && k.kritisch}<path d={w.d} class="kn-karte-leuchten" />{/if}
        {/each}
        {#each karte.kanten as k (k.von + '-' + k.nach)}
          {@const w = weg(k)}
          {#if w}
            <path d={w.d} class="kn-karte-kante kn-karte-kante-{k.art}" data-kante={k.von + '-' + k.nach} data-art={k.art}
                  marker-end={k.art === 'aktiv' ? 'url(#kn-spitze-aktiv)' : k.art === 'folge' ? 'url(#kn-spitze-folge)' : undefined}>
              <title>#{k.nach} wartet auf #{k.von}{k.art === 'frage' ? ' (offene Frage)' : k.art === 'aktiv' ? ' (läuft)' : ''}</title>
            </path>
            {#if w.schranke}
              <g class="kn-karte-schranke" data-schranke={k.von + '-' + k.nach}>
                <rect x={w.schranke.x - 2} y={w.schranke.y - 11} width="4" height="22" rx="1" />
                <circle cx={w.schranke.x} cy={w.schranke.y + 19} r="6.5" class="kn-karte-schloss-grund" />
                <rect x={w.schranke.x - 3.5} y={w.schranke.y + 18} width="7" height="5" rx="1" class="kn-karte-schloss" />
                <path d="M{w.schranke.x - 2} {w.schranke.y + 18} v-2 a2 2 0 0 1 4 0 v2" class="kn-karte-buegel" />
              </g>
            {/if}
          {/if}
        {/each}

        <!-- Fragen -->
        {#each karte.fragen as f (f.nummer)}
          {@const p = plan.fragen.get(f.nummer)}
          {@const zeichen = Math.max(12, Math.floor((p.w - 100) / 6.4))}
          <g class="kn-karte-frage kn-karte-prio-{f.prio}" class:kn-karte-kritisch={f.kritisch} data-frage={f.nummer} data-prio={f.prio}
             role="button" tabindex="0" onclick={() => oeffnen(f)} onkeydown={(ev) => taste(ev, f)}>
            {#if f.kritisch}<rect x={p.x - 3} y={p.oben - 3} width={p.w + 6} height={FRAGE_H + 6} rx="8" class="kn-karte-frage-leuchten" />{/if}
            <rect x={p.x} y={p.oben} width={p.w} height={FRAGE_H} rx="6" class="kn-karte-frage-rahmen" />
            <circle cx={p.x + 18} cy={p.oben + 18} r="9" class="kn-karte-frage-zeichen" />
            <text x={p.x + 18} y={p.oben + 22} text-anchor="middle" class="kn-karte-frage-zeichen-text">?</text>
            <rect x={p.x + 7} y={p.oben + 33} width="22" height="13" rx="3" class="kn-karte-frage-zeichen" />
            <text x={p.x + 18} y={p.oben + 43} text-anchor="middle" class="kn-karte-frage-prio">P{f.prio}</text>
            <text x={p.x + 38} y={p.oben + 16} class="kn-karte-frage-titel"><tspan class="kn-karte-frage-nr">#{f.nummer}</tspan> {kurz(f.titel, zeichen - 5)}</text>
            <text x={p.x + 38} y={p.oben + 32} class="kn-karte-frage-wortlaut">„{kurz(f.wortlaut || f.titel, zeichen)}“</text>
            <text x={p.x + 38} y={p.oben + 48} class="kn-karte-frage-fuss">wer: {f.wer || 'offen'} · seit {f.seit_tagen} {f.seit_tagen === 1 ? 'Tag' : 'Tagen'} · <tspan class="kn-karte-frage-sperrt">sperrt direkt: {f.sperrt.length ? f.sperrt.map((n) => '#' + n).join(', ') : 'nichts'}</tspan></text>
            <circle cx={p.x + p.w - 22} cy={p.oben + 22} r="13" class="kn-karte-hebel" />
            <text x={p.x + p.w - 22} y={p.oben + 27} text-anchor="middle" class="kn-karte-hebel-zahl" data-hebel={f.hebel}>{f.hebel}</text>
            <text x={p.x + p.w - 22} y={p.oben + 50} text-anchor="middle" class="kn-karte-hebel-text">Hebel</text>
            <title>#{f.nummer} {f.titel} · Prio {f.prio} · hält {f.hebel} zurück</title>
          </g>
        {/each}

        <!-- Stationen -->
        {#each karte.stationen as s (s.nummer)}
          {@const p = plan.stationen.get(s.nummer)}
          {@const g = plan.linien.get(s.linie)}
          {@const zeichen = Math.max(6, Math.floor((p.abstand - 6) / 6))}
          <g class="kn-karte-station kn-karte-zone-{s.zone}" class:kn-karte-gesperrt={s.gesperrt} class:kn-karte-kritisch={s.kritisch}
             class:kn-karte-bereit={s.bereit} class:kn-karte-markiert={gewaehlt === s.nummer} data-station={s.nummer} data-zone={s.zone} data-rang={s.rang || undefined}
             role="button" tabindex="0" onclick={() => waehlen(s.nummer, true)} ondblclick={() => oeffnen(s)} onkeydown={(ev) => stationTaste(ev, s)}>
            {#if gewaehlt === s.nummer}<circle cx={p.x} cy={p.y} r="17" class="kn-karte-auswahlring" />{/if}
            {#if s.gesperrt}
              {@const unten = s.zone === 'naechste' && s.slot % 2 === 1}
              <rect x={p.x - p.abstand + 4} y={unten ? p.y - 14 : p.y - 34} width={p.abstand * 2 - 8} height="52" class="kn-karte-schraffur" />
            {/if}
            {#if s.kritisch}<circle cx={p.x} cy={p.y} r="15" class="kn-karte-leuchtrand" />{/if}
            {#if s.zone === 'erledigt'}
              <text x={p.x} y={p.y - 12} text-anchor="middle" class="kn-karte-st-nr">#{s.nummer}</text>
              <circle cx={p.x} cy={p.y} r="5.5" class="kn-karte-punkt-erledigt" />
              <text x={p.x} y={p.y + 19} text-anchor="middle" class="kn-karte-st-klein">{kurz(s.titel, zeichen + 2)}</text>
            {:else if s.zone === 'jetzt'}
              {@const bw = Math.min(176, p.abstand - 10)}
              {@const bx = p.x - bw / 2}
              {@const zj = Math.max(8, Math.floor((bw - 16) / 6.6))}
              <circle cx={p.x} cy={p.y} r="10" class="kn-karte-punkt-jetzt" />
              <circle cx={p.x} cy={p.y} r="3.5" class="kn-karte-punkt-jetzt-kern" />
              <line x1={p.x} x2={p.x} y1={p.y + 10} y2={p.y + 18} class="kn-karte-stiel" />
              <g class="kn-karte-jetztkarte" data-jetzt-karte={s.nummer}>
                <rect x={bx} y={p.y + 18} width={bw} height={KARTE_H - 8} rx="5" class="kn-karte-jetzt-rahmen" />
                <rect x={bx} y={p.y + 18} width="3" height={KARTE_H - 8} class="kn-karte-jetzt-kante" />
                <text x={bx + 9} y={p.y + 32} class="kn-karte-jetzt-nr">#{s.nummer}</text>
                <text x={bx + 9} y={p.y + 45} class="kn-karte-jetzt-titel">{kurz(s.titel, zj)}</text>
                <text x={bx + 9} y={p.y + 56} class="kn-karte-jetzt-branch">{kurz(s.branch || 'kein Branch', zj + 4)}</text>
                {#each STUFEN as st, i (st)}
                  <rect x={bx + 9 + i * ((bw - 18) / 3)} y={p.y + 60} width={(bw - 18) / 3 - 3} height="11" rx="2" class="kn-karte-stufe {stufeAn(s, st)}" />
                  <text x={bx + 9 + i * ((bw - 18) / 3) + ((bw - 18) / 3 - 3) / 2} y={p.y + 68.5} text-anchor="middle" class="kn-karte-stufe-text {stufeAn(s, st)}">{st}</text>
                {/each}
                <text x={bx + 9} y={p.y + 84} class="kn-karte-jetzt-fuss">{#if s.aufgaben_gesamt}Aufgaben {s.aufgaben_erledigt ?? 0}/{s.aufgaben_gesamt}{:else}ohne Aufgaben{/if}</text>
                {#if s.aufgaben_gesamt}
                  <rect x={bx + bw / 2 + 4} y={p.y + 79} width={bw / 2 - 13} height="4" rx="2" class="kn-karte-fortschritt" />
                  <rect x={bx + bw / 2 + 4} y={p.y + 79} width={(bw / 2 - 13) * ((s.aufgaben_erledigt ?? 0) / s.aufgaben_gesamt)} height="4" rx="2" class="kn-karte-jetzt-balken" />
                {/if}
                <text x={bx + 9} y={p.y + 97} class="kn-karte-jetzt-fuss">{#if s.pr}<tspan class="kn-karte-jetzt-pr">PR #{s.pr}</tspan>{#if s.checks}{'\u00a0'}<tspan class="kn-karte-checks kn-karte-checks-{s.checks}" data-checks={s.checks}>● Checks {s.checks}</tspan>{/if}{:else}noch kein PR{/if}</text>
              </g>
            {:else}
              <!-- Beschriftung abwechselnd ueber und unter der Linie, damit
                   Nachbarn den doppelten Platz haben. -->
              {@const unten = s.slot % 2 === 1}
              {@const zn = zeichen * 2 - 2}
              <text x={p.x} y={unten ? p.y + 24 : p.y - 24} text-anchor="middle" class="kn-karte-st-titel"><tspan class="kn-karte-st-nr">#{s.nummer}</tspan> {kurz(s.titel, zn - 5)}</text>
              <text x={p.x} y={unten ? p.y + 35 : p.y - 13} text-anchor="middle" class="kn-karte-st-wartet {wartetArt(s)}">{kurz(wartetText(s), zn)}</text>
              <circle cx={p.x} cy={p.y} r="10" class="kn-karte-punkt-naechst" />
              <text x={p.x} y={p.y + 4} text-anchor="middle" class="kn-karte-rang">{s.rang}</text>
            {/if}
            <title>#{s.nummer} {s.titel}{s.erledigt_am ? ' · erledigt am ' + s.erledigt_am : ''}{s.rang ? ' · als Nächstes Nr. ' + s.rang : ''}{s.wartet_auf?.length ? ' · ' + wartetText(s) : ''}{s.gesperrt ? ' · gesperrt durch eine offene Frage' : ''}{s.bereit ? ' · bereit' : ''}</title>
          </g>
        {/each}

        <!-- Folge-Achse -->
        <line x1={plan.zonen.erledigt.x} x2={plan.breite - 6} y1={plan.achse} y2={plan.achse} class="kn-karte-achse" />
        <text x="4" y={plan.achse + 5} class="kn-karte-achse-titel">Folge-Achse</text>
        <text x="4" y={plan.achse + 19} class="kn-karte-achse-text">Datum | jetzt | Rang</text>
        {#each karte.stationen as s (s.nummer)}
          {@const p = plan.stationen.get(s.nummer)}
          {#if s.zone === 'erledigt'}
            <text x={p.x} y={plan.achse + 17} text-anchor="middle" class="kn-karte-achse-datum">{s.marke}</text>
          {:else if s.zone === 'naechste'}
            <circle cx={p.x} cy={plan.achse + 14} r="8" class="kn-karte-achse-rang" />
            <text x={p.x} y={plan.achse + 17.5} text-anchor="middle" class="kn-karte-achse-rang-text">{s.marke}</text>
          {/if}
        {/each}
        <text x={plan.zonen.jetzt.x + plan.zonen.jetzt.w / 2} y={plan.achse + 17} text-anchor="middle" class="kn-karte-achse-jetzt">JETZT · Stufe statt Zeit</text>
      </svg>
      </div>
      {#if karte.gruppen?.length}
        <KarteGruppen gruppen={karte.gruppen} {gewaehlt} waehlen={(nr) => waehlen(nr, false)} {oeffnen} {farbe} />
      {/if}
    {/if}
  {/if}
</section>

<style>
  .kn-karte {
    margin-top: 22px;
  }
  .kn-karte-kopf {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    box-sizing: border-box;
    padding: 0 8px 6px;
    border-bottom: 1px solid var(--t-linie);
    color: var(--t-blau);
    font-size: 15px;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    cursor: pointer;
    user-select: none;
  }
  .kn-karte-klapp {
    display: grid;
    place-items: center;
    padding: 2px;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .kn-karte-titel {
    flex: none;
  }
  .kn-karte-nr {
    padding: 0;
    border: 0;
    background: transparent;
    font: inherit;
    color: var(--t-blau);
    cursor: pointer;
  }
  .kn-karte-nr:hover {
    text-decoration: underline;
  }
  .kn-karte-nr.kn-karte-p1 {
    color: var(--t-rot);
    font-weight: 600;
  }
  .kn-karte-pfeil {
    transition: transform 0.15s;
  }
  .kn-karte-pfeil.kn-zu {
    transform: rotate(-90deg);
  }
  .kn-karte-zahl {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 13px;
    font-weight: 400;
    color: var(--t-text2);
  }
  .kn-karte-ziel {
    margin: 6px 8px 0;
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-karte-auswahlring {
    fill: color-mix(in srgb, #c2366f 12%, transparent);
    stroke: #c2366f;
    stroke-width: 2.5;
  }
  .kn-karte-leer {
    color: var(--t-dim);
    font-size: 13px;
    padding: 8px;
    margin: 0;
  }
  /* Der Linienplan braucht Breite: er nutzt die ganze mittlere Spalte (nicht
     nur die Textbreite) und scrollt waagerecht, wenn auch die nicht reicht. */
  .kn-karte-rahmen {
    width: calc(100cqw - 32px);
    margin-left: calc((100% - (100cqw - 32px)) / 2);
    min-width: 100%;
    overflow-x: auto;
  }
  .kn-karte-svg {
    display: block;
    margin-top: 8px;
    font-family: inherit;
  }
  .kn-karte-band-erledigt {
    fill: color-mix(in srgb, var(--t-grau) 12%, transparent);
  }
  .kn-karte-band-jetzt {
    fill: color-mix(in srgb, var(--t-blau) 7%, transparent);
    stroke: var(--t-blau);
    stroke-width: 1.5;
  }
  .kn-karte-band-naechste {
    fill: transparent;
  }
  .kn-karte-bandkopf-erledigt {
    fill: color-mix(in srgb, var(--t-grau) 30%, transparent);
  }
  .kn-karte-bandkopf-jetzt {
    fill: var(--t-blau);
  }
  .kn-karte-bandkopf-naechste {
    fill: var(--t-text);
  }
  .kn-karte-bandtitel {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.04em;
    fill: var(--t-text2, var(--t-text));
  }
  .kn-karte-bandtitel-jetzt,
  .kn-karte-bandtitel-naechste {
    fill: var(--t-bg);
  }
  .kn-karte-klaeren {
    fill: color-mix(in srgb, var(--t-rot) 5%, transparent);
  }
  .kn-karte-klaeren-titel {
    font-size: 13px;
    font-weight: 700;
    fill: var(--t-rot);
  }
  .kn-karte-klaeren-text {
    font-size: 11px;
    fill: var(--t-dim);
  }
  .kn-karte-trenner {
    stroke: var(--t-linie);
    stroke-width: 1;
  }
  .kn-karte-spur {
    stroke-width: 6;
    stroke-linecap: round;
    opacity: 0.85;
  }
  .kn-karte-spur-frueh {
    stroke-width: 1.5;
    stroke-dasharray: 4 4;
  }
  .kn-karte-prellbock {
    stroke-width: 4;
  }
  .kn-karte-klickbar {
    cursor: pointer;
    outline: none;
  }
  .kn-karte-linienrahmen {
    fill: var(--t-karte, var(--t-bg));
    stroke-width: 2;
  }
  .kn-karte-klickbar:hover .kn-karte-linienrahmen,
  .kn-karte-klickbar:focus .kn-karte-linienrahmen {
    fill: var(--t-hover);
  }
  .kn-karte-linien-nr {
    font-size: 9.5px;
    font-weight: 700;
    fill: #fff;
  }
  .kn-karte-linien-titel {
    font-size: 12.5px;
    font-weight: 700;
  }
  .kn-karte-fortschritt {
    fill: var(--t-linie);
  }
  .kn-karte-linien-stand {
    font-size: 10.5px;
    fill: var(--t-dim);
  }
  .kn-karte-linien-zahl {
    font-weight: 700;
    fill: var(--t-text);
  }
  .kn-karte-leuchten {
    fill: none;
    stroke: var(--t-gelb);
    stroke-width: 11;
    stroke-linecap: round;
    stroke-linejoin: round;
    opacity: 0.55;
  }
  .kn-karte-kante {
    fill: none;
    stroke-linejoin: round;
  }
  .kn-karte-kante-frage {
    stroke: var(--t-rot);
    stroke-width: 2;
    stroke-dasharray: 6 4;
  }
  .kn-karte-kante-aktiv {
    stroke: var(--t-blau);
    stroke-width: 3;
  }
  .kn-karte-kante-folge {
    stroke: var(--t-dim);
    stroke-width: 2;
  }
  .kn-karte-schranke rect:first-child {
    fill: var(--t-rot);
  }
  .kn-karte-schloss-grund {
    fill: var(--t-bg);
    stroke: var(--t-rot);
    stroke-width: 1.5;
  }
  .kn-karte-schloss {
    fill: var(--t-rot);
  }
  .kn-karte-buegel {
    fill: none;
    stroke: var(--t-rot);
    stroke-width: 1.3;
  }
  .kn-karte-frage,
  .kn-karte-station {
    cursor: pointer;
    outline: none;
  }
  .kn-karte-frage-rahmen {
    fill: var(--t-karte, var(--t-bg));
    stroke: var(--t-rot);
    stroke-width: 1.6;
  }
  .kn-karte-prio-2 .kn-karte-frage-rahmen {
    stroke: #e07b00;
  }
  .kn-karte-frage:hover .kn-karte-frage-rahmen,
  .kn-karte-frage:focus .kn-karte-frage-rahmen {
    fill: color-mix(in srgb, var(--t-rot) 6%, var(--t-bg));
  }
  .kn-karte-frage-leuchten {
    fill: none;
    stroke: var(--t-gelb);
    stroke-width: 5;
    opacity: 0.7;
  }
  .kn-karte-frage-zeichen {
    fill: var(--t-rot);
  }
  .kn-karte-prio-2 .kn-karte-frage-zeichen {
    fill: #e07b00;
  }
  .kn-karte-frage-zeichen-text,
  .kn-karte-frage-prio {
    font-size: 11px;
    font-weight: 800;
    fill: #fff;
  }
  .kn-karte-frage-prio {
    font-size: 9px;
  }
  .kn-karte-frage-titel {
    font-size: 12.5px;
    font-weight: 700;
    fill: var(--t-text);
  }
  .kn-karte-frage-nr {
    fill: var(--t-rot);
  }
  .kn-karte-prio-2 .kn-karte-frage-nr {
    fill: #e07b00;
  }
  .kn-karte-frage-wortlaut {
    font-size: 11px;
    font-style: italic;
    fill: var(--t-text2, var(--t-text));
  }
  .kn-karte-frage-fuss {
    font-size: 10px;
    fill: var(--t-dim);
  }
  .kn-karte-frage-sperrt {
    font-weight: 700;
    fill: var(--t-rot);
  }
  .kn-karte-prio-2 .kn-karte-frage-sperrt {
    fill: var(--t-text2, var(--t-text));
  }
  .kn-karte-hebel {
    fill: var(--t-rot);
  }
  .kn-karte-prio-2 .kn-karte-hebel {
    fill: var(--t-bg);
    stroke: #e07b00;
    stroke-width: 2;
  }
  .kn-karte-hebel-zahl {
    font-size: 13px;
    font-weight: 800;
    fill: #fff;
  }
  .kn-karte-prio-2 .kn-karte-hebel-zahl {
    fill: #e07b00;
  }
  .kn-karte-hebel-text {
    font-size: 8.5px;
    fill: var(--t-rot);
  }
  .kn-karte-schraffur {
    fill: url(#kn-schraffur);
  }
  .kn-karte-leuchtrand {
    fill: none;
    stroke: var(--t-gelb);
    stroke-width: 6;
    opacity: 0.75;
  }
  .kn-karte-st-nr {
    font-size: 10px;
    font-weight: 700;
    fill: var(--t-text);
  }
  .kn-karte-st-klein {
    font-size: 9px;
    fill: var(--t-dim);
  }
  .kn-karte-st-titel {
    font-size: 10.5px;
    fill: var(--t-text);
  }
  .kn-karte-st-wartet {
    font-size: 9.5px;
    fill: var(--t-dim);
  }
  .kn-karte-wartet-gesperrt {
    fill: var(--t-rot);
  }
  .kn-karte-wartet-aktiv {
    fill: var(--t-blau);
  }
  .kn-karte-bereit .kn-karte-st-wartet {
    fill: var(--t-gruen);
    font-weight: 700;
  }
  .kn-karte-punkt-erledigt {
    fill: var(--t-bg);
    stroke: var(--t-grau);
    stroke-width: 2.5;
  }
  .kn-karte-punkt-jetzt {
    fill: var(--t-bg);
    stroke: var(--t-blau);
    stroke-width: 4;
  }
  .kn-karte-punkt-jetzt-kern {
    fill: var(--t-blau);
  }
  .kn-karte-stiel {
    stroke: var(--t-blau);
    stroke-width: 2;
  }
  .kn-karte-punkt-naechst {
    fill: var(--t-bg);
    stroke: var(--t-text);
    stroke-width: 2.5;
  }
  .kn-karte-gesperrt .kn-karte-punkt-naechst {
    stroke: var(--t-rot);
  }
  .kn-karte-bereit .kn-karte-punkt-naechst {
    fill: var(--t-gruen);
    stroke: var(--t-gruen);
  }
  .kn-karte-rang {
    font-size: 10.5px;
    font-weight: 800;
    fill: var(--t-text);
  }
  .kn-karte-gesperrt .kn-karte-rang {
    fill: var(--t-rot);
  }
  .kn-karte-bereit .kn-karte-rang {
    fill: #fff;
  }
  .kn-karte-station:hover circle,
  .kn-karte-station:focus circle {
    filter: brightness(0.92);
  }
  .kn-karte-jetzt-rahmen {
    fill: var(--t-karte, var(--t-bg));
    stroke: var(--t-blau);
    stroke-width: 1.4;
  }
  .kn-karte-jetzt-kante {
    fill: var(--t-blau);
  }
  .kn-karte-jetzt-nr {
    font-size: 12px;
    font-weight: 800;
    fill: var(--t-blau);
  }
  .kn-karte-jetzt-titel {
    font-size: 11px;
    font-weight: 700;
    fill: var(--t-text);
  }
  .kn-karte-jetzt-branch {
    font-size: 9px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    fill: var(--t-dim);
  }
  .kn-karte-stufe {
    fill: var(--t-linie);
  }
  .kn-karte-stufe.kn-karte-stufe-vor {
    fill: color-mix(in srgb, var(--t-blau) 40%, transparent);
  }
  .kn-karte-stufe.kn-karte-stufe-jetzt {
    fill: var(--t-blau);
  }
  .kn-karte-stufe-text {
    font-size: 8px;
    font-weight: 700;
    fill: var(--t-dim);
  }
  .kn-karte-stufe-text.kn-karte-stufe-vor,
  .kn-karte-stufe-text.kn-karte-stufe-jetzt {
    fill: #fff;
  }
  .kn-karte-jetzt-fuss {
    font-size: 10px;
    fill: var(--t-text2, var(--t-text));
  }
  .kn-karte-jetzt-balken {
    fill: var(--t-blau);
  }
  .kn-karte-jetzt-pr {
    font-weight: 700;
  }
  .kn-karte-checks-rot {
    fill: var(--t-rot);
    font-weight: 700;
  }
  .kn-karte-checks-gruen {
    fill: var(--t-gruen);
    font-weight: 700;
  }
  .kn-karte-checks-laeuft {
    fill: var(--t-gelb);
  }
  .kn-karte-achse {
    stroke: var(--t-text);
    stroke-width: 1.5;
  }
  .kn-karte-achse-titel {
    font-size: 11px;
    font-weight: 700;
    fill: var(--t-text);
  }
  .kn-karte-achse-text,
  .kn-karte-achse-datum {
    font-size: 10px;
    fill: var(--t-dim);
  }
  .kn-karte-achse-rang {
    fill: var(--t-text);
  }
  .kn-karte-achse-rang-text {
    font-size: 9.5px;
    font-weight: 700;
    fill: var(--t-bg);
  }
  .kn-karte-achse-jetzt {
    font-size: 10.5px;
    font-weight: 700;
    fill: var(--t-blau);
  }
</style>

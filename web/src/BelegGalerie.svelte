<script>
  // Die Beleg-Galerie eines Repos (beleg-galerie, #54): alle Belege aller
  // Changes, offen wie archiviert, in der Folge, in der Go sie liefert
  // (juengster zuerst, internal/appctx/beleggalerie.go). Ein zeigbarer Beleg
  // erscheint als Bild; alles andere (.dot, Text, …) bekommt eine Kachel mit
  // Typ und Namen. Hier wird nichts sortiert und nichts gefiltert.
  //
  // Darunter "Geaenderte Bilder" (beleg-bilder, #105): die Bilddateien der
  // juengsten Commits, fertig ausgewaehlt und dedupliziert in Go
  // (internal/appctx/geaendertebilder.go). Ein Klick auf ein Bild oeffnet
  // die Bildansicht im selben Fenster (BildModal.svelte).
  //
  // Darunter "HTML-Dateien" (html-dateien, #112): die neuesten HTML-Dateien
  // aus dem Baum der Spitze (html-dateien-baum, #114) als Liste,
  // Erstellt-Datum und Bildzaehler fertig aus Go
  // (internal/appctx/htmldateien.go). Ein Klick oeffnet die Datei gerendert
  // in einem neuen Tab ueber /api/datei/…. Der Abschnitt steht da, sobald die
  // Antwort vorliegt, auch leer: dann mit "Keine HTML-Dateien im Repo".
  import { holen, mitQuery } from './api.js';
  import BildModal from './BildModal.svelte';

  let { repo } = $props();

  const ERST = 12;
  let galerie = $state(null);
  let fehler = $state('');
  let alle = $state(false);
  let geaendert = $state(null);
  let gFehler = $state('');
  let gAlle = $state(false);
  let html = $state(null);
  let hFehler = $state('');
  // Die offene Bildansicht: welche Liste, welches Bild.
  let ansicht = $state(null);

  $effect(() => {
    const r = repo;
    galerie = null;
    fehler = '';
    alle = false;
    geaendert = null;
    gFehler = '';
    gAlle = false;
    html = null;
    hFehler = '';
    ansicht = null;
    if (!r) return;
    holen(mitQuery('/api/beleg-galerie', { repo: r }))
      .then((g) => {
        if (repo === r) galerie = g;
      })
      .catch((e) => {
        if (repo === r) fehler = e.message;
      });
    holen(mitQuery('/api/geaenderte-bilder', { repo: r }))
      .then((g) => {
        if (repo === r) geaendert = g;
      })
      .catch((e) => {
        if (repo === r) gFehler = e.message;
      });
    holen(mitQuery('/api/html-dateien', { repo: r }))
      .then((h) => {
        if (repo === r) html = h;
      })
      .catch((e) => {
        if (repo === r) hFehler = e.message;
      });
  });

  let belege = $derived(galerie?.belege || []);
  let sichtbar = $derived(alle ? belege : belege.slice(0, ERST));
  const zeit = (iso) => String(iso || '').slice(0, 16).replace('T', ' ');
  // Die Bildliste der Galerie fuer die Bildansicht: die zeigbaren Belege in
  // der Folge der Route.
  let belegBilder = $derived(
    belege.filter((b) => b.zeigbar).map((b) => ({ bild: b.bild, titel: b.kennung + ' · ' + b.name, stufe: b.stufe, zeit: b.zeit, link: b.link }))
  );
  let bilder = $derived(geaendert?.bilder || []);
  let gSichtbar = $derived(gAlle ? bilder : bilder.slice(0, ERST));
  let geaendertBilder = $derived(bilder.map((b) => ({ bild: b.bild, titel: b.pfad, zeit: b.zeit, notiz: b.betreff, link: b.link })));
  function belegOeffnen(b) {
    ansicht = { liste: 'belege', start: belegBilder.findIndex((x) => x.bild === b.bild) };
  }
  let dateien = $derived(html?.dateien || []);
  const fehltTitel = (z) => (z.fehlt ? 'fehlt: ' + (z.fehlende || []).join(', ') : z.grund || z.text);
  let ansichtBilder = $derived(ansicht?.liste === 'geaendert' ? geaendertBilder : belegBilder);
</script>

{#if fehler || galerie?.grund || belege.length}
  <section class="kn-galerie">
    <h2>Belegt</h2>
    {#if fehler || galerie?.grund}
      <p class="kn-g-hinweis" title={fehler || galerie.grund}>Belege nicht verfügbar · {fehler || galerie.grund}</p>
    {/if}
    <div class="kn-g-raster">
      {#each sichtbar as b (b.kennung + '/' + b.pfad + '/' + b.zeit)}
        <figure class="kn-g-kachel" data-beleg={b.kennung + '/' + b.name} class:kn-g-fehlt={b.fehlt}>
          {#if b.zeigbar}
            <button type="button" class="kn-g-blatt kn-g-knopf" data-bild-oeffnen onclick={() => belegOeffnen(b)}
                    title={b.stufe + ' · ' + b.art + ' · ' + b.name}>
              <img src={b.bild} alt={b.kennung + ' — ' + b.stufe} loading="lazy" />
            </button>
          {:else}
            <svelte:element this={b.link ? 'a' : 'div'} class="kn-g-blatt kn-g-ohne" href={b.link || undefined}
                            target={b.link ? '_blank' : undefined} rel={b.link ? 'noopener' : undefined}
                            title={b.fehlt ? 'fehlt' + (b.grund ? ': ' + b.grund : '') : b.name}>
              <span class="kn-g-typ">{b.typ}</span>
              <span class="kn-g-name">{b.name}</span>
              {#if b.fehlt}<span class="kn-g-fehltext">fehlt</span>{/if}
            </svelte:element>
          {/if}
          <figcaption>
            <b>{b.kennung}</b>
            <span class="kn-g-stufe" class:kn-g-vorher={b.stufe === 'vorher'}>{b.stufe}</span>
            <span class="kn-g-meta">· {b.art} · {zeit(b.zeit)}</span>
            {#if b.archiviert}<span class="kn-g-archiv">· archiviert</span>{/if}
          </figcaption>
        </figure>
      {/each}
    </div>
    {#if belege.length > ERST}
      <button type="button" class="kn-g-mehr" onclick={() => (alle = !alle)}>
        {alle ? 'Weniger zeigen' : 'Alle ' + belege.length + ' zeigen'}
      </button>
    {/if}
  </section>
{/if}

{#if gFehler || geaendert?.grund || bilder.length}
  <section class="kn-geaendert">
    <h2>Geänderte Bilder</h2>
    {#if gFehler || geaendert?.grund}
      <p class="kn-g-hinweis" title={gFehler || geaendert.grund}>Geänderte Bilder nicht verfügbar · {gFehler || geaendert.grund}</p>
    {/if}
    <div class="kn-g-raster">
      {#each gSichtbar as b, n (b.pfad)}
        <figure class="kn-g-kachel" data-geaendert={b.pfad}>
          <button type="button" class="kn-g-blatt kn-g-knopf" data-bild-oeffnen onclick={() => (ansicht = { liste: 'geaendert', start: n })}
                  title={b.pfad + ' · ' + b.betreff}>
            <img src={b.bild} alt={b.name} loading="lazy" />
          </button>
          <figcaption>
            <b class="kn-gb-name">{b.name}</b>
            <span class="kn-gb-pfad" title={b.pfad}>{b.pfad}</span>
            <span class="kn-gb-zeile"><span class="kn-gb-datum">{zeit(b.zeit)}</span> · <span class="kn-gb-betreff" title={b.betreff}>{b.betreff}</span></span>
          </figcaption>
        </figure>
      {/each}
    </div>
    {#if bilder.length > ERST}
      <button type="button" class="kn-g-mehr kn-gb-mehr" onclick={() => (gAlle = !gAlle)}>
        {gAlle ? 'Weniger zeigen' : 'Alle ' + bilder.length + ' zeigen'}
      </button>
    {/if}
  </section>
{/if}

{#if hFehler || html}
  <section class="kn-html">
    <h2>HTML-Dateien</h2>
    {#if hFehler || (html?.grund && !dateien.length)}
      <p class="kn-g-hinweis" title={hFehler || html.grund}>HTML-Dateien nicht verfügbar · {hFehler || html.grund}</p>
    {:else if html?.grund}
      <p class="kn-g-hinweis" title={html.grund}>{html.grund}</p>
    {:else if !dateien.length}
      <p class="kn-g-hinweis">Keine HTML-Dateien im Repo</p>
    {/if}
    {#if dateien.length}
    <ul class="kn-html-liste">
      {#each dateien as d (d.pfad)}
        <li>
          <a class="kn-html-zeile" data-html={d.pfad} href={d.adresse} target="_blank" rel="noopener" title={'gerendert öffnen · ' + d.pfad}>
            <span class="kn-html-kopf">
              <b class="kn-html-name">{d.name}</b>
              <span class="kn-html-pfad">{d.pfad}</span>
            </span>
            <span class="kn-html-meta">
              <!-- Die Zeittexte kommen in der Anzeigezone aus Go (letzte-aktionen, #126). -->
              <span class="kn-html-erstellt" title={d.erstellt_voll || undefined}>{d.erstellt ? 'erstellt ' + (d.erstellt_text || zeit(d.erstellt)) : 'erstellt unbekannt'}</span>
              <span class="kn-html-geaendert" title={d.geaendert_voll || undefined}>geändert {d.geaendert_text || zeit(d.zeit)}</span>
              <span class="kn-html-betreff" title={d.betreff}>{d.betreff}</span>
            </span>
            <span class="kn-html-bilder" class:kn-html-fehlt={d.bilder?.fehlt > 0} class:kn-html-ohne={!d.bilder?.gesamt} title={fehltTitel(d.bilder || {})}>{d.bilder?.text || ''}</span>
          </a>
        </li>
      {/each}
    </ul>
    {/if}
  </section>
{/if}

{#if ansicht && ansichtBilder.length}
  <BildModal bilder={ansichtBilder} start={ansicht.start} schliessen={() => (ansicht = null)} />
{/if}

<style>
  .kn-galerie,
  .kn-geaendert,
  .kn-html {
    margin-top: 28px;
  }
  h2 {
    margin: 0 0 10px;
    padding: 0 8px 6px;
    font-size: 15px;
    font-weight: 600;
    color: var(--t-blau);
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-g-raster {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 14px;
    padding: 0 8px;
  }
  .kn-g-kachel {
    margin: 0;
    min-width: 0;
  }
  .kn-g-blatt {
    display: block;
    box-sizing: border-box;
    height: 110px;
    border-radius: 8px;
    border: 1px solid var(--t-linie);
    background: var(--t-flaeche);
    overflow: hidden;
    text-decoration: none;
  }
  .kn-g-knopf {
    width: 100%;
    padding: 0;
    cursor: zoom-in;
  }
  .kn-g-blatt img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    object-position: top left;
    display: block;
  }
  .kn-g-ohne {
    display: flex;
    flex-direction: column;
    justify-content: center;
    align-items: center;
    gap: 6px;
    padding: 8px;
    background: var(--t-hover);
    color: var(--t-text2);
  }
  .kn-g-typ {
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--t-lila);
    border: 1px solid currentColor;
    border-radius: 4px;
    padding: 1px 6px;
  }
  .kn-g-name {
    font-size: 12px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-g-fehlt .kn-g-blatt {
    border-style: dashed;
  }
  .kn-g-fehltext {
    font-size: 11px;
    color: var(--t-rot);
  }
  figcaption {
    margin-top: 5px;
    font-size: 11px;
    line-height: 1.4;
    color: var(--t-dim);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kn-geaendert figcaption {
    display: flex;
    flex-direction: column;
  }
  .kn-gb-pfad,
  .kn-gb-zeile {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  figcaption b {
    color: var(--t-text);
    font-weight: 600;
  }
  .kn-g-stufe {
    color: var(--t-gruen);
  }
  .kn-g-vorher {
    color: var(--t-dim);
  }
  .kn-g-mehr {
    margin: 10px 8px 0;
    border: 0;
    background: transparent;
    color: var(--t-blau);
    font-size: 13px;
    cursor: pointer;
    padding: 2px 0;
  }
  .kn-html-liste {
    list-style: none;
    margin: 0;
    padding: 0 8px;
  }
  .kn-html-zeile {
    display: grid;
    grid-template-columns: minmax(0, 1.3fr) minmax(0, 2fr) minmax(200px, auto);
    gap: 14px;
    align-items: center;
    padding: 7px 8px;
    border-bottom: 1px solid var(--t-linie);
    color: var(--t-text2);
    text-decoration: none;
    font-size: 12px;
  }
  .kn-html-zeile:hover {
    background: var(--t-hover);
  }
  .kn-html-kopf,
  .kn-html-meta {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .kn-html-kopf > span,
  .kn-html-meta > span,
  .kn-html-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-html-name {
    color: var(--t-text);
    font-weight: 600;
    font-size: 13px;
  }
  .kn-html-pfad,
  .kn-html-erstellt,
  .kn-html-geaendert {
    color: var(--t-dim);
  }
  .kn-html-bilder {
    white-space: nowrap;
    text-align: right;
    color: var(--t-gruen);
  }
  .kn-html-bilder.kn-html-ohne {
    color: var(--t-dim);
  }
  .kn-html-bilder.kn-html-fehlt {
    color: var(--t-rot);
    font-weight: 600;
  }
  .kn-g-hinweis {
    margin: 6px 0;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
    color: var(--t-dim);
    background: var(--t-hover);
  }
</style>

<script>
  // Issues nach Parent unter dem Linienplan (karte-gruppen, #121, Entwurf H):
  // je Gruppe der Kopf und die Abschnitte offene Fragen, aktiv, ausstehend
  // und gemergt. Gruppen, ihre Folge, die Zuordnung der Fragen, der Baum der
  // ausstehenden Zeilen und alle Texte kommen fertig aus Go
  // (internal/appctx/kartegruppen.go); hier wird nur gezeichnet.
  // Ein Klick auf eine Zeile markiert sie und ihre Station, das Symbol und
  // der Doppelklick oeffnen die Detailspalte.
  let { gruppen, gewaehlt, waehlen, oeffnen, farbe } = $props();

  let aufgeklappt = $state([]);

  const STUFEN = ['gate1', 'gate2', 'im_pr'];
  const STATUS = { frage: 'Frage offen', aktiv: 'aktiv', gesperrt: 'gesperrt', bereit: 'bereit', wartet: 'wartet', offen: 'offen', gemergt: 'gemergt' };

  function statusText(z) {
    if (z.status === 'aktiv' && z.stufe) return 'aktiv · ' + z.stufe;
    return STATUS[z.status] || z.status;
  }

  function stufeKlasse(z, st) {
    const i = STUFEN.indexOf(z.stufe);
    const j = STUFEN.indexOf(st);
    if (j < i) return 'kn-g-stufe-vor';
    if (j === i) return 'kn-g-stufe-jetzt';
    return '';
  }

  function sichtbar(g) {
    if (aufgeklappt.includes(g.nummer)) return g.gemergt;
    return g.gemergt.slice(0, g.gemergt_anfangs);
  }

  function taste(ev, z) {
    if (ev.key === 'Enter') {
      ev.preventDefault();
      oeffnen(z);
    } else if (ev.key === ' ') {
      ev.preventDefault();
      waehlen(z.nummer);
    }
  }

  const tage = (n) => (n === 1 ? 'Tag' : 'Tagen');
</script>

{#snippet rechts(z, geaendert, geaendertVoll)}
  <span class="kn-g-schaetzung">{#if z.schaetzung}<span class="kn-g-chip kn-g-chip-schaetzung">~{z.schaetzung} T</span>{/if}</span>
  <span class="kn-g-status kn-g-status-{z.status}">{statusText(z)}</span>
  <span class="kn-g-geaendert" title={geaendertVoll || undefined}>{geaendert || '–'}</span>
  <span class="kn-g-link">
    <button type="button" class="kn-g-detail" data-detail title="Details" aria-label="Details zu #{z.nummer}"
            onclick={(ev) => { ev.stopPropagation(); oeffnen(z); }}>
      <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><rect x="1.5" y="1.5" width="9" height="9" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M4 4.5h4M4 6.5h4M4 8.5h2.5" stroke="currentColor" stroke-width="1.1" /></svg>
    </button>
    {#if z.url}<a href={z.url} target="_blank" rel="noopener" onclick={(ev) => ev.stopPropagation()}>↗ #{z.nummer}</a>{/if}
  </span>
{/snippet}

<section class="kn-gruppen">
  <h3 class="kn-gruppen-titel">Issues nach Parent <span>Gruppe = Parent-Issue (Map), „ohne Parent“ zuletzt. Je Gruppe: offene Fragen · aktiv · ausstehend · gemergt.</span></h3>
  <div class="kn-g-spalten" aria-hidden="true">
    <span>Issue</span><span>Details</span><span>Schätzung</span><span>Status</span><span>Geändert</span><span>Link</span>
  </div>
  {#each gruppen as g (g.nummer)}
    <section class="kn-gruppe" data-gruppe={g.nummer} style="--g-farbe: {farbe(g.nummer)}">
      <header class="kn-gruppe-kopf">
        <span class="kn-g-kreis">{g.nummer > 0 ? g.nummer : '–'}</span>
        {#if g.nummer > 0}
          <span class="kn-g-name">#{g.nummer} {g.titel}</span>
        {:else}
          <span class="kn-g-name kn-g-name-ohne">{g.titel}</span>
        {/if}
        <span class="kn-g-zahlen">· {g.erledigt}/{g.gesamt} erledigt · {g.anzahl_aktiv} aktiv · {g.anzahl_ausstehend} ausstehend</span>
        <span class="kn-g-balken"><span style="width: {g.gesamt ? (100 * g.erledigt) / g.gesamt : 0}%"></span></span>
        {#if g.p1_frage}<span class="kn-g-p1" data-p1>trägt P1-Frage #{g.p1_frage}</span>{/if}
      </header>

      {#if g.fragen.length}
        <div class="kn-g-abschnitt kn-g-fragen" data-abschnitt="fragen">
          <h4>Offene Fragen dieser Gruppe <span class="kn-g-anzahl">{g.fragen.length}</span></h4>
          {#each g.fragen as f (f.nummer)}
            <div class="kn-g-frage kn-g-prio-{f.prio}" data-frage-zeile={f.nummer} role="button" tabindex="0"
                 ondblclick={() => oeffnen(f)} onkeydown={(ev) => ev.key === 'Enter' && oeffnen(f)}>
              <span class="kn-g-issue">
                <span class="kn-g-frage-zeichen">?</span><span class="kn-g-prio">P{f.prio}</span>
                <span class="kn-zeile-titel"><b class="kn-g-nr">#{f.nummer}</b> <b>{f.titel}</b></span>
                <span class="kn-g-wortlaut">„{f.wortlaut || f.titel}“</span>
              </span>
              <span class="kn-g-details">
                <span>{f.wer || 'offen'} · seit {f.seit_tagen} {tage(f.seit_tagen)}</span>
                <span class="kn-g-chip kn-g-halt">hält {f.hebel} zurück</span>
                <span class="kn-g-sperrt">sperrt:
                  {#each f.chips as c (c.nummer)}<span class="kn-g-chip kn-g-chip-{c.art}" data-chip={c.nummer} data-art={c.art}>#{c.nummer}</span>{:else}nichts{/each}
                </span>
              </span>
              {@render rechts({ ...f, status: 'frage' }, 'offen seit ' + f.seit_tagen + ' T', '')}
            </div>
          {/each}
        </div>
      {/if}

      {#if g.aktiv.length}
        <div class="kn-g-abschnitt kn-g-aktiv" data-abschnitt="aktiv">
          <h4>Aktiv <span class="kn-g-anzahl">{g.aktiv.length}</span></h4>
          {#each g.aktiv as z (z.nummer)}
            <div class="kn-zeile" class:kn-markiert={gewaehlt === z.nummer} data-zeile={z.nummer} data-tiefe="0" role="button" tabindex="0"
                 onclick={() => waehlen(z.nummer)} ondblclick={() => oeffnen(z)} onkeydown={(ev) => taste(ev, z)}>
              <span class="kn-g-issue"><span class="kn-g-punkt-aktiv"></span><span class="kn-zeile-titel"><b class="kn-g-nr">#{z.nummer}</b> <b>{z.titel}</b></span></span>
              <span class="kn-g-details">
                <span class="kn-g-branch">{z.branch || 'kein Branch'}</span>
                {#each STUFEN as st (st)}<span class="kn-g-stufe {stufeKlasse(z, st)}">{st}</span>{/each}
                <span>{#if z.aufgaben_gesamt}Aufgaben {z.aufgaben_erledigt ?? 0}/{z.aufgaben_gesamt}{:else}ohne Aufgaben{/if}</span>
                <span>{#if z.pr}<b>PR #{z.pr}</b>{#if z.checks} <span class="kn-g-checks kn-g-checks-{z.checks}">● Checks {z.checks}</span>{/if}{:else}noch kein PR{/if}</span>
              </span>
              {@render rechts(z, z.geaendert ? 'geändert ' + z.geaendert : '', z.geaendert_voll)}
            </div>
          {/each}
        </div>
      {/if}

      {#if g.ausstehend.length}
        <div class="kn-g-abschnitt kn-g-ausstehend" data-abschnitt="ausstehend">
          <h4>Ausstehend <span class="kn-g-anzahl">{g.ausstehend.length}</span></h4>
          {#each g.ausstehend as z (z.nummer)}
            <div class="kn-zeile" class:kn-markiert={gewaehlt === z.nummer} class:kn-g-gesperrt={z.gesperrt} data-zeile={z.nummer} data-tiefe={z.tiefe}
                 role="button" tabindex="0" onclick={() => waehlen(z.nummer)} ondblclick={() => oeffnen(z)} onkeydown={(ev) => taste(ev, z)}>
              <span class="kn-g-issue">
                <span class="kn-zeile-titel" style="margin-left: {z.tiefe * 22}px">
                  {#if z.tiefe}<span class="kn-g-ast" aria-hidden="true">└</span>{/if}
                  <span class="kn-g-rang" class:kn-g-rang-bereit={z.bereit} class:kn-g-rang-gesperrt={z.gesperrt}>{z.rang}</span>
                  <b class="kn-g-nr">#{z.nummer}</b> {z.titel}
                </span>
              </span>
              <span class="kn-g-details">
                {#if z.wartet_auf.length}
                  <span class="kn-g-klein">wartet auf</span>
                  {#each z.wartet_auf as w (w.nummer)}<span class="kn-g-chip kn-g-wartet-{w.art}" data-wartet={w.nummer} data-art={w.art}>{w.art === 'frage' ? '? #' + w.nummer + ' Frage' : w.art === 'aktiv' ? '#' + w.nummer + ' aktiv' : '#' + w.nummer}</span>{/each}
                  {#if z.haengt_an}<span class="kn-g-haengt">→ hängt an Frage #{z.haengt_an}</span>{/if}
                {:else if z.bereit}
                  <span class="kn-g-chip kn-g-bereit">bereit – frei startbar</span> <span class="kn-g-klein">wartet auf nichts</span>
                {:else}
                  <span class="kn-g-klein">wartet auf nichts</span>
                {/if}
              </span>
              {@render rechts(z, z.geaendert ? 'geändert ' + z.geaendert : '', z.geaendert_voll)}
            </div>
          {/each}
        </div>
      {/if}

      {#if g.gemergt.length}
        <div class="kn-g-abschnitt kn-g-gemergt" data-abschnitt="gemergt">
          <h4>Gemergt <span class="kn-g-anzahl">{g.gemergt.length}</span> <span class="kn-g-klein">neueste zuerst</span></h4>
          {#each sichtbar(g) as z (z.nummer)}
            <div class="kn-zeile kn-zeile-kompakt" class:kn-markiert={gewaehlt === z.nummer} data-zeile={z.nummer} data-tiefe="0" role="button" tabindex="0"
                 onclick={() => waehlen(z.nummer)} ondblclick={() => oeffnen(z)} onkeydown={(ev) => taste(ev, z)}>
              <span class="kn-g-issue"><span class="kn-g-haken">✓</span><span class="kn-zeile-titel"><span class="kn-g-nr">#{z.nummer}</span> {z.titel}</span></span>
              <span class="kn-g-details">
                <span class="kn-g-datum">{z.erledigt_am}</span>
                <span class="kn-g-branch">{z.change || '–'}</span>
                {#if z.gemergt_mit}<span>gemergt mit <a href={z.gemergt_mit.url} target="_blank" rel="noopener" title={z.gemergt_mit.titel} onclick={(ev) => ev.stopPropagation()}>#{z.gemergt_mit.nummer}</a></span>{/if}
              </span>
              {@render rechts(z, z.geaendert ? 'geändert ' + z.geaendert : '', z.geaendert_voll)}
            </div>
          {/each}
          {#if !aufgeklappt.includes(g.nummer) && g.gemergt.length > g.gemergt_anfangs}
            <button type="button" class="kn-g-mehr" data-mehr onclick={() => (aufgeklappt = [...aufgeklappt, g.nummer])}>+ {g.gemergt.length - g.gemergt_anfangs} weitere</button>
          {/if}
        </div>
      {/if}
    </section>
  {/each}
</section>

<style>
  .kn-gruppen {
    margin-top: 18px;
    font-size: 12.5px;
  }
  .kn-gruppen-titel {
    margin: 0 0 6px;
    font-size: 15px;
    color: var(--t-text);
  }
  .kn-gruppen-titel span {
    font-size: 11px;
    font-weight: 400;
    color: var(--t-dim);
    margin-left: 8px;
  }
  .kn-g-spalten,
  .kn-zeile,
  .kn-g-frage {
    display: grid;
    grid-template-columns: minmax(260px, 2.2fr) minmax(260px, 3fr) 80px 120px 130px 110px;
    gap: 10px;
    align-items: center;
  }
  .kn-g-spalten {
    padding: 0 14px 4px;
    font-size: 9.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--t-dim);
  }
  .kn-gruppe {
    border: 1px solid var(--t-linie);
    border-left: 5px solid var(--g-farbe);
    border-radius: 6px;
    margin-bottom: 12px;
    padding: 8px 10px 10px;
    background: var(--t-karte, var(--t-bg));
  }
  .kn-gruppe-kopf {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 2px 0 6px;
  }
  .kn-g-kreis {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    border-radius: 10px;
    background: var(--g-farbe);
    color: #fff;
    font-size: 10px;
    font-weight: 700;
  }
  .kn-g-name {
    font-size: 14px;
    font-weight: 700;
    color: var(--g-farbe);
  }
  .kn-g-name-ohne {
    color: var(--t-text);
  }
  .kn-g-zahlen {
    color: var(--t-text2, var(--t-text));
  }
  .kn-g-balken {
    display: inline-block;
    width: 90px;
    height: 5px;
    border-radius: 3px;
    background: var(--t-linie);
    overflow: hidden;
  }
  .kn-g-balken span {
    display: block;
    height: 100%;
    background: var(--g-farbe);
  }
  .kn-g-p1 {
    background: var(--t-rot);
    color: #fff;
    border-radius: 4px;
    padding: 1px 7px;
    font-size: 10.5px;
    font-weight: 700;
  }
  .kn-g-abschnitt {
    margin-top: 6px;
    border-radius: 5px;
    padding: 4px 4px 6px;
  }
  .kn-g-fragen {
    background: color-mix(in srgb, var(--t-rot) 5%, transparent);
  }
  .kn-g-aktiv {
    background: color-mix(in srgb, var(--t-blau) 6%, transparent);
  }
  .kn-g-gemergt {
    background: color-mix(in srgb, var(--t-grau) 8%, transparent);
  }
  .kn-g-abschnitt h4 {
    margin: 2px 4px 4px;
    font-size: 10px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--t-text2, var(--t-text));
  }
  .kn-g-fragen h4 {
    color: var(--t-rot);
  }
  .kn-g-aktiv h4 {
    color: var(--t-blau);
  }
  .kn-g-anzahl {
    display: inline-block;
    min-width: 16px;
    padding: 0 4px;
    border-radius: 8px;
    background: var(--t-text2, var(--t-text));
    color: var(--t-bg);
    text-align: center;
    font-size: 9.5px;
  }
  .kn-g-fragen .kn-g-anzahl {
    background: var(--t-rot);
  }
  .kn-g-aktiv .kn-g-anzahl {
    background: var(--t-blau);
  }
  .kn-zeile,
  .kn-g-frage {
    padding: 4px 10px;
    border-radius: 4px;
    border: 1.5px solid transparent;
    cursor: pointer;
    outline: none;
  }
  .kn-g-frage {
    background: var(--t-bg);
    border-color: color-mix(in srgb, var(--t-rot) 35%, transparent);
    margin-bottom: 4px;
    cursor: default;
  }
  .kn-g-prio-2 {
    border-color: color-mix(in srgb, #e07b00 45%, transparent);
  }
  .kn-zeile:hover,
  .kn-zeile:focus {
    background: var(--t-hover);
  }
  .kn-zeile.kn-g-gesperrt {
    background: repeating-linear-gradient(45deg, color-mix(in srgb, var(--t-rot) 7%, transparent) 0 6px, transparent 6px 12px);
  }
  .kn-zeile.kn-markiert {
    border-color: #c2366f;
    background: color-mix(in srgb, #c2366f 10%, transparent);
    scroll-margin: 120px;
  }
  .kn-zeile-kompakt {
    padding-top: 2px;
    padding-bottom: 2px;
    font-size: 11.5px;
    color: var(--t-text2, var(--t-text));
  }
  .kn-g-issue {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .kn-zeile-titel {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .kn-g-nr {
    color: var(--g-farbe);
  }
  .kn-g-fragen .kn-g-nr {
    color: var(--t-rot);
  }
  .kn-g-wortlaut {
    flex-basis: 100%;
    font-style: italic;
    color: var(--t-text2, var(--t-text));
    padding-left: 42px;
  }
  .kn-g-frage-zeichen,
  .kn-g-prio {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--t-rot);
    color: #fff;
    font-weight: 800;
    font-size: 10px;
  }
  .kn-g-frage-zeichen {
    width: 16px;
    height: 16px;
    border-radius: 8px;
  }
  .kn-g-prio {
    padding: 0 4px;
    border-radius: 3px;
  }
  .kn-g-prio-2 .kn-g-frage-zeichen,
  .kn-g-prio-2 .kn-g-prio {
    background: #e07b00;
  }
  .kn-g-details {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    color: var(--t-text2, var(--t-text));
    min-width: 0;
  }
  .kn-g-klein {
    font-size: 10.5px;
    color: var(--t-dim);
  }
  .kn-g-chip {
    display: inline-block;
    padding: 0 5px;
    border-radius: 3px;
    font-size: 10.5px;
    border: 1px solid var(--t-rahmen);
    color: var(--t-text2, var(--t-text));
    background: var(--t-flaeche, transparent);
  }
  .kn-g-chip-direkt {
    background: color-mix(in srgb, var(--t-rot) 15%, transparent);
    border-color: var(--t-rot);
    color: var(--t-rot);
    font-weight: 700;
  }
  .kn-g-chip-transitiv {
    background: transparent;
    border: 1px dashed var(--t-rot);
    color: var(--t-rot);
  }
  .kn-g-halt {
    background: var(--t-rot);
    border-color: var(--t-rot);
    color: #fff;
    font-weight: 700;
  }
  .kn-g-prio-2 .kn-g-halt {
    background: transparent;
    color: #e07b00;
    border-color: #e07b00;
  }
  .kn-g-sperrt {
    display: inline-flex;
    gap: 4px;
    align-items: center;
    font-size: 10.5px;
    color: var(--t-dim);
  }
  .kn-g-wartet-frage {
    border-color: var(--t-rot);
    color: var(--t-rot);
    background: color-mix(in srgb, var(--t-rot) 8%, transparent);
  }
  .kn-g-wartet-aktiv {
    border-color: var(--t-blau);
    color: var(--t-blau);
    background: color-mix(in srgb, var(--t-blau) 8%, transparent);
  }
  .kn-g-wartet-issue {
    color: var(--t-dim);
  }
  .kn-g-haengt {
    color: var(--t-rot);
    font-size: 10.5px;
    font-weight: 600;
  }
  .kn-g-bereit {
    background: var(--t-gruen);
    border-color: var(--t-gruen);
    color: #fff;
    font-weight: 700;
  }
  .kn-g-chip-schaetzung {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .kn-g-rang {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 9px;
    border: 2px solid var(--t-text);
    font-size: 9.5px;
    font-weight: 800;
    box-sizing: border-box;
  }
  .kn-g-rang-gesperrt {
    border-color: var(--t-rot);
    color: var(--t-rot);
  }
  .kn-g-rang-bereit {
    border-color: var(--t-gruen);
    background: var(--t-gruen);
    color: #fff;
  }
  .kn-g-ast {
    color: var(--t-dim);
  }
  .kn-g-punkt-aktiv {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    border: 3px solid var(--t-blau);
  }
  .kn-g-haken {
    color: var(--t-dim);
  }
  .kn-g-branch {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 10.5px;
    color: var(--t-dim);
  }
  .kn-g-datum {
    font-weight: 600;
  }
  .kn-g-stufe {
    font-size: 9.5px;
    font-weight: 700;
    padding: 0 5px;
    border-radius: 3px;
    background: var(--t-linie);
    color: var(--t-dim);
  }
  .kn-g-stufe-vor {
    background: color-mix(in srgb, var(--t-blau) 40%, transparent);
    color: #fff;
  }
  .kn-g-stufe-jetzt {
    background: var(--t-blau);
    color: #fff;
  }
  .kn-g-checks-rot {
    color: var(--t-rot);
    font-weight: 700;
  }
  .kn-g-checks-gruen {
    color: var(--t-gruen);
    font-weight: 700;
  }
  .kn-g-status {
    justify-self: start;
    padding: 1px 8px;
    border-radius: 9px;
    font-size: 10.5px;
    font-weight: 700;
    background: var(--t-linie);
    color: var(--t-text2, var(--t-text));
  }
  .kn-g-status-frage {
    background: var(--t-rot);
    color: #fff;
  }
  .kn-g-status-aktiv {
    background: var(--t-blau);
    color: #fff;
  }
  .kn-g-status-gesperrt {
    background: transparent;
    border: 1px solid var(--t-rot);
    color: var(--t-rot);
  }
  .kn-g-status-bereit {
    background: var(--t-gruen);
    color: #fff;
  }
  .kn-g-geaendert {
    font-size: 11px;
    color: var(--t-dim);
  }
  .kn-g-link {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .kn-g-link a,
  .kn-g-details a {
    color: var(--t-blau);
    text-decoration: none;
    font-weight: 600;
  }
  .kn-g-detail {
    display: inline-flex;
    padding: 2px;
    border: 0;
    background: transparent;
    color: var(--t-dim);
    cursor: pointer;
  }
  .kn-g-detail:hover {
    color: var(--t-blau);
  }
  .kn-g-mehr {
    margin: 4px 0 0 10px;
    border: 0;
    background: transparent;
    color: var(--t-blau);
    font: inherit;
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
  }
</style>

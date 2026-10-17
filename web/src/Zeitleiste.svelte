<script>
  // Abschnitt "Wann" in der Detailspalte eines Changes (aufgaben-zeitleiste,
  // #55): wann welche Aufgabe abgehakt wurde. Ausgewertet und sortiert wird
  // in Go (/api/zeitleiste); hier wird nur gezeichnet.
  import { holen, mitQuery } from './api.js';

  let { repo, kennung } = $props();

  let daten = $state(null);
  let fehler = $state('');

  $effect(() => {
    const r = repo;
    const k = kennung;
    daten = null;
    fehler = '';
    if (!r || !k) return;
    let aktuell = true;
    holen(mitQuery('/api/zeitleiste', { repo: r, kennung: k })).then(
      (d) => {
        if (!aktuell) return;
        if (d && typeof d === 'object') daten = d;
        else fehler = 'Keine Zeitleiste erhalten.';
      },
      (e) => { if (aktuell) fehler = e.message; },
    );
    return () => { aktuell = false; };
  });

  function zeitpunkt(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleString('de-DE', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  }
</script>

<section class="kn-zl" aria-label="Wann">
  <h3>Wann</h3>
  {#if fehler || daten?.grund}
    <p class="kn-zl-fehler" title={fehler || daten.grund}>{fehler || daten.grund}</p>
  {:else if !daten}
    <p class="kn-zl-leer" aria-busy="true">…</p>
  {:else if !daten.eintraege?.length}
    <p class="kn-zl-leer">Noch nichts abgehakt.</p>
  {:else}
    <ol class="kn-zl-liste">
      {#each daten.eintraege as e (e.sha + '|' + e.aufgabe)}
        <li class="kn-zl-eintrag" data-sha={e.sha}>
          <span class="kn-zl-zeit">{zeitpunkt(e.zeit)}</span>
          <span class="kn-zl-aufgabe">{e.aufgabe}</span>
          <span class="kn-zl-commit"><code>{e.kurz_sha}</code> {e.betreff}</span>
        </li>
      {/each}
    </ol>
  {/if}
</section>

<style>
  h3 {
    margin: 22px 0 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--t-blau);
  }
  .kn-zl-liste {
    list-style: none;
    margin: 0;
    padding: 0 0 0 12px;
    border-left: 2px solid var(--t-linie);
  }
  .kn-zl-eintrag {
    position: relative;
    display: grid;
    grid-template-columns: 7.5em 1fr;
    gap: 0 10px;
    padding: 4px 0;
    font-size: 12.5px;
  }
  .kn-zl-eintrag::before {
    content: '';
    position: absolute;
    left: -17px;
    top: 9px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--t-gruen);
  }
  .kn-zl-zeit {
    color: var(--t-dim);
    font-variant-numeric: tabular-nums;
  }
  .kn-zl-aufgabe {
    color: var(--t-text);
  }
  .kn-zl-commit {
    grid-column: 2;
    color: var(--t-dim);
    font-size: 11.5px;
  }
  .kn-zl-commit code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 11.5px;
  }
  .kn-zl-leer {
    color: var(--t-dim);
    font-size: 13px;
  }
  .kn-zl-fehler {
    color: var(--t-rot);
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>

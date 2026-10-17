<script>
  // Die archivierten Changes eines Repos (archiv-liste, #56): eingeklappt
  // unter "Zuletzt erledigt", aufgeklappt je Zeile Datum, Kennung,
  // erledigt/gesamt und das Vorschaubild. Die Folge kommt aus Go.
  let { archiv = [] } = $props();
  let offen = $state(false);
</script>

<section class="kn-a-abschnitt kn-ar" data-archiv data-offen={offen ? 'ja' : 'nein'}>
  <h2>
    <button type="button" class="kn-ar-kopf" aria-expanded={offen ? 'true' : 'false'} onclick={() => (offen = !offen)}>
      <svg class="kn-ar-pfeil" class:kn-ar-pfeil-offen={offen} width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M3.5 2 6.5 5 3.5 8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
      </svg>
      <span>Abgeschlossen</span>
      <span class="kn-ar-zahl">{archiv.length}</span>
    </button>
  </h2>
  {#if offen}
    <ul class="kn-ar-liste">
      {#each archiv as a, i (a.kennung + '|' + a.datum + '|' + i)}
        <li class="kn-ar-zeile" data-archiv-zeile={a.kennung}>
          <span class="kn-ar-datum">{a.datum || '—'}</span>
          <span class="kn-ar-kennung" title={a.issue ? 'Issue #' + a.issue : ''}>{a.kennung}</span>
          <span class="kn-ar-stand">{a.erledigt} / {a.gesamt}</span>
          {#if a.vorschau?.bild}
            <a class="kn-ar-vorschau" href={a.vorschau.bild} target="_blank" rel="noopener" title={a.vorschau.stufe + ' · ' + a.vorschau.pfad}>
              <img src={a.vorschau.bild} alt={'Beleg ' + a.kennung} />
            </a>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  h2 {
    margin: 0 0 4px;
    padding: 0 8px 6px;
    font-size: 15px;
    font-weight: 600;
    color: var(--t-blau);
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-a-abschnitt {
    margin-top: 28px;
  }
  .kn-ar-kopf {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 0;
    background: transparent;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }
  .kn-ar-pfeil {
    transition: transform 0.12s;
  }
  .kn-ar-pfeil-offen {
    transform: rotate(90deg);
  }
  .kn-ar-zahl {
    font-size: 12px;
    font-weight: 500;
    color: var(--t-dim);
  }
  .kn-ar-liste {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .kn-ar-zeile {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 5px 8px;
    font-size: 14px;
    color: var(--t-text2);
    border-bottom: 1px solid color-mix(in srgb, var(--t-linie) 50%, transparent);
  }
  .kn-ar-datum {
    font-variant-numeric: tabular-nums;
    color: var(--t-dim);
    font-size: 12px;
    min-width: 76px;
  }
  .kn-ar-kennung {
    flex: 1;
    color: var(--t-text);
  }
  .kn-ar-stand {
    font-variant-numeric: tabular-nums;
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-ar-vorschau img {
    display: block;
    height: 36px;
    max-width: 64px;
    object-fit: cover;
    border-radius: 4px;
    border: 1px solid var(--t-linie);
  }
</style>

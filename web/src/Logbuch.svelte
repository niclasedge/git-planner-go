<script>
  // Das Logbuch wie in Things (heute-logbuch, #42): erledigte Issues aller
  // Repos, je Tag ein Abschnitt, neuester zuerst. Gruppen und Folge kommen
  // fertig aus Go (/api/logbuch); Mehr laden holt 14 Tage mehr.
  import AnsichtSymbol from './AnsichtSymbol.svelte';
  import { tagUeberschrift, tagIn } from './status.js';

  let { logbuch, laedt = false, fehler = '', details, detailId = '', mehr = () => {} } = $props();

  let hinweise = $derived((logbuch?.quellen || []).filter((q) => q.grund));
  let laedtMehr = $state(false);

  function ueberschrift(datum) {
    if (datum === tagIn(0)) return 'Heute';
    if (datum === tagIn(-1)) return 'Gestern';
    return tagUeberschrift(datum, 0);
  }

  function idVon(e) {
    return e.repo + '#' + e.nummer;
  }

  async function mehrLaden() {
    laedtMehr = true;
    try {
      await mehr();
    } finally {
      laedtMehr = false;
    }
  }
</script>

<header class="kn-lb-kopf" data-logbuch>
  <AnsichtSymbol art="logbuch" groesse={26} />
  <h1>Logbuch</h1>
</header>

{#if fehler}
  <p class="kn-lb-fehler" title={fehler}>{fehler}</p>
{:else if laedt && !logbuch}
  <p class="kn-lb-leer">Lade erledigte Issues aller Repos …</p>
{/if}
{#each hinweise as h (h.quelle)}
  <p class="kn-lb-leer" title={h.grund}><b>{h.quelle}</b> · {h.grund}</p>
{/each}

{#each logbuch?.tage || [] as tag (tag.datum)}
  <section class="kn-lb-tag" data-abschnitt={tag.datum}>
    <h2>{ueberschrift(tag.datum)}</h2>
    {#each tag.eintraege as e (idVon(e))}
      <div class="kn-lb-zeile" class:kn-lb-gewaehlt={detailId === 'u:' + idVon(e)} data-l-eintrag={idVon(e)}>
        <span class="kn-lb-check" role="checkbox" aria-checked="true" aria-disabled="true" tabindex="-1">
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 5.2 4.2 7.4 8.2 2.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </span>
        <button type="button" class="kn-lb-titel" onclick={() => details(e, 'u:' + idVon(e))}>
          <span class="kn-lb-text">{e.titel}</span>
          <span class="kn-u-projekt">{e.repo.split('/')[1]}</span>
        </button>
        <span class="kn-lb-nr">#{e.nummer}</span>
      </div>
    {/each}
  </section>
{:else}
  {#if logbuch}<p class="kn-lb-leer">In den letzten {logbuch.fenster} Tagen wurde nichts erledigt.</p>{/if}
{/each}

{#if logbuch && logbuch.fenster < 365}
  <button type="button" class="kn-mehr" disabled={laedtMehr} onclick={mehrLaden}>
    Mehr laden <span>· bisher {logbuch.fenster} Tage, {logbuch.anzahl} erledigt</span>
  </button>
{/if}

<style>
  .kn-lb-kopf {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 22px;
  }
  h1 {
    margin: 0;
    font-size: 26px;
    font-weight: 700;
  }
  .kn-lb-tag {
    margin: 0 0 22px;
  }
  h2 {
    margin: 0 0 4px;
    padding: 0 8px 5px;
    font-size: 14px;
    font-weight: 600;
    color: var(--t-text);
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-lb-zeile {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 34px;
    padding: 0 8px;
    border-radius: 8px;
  }
  .kn-lb-zeile:hover {
    background: var(--t-hover);
  }
  .kn-lb-gewaehlt {
    background: var(--t-auswahl);
  }
  .kn-lb-check {
    flex: none;
    width: 14px;
    height: 14px;
    border-radius: 4px;
    display: grid;
    place-items: center;
    color: #fff;
    background: var(--t-blau);
  }
  .kn-lb-titel {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 1px;
    border: 0;
    background: transparent;
    padding: 5px 0;
    color: var(--t-dim);
    font: inherit;
    font-size: 15px;
    text-align: left;
    cursor: pointer;
  }
  .kn-lb-text {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-u-projekt,
  .kn-lb-nr {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-mehr {
    margin: 4px 0 0;
    padding: 7px 8px;
    border: 0;
    background: transparent;
    color: var(--t-blau);
    font: inherit;
    font-size: 14px;
    border-radius: 8px;
    cursor: pointer;
  }
  .kn-mehr span {
    color: var(--t-dim);
    font-size: 12px;
  }
  .kn-mehr:hover {
    background: var(--t-hover);
  }
  .kn-lb-leer {
    color: var(--t-dim);
    font-size: 13px;
    padding: 0 8px;
  }
  .kn-lb-fehler {
    color: var(--t-rot);
    font-size: 13px;
  }
</style>

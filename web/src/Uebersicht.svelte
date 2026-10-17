<script>
  // Heute und Irgendwann ueber alle Repos (faelligkeit, #39) — wie die Listen
  // oben in der Seitenleiste von Things. Heute zeigt nach Ueberfaellig und
  // Heute auch die naechsten Tage, je Tag eine Ueberschrift, hoechstens das
  // Fenster aus Go; Mehr laden holt 30 Tage mehr (heute-logbuch, #42).
  // Einteilung und Folge kommen fertig aus Go (/api/uebersicht); die Seite
  // gruppiert nur nach dem, was jede Zeile schon traegt (Termin, Datum, Repo).
  import AnsichtSymbol from './AnsichtSymbol.svelte';
  import Faellig from './Faellig.svelte';
  import { tagUeberschrift } from './status.js';

  let { art, uebersicht, laedt = false, fehler = '', details, detailId = '', umhaken, mehr = () => {} } = $props();

  const TITEL = { heute: 'Heute', irgendwann: 'Irgendwann' };

  function gruppen(u, a) {
    if (!u) return [];
    const out = [];
    const nach = (liste, schluessel, titel) => {
      for (const e of liste) {
        const k = schluessel(e);
        let g = out.find((x) => x.id === k);
        if (!g) out.push((g = { id: k, titel: titel(e), eintraege: [] }));
        g.eintraege.push(e);
      }
    };
    if (a === 'heute') {
      nach(u.heute || [], (e) => (e.termin === 'ueberfaellig' ? 'ueberfaellig' : 'heute'), (e) => (e.termin === 'ueberfaellig' ? 'Überfällig' : 'Heute'));
      nach(u.demnaechst || [], (e) => e.faellig, (e) => tagUeberschrift(e.faellig, e.tage));
    } else {
      nach(u.irgendwann || [], (e) => e.repo, (e) => e.repo);
    }
    return out;
  }

  let liste = $derived(gruppen(uebersicht, art));
  let laedtMehr = $state(false);
  async function mehrLaden() {
    laedtMehr = true;
    try {
      await mehr();
    } finally {
      laedtMehr = false;
    }
  }
  let hinweise = $derived((uebersicht?.quellen || []).filter((q) => q.grund));
  let arbeitet = $state('');

  function idVon(e) {
    return e.repo + '#' + e.nummer;
  }

  async function haken(e) {
    if (arbeitet) return;
    arbeitet = idVon(e);
    try {
      await umhaken(e);
    } finally {
      arbeitet = '';
    }
  }

  const LEER = {
    heute: 'Nichts fällig. Ein Datum setzt man in den Details eines Issues.',
    irgendwann: 'Jedes offene Issue hat ein Datum.',
  };
</script>

<header class="kn-u-kopf" data-uebersicht={art}>
  <AnsichtSymbol {art} groesse={26} />
  <h1>{TITEL[art]}</h1>
</header>

{#if fehler}
  <p class="kn-u-fehler" title={fehler}>{fehler}</p>
{:else if laedt && !uebersicht}
  <p class="kn-u-leer">Lade offene Issues aller Repos …</p>
{/if}
{#each hinweise as h (h.quelle)}
  <p class="kn-u-hinweis" title={h.grund}><b>{h.quelle}</b> · {h.grund}</p>
{/each}

{#each liste as g (g.id)}
  <section class="kn-u-abschnitt" data-abschnitt={g.id} class:kn-u-rot={g.id === 'ueberfaellig'}>
    <h2>{g.titel}</h2>
    {#each g.eintraege as e (idVon(e))}
      <div class="kn-u-zeile" class:kn-u-gewaehlt={detailId === 'u:' + idVon(e)} data-u-eintrag={idVon(e)}>
        <button type="button" class="kn-e-check" class:kn-u-arbeitet={arbeitet === idVon(e)} role="checkbox" aria-checked="false"
                title="Schließen" aria-label={'#' + e.nummer + ' schließen'} onclick={() => haken(e)}></button>
        <button type="button" class="kn-u-titel" onclick={() => details(e, 'u:' + idVon(e))}>
          <span class="kn-u-text">{e.titel}</span>
          <span class="kn-u-projekt">{e.repo.split('/')[1]}</span>
        </button>
        <span class="kn-u-rechts">
          {#each e.labels || [] as l (l.name)}
            <span class="kn-u-label" style="--l: #{l.farbe || '8a8f98'}">{l.name}</span>
          {/each}
          {#if e.termin === 'ueberfaellig'}<Faellig faellig={e.faellig} termin={e.termin} tage={e.tage} />{/if}
          <span class="kn-u-nr">#{e.nummer}</span>
          <button type="button" class="kn-e-details" title="Details" aria-label="Details" onclick={() => details(e, 'u:' + idVon(e))}>
            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><rect x="1" y="1.5" width="12" height="11" rx="2" fill="none" stroke="currentColor" stroke-width="1.1" /><path d="M8.5 1.5v11" stroke="currentColor" stroke-width="1.1" /></svg>
          </button>
        </span>
      </div>
    {/each}
  </section>
{:else}
  {#if uebersicht}<p class="kn-u-leer">{LEER[art]}</p>{/if}
{/each}

{#if art === 'heute' && uebersicht?.weitere > 0}
  <button type="button" class="kn-mehr" disabled={laedtMehr} onclick={mehrLaden}>
    Mehr laden <span>· {uebersicht.weitere} später als {uebersicht.fenster} Tage</span>
  </button>
{/if}

<style>
  .kn-u-kopf {
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
  .kn-u-abschnitt {
    margin: 0 0 22px;
  }
  h2 {
    margin: 0 0 4px;
    padding: 0 8px 5px;
    font-size: 14px;
    font-weight: 600;
    color: var(--t-blau);
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-u-rot h2 {
    color: var(--t-rot);
  }
  .kn-u-zeile {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 34px;
    padding: 0 8px;
    border-radius: 8px;
  }
  .kn-u-zeile:hover {
    background: var(--t-hover);
  }
  .kn-u-gewaehlt {
    background: var(--t-auswahl);
  }
  .kn-e-check {
    flex: none;
    width: 14px;
    height: 14px;
    padding: 0;
    border: 1.3px solid var(--t-rahmen);
    border-radius: 4px;
    background: var(--t-flaeche);
    cursor: pointer;
  }
  .kn-e-check:hover {
    border-color: var(--t-blau);
  }
  .kn-u-arbeitet {
    opacity: 0.4;
  }
  .kn-u-titel {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 1px;
    border: 0;
    background: transparent;
    padding: 5px 0;
    color: var(--t-text);
    font: inherit;
    font-size: 15px;
    text-align: left;
    cursor: pointer;
  }
  .kn-u-text {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-u-projekt {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-u-nr {
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
  .kn-u-rechts {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .kn-u-label {
    font-size: 11px;
    padding: 1px 7px;
    border-radius: 10px;
    border: 1px solid color-mix(in srgb, var(--l) 60%, transparent);
    color: var(--t-text2);
  }
  .kn-e-details {
    border: 0;
    background: transparent;
    color: var(--t-dim);
    padding: 4px;
    border-radius: 5px;
    cursor: pointer;
    display: grid;
    place-items: center;
    opacity: 0;
  }
  .kn-u-zeile:hover .kn-e-details,
  .kn-u-gewaehlt .kn-e-details {
    opacity: 1;
  }
  .kn-u-leer,
  .kn-u-hinweis {
    color: var(--t-dim);
    font-size: 13px;
    padding: 0 8px;
  }
  .kn-u-fehler {
    color: var(--t-rot);
    font-size: 13px;
  }
  @media (max-width: 760px) {
    .kn-u-label {
      display: none;
    }
    .kn-e-details {
      opacity: 1;
    }
  }
</style>

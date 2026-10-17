<script>
  // Aktionen eines aufgeklappten Pull Requests (pr-aktionen, #32): einen
  // Entwurf freigeben; mergen samt Rueckfrage und Branch loeschen steht in
  // PRMergen (pr-merge-optionen, #82).
  // Ob ein PR ein Entwurf ist oder wo sein Branch liegt, prueft der Dienst
  // selbst noch einmal — hier wird nur angeboten. Den Konfliktzustand rechnet
  // Go (konflikte-anzeigen, #62); hier stehen nur Dateien und der Knopf.
  // Geloest wird je Datei in KonfliktLoesen (konflikte-loesen, #75).
  import KonfliktLoesen from './KonfliktLoesen.svelte';
  // CI-Pruefungen je PR (pr-checks, #80).
  import PRChecks from './PRChecks.svelte';
  // Merge-Dialog, Auto-Merge, Entwurf und Labels am PR (pr-merge-optionen, #82).
  import PRMergen from './PRMergen.svelte';
  import PRBearbeiten from './PRBearbeiten.svelte';

  let { e, aktion, repo = '' } = $props();

  let arbeitet = $state(false);
  let fehler = $state('');
  let meldung = $state('');

  async function tun(name, felder = {}) {
    arbeitet = true;
    fehler = '';
    try {
      const antwort = await aktion(name, felder);
      meldung = antwort && antwort.branch ? 'Branch ' + antwort.branch : '';
    } catch (err) {
      fehler = err.message;
    } finally {
      arbeitet = false;
    }
  }
</script>

<div class="kn-pr">
  <div class="kn-pr-info">
    <span><b>{e.head || '?'}</b> → {e.base || 'main'}</span>
    {#if e.zustand && e.zustand !== 'unbekannt'}<span class="kn-pr-vz" title="voraus / zurück gegen {e.base || 'main'}">↑{e.voraus || 0} ↓{e.zurueck || 0}</span>{/if}
    {#if e.merge_status}<span class="kn-pr-status">GitHub: {e.merge_status}</span>{/if}
  </div>
  <PRChecks checks={e.checks} neuStarten={(id) => aktion('checks-neu', { check_run_id: id })} />
  {#if e.zustand === 'konflikt'}
    <div class="kn-pr-konflikt">
      <span>Konflikt mit {e.base || 'main'}{(e.konflikt_dateien || []).length ? ' in:' : ''}</span>
      {#if (e.konflikt_dateien || []).length}
        <ul class="kn-pr-dateien">
          {#each e.konflikt_dateien as d (d)}<li><code>{d}</code></li>{/each}
        </ul>
      {/if}
    </div>
    {#if repo}<KonfliktLoesen {repo} nummer={e.nummer} loesen={(felder) => aktion('konflikt-loesen', felder)} />{/if}
  {/if}
  {#if e.zustand === 'veraltet'}
    <div class="kn-pr-knoepfe">
      <button type="button" class="kn-pr-base" disabled={arbeitet} onclick={() => tun('base-holen')}
              title="Holt {e.base || 'main'} per GitHub in {e.head || 'den Branch'} (update-branch)">Base in den Branch holen</button>
    </div>
  {/if}
  {#if e.draft}
    <div class="kn-pr-knoepfe">
      <button type="button" class="kn-pr-freigeben kn-pr-primaer" disabled={arbeitet} onclick={() => tun('freigeben')}>Für Review freigeben</button>
    </div>
  {:else}
    <PRMergen {e} {aktion} {repo} />
  {/if}
  <PRBearbeiten {e} {aktion} {repo} />
  {#if meldung}<p class="kn-pr-meldung">{meldung}</p>{/if}
  {#if fehler}<p class="kn-pr-fehler" title={fehler}>{fehler}</p>{/if}
</div>

<style>
  .kn-pr {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .kn-pr-info {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    font-size: 12.5px;
    color: var(--t-dim);
  }
  .kn-pr-info b {
    color: var(--t-text2);
    font-weight: 600;
  }
  .kn-pr-knoepfe {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  button {
    font: inherit;
    font-size: 13px;
    padding: 4px 12px;
    border-radius: 6px;
    border: 1px solid var(--t-rahmen);
    background: var(--t-flaeche);
    color: var(--t-text);
    cursor: pointer;
  }
  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .kn-pr-primaer {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-pr-konflikt {
    font-size: 12.5px;
    color: var(--t-rot);
  }
  .kn-pr-dateien {
    margin: 4px 0 0;
    padding-left: 18px;
    color: var(--t-text2);
  }
  .kn-pr-dateien code {
    font-size: 12px;
  }
  .kn-pr-meldung {
    font-size: 12px;
    color: var(--t-gruen);
    margin: 0;
  }
  .kn-pr-fehler {
    font-size: 12px;
    color: var(--t-rot);
    margin: 0;
  }
</style>

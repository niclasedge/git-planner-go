<script>
  // Historie "Geschlossene PRs" (pr-merge-optionen, #82): die letzten 30,
  // zusammengestellt in Go (appctx.GeschlossenePRsBauen, /api/pr-geschlossen).
  // Geladen wird erst beim Aufklappen; "wieder öffnen" gibt es nur, wo Go es
  // erlaubt (nicht gemergt, Branch steht noch).
  import { holen, mitQuery } from './api.js';

  let { repo = '', prAktion = null } = $props();

  let offen = $state(false);
  let prs = $state(null);
  let grund = $state('');
  let arbeitet = $state(0);
  let fehler = $state('');

  async function laden() {
    try {
      const a = await holen(mitQuery('/api/pr-geschlossen', { repo }));
      prs = (a && a.prs) || [];
      grund = (a && a.grund) || '';
    } catch (err) {
      prs = [];
      grund = err.message;
    }
  }

  function umschalten() {
    offen = !offen;
    if (offen) laden();
  }

  async function oeffnen(p) {
    if (!prAktion) return;
    arbeitet = p.nummer;
    fehler = '';
    try {
      await prAktion({ nummer: p.nummer }, 'wieder-oeffnen', {});
      await laden();
    } catch (err) {
      fehler = '#' + p.nummer + ': ' + err.message;
    } finally {
      arbeitet = 0;
    }
  }
</script>

<section class="kn-gp">
  <h2>
    <button type="button" class="kn-gp-kopf" aria-expanded={offen} onclick={umschalten}>
      <span class="kn-gp-pfeil" aria-hidden="true">{offen ? '▾' : '▸'}</span> Geschlossene PRs
    </button>
  </h2>
  {#if offen}
    {#if prs === null}
      <p class="kn-gp-leer">Lade geschlossene PRs …</p>
    {:else}
      {#if grund}<p class="kn-gp-hinweis" title={grund}>{grund}</p>{/if}
      {#each prs as p (p.nummer)}
        <div class="kn-gp-zeile" data-geschlossen={p.nummer} data-gemergt={p.gemergt ? 'ja' : 'nein'}>
          <a class="kn-gp-titel" href={p.url} target="_blank" rel="noreferrer">#{p.nummer} {p.titel}</a>
          <span class="kn-gp-art" class:kn-gp-gemergt={p.gemergt}>{p.gemergt ? 'gemergt' : 'geschlossen'}</span>
          {#if p.tag}<span class="kn-gp-datum">{p.tag}</span>{/if}
          <span class="kn-gp-branch" title={p.head || ''}>{p.branch_existiert ? 'Branch da' : 'Branch gelöscht'}</span>
          {#if p.wieder_oeffnen}
            <button type="button" class="kn-gp-oeffnen" disabled={arbeitet !== 0} onclick={() => oeffnen(p)}>wieder öffnen</button>
          {/if}
        </div>
      {:else}
        <p class="kn-gp-leer">Keine geschlossenen PRs.</p>
      {/each}
      {#if fehler}<p class="kn-gp-fehler" title={fehler}>{fehler}</p>{/if}
    {/if}
  {/if}
</section>

<style>
  .kn-gp {
    margin-top: 26px;
  }
  h2 {
    margin: 0 0 8px;
    font-size: 16px;
    font-weight: 700;
  }
  .kn-gp-kopf {
    font: inherit;
    border: 0;
    background: transparent;
    color: var(--t-blau);
    padding: 0;
    cursor: pointer;
  }
  .kn-gp-pfeil {
    display: inline-block;
    width: 1em;
    color: var(--t-dim);
  }
  .kn-gp-zeile {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 6px 0;
    border-bottom: 1px solid var(--t-rahmen);
    font-size: 13.5px;
  }
  .kn-gp-titel {
    flex: 1 1 240px;
    color: var(--t-text);
    text-decoration: none;
  }
  .kn-gp-titel:hover {
    text-decoration: underline;
  }
  .kn-gp-art {
    font-size: 11px;
    padding: 1px 7px;
    border: 1px solid var(--t-rahmen);
    border-radius: 10px;
    color: var(--t-dim);
  }
  .kn-gp-gemergt {
    color: var(--t-lila, #8250df);
    border-color: var(--t-lila, #8250df);
  }
  .kn-gp-datum,
  .kn-gp-branch {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-gp-oeffnen {
    font: inherit;
    font-size: 12.5px;
    padding: 3px 10px;
    border-radius: 6px;
    border: 1px solid var(--t-rahmen);
    background: var(--t-flaeche);
    color: var(--t-text);
    cursor: pointer;
  }
  .kn-gp-oeffnen:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .kn-gp-leer,
  .kn-gp-hinweis {
    font-size: 12.5px;
    color: var(--t-dim);
  }
  .kn-gp-fehler {
    font-size: 12px;
    color: var(--t-rot);
  }
</style>

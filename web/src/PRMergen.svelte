<script>
  // Merge-Dialog eines offenen PRs (pr-merge-optionen, #82). Der erste Klick
  // auf "PR mergen" oeffnet den Dialog, der zweite mergt. Welche Methoden das
  // Repo erlaubt und welche vorausgewaehlt ist, rechnet Go
  // (/api/pr-optionen); hier wird nur gezeichnet und gesendet. Dazu Auto-Merge
  // an und aus und der Weg zurueck zum Entwurf.
  import { holen, mitQuery } from './api.js';

  let { e, aktion, repo = '' } = $props();

  const namen = { merge: 'Merge-Commit', squash: 'Squash', rebase: 'Rebase' };

  let offen = $state(false);
  let methoden = $state([]);
  let methode = $state('merge');
  let titel = $state('');
  let text = $state('');
  let branchLoeschen = $state(true);
  let arbeitet = $state(false);
  let fehler = $state('');
  let meldung = $state('');
  let hinweis = $state('');

  async function optionenLaden() {
    try {
      const o = await holen(mitQuery('/api/pr-optionen', { repo }));
      methoden = o && Array.isArray(o.methoden) ? o.methoden : ['merge', 'squash', 'rebase'];
      methode = (o && o.vorgabe) || methoden[0] || 'merge';
      hinweis = (o && o.grund) || '';
    } catch (err) {
      methoden = ['merge', 'squash', 'rebase'];
      methode = 'merge';
      hinweis = err.message;
    }
  }

  async function tun(name, felder) {
    arbeitet = true;
    fehler = '';
    meldung = '';
    try {
      const antwort = await aktion(name, felder);
      meldung = antwort && antwort.branch ? 'Branch ' + antwort.branch : '';
      return true;
    } catch (err) {
      fehler = err.message;
      return false;
    } finally {
      arbeitet = false;
    }
  }

  function commitFelder() {
    const f = { methode };
    if (methode !== 'rebase') {
      if (titel.trim()) f.titel = titel.trim();
      if (text.trim()) f.text = text;
    }
    return f;
  }

  async function mergen() {
    if (!offen) {
      offen = true;
      titel = '';
      text = '';
      await optionenLaden();
      return;
    }
    if (await tun('mergen', { ...commitFelder(), branch_loeschen: branchLoeschen })) offen = false;
  }

  async function autoAn() {
    if (await tun('auto-merge', { an: true, ...commitFelder() })) offen = false;
  }
</script>

{#if e.auto_merge}
  <div class="kn-pm-auto">
    <span class="kn-pm-auto-marke" data-auto-merge={e.auto_merge.methode}>Auto-Merge · {namen[e.auto_merge.methode] || e.auto_merge.methode}</span>
    {#if e.auto_merge.von}<span class="kn-pm-von">von {e.auto_merge.von}</span>{/if}
    <button type="button" class="kn-pm-auto-aus" disabled={arbeitet} onclick={() => tun('auto-merge', { an: false })}>Auto-Merge ausschalten</button>
  </div>
{/if}
<div class="kn-pm-knoepfe">
  <button type="button" class="kn-pr-mergen" class:kn-pm-primaer={!offen} class:kn-pm-warnung={offen}
          disabled={arbeitet || e.mergebar === false} onclick={mergen}
          title={e.mergebar === false ? 'Konflikt: erst lösen (#33)' : ''}>
    {offen ? 'Wirklich PR mergen?' : 'PR mergen'}
  </button>
  {#if offen}<button type="button" class="kn-pm-abbrechen" onclick={() => (offen = false)}>Abbrechen</button>{/if}
  <label class="kn-pr-branch" title={e.head_repo || ''}>
    <input type="checkbox" bind:checked={branchLoeschen} /> Branch danach löschen
  </label>
  <button type="button" class="kn-pr-entwurf" disabled={arbeitet} onclick={() => tun('zu-entwurf', {})}
          title="Macht den PR wieder zum Entwurf (Gegenstück zu „Für Review freigeben“)">Zurück zum Draft</button>
</div>
{#if offen}
  <div class="kn-pm-dialog" role="group" aria-label="Merge-Dialog">
    <div class="kn-pm-methoden" role="radiogroup" aria-label="Merge-Methode">
      {#each methoden as m (m)}
        <label class="kn-pm-methode" class:kn-pm-gewaehlt={methode === m}>
          <input type="radio" name={'methode-' + e.nummer} value={m} data-methode={m} bind:group={methode} />
          {namen[m] || m}
        </label>
      {:else}
        <span class="kn-pm-leer">Das Repo erlaubt keine Merge-Methode.</span>
      {/each}
    </div>
    {#if hinweis}<p class="kn-pm-hinweis" title={hinweis}>Einstellungen nicht lesbar, GitHub entscheidet · {hinweis}</p>{/if}
    {#if methode !== 'rebase'}
      <input type="text" name="commit_titel" placeholder="Commit-Titel (optional)" aria-label="Commit-Titel" bind:value={titel} />
      <textarea name="commit_text" rows="3" placeholder="Commit-Text (optional)" aria-label="Commit-Text" bind:value={text}></textarea>
    {:else}
      <p class="kn-pm-hinweis">Rebase übernimmt die Commits des Branches; ein eigener Commit-Text entfällt.</p>
    {/if}
    {#if !e.auto_merge}
      <div class="kn-pm-knoepfe">
        <button type="button" class="kn-pm-auto-an" disabled={arbeitet || !methoden.length} onclick={autoAn}
                title="GitHub mergt mit dieser Methode, sobald alle Bedingungen erfüllt sind">Auto-Merge einschalten</button>
      </div>
    {/if}
  </div>
{/if}
{#if meldung}<p class="kn-pm-meldung">{meldung}</p>{/if}
{#if fehler}<p class="kn-pm-fehler" title={fehler}>{fehler}</p>{/if}

<style>
  .kn-pm-knoepfe,
  .kn-pm-auto {
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
  .kn-pm-primaer {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-pm-warnung {
    background: var(--t-rot);
    border-color: var(--t-rot);
    color: #fff;
  }
  .kn-pr-branch {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 12.5px;
    color: var(--t-text2);
  }
  .kn-pm-auto-marke {
    font-size: 11.5px;
    padding: 1px 8px;
    border-radius: 10px;
    border: 1px solid var(--t-gruen);
    color: var(--t-gruen);
  }
  .kn-pm-von {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-pm-dialog {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border: 1px solid var(--t-rahmen);
    border-radius: 8px;
    background: var(--t-flaeche);
  }
  .kn-pm-methoden {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .kn-pm-methode {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 12.5px;
    padding: 3px 10px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    cursor: pointer;
    color: var(--t-text2);
  }
  .kn-pm-gewaehlt {
    border-color: var(--t-blau);
    color: var(--t-text);
    background: color-mix(in srgb, var(--t-blau) 14%, transparent);
  }
  input[type='text'],
  textarea {
    font: inherit;
    font-size: 13px;
    padding: 5px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
  }
  .kn-pm-hinweis,
  .kn-pm-leer {
    font-size: 12px;
    color: var(--t-dim);
    margin: 0;
  }
  .kn-pm-meldung {
    font-size: 12px;
    color: var(--t-gruen);
    margin: 0;
  }
  .kn-pm-fehler {
    font-size: 12px;
    color: var(--t-rot);
    margin: 0;
  }
</style>

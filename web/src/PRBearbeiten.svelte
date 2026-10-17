<script>
  // Labels, Zustaendige und Milestone am PR (pr-merge-optionen, #82) — ueber
  // die Issues-API, mit denselben Auswahllisten wie beim Issue
  // (/api/repo-meta). Die Liste laedt erst beim Aufklappen.
  import { holen, mitQuery } from './api.js';

  let { e, aktion, repo = '' } = $props();

  let offen = $state(false);
  let meta = $state(null);
  let arbeitet = $state(false);
  let fehler = $state('');

  let labels = $derived((e.labels || []).map((l) => l.name));
  let zustaendig = $derived(e.zustaendig || []);
  let alleLabels = $derived.by(() => {
    const bekannt = new Map((meta?.labels || []).map((l) => [l.name, l]));
    for (const l of e.labels || []) if (!bekannt.has(l.name)) bekannt.set(l.name, l);
    return [...bekannt.values()];
  });
  let alleLeute = $derived([...new Set([...(meta?.zustaendige || []), ...zustaendig])]);
  let meilensteine = $derived.by(() => {
    const liste = [...(meta?.meilensteine || [])];
    if (e.meilenstein && !liste.some((m) => m.nummer === e.meilenstein.nummer)) liste.push(e.meilenstein);
    return liste;
  });

  async function aufklappen() {
    offen = !offen;
    if (offen && !meta) {
      try {
        meta = (await holen(mitQuery('/api/repo-meta', { repo }))) || {};
      } catch (err) {
        meta = {};
        fehler = err.message;
      }
    }
  }

  async function tun(felder) {
    arbeitet = true;
    fehler = '';
    try {
      await aktion('bearbeiten', felder);
    } catch (err) {
      fehler = err.message;
    } finally {
      arbeitet = false;
    }
  }

  function label(name) {
    tun({ labels: labels.includes(name) ? labels.filter((l) => l !== name) : [...labels, name] });
  }

  function person(login) {
    tun({ zustaendig: zustaendig.includes(login) ? zustaendig.filter((z) => z !== login) : [...zustaendig, login] });
  }
</script>

<div class="kn-prb">
  <button type="button" class="kn-prb-knopf" aria-expanded={offen} onclick={aufklappen}>Labels & Zuständige</button>
  {#if e.meilenstein}<span class="kn-prb-ms" data-meilenstein={e.meilenstein.nummer}>Milestone: {e.meilenstein.titel}</span>{/if}
  {#if offen && meta}
    <div class="kn-prb-wahl">
      {#each alleLabels as l (l.name)}
        <button type="button" data-label={l.name} aria-pressed={labels.includes(l.name)} disabled={arbeitet}
                style="--l: #{l.farbe || '8a8f98'}" onclick={() => label(l.name)}>{l.name}</button>
      {:else}
        <span class="kn-prb-leer">Das Repo hat keine Labels.</span>
      {/each}
    </div>
    {#if alleLeute.length}
      <div class="kn-prb-wahl kn-prb-leute">
        {#each alleLeute as login (login)}
          <button type="button" data-person={login} aria-pressed={zustaendig.includes(login)} disabled={arbeitet} onclick={() => person(login)}>{login}</button>
        {/each}
      </div>
    {/if}
    <label class="kn-prb-zeile">Milestone
      <select name="meilenstein" disabled={arbeitet} value={String(e.meilenstein?.nummer || 0)}
              onchange={(ev) => tun({ meilenstein: parseInt(ev.currentTarget.value, 10) || 0 })}>
        <option value="0">kein Milestone</option>
        {#each meilensteine as m (m.nummer)}<option value={String(m.nummer)}>{m.titel}</option>{/each}
      </select>
    </label>
  {/if}
  {#if fehler}<p class="kn-prb-fehler" title={fehler}>{fehler}</p>{/if}
</div>

<style>
  .kn-prb {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }
  button,
  select {
    font: inherit;
    font-size: 12.5px;
    padding: 3px 10px;
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
  .kn-prb-ms {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-prb-wahl {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .kn-prb-wahl button {
    font-size: 11.5px;
    padding: 1px 9px;
    border-radius: 10px;
    border-color: color-mix(in srgb, var(--l, var(--t-rahmen)) 60%, transparent);
    color: var(--t-text2);
  }
  .kn-prb-wahl button[aria-pressed='true'] {
    background: color-mix(in srgb, var(--l, var(--t-blau)) 22%, transparent);
    color: var(--t-text);
  }
  .kn-prb-zeile {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    color: var(--t-text2);
  }
  .kn-prb-leer {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-prb-fehler {
    font-size: 12px;
    color: var(--t-rot);
    margin: 0;
  }
</style>

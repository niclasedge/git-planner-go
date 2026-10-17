<script>
  // Die CI-Pruefungen eines PRs (pr-checks, #80): Zusammenfassung, je
  // Pruefung Zustand, Name, Dauer und Link zum Log, bei fehlgeschlagenen
  // Check-Runs der Knopf "neu starten". Zustand, Reihenfolge, Dauer und
  // Zusammenfassung kommen fertig aus Go (appctx.ChecksRechnen).
  let { checks = null, neuStarten = null } = $props();

  let arbeitet = $state(0);
  let fehler = $state('');

  const zeichen = { gruen: '✓', rot: '✗', laeuft: '◌', neutral: '–' };

  async function starten(p) {
    if (!neuStarten || arbeitet) return;
    arbeitet = p.check_run_id;
    fehler = '';
    try {
      await neuStarten(p.check_run_id);
    } catch (err) {
      fehler = err.message;
    } finally {
      arbeitet = 0;
    }
  }
</script>

{#if checks}
  <div class="kn-pc" data-checks={checks.gesamt}>
    {#if checks.gesamt === 'keine'}
      <p class="kn-pc-leer">Keine Prüfungen auf diesem Stand.</p>
    {:else}
      <div class="kn-pc-kopf">
        <span class="kn-pc-zeichen kn-pc-{checks.gesamt}">{checks.marke}</span>
        <span>Prüfungen: {checks.zusammenfassung?.text}</span>
      </div>
      <ul class="kn-pc-liste">
        {#each checks.pruefungen || [] as p, i (p.art + ':' + p.name + ':' + i)}
          <li class="kn-pc-pruefung" data-pruefung={p.name} data-zustand={p.zustand}>
            <span class="kn-pc-zeichen kn-pc-{p.zustand}" title={p.ergebnis}>{zeichen[p.zustand] || '–'}</span>
            <span class="kn-pc-name">{p.name}</span>
            {#if p.dauer}<span class="kn-pc-dauer">{p.dauer}</span>{/if}
            {#if p.link}<a class="kn-pc-log" href={p.link} target="_blank" rel="noopener">Log ↗</a>{/if}
            {#if p.neu_startbar && neuStarten}
              <button type="button" class="kn-pc-neu" disabled={!!arbeitet} onclick={() => starten(p)}>
                {arbeitet === p.check_run_id ? 'startet …' : 'neu starten'}
              </button>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
    {#if fehler}<p class="kn-pc-fehler" title={fehler}>{fehler}</p>{/if}
  </div>
{/if}

<style>
  .kn-pc {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12.5px;
  }
  .kn-pc-kopf {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--t-text2);
  }
  .kn-pc-leer {
    margin: 0;
    color: var(--t-dim);
  }
  .kn-pc-liste {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .kn-pc-pruefung {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 22px;
  }
  .kn-pc-zeichen {
    width: 14px;
    text-align: center;
    font-weight: 700;
  }
  .kn-pc-gruen {
    color: var(--t-gruen);
  }
  .kn-pc-rot {
    color: var(--t-rot);
  }
  .kn-pc-laeuft {
    color: #b8860b;
  }
  .kn-pc-neutral {
    color: var(--t-dim);
  }
  .kn-pc-name {
    color: var(--t-text);
  }
  .kn-pc-dauer {
    color: var(--t-dim);
    font-variant-numeric: tabular-nums;
  }
  .kn-pc-log {
    color: var(--t-blau);
    text-decoration: none;
  }
  .kn-pc-neu {
    font: inherit;
    font-size: 12px;
    padding: 1px 8px;
    border-radius: 5px;
    border: 1px solid var(--t-rot);
    background: transparent;
    color: var(--t-rot);
    cursor: pointer;
  }
  .kn-pc-neu:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .kn-pc-fehler {
    margin: 0;
    font-size: 12px;
    color: var(--t-rot);
  }
</style>

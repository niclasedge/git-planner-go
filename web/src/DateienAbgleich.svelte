<script>
  // Zusage gegen Diff eines Changes (dateien-abgleich, #53): die
  // Capabilities-Zeile und die Klappe "Dateien" mit den drei Listen. Listen,
  // Folge und Lage kommen fertig aus /api/detail; hier wird nur gezeichnet.
  let { abgleich } = $props();

  const LAGEN = [
    { lage: 'gedeckt', wort: 'gedeckt' },
    { lage: 'ohne_zusage', wort: 'ohne Zusage' },
    { lage: 'ohne_aenderung', wort: 'ohne Änderung' },
  ];

  let dateien = $derived(abgleich?.dateien || {});
  let zeile = $derived([abgleich?.unveraendert ? 'unverändert' : '', ...(abgleich?.capabilities || [])].filter(Boolean).join(' · '));
  let auchIn = $derived((abgleich?.auch_in || []).join(', '));
  let titel = $derived(
    dateien.grund
      ? (dateien.zugesagt || []).length + ' zugesagt'
      : LAGEN.map((l) => (dateien[l.lage] || []).length + ' ' + l.wort).join(' · '),
  );
</script>

<div class="kn-abgleich">
  {#if zeile || auchIn}
    <div class="kn-abgleich-caps">
      {#if zeile}<span>{zeile}</span>{/if}
      {#if auchIn}<span class="kn-abgleich-auch">liegt abweichend auch in {auchIn}</span>{/if}
    </div>
  {/if}
  <details class="kn-abgleich-dateien" open>
    <summary>Dateien: {titel}</summary>
    {#if dateien.grund}
      <p class="kn-abgleich-grund" title={dateien.grund}>{dateien.grund}</p>
      {#if (dateien.zugesagt || []).length}
        <ul data-lage="zugesagt">
          {#each dateien.zugesagt as p (p)}<li><code>{p}</code></li>{/each}
        </ul>
      {/if}
    {:else}
      {#each LAGEN as l (l.lage)}
        {#if (dateien[l.lage] || []).length}
          <div class="kn-abgleich-lage">{l.wort}</div>
          <ul data-lage={l.lage}>
            {#each dateien[l.lage] as p (p)}<li><code>{p}</code></li>{/each}
          </ul>
        {/if}
      {/each}
    {/if}
  </details>
</div>

<style>
  .kn-abgleich {
    margin: 14px 0 4px;
    font-size: 12.5px;
  }
  .kn-abgleich-caps {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    color: var(--t-dim);
    margin-bottom: 6px;
  }
  .kn-abgleich-auch {
    color: var(--t-rot);
  }
  summary {
    cursor: pointer;
    font-size: 13px;
    font-weight: 600;
    color: var(--t-blau);
  }
  .kn-abgleich-lage {
    margin: 8px 0 2px;
    color: var(--t-dim);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    padding: 2px 0 2px 10px;
    border-left: 3px solid var(--t-linie);
  }
  ul[data-lage='gedeckt'] li {
    border-left-color: var(--t-gruen);
  }
  ul[data-lage='ohne_zusage'] li {
    border-left-color: var(--t-rot);
  }
  code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 12px;
  }
  .kn-abgleich-grund {
    color: var(--t-dim);
    margin: 6px 0;
  }
</style>

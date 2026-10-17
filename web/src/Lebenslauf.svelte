<script>
  // Der Lebenslauf eines Issues oder PRs (pr-lebenslauf, #83): acht Stufen
  // vom Branch bis zum Aufraeumen. Kompakt als Punktleiste in der Zeile,
  // gross in der Detailspalte mit Grund und Zeitpunkt je Stufe und der
  // Nachsorge nach dem Merge. Stufen, Zustaende und Nachsorge kommen fertig
  // aus Go (appctx.LebenslaufBauen); hier wird nur gezeichnet.
  import Schritt from './Schritt.svelte';

  let { lauf, gross = false, nachsorgen = null } = $props();

  const zustandText = { erledigt: 'erledigt', jetzt: 'jetzt', offen: 'offen', blockiert: 'blockiert' };
  const knopfText = { issue_schliessen: 'Issue schließen', branch_loeschen: 'Branch löschen' };

  let arbeitet = $state('');
  let fehler = $state('');

  function punktTitel(s) {
    return [s.titel + ': ' + (zustandText[s.zustand] || s.zustand), s.grund].filter(Boolean).join(' · ');
  }
  function zeitpunkt(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleString('de-DE', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  }
  async function ausfuehren(p) {
    if (!nachsorgen || arbeitet) return;
    arbeitet = p.art;
    fehler = '';
    try {
      await nachsorgen(p);
    } catch (e) {
      fehler = e?.message || String(e);
    } finally {
      arbeitet = '';
    }
  }
</script>

{#if lauf && !gross}
  <span class="kn-ll" data-stufe={lauf.stufe}
        title={(lauf.stufen || []).filter((s) => s.zustand !== 'offen').map(punktTitel).join('\n')}>
    {#each lauf.stufen || [] as s (s.name)}
      <span class="kn-ll-punkt kn-ll-{s.zustand}" data-name={s.name} data-zustand={s.zustand} title={punktTitel(s)}></span>
    {/each}
  </span>
{:else if lauf}
  <div class="kn-ll-gross" data-stufe={lauf.stufe}>
    <ol>
      {#each lauf.stufen || [] as s (s.name)}
        <li class="kn-ll-stufe kn-ll-{s.zustand}" data-name={s.name} data-zustand={s.zustand}>
          <span class="kn-ll-marke" aria-hidden="true">
            {#if s.zustand === 'erledigt'}✓{:else if s.zustand === 'blockiert'}!{/if}
          </span>
          <span class="kn-ll-titel">{s.titel}</span>
          <span class="kn-ll-grund">{s.grund || ''}</span>
          {#if s.zustand === 'erledigt' && s.zeit}<time class="kn-ll-zeit" datetime={s.zeit}>{zeitpunkt(s.zeit)}</time>{/if}
        </li>
      {/each}
    </ol>
    {#if lauf.nachsorge?.length}
      <h4>Nachsorge nach dem Merge</h4>
      <ul class="kn-ll-nachsorge">
        {#each lauf.nachsorge as p (p.art)}
          <li data-art={p.art} data-erledigt={p.erledigt ? 'ja' : 'nein'} class:kn-ll-ns-erledigt={p.erledigt}>
            <span class="kn-ll-ns-haken" aria-hidden="true">{p.erledigt ? '✓' : ''}</span>
            <span class="kn-ll-ns-text">{p.text}</span>
            {#if !p.erledigt && knopfText[p.art] && nachsorgen}
              <button type="button" class="kn-ll-knopf" disabled={!!arbeitet} onclick={() => ausfuehren(p)}>
                {knopfText[p.art]}
              </button>
            {/if}
            {#if !p.erledigt && p.befehl}
              <Schritt schritt={{ lage: 'tun', text: 'Lokal ausführen:', befehl: p.befehl }} />
            {/if}
          </li>
        {/each}
      </ul>
      {#if fehler}<p class="kn-ll-fehler" title={fehler}>{fehler}</p>{/if}
    {/if}
  </div>
{/if}

<style>
  .kn-ll {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 0 2px;
  }
  .kn-ll-punkt {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    border: 1px solid var(--t-rahmen);
    background: transparent;
    box-sizing: border-box;
  }
  .kn-ll-punkt.kn-ll-erledigt {
    background: var(--t-gruen);
    border-color: var(--t-gruen);
  }
  .kn-ll-punkt.kn-ll-jetzt {
    background: var(--t-blau);
    border-color: var(--t-blau);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--t-blau) 25%, transparent);
  }
  .kn-ll-punkt.kn-ll-blockiert {
    background: var(--t-rot);
    border-color: var(--t-rot);
  }
  .kn-ll-gross {
    margin: 4px 0 14px;
  }
  .kn-ll-gross ol {
    list-style: none;
    margin: 0;
    padding: 0;
    position: relative;
  }
  .kn-ll-stufe {
    display: grid;
    grid-template-columns: 22px 80px 1fr auto;
    align-items: center;
    gap: 6px;
    padding: 3px 0;
    font-size: 12.5px;
    color: var(--t-text2);
  }
  .kn-ll-marke {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 1.5px solid var(--t-rahmen);
    display: grid;
    place-items: center;
    font-size: 10px;
    font-weight: 700;
    color: #fff;
    box-sizing: border-box;
  }
  .kn-ll-stufe.kn-ll-erledigt .kn-ll-marke {
    background: var(--t-gruen);
    border-color: var(--t-gruen);
  }
  .kn-ll-stufe.kn-ll-jetzt .kn-ll-marke {
    background: var(--t-blau);
    border-color: var(--t-blau);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--t-blau) 22%, transparent);
  }
  .kn-ll-stufe.kn-ll-blockiert .kn-ll-marke {
    background: var(--t-rot);
    border-color: var(--t-rot);
  }
  .kn-ll-stufe.kn-ll-jetzt .kn-ll-titel,
  .kn-ll-stufe.kn-ll-blockiert .kn-ll-titel {
    font-weight: 600;
    color: var(--t-text);
  }
  .kn-ll-stufe.kn-ll-offen {
    color: var(--t-dim);
  }
  .kn-ll-stufe.kn-ll-blockiert .kn-ll-grund {
    color: var(--t-rot);
  }
  .kn-ll-grund {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kn-ll-zeit {
    font-size: 11.5px;
    color: var(--t-dim);
    white-space: nowrap;
  }
  h4 {
    margin: 12px 0 4px;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--t-text);
  }
  .kn-ll-nachsorge {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .kn-ll-nachsorge > li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 5px 0;
    border-top: 1px solid var(--t-linie);
    font-size: 12.5px;
  }
  .kn-ll-ns-haken {
    width: 14px;
    height: 14px;
    border: 1.5px solid var(--t-rahmen);
    border-radius: 4px;
    display: grid;
    place-items: center;
    font-size: 10px;
    color: var(--t-gruen);
    box-sizing: border-box;
  }
  .kn-ll-ns-erledigt .kn-ll-ns-text {
    color: var(--t-dim);
    text-decoration: line-through;
  }
  .kn-ll-knopf {
    margin-left: auto;
    font: inherit;
    font-size: 12px;
    padding: 2px 10px;
    border-radius: 6px;
    border: 1px solid var(--t-blau);
    background: transparent;
    color: var(--t-blau);
    cursor: pointer;
  }
  .kn-ll-knopf:hover:not(:disabled) {
    background: color-mix(in srgb, var(--t-blau) 10%, transparent);
  }
  .kn-ll-knopf:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .kn-ll-fehler {
    margin: 4px 0;
    font-size: 12px;
    color: var(--t-rot);
  }
</style>

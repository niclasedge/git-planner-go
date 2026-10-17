<script>
  // Die Leiste: Kategorien wie Areas in Things, darunter je Repo eine Zeile.
  // Reihenfolge, Gruppierung und Einklappzustand kommen fertig aus Go
  // (/api/repos); ein Klick auf eine Kategorie schreibt den Zustand nach
  // areas.json und laedt die Leiste neu. Oben stehen wie in Things die
  // Ansichten Heute, Irgendwann und Logbuch ueber alle Repos
  // (faelligkeit, #39; heute-logbuch, #42); ihre Zaehler kommen aus /api/uebersicht.
  import Kreis from './Kreis.svelte';
  import AnsichtSymbol from './AnsichtSymbol.svelte';

  // Je Repo der Stand aus /api/repo-stand (things-farben-fortschritt, #41):
  // Kreis = Anteil erledigter Issues, rechts offene Issues und offene PRs.
  let { leiste, gewaehlt, waehlen, einklappen, fehler = '', uebersicht = null, ansicht = '', ansichtWaehlen = () => {}, standJe = {} } = $props();

  let z = $derived(uebersicht?.zaehler);
  let ansichten = $derived([
    { art: 'heute', name: 'Heute', zahl: z ? z.ueberfaellig + z.heute : 0, rot: !!z?.ueberfaellig },
    { art: 'irgendwann', name: 'Irgendwann', zahl: z?.irgendwann || 0 },
    // Das Logbuch ohne Zaehler, wie in Things (heute-logbuch, #42).
    { art: 'logbuch', name: 'Logbuch', zahl: null },
  ]);

  let kontenFehler = $derived((leiste?.konten || []).filter((k) => k.grund));
</script>

<div class="kn-l">
  <ul class="kn-ansichten">
    {#each ansichten as a (a.art)}
      <li>
        <button type="button" class="kn-r kn-ansicht" class:kn-aktiv={ansicht === a.art} data-ansicht={a.art}
                aria-current={ansicht === a.art ? 'true' : undefined} onclick={() => ansichtWaehlen(a.art)}>
          <AnsichtSymbol art={a.art} />
          <span class="kn-r-name kn-ansicht-name">{a.name}</span>
          {#if uebersicht && a.zahl !== null}<span class="kn-ansicht-zahl" class:kn-rot={a.rot}>{a.zahl}</span>{/if}
        </button>
      </li>
    {/each}
  </ul>
  {#each leiste?.kategorien || [] as k (k.name)}
    <section class="kn-k" data-kategorie={k.name} data-eingeklappt={k.eingeklappt ? 'true' : 'false'}>
      <button
        type="button"
        class="kn-k-kopf"
        aria-expanded={!k.eingeklappt}
        title={k.eingeklappt ? 'Aufklappen' : 'Einklappen'}
        onclick={() => einklappen(k.name, !k.eingeklappt)}
      >
        <svg class="kn-k-symbol" width="17" height="17" viewBox="0 0 18 18" aria-hidden="true">
          <path d="M3 6.2 9 3l6 3.2v6.1L9 15.5 3 12.3z M3 6.2 9 9.4l6-3.2 M9 9.4v6.1" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
        </svg>
        <span class="kn-k-name">{k.ohne ? 'Ohne Kategorie' : k.name}</span>
        <svg class="kn-k-pfeil" class:kn-zu={k.eingeklappt} width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <path d="M2 3.5 5 6.5 8 3.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
      </button>
      {#if !k.eingeklappt}
        <ul>
          {#each k.repos as r (r.nwo)}
            {@const st = standJe[r.nwo]}
            <li>
              <button
                type="button"
                class="kn-r"
                class:kn-aktiv={r.nwo === gewaehlt}
                data-repo={r.nwo}
                aria-current={r.nwo === gewaehlt ? 'true' : undefined}
                title={r.nwo + (r.beschreibung ? ' — ' + r.beschreibung : '')}
                onclick={() => waehlen(r.nwo)}
              >
                <span class="kn-r-kreis" title={st ? Math.round(st.anteil * 100) + ' % der Issues erledigt (' + st.erledigte_issues + ' von ' + (st.erledigte_issues + st.offene_issues) + ')' : ''}>
                  <Kreis anteil={st ? st.anteil : 0} farbe={st ? 'var(--t-blau)' : 'var(--t-grau)'} />
                </span>
                <span class="kn-r-name">{r.name}</span>
                {#if r.ci}
                  <span class="kn-r-ci" data-ci={r.ci} title={'CI: jüngster Run ' + ({ gruen: 'grün', rot: 'rot', grau: 'ohne Ergebnis', laeuft: 'läuft' }[r.ci] || r.ci)}></span>
                {/if}
                <span class="kn-r-owner">{r.owner}</span>
                {#if st?.offene_prs}
                  <span class="kn-r-prs" title={st.offene_prs + ' offene Pull Requests'}>
                    <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true"><circle cx="3" cy="2.5" r="1.4" fill="none" stroke="currentColor" stroke-width="1.1" /><circle cx="3" cy="9.5" r="1.4" fill="none" stroke="currentColor" stroke-width="1.1" /><circle cx="9" cy="9.5" r="1.4" fill="none" stroke="currentColor" stroke-width="1.1" /><path d="M3 3.9v4.2M9 8.1V5.2A2 2 0 0 0 7 3.2H5.2M6.4 2l-1.2 1.2 1.2 1.2" fill="none" stroke="currentColor" stroke-width="1.1" /></svg>
                    {st.offene_prs}
                  </span>
                {/if}
                {#if st ? st.offene_issues : r.offene_issues}
                  <span class="kn-r-zahl" title={st ? st.offene_issues + ' offene Issues' : 'offene Issues und PRs'}>{st ? st.offene_issues : r.offene_issues}</span>
                {/if}
              </button>
            </li>
          {:else}
            <li class="kn-r-leer">leer</li>
          {/each}
        </ul>
      {/if}
    </section>
  {/each}

  {#if fehler}
    <p class="kn-l-fehler" title={fehler}>{fehler}</p>
  {/if}
  {#each kontenFehler as k (k.pat)}
    <p class="kn-l-hinweis" title={k.detail || k.grund}><b>{k.pat}</b> · {k.grund}</p>
  {/each}
</div>

<style>
  .kn-l {
    padding: 4px 0 12px;
  }
  .kn-ansichten {
    margin: 6px 0 4px;
  }
  .kn-ansicht-name {
    flex: 1;
  }
  .kn-ansicht-zahl {
    font-size: 12px;
    color: var(--t-dim);
    font-variant-numeric: tabular-nums;
  }
  .kn-ansicht-zahl.kn-rot {
    color: #fff;
    background: var(--t-rot);
    border-radius: 9px;
    padding: 0 6px;
    font-weight: 600;
  }
  .kn-k {
    margin-top: 14px;
  }
  .kn-k-kopf {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 5px 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--t-text);
    font: inherit;
    font-weight: 600;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
  }
  .kn-k-kopf:hover {
    background: var(--t-hover);
  }
  .kn-k-symbol {
    color: var(--t-dim);
    flex: none;
  }
  .kn-k-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-k-pfeil {
    color: var(--t-dim);
    opacity: 0;
    transition: transform 0.15s, opacity 0.15s;
  }
  .kn-k-kopf:hover .kn-k-pfeil,
  .kn-k-pfeil.kn-zu {
    opacity: 1;
  }
  .kn-k-pfeil.kn-zu {
    transform: rotate(-90deg);
  }
  ul {
    list-style: none;
    margin: 2px 0 0;
    padding: 0;
  }
  .kn-r {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 5px 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--t-text);
    font: inherit;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
  }
  .kn-r:hover {
    background: var(--t-hover);
  }
  .kn-r.kn-aktiv {
    background: var(--t-auswahl);
  }
  .kn-r-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-r-ci {
    flex: 0 0 auto;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--t-grau);
  }
  .kn-r-ci[data-ci='gruen'] {
    background: var(--t-gruen);
  }
  .kn-r-ci[data-ci='rot'] {
    background: var(--t-rot);
  }
  .kn-r-ci[data-ci='laeuft'] {
    background: transparent;
    border: 1.5px solid var(--t-gelb);
    box-sizing: border-box;
  }
  .kn-r-owner {
    flex: 1;
    color: var(--t-dim);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-r-kreis {
    display: grid;
    place-items: center;
  }
  .kn-r-prs {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 12px;
    color: var(--t-lila);
    font-variant-numeric: tabular-nums;
  }
  .kn-r-zahl {
    color: var(--t-dim);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
  .kn-r-leer {
    padding: 3px 10px 3px 35px;
    color: var(--t-dim);
    font-size: 12px;
  }
  .kn-l-fehler,
  .kn-l-hinweis {
    margin: 14px 10px 0;
    font-size: 11px;
    line-height: 1.35;
    color: var(--t-dim);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .kn-l-fehler {
    color: var(--t-rot);
  }
</style>

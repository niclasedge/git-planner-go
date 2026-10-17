<script>
  // Die Reviews eines PRs (pr-reviews, #81): Entscheidung, Stimmen,
  // angefragte Reviewer mit Entfernen, eine Auswahl aus den moeglichen
  // Zustaendigen zum Hinzufuegen, und die Threads mit Code-Stelle, Verlauf,
  // Antwortfeld und "auflösen" / "wieder öffnen". Entscheidung, Reihenfolge
  // und Gruppierung kommen fertig aus Go (appctx.ReviewsRechnen).
  let { reviews = null, zustaendige = [], schreiben = null } = $props();

  let arbeitet = $state('');
  let fehler = $state('');
  let entwurf = $state({});
  let wahl = $state('');

  const entscheidungText = { genehmigt: 'Genehmigt', aenderungen: 'Änderungen verlangt', offen: 'Noch keine Entscheidung' };
  const stimmeText = { genehmigt: '✓ genehmigt', aenderungen: '✎ Änderungen', verworfen: 'verworfen', kommentiert: 'kommentiert' };

  let auswahl = $derived((zustaendige || []).filter((l) => !(reviews?.angefordert || []).includes(l)));

  async function tun(schluessel, aktion, felder) {
    if (!schreiben || arbeitet) return false;
    arbeitet = schluessel;
    fehler = '';
    try {
      await schreiben(aktion, felder);
      return true;
    } catch (err) {
      fehler = err.message;
      return false;
    } finally {
      arbeitet = '';
    }
  }

  async function antworten(t) {
    const text = (entwurf[t.id] || '').trim();
    if (!text) return;
    if (await tun('antworten-' + t.id, 'antworten', { kommentar_id: t.id, text })) entwurf[t.id] = '';
  }

  function ort(t) {
    return t.pfad + (t.zeile ? ':' + (t.start_zeile && t.start_zeile !== t.zeile ? t.start_zeile + '–' : '') + t.zeile : '');
  }

  function zeitpunkt(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleString('de-DE', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  }
</script>

{#if reviews}
  <div class="kn-rv" data-entscheidung={reviews.entscheidung}>
    <div class="kn-rv-kopf">
      {#if reviews.marke}<span class="kn-rv-marke kn-rv-{reviews.entscheidung}">{reviews.marke}</span>{/if}
      <span>{entscheidungText[reviews.entscheidung] || reviews.entscheidung}</span>
      {#if reviews.offene_threads}<span class="kn-rv-zahl">{reviews.offene_threads} offen</span>{/if}
      {#if reviews.aufgeloeste_threads}<span class="kn-rv-zahl">{reviews.aufgeloeste_threads} aufgelöst</span>{/if}
    </div>
    {#if (reviews.stimmen || []).length}
      <ul class="kn-rv-stimmen">
        {#each reviews.stimmen as s (s.login)}
          <li class="kn-rv-stimme kn-rv-{s.zustand}" data-login={s.login} data-zustand={s.zustand}>
            <b>{s.login}</b> {stimmeText[s.zustand] || s.zustand}
          </li>
        {/each}
      </ul>
    {/if}
    <div class="kn-rv-reviewer">
      <span class="kn-rv-label">Angefragt:</span>
      <span class="kn-rv-angefordert">
        {#each reviews.angefordert || [] as l (l)}
          <span class="kn-rv-person" data-reviewer={l}>
            {l}
            {#if schreiben}
              <button type="button" class="kn-rv-entfernen" title="Anfrage an {l} zurücknehmen" aria-label="{l} entfernen"
                      disabled={!!arbeitet} onclick={() => tun('entfernen-' + l, 'reviewer', { entfernen: [l] })}>×</button>
            {/if}
          </span>
        {:else}
          {#if !(reviews.angefordert_teams || []).length}<span class="kn-rv-leer">niemand</span>{/if}
        {/each}
        {#each reviews.angefordert_teams || [] as team (team)}<span class="kn-rv-person kn-rv-team">@{team}</span>{/each}
      </span>
      {#if schreiben && auswahl.length}
        <select class="kn-rv-auswahl" bind:value={wahl} aria-label="Reviewer wählen">
          <option value="">Reviewer …</option>
          {#each auswahl as l (l)}<option value={l}>{l}</option>{/each}
        </select>
        <button type="button" class="kn-rv-anfordern" disabled={!wahl || !!arbeitet}
                onclick={async () => { if (await tun('anfordern', 'reviewer', { hinzufuegen: [wahl] })) wahl = ''; }}>anfordern</button>
      {/if}
    </div>
    {#if !reviews.thread_zustand && (reviews.threads || []).length}
      <p class="kn-rv-hinweis">Ob Threads aufgelöst sind, war nicht abrufbar.</p>
    {/if}
    {#each reviews.threads || [] as t (t.id)}
      <div class="kn-rv-thread" class:kn-rv-zu={t.aufgeloest} data-thread={t.id} data-aufgeloest={t.aufgeloest ? 'ja' : 'nein'}>
        <div class="kn-rv-thread-kopf">
          <code class="kn-rv-ort">{ort(t)}</code>
          {#if t.veraltet}<span class="kn-rv-veraltet">veraltet</span>{/if}
          {#if t.aufgeloest}<span class="kn-rv-aufgeloest">aufgelöst</span>{/if}
        </div>
        {#if t.diff && !t.aufgeloest}<pre class="kn-rv-diff">{t.diff}</pre>{/if}
        <ul class="kn-rv-verlauf">
          {#each t.kommentare || [] as k (k.id)}
            <li class="kn-rv-kommentar" data-kommentar={k.id}>
              <div class="kn-rv-kommentar-kopf">
                <b class="kn-rv-autor">{k.autor}</b>
                {#if k.zeit}<span class="kn-rv-zeit">{zeitpunkt(k.zeit)}</span>{/if}
                {#if k.url}<a class="kn-rv-link" href={k.url} target="_blank" rel="noopener">↗</a>{/if}
              </div>
              <div class="kn-rv-text">{k.text}</div>
            </li>
          {/each}
        </ul>
        {#if schreiben}
          {#if t.aufgeloest}
            <div class="kn-rv-knoepfe">
              <button type="button" class="kn-rv-oeffnen" disabled={!!arbeitet}
                      onclick={() => tun('thread-' + t.id, 'thread', { kommentar_id: t.id, aufgeloest: false })}>wieder öffnen</button>
            </div>
          {:else}
            <textarea class="kn-rv-antwort" rows="2" placeholder="Antworten …" bind:value={entwurf[t.id]}></textarea>
            <div class="kn-rv-knoepfe">
              <button type="button" class="kn-rv-senden kn-rv-primaer" disabled={!!arbeitet || !(entwurf[t.id] || '').trim()}
                      onclick={() => antworten(t)}>{arbeitet === 'antworten-' + t.id ? 'sendet …' : 'Antworten'}</button>
              <button type="button" class="kn-rv-aufloesen" disabled={!!arbeitet}
                      onclick={() => tun('thread-' + t.id, 'thread', { kommentar_id: t.id, aufgeloest: true })}>auflösen</button>
            </div>
          {/if}
        {/if}
      </div>
    {:else}
      <p class="kn-rv-leer">Keine Review-Threads.</p>
    {/each}
    {#if fehler}<p class="kn-rv-fehler" title={fehler}>{fehler}</p>{/if}
  </div>
{/if}

<style>
  .kn-rv {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-size: 12.5px;
  }
  .kn-rv-kopf {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--t-text);
    font-weight: 600;
  }
  .kn-rv-marke {
    font-weight: 700;
  }
  .kn-rv-genehmigt {
    color: var(--t-gruen);
  }
  .kn-rv-aenderungen {
    color: var(--t-rot);
  }
  .kn-rv-zahl {
    font-weight: 400;
    color: var(--t-dim);
  }
  .kn-rv-stimmen {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    color: var(--t-text2);
  }
  .kn-rv-stimme b {
    color: var(--t-text);
    font-weight: 600;
  }
  .kn-rv-verworfen,
  .kn-rv-kommentiert {
    color: var(--t-dim);
  }
  .kn-rv-reviewer {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  .kn-rv-label {
    color: var(--t-dim);
  }
  .kn-rv-angefordert {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .kn-rv-person {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 0 6px;
    border: 1px solid var(--t-rahmen);
    border-radius: 10px;
    color: var(--t-text2);
  }
  .kn-rv-entfernen {
    border: none;
    background: transparent;
    padding: 0 2px;
    color: var(--t-dim);
    cursor: pointer;
    font: inherit;
  }
  .kn-rv-leer,
  .kn-rv-hinweis {
    margin: 0;
    color: var(--t-dim);
  }
  select,
  button,
  textarea {
    font: inherit;
    font-size: 12.5px;
  }
  select,
  .kn-rv-anfordern,
  .kn-rv-knoepfe button {
    padding: 2px 10px;
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
  .kn-rv-thread {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 10px;
    border: 1px solid var(--t-rahmen);
    border-radius: 8px;
  }
  .kn-rv-zu {
    opacity: 0.7;
  }
  .kn-rv-thread-kopf {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .kn-rv-ort {
    font-size: 12px;
    color: var(--t-text);
  }
  .kn-rv-veraltet,
  .kn-rv-aufgeloest {
    font-size: 11px;
    padding: 0 6px;
    border-radius: 4px;
    border: 1px solid var(--t-rahmen);
    color: var(--t-dim);
  }
  .kn-rv-aufgeloest {
    color: var(--t-gruen);
    border-color: var(--t-gruen);
  }
  .kn-rv-diff {
    margin: 0;
    padding: 6px 8px;
    font-size: 11.5px;
    line-height: 1.4;
    overflow-x: auto;
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text2);
  }
  .kn-rv-verlauf {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .kn-rv-kommentar-kopf {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .kn-rv-autor {
    color: var(--t-text);
    font-weight: 600;
  }
  .kn-rv-zeit {
    color: var(--t-dim);
  }
  .kn-rv-link {
    color: var(--t-blau);
    text-decoration: none;
  }
  .kn-rv-text {
    white-space: pre-wrap;
    color: var(--t-text2);
  }
  .kn-rv-antwort {
    width: 100%;
    box-sizing: border-box;
    padding: 4px 6px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: transparent;
    color: var(--t-text);
    resize: vertical;
  }
  .kn-rv-knoepfe {
    display: flex;
    gap: 6px;
  }
  .kn-rv-knoepfe .kn-rv-primaer {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-rv-fehler {
    margin: 0;
    font-size: 12px;
    color: var(--t-rot);
  }
</style>

<script>
  // Settings eines Repos: die Details, die man selten braucht, und die Pflege
  // der Kategorie. Schreiben laeuft ueber die bestehenden Routen
  // bereich/anlegen und bereich; danach laedt App die Leiste neu.
  import { datum } from './status.js';
  import { holen } from './api.js';

  let { repo, kategorien, kategorie, zuordnen, anlegen, schliessen, fehler = '', synchronisieren = null } = $props();

  // Sync-Uebersicht (sync-status, #38): je Quelle der letzte Abruf, dazu die
  // GitHub-Zaehler und das Rate-Limit.
  let sync = $state(null);
  async function syncLaden() {
    try {
      sync = await holen('/api/sync');
    } catch {
      sync = null;
    }
  }
  $effect(() => {
    syncLaden();
  });
  function quellName(n) {
    const [art, repo] = n.split(':');
    const namen = { leiste: 'Repo-Liste', arbeit: 'Arbeitsliste', karte: 'Karte' };
    return (namen[art] || art) + (repo ? ' · ' + repo.split('/').pop() : '');
  }
  function zeitText(z) {
    return z ? new Date(z).toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '–';
  }
  let anteil304 = $derived(sync?.github?.aufrufe ? Math.round((100 * sync.github.unveraendert) / sync.github.aufrufe) : 0);

  let auswahl = $state('');
  let neu = $state('');
  let arbeitet = $state(false);

  let waehlbar = $derived((kategorien || []).filter((k) => !k.ohne && k.name !== kategorie).map((k) => k.name));

  $effect(() => {
    if (!waehlbar.includes(auswahl)) auswahl = waehlbar[0] || '';
  });

  async function mit(f) {
    arbeitet = true;
    try {
      await f();
    } finally {
      arbeitet = false;
    }
  }
</script>

<div class="kn-s-schleier" role="presentation" onclick={schliessen}></div>
<div class="kn-einstellungen" role="dialog" aria-label="Settings {repo.nwo}">
  <div class="kn-s-kopf">
    <h2>{repo.nwo}</h2>
    <button type="button" class="kn-s-zu" aria-label="Schließen" onclick={schliessen}>
      <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
    </button>
  </div>

  <h3>Details</h3>
  <dl>
    {#if repo.beschreibung}<dt>Beschreibung</dt><dd>{repo.beschreibung}</dd>{/if}
    <dt>Sichtbarkeit</dt><dd>{repo.privat ? 'privat' : 'öffentlich'}{repo.archiviert ? ' · archiviert' : ''}</dd>
    {#if repo.sprache}<dt>Sprache</dt><dd>{repo.sprache}</dd>{/if}
    {#if repo.default_branch}<dt>Default-Branch</dt><dd>{repo.default_branch}</dd>{/if}
    {#if repo.gepusht}<dt>Zuletzt gepusht</dt><dd>{datum(repo.gepusht)}</dd>{/if}
    <dt>PAT</dt><dd>{repo.pat || '— (nur lokal bekannt)'}</dd>
    {#if repo.lokal}<dt>Lokaler Clone</dt><dd>{repo.lokal.pfad}</dd>{/if}
  </dl>
  <p class="kn-s-links">
    {#if repo.url}<a href={repo.url} target="_blank" rel="noopener">Auf GitHub ↗</a>{/if}
    {#if repo.lokal}<a href={'/' + encodeURIComponent(repo.lokal.name)}>Projektansicht (Kosten, CLAUDE.md, Memory, Sessions) ›</a>{/if}
    <a href="/ueberblick">Überblick aller Projekte ›</a>
  </p>

  <h3>Kategorie</h3>
  <p class="kn-s-jetzt">Jetzt: <b>{kategorie || 'Ohne Kategorie'}</b></p>
  {#if waehlbar.length}
    <div class="kn-kategorie-waehlen kn-s-reihe">
      <select bind:value={auswahl} aria-label="Bestehende Kategorie">
        {#each waehlbar as n (n)}<option value={n}>{n}</option>{/each}
      </select>
      <button type="button" disabled={arbeitet || !auswahl} onclick={() => mit(() => zuordnen(auswahl))}>Zu Kategorie hinzufügen</button>
    </div>
  {/if}
  <form class="kn-neue-kategorie kn-s-reihe" onsubmit={(ev) => { ev.preventDefault(); if (neu.trim()) mit(() => anlegen(neu.trim())).then(() => (neu = '')); }}>
    <input type="text" placeholder="Neue Kategorie" bind:value={neu} aria-label="Neue Kategorie" />
    <button type="submit" disabled={arbeitet || !neu.trim()}>Anlegen</button>
  </form>
  {#if kategorie}
    <button type="button" class="kn-s-loesen" disabled={arbeitet} onclick={() => mit(() => zuordnen(''))}>Aus Kategorie lösen</button>
  {/if}
  {#if fehler}<p class="kn-s-fehler" title={fehler}>{fehler}</p>{/if}

  <h3>Sync</h3>
  {#if sync}
    <table class="kn-sync">
      <tbody>
        {#each sync.quellen as q (q.name)}
          <tr data-sync-quelle={q.name}>
            <td title={q.name}>{quellName(q.name)}</td>
            <td>{q.laeuft ? 'läuft …' : zeitText(q.zeit)}</td>
            <td>{q.dauer_ms} ms</td>
            <td title="GitHub-Aufrufe, davon unverändert (304)">{q.aufrufe} / {q.unveraendert} unv.</td>
            <td class:kn-s-fehler={q.fehler} title={q.fehler}>{q.fehler ? 'Fehler' : q.geaendert ? 'geändert' : 'aktuell'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="kn-s-github">
      GitHub: {sync.github.aufrufe} Aufrufe, {anteil304} % unverändert (304, ohne Rate-Limit)
      {#if sync.github.rate_limit > 0} · Rate-Limit {sync.github.rate_verbleibend}/{sync.github.rate_limit}{/if}
      · Speicher {sync.github.gemerkt} Antworten · TTL {sync.ttl_s} s
    </p>
    {#each sync.konten || [] as k (k.pat)}
      <p class="kn-s-konto" title={k.detail || k.grund}><b>{k.pat}</b>: {k.grund || k.anzahl + ' Repos'}</p>
    {/each}
  {/if}
  {#if synchronisieren}
    <button type="button" onclick={async () => { await synchronisieren(); setTimeout(syncLaden, 800); }}>Jetzt synchronisieren</button>
  {/if}
</div>

<style>
  .kn-s-schleier {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.18);
    z-index: 20;
  }
  .kn-einstellungen {
    position: fixed;
    top: 8vh;
    left: 50%;
    transform: translateX(-50%);
    width: min(520px, 92vw);
    max-height: 84vh;
    overflow-y: auto;
    background: var(--t-karte);
    color: var(--t-text);
    border-radius: 12px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.25);
    padding: 18px 22px 22px;
    z-index: 21;
  }
  .kn-s-kopf {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h2 {
    margin: 0;
    font-size: 18px;
  }
  h3 {
    margin: 18px 0 8px;
    font-size: 13px;
    font-weight: 600;
    color: var(--t-blau);
  }
  .kn-s-zu {
    border: 0;
    background: transparent;
    color: var(--t-dim);
    padding: 6px;
    border-radius: 6px;
    cursor: pointer;
  }
  dl {
    display: grid;
    grid-template-columns: 130px 1fr;
    gap: 5px 12px;
    margin: 0;
    font-size: 13.5px;
  }
  dt {
    color: var(--t-dim);
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .kn-s-links {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 13px;
    margin: 12px 0 0;
  }
  .kn-s-links a {
    color: var(--t-blau);
    text-decoration: none;
  }
  .kn-s-jetzt {
    font-size: 13px;
    margin: 0 0 8px;
  }
  .kn-s-reihe {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
  }
  select,
  input {
    flex: 1;
    font: inherit;
    font-size: 13.5px;
    padding: 5px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
  }
  button:not(.kn-s-zu) {
    font: inherit;
    font-size: 13px;
    padding: 5px 12px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
    cursor: pointer;
  }
  button:not(.kn-s-zu):hover:not(:disabled) {
    border-color: var(--t-blau);
    color: var(--t-blau);
  }
  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .kn-sync {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .kn-sync td {
    padding: 3px 6px 3px 0;
    border-bottom: 1px solid var(--t-linie);
    color: var(--t-text2);
    white-space: nowrap;
  }
  .kn-sync td:first-child {
    color: var(--t-text);
    white-space: normal;
  }
  .kn-s-github,
  .kn-s-konto {
    font-size: 12px;
    color: var(--t-dim);
    margin: 8px 0 0;
  }
  .kn-s-fehler {
    color: var(--t-rot);
    font-size: 12px;
  }
</style>

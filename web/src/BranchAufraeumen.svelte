<script>
  // Branches aufräumen (branch-aufraeumen, #78): die Remote-Branches eines
  // Repos in den vier Gruppen, die Go liefert (internal/appctx/
  // branchaufraeumen.go) — Folge, Zuordnung, loeschbar und Grund kommen von
  // dort. Hier wird nur gezeichnet, ausgewaehlt und nachgefragt: gemergte
  // sind vorausgewaehlt und gehen gesammelt, verlassene und aktive nur
  // einzeln mit Warnung, geschuetzte werden nur gezeigt.
  import { holen, mitQuery, senden } from './api.js';

  let { repo } = $props();

  const TITEL = { gemergt: 'gemergt', verlassen: 'verlassen', aktiv: 'aktiv', geschuetzt: 'geschützt' };
  const HINWEIS = {
    gemergt: 'in main enthalten oder über einen gemergten PR',
    verlassen: 'PR geschlossen, nicht gemergt',
    aktiv: 'eigene Commits, kein PR',
    geschuetzt: 'Default-Branch, protected oder offener PR — bleibt',
  };

  let lage = $state(null);
  let fehler = $state('');
  let abgewaehlt = $state({});
  let frage = $state(null);
  let arbeitet = $state(false);
  let meldungen = $state([]);

  function laden(r, frisch = false) {
    return holen(mitQuery('/api/branches', { repo: r, frisch: frisch ? 1 : '' }))
      .then((l) => {
        if (repo === r) lage = l;
      })
      .catch((e) => {
        if (repo === r) fehler = e.message;
      });
  }

  $effect(() => {
    const r = repo;
    lage = null;
    fehler = '';
    abgewaehlt = {};
    frage = null;
    meldungen = [];
    if (r) laden(r);
  });

  let gruppen = $derived(lage?.gruppen || []);
  let gemergt = $derived((gruppen.find((g) => g.name === 'gemergt')?.branches || []).filter((b) => b.loeschbar));
  let gewaehlt = $derived(gemergt.filter((b) => !abgewaehlt[b.name]));

  const zeit = (iso) => String(iso || '').slice(0, 10);
  const commits = (n) => (n === 1 ? '1 Commit' : n + ' Commits');

  function sammelFragen() {
    if (!gewaehlt.length) return;
    frage = {
      text: gewaehlt.length + ' gemergte Branches löschen? ' + gewaehlt.map((b) => b.name).join(', '),
      ziele: gewaehlt.map((b) => ({ branch: b.name, sha: b.sha })),
    };
  }

  function einzelnFragen(b) {
    frage = {
      text: 'Branch ' + b.name + ' löschen? ' + commits(b.voraus) + ' gehen verloren.',
      ziele: [{ branch: b.name, sha: b.sha }],
      einzeln: true,
    };
  }

  async function bestaetigen() {
    const f = frage;
    const r = repo;
    if (!f || arbeitet) return;
    arbeitet = true;
    meldungen = [];
    try {
      const rumpf = f.einzeln ? { repo: r, branch: f.ziele[0].branch, sha: f.ziele[0].sha } : { repo: r, branches: f.ziele };
      const antwort = await senden('/action/branch/loeschen', rumpf);
      const ergebnisse = antwort.ergebnisse || [{ branch: f.ziele[0].branch, ok: antwort.ok, fehler: antwort.fehler }];
      meldungen = ergebnisse.filter((e) => !e.ok).map((e) => e.branch + ': ' + (e.fehler || 'nicht gelöscht'));
    } catch (e) {
      meldungen = [e.message];
    } finally {
      frage = null;
      arbeitet = false;
    }
    abgewaehlt = {};
    await laden(r, true);
  }
</script>

{#if repo}
  <section class="kn-ba">
    <h2>Branches aufräumen <span class="kn-ba-zahl">{lage ? lage.anzahl : '…'}</span></h2>
    {#if fehler || lage?.grund}
      <p class="kn-ba-hinweis" title={fehler || lage.grund}>Branches nicht vollständig · {fehler || lage.grund}</p>
    {/if}
    {#each meldungen as m (m)}<p class="kn-ba-fehler">{m}</p>{/each}
    {#if frage}
      <div class="kn-ba-rueckfrage" role="alertdialog">
        <span class:kn-ba-warnung={frage.einzeln}>{frage.text}</span>
        <button type="button" class="kn-ba-ja" disabled={arbeitet} onclick={bestaetigen}>Löschen</button>
        <button type="button" class="kn-ba-nein" disabled={arbeitet} onclick={() => (frage = null)}>Abbrechen</button>
      </div>
    {/if}
    {#each gruppen as g (g.name)}
      <div class="kn-ba-gruppe" data-gruppe={g.name}>
        <h3>{TITEL[g.name] || g.name} <span class="kn-ba-gzahl">{g.branches.length}</span> <span class="kn-ba-ghinweis">{HINWEIS[g.name] || ''}</span></h3>
        {#if g.name === 'gemergt' && gemergt.length}
          <div class="kn-ba-knoepfe">
            <button type="button" class="kn-ba-sammel" disabled={arbeitet || !gewaehlt.length} onclick={sammelFragen}>{gewaehlt.length} gemergte löschen</button>
          </div>
        {/if}
        <ul>
          {#each g.branches as b (b.name)}
            <li data-branch={b.name} class:kn-ba-nicht={!b.loeschbar}>
              {#if g.name === 'gemergt' && b.loeschbar}
                <input type="checkbox" checked={!abgewaehlt[b.name]} aria-label={'auswählen: ' + b.name}
                       onchange={(ev) => (abgewaehlt = { ...abgewaehlt, [b.name]: !ev.currentTarget.checked })} />
              {/if}
              <span class="kn-ba-name">{b.name}</span>
              <span class="kn-ba-stand" title="voraus / zurück gegen {lage?.standard || 'main'}">↑{b.voraus} ↓{b.zurueck}</span>
              <span class="kn-ba-commit"><code>{b.kurz_sha}</code>{#if b.zeit}{' · ' + zeit(b.zeit)}{/if}{#if b.betreff}{' · ' + b.betreff}{/if}</span>
              {#if b.pr}
                <a class="kn-ba-pr kn-ba-pr-{b.pr.zustand}" href={b.pr.url || undefined} target="_blank" rel="noopener" title={b.pr.titel}>#{b.pr.nummer} {b.pr.zustand}</a>
              {/if}
              {#each b.issues as i (i.nummer)}
                <span class="kn-ba-issue" title={i.titel + ' (' + i.gruende.join(', ') + ')'}>Issue #{i.nummer}</span>
              {/each}
              <span class="kn-ba-grund">{b.grund}</span>
              {#if (g.name === 'verlassen' || g.name === 'aktiv') && b.loeschbar}
                <button type="button" class="kn-ba-einzeln" disabled={arbeitet} onclick={() => einzelnFragen(b)}>löschen</button>
              {/if}
            </li>
          {:else}
            <li class="kn-ba-leer">keine</li>
          {/each}
        </ul>
      </div>
    {/each}
  </section>
{/if}

<style>
  .kn-ba {
    margin-top: 28px;
  }
  h2 {
    margin: 0 0 4px;
    padding: 0 8px 6px;
    font-size: 15px;
    font-weight: 600;
    color: var(--t-blau);
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-ba-zahl,
  .kn-ba-gzahl {
    display: inline-block;
    min-width: 18px;
    padding: 0 6px;
    border-radius: 9px;
    background: var(--t-hover);
    color: var(--t-text2);
    font-size: 12px;
    text-align: center;
  }
  .kn-ba-gruppe {
    margin: 10px 0 0;
    padding: 0 8px;
  }
  h3 {
    margin: 8px 0 4px;
    font-size: 13px;
    font-weight: 600;
    color: var(--t-text);
  }
  .kn-ba-ghinweis {
    font-weight: 400;
    color: var(--t-dim);
    font-size: 12px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 4px 0;
    font-size: 13px;
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-ba-nicht {
    color: var(--t-dim);
  }
  .kn-ba-name {
    font-weight: 600;
    font-family: ui-monospace, monospace;
    font-size: 12.5px;
  }
  .kn-ba-stand,
  .kn-ba-commit,
  .kn-ba-grund {
    color: var(--t-dim);
    font-size: 12px;
  }
  .kn-ba-grund {
    margin-left: auto;
  }
  .kn-ba-pr,
  .kn-ba-issue {
    font-size: 11.5px;
    padding: 0 6px;
    border-radius: 8px;
    background: var(--t-hover);
    color: var(--t-text2);
    text-decoration: none;
  }
  .kn-ba-pr-gemergt {
    color: var(--t-lila);
  }
  .kn-ba-pr-offen {
    color: var(--t-gruen);
  }
  .kn-ba-pr-geschlossen {
    color: var(--t-rot);
  }
  .kn-ba-leer {
    color: var(--t-dim);
    font-size: 12.5px;
  }
  .kn-ba-knoepfe {
    margin: 2px 0 6px;
  }
  button {
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
  .kn-ba-sammel {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-ba-einzeln {
    color: var(--t-rot);
  }
  .kn-ba-rueckfrage {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin: 8px;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 13px;
    background: color-mix(in srgb, var(--t-rot) 8%, transparent);
  }
  .kn-ba-warnung {
    color: var(--t-rot);
    font-weight: 600;
  }
  .kn-ba-ja {
    background: var(--t-rot);
    border-color: var(--t-rot);
    color: #fff;
  }
  .kn-ba-hinweis,
  .kn-ba-fehler {
    margin: 6px 8px;
    font-size: 12px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kn-ba-hinweis {
    color: var(--t-dim);
  }
  .kn-ba-fehler {
    color: var(--t-rot);
  }
</style>

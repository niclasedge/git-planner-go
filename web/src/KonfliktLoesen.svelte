<script>
  // Konflikt je Datei loesen (konflikte-loesen, #75). Je Konfliktdatei eine
  // Karte: drei Zeitpunkte (Basis, meine, extern), drei Optionen und darunter
  // die Vorschau der gewaehlten Option. Die Vorschau und die Hunks rechnet Go
  // (/api/pr-konflikt); hier wird nur gewaehlt und gezeichnet. "Lösen" sendet
  // die Wahl mit dem gesehenen Head-sha; den Merge-Commit baut der Dienst.
  import { holen, mitQuery } from './api.js';

  let { repo, nummer, loesen } = $props();

  let offen = $state(false);
  let laedt = $state(false);
  let arbeitet = $state(false);
  let fehler = $state('');
  let meldung = $state('');
  let daten = $state(null);
  let wahl = $state({});
  let hunkWahl = $state({});

  const optionen = [
    { id: 'meine', text: 'Meine behalten' },
    { id: 'extern', text: 'Externe behalten' },
    { id: 'mergen', text: 'Mergen' },
  ];
  const seitenName = { basis: 'Basis', meine: 'meine', extern: 'extern' };

  function zeitpunkt(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleString('de-DE', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  }

  async function laden() {
    offen = true;
    laedt = true;
    fehler = '';
    meldung = '';
    try {
      daten = await holen(mitQuery('/api/pr-konflikt', { repo, nummer }));
      wahl = {};
      hunkWahl = {};
    } catch (err) {
      fehler = err.message;
    } finally {
      laedt = false;
    }
  }

  function waehlen(pfad, w) {
    wahl = { ...wahl, [pfad]: w };
  }

  function hunkWaehlen(pfad, i, w) {
    const liste = [...(hunkWahl[pfad] || [])];
    liste[i] = w;
    hunkWahl = { ...hunkWahl, [pfad]: liste };
  }

  function entschieden(d) {
    const w = wahl[d.pfad];
    if (!w) return false;
    if (w !== 'mergen' || d.mergen.hunks === 0) return true;
    const h = hunkWahl[d.pfad] || [];
    for (let i = 0; i < d.mergen.hunks; i++) if (!h[i]) return false;
    return true;
  }

  let dateien = $derived(daten?.dateien || []);
  let bereit = $derived(dateien.length > 0 && dateien.every(entschieden));

  async function absenden() {
    if (!bereit || arbeitet) return;
    arbeitet = true;
    fehler = '';
    try {
      const antwort = await loesen({
        expected_head_sha: daten.head_sha,
        dateien: dateien.map((d) => {
          const e = { pfad: d.pfad, wahl: wahl[d.pfad] };
          if (e.wahl === 'mergen' && d.mergen.hunks > 0) e.hunks = (hunkWahl[d.pfad] || []).slice(0, d.mergen.hunks);
          return e;
        }),
      });
      meldung = 'Gelöst' + (antwort && antwort.commit ? ': Merge-Commit ' + String(antwort.commit).slice(0, 7) : '');
      daten = null;
      offen = false;
    } catch (err) {
      fehler = err.message;
    } finally {
      arbeitet = false;
    }
  }
</script>

{#snippet diff(zeilen)}
  <pre class="kn-kl-diff">{#each zeilen as z, i (i)}<div class="kn-kl-z kn-kl-z-{z.art}">{z.text}</div>{/each}</pre>
{/snippet}

<div class="kn-kl">
  {#if !offen}
    <button type="button" class="kn-kl-oeffnen" onclick={laden}>Konflikt lösen…</button>
    {#if meldung}<p class="kn-kl-meldung">{meldung}</p>{/if}
  {:else if laedt}
    <p class="kn-kl-leer">Lade die Stände …</p>
  {:else if daten}
    {#if !dateien.length}
      <p class="kn-kl-leer">Kein Konflikt (mehr) — Zustand: {daten.zustand}.</p>
    {/if}
    {#each dateien as d (d.pfad)}
      <div class="kn-kl-datei" data-pfad={d.pfad} data-wahl={wahl[d.pfad] || ''}>
        <div class="kn-kl-kopf">
          <code>{d.pfad}</code>
          {#if d.generiert}<span class="kn-kl-hinweis">{d.hinweis}</span>{/if}
        </div>
        <ul class="kn-kl-seiten">
          {#each ['basis', 'meine', 'extern'] as s (s)}
            {@const seite = d.seiten?.[s] || {}}
            <li class="kn-kl-seite kn-kl-seite-{s}" data-seite={s}>
              <span class="kn-kl-seite-name">{seitenName[s]}</span>
              <code>{seite.kurz_sha || '—'}</code>
              <span class="kn-kl-betreff">{seite.betreff || ''}</span>
              <span class="kn-kl-zeit">{zeitpunkt(seite.zeit)}</span>
            </li>
          {/each}
        </ul>
        <div class="kn-kl-wahl" role="group" aria-label="Option für {d.pfad}">
          {#each optionen as o (o.id)}
            <button type="button" data-wahl={o.id} class:kn-kl-an={wahl[d.pfad] === o.id} aria-pressed={wahl[d.pfad] === o.id}
                    onclick={() => waehlen(d.pfad, o.id)}>
              {o.text}{#if o.id === 'mergen' && d.mergen.hunks > 0}<span class="kn-kl-zahl" title="überlappende Hunks">{d.mergen.hunks}</span>{/if}
            </button>
          {/each}
        </div>
        {#if wahl[d.pfad] === 'meine' || wahl[d.pfad] === 'extern'}
          {@const v = d[wahl[d.pfad]]}
          <div class="kn-kl-vorschau">
            <p class="kn-kl-gegen">
              {#if v.geloescht}Die Datei wird gelöscht.{:else}Ergebnis = {wahl[d.pfad] === 'meine' ? 'PR-Kopf' : 'Base-Branch'}.{/if}
              Unterschied zu {v.gegen}: <span class="kn-kl-l-weg">weg</span> ginge {v.gegen === 'extern' ? 'von der Base' : 'vom PR'} verloren.
            </p>
            {@render diff(v.diff || [])}
          </div>
        {:else if wahl[d.pfad] === 'mergen'}
          {@const m = d.mergen}
          <div class="kn-kl-vorschau">
            {#if m.konfliktfrei}
              <p class="kn-kl-konfliktfrei">Konfliktfrei zusammengeführt{m.geloescht ? ' — die Datei wird gelöscht' : ''}.</p>
              <p class="kn-kl-gegen">Gegenüber meine:</p>
              {@render diff(m.gegen_meine || [])}
              <p class="kn-kl-gegen">Gegenüber extern:</p>
              {@render diff(m.gegen_extern || [])}
            {:else}
              <p class="kn-kl-gegen">{m.hunks === 1 ? 'Ein Hunk überlappt' : m.hunks + ' Hunks überlappen'} — je Hunk meine oder extern wählen.</p>
              <div class="kn-kl-merge">
                {#each m.abschnitte as a, i (i)}
                  {#if a.art === 'gleich'}
                    {#each a.zeilen || [] as z, j (j)}<div class="kn-kl-z kn-kl-z-{z.art}">{z.text}</div>{/each}
                  {:else}
                    {@const gewaehlt = (hunkWahl[d.pfad] || [])[a.hunk]}
                    <div class="kn-kl-hunk" data-hunk={a.hunk} data-gewaehlt={gewaehlt || ''}>
                      <div class="kn-kl-hunk-kopf">
                        <span>Konflikt-Hunk {a.hunk + 1}</span>
                        <button type="button" data-hunk-wahl="meine" class:kn-kl-an={gewaehlt === 'meine'} aria-pressed={gewaehlt === 'meine'}
                                onclick={() => hunkWaehlen(d.pfad, a.hunk, 'meine')}>meine</button>
                        <button type="button" data-hunk-wahl="extern" class:kn-kl-an={gewaehlt === 'extern'} aria-pressed={gewaehlt === 'extern'}
                                onclick={() => hunkWaehlen(d.pfad, a.hunk, 'extern')}>extern</button>
                      </div>
                      <div class="kn-kl-hunk-meine" class:kn-kl-ab={gewaehlt === 'extern'}>
                        {#if a.meine_geloescht}<div class="kn-kl-z kn-kl-z-ausgelassen">(meine: gelöscht)</div>{/if}
                        {#each a.meine || [] as z, j (j)}<div class="kn-kl-z">{z}</div>{/each}
                      </div>
                      <div class="kn-kl-hunk-extern" class:kn-kl-ab={gewaehlt === 'meine'}>
                        {#if a.extern_geloescht}<div class="kn-kl-z kn-kl-z-ausgelassen">(extern: gelöscht)</div>{/if}
                        {#each a.extern || [] as z, j (j)}<div class="kn-kl-z">{z}</div>{/each}
                      </div>
                    </div>
                  {/if}
                {/each}
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
    <div class="kn-kl-knoepfe">
      <button type="button" class="kn-kl-loesen" disabled={!bereit || arbeitet} onclick={absenden}
              title={bereit ? 'Merge-Commit auf ' + (daten.head || 'den Branch') + ' schreiben' : 'Erst jede Datei (und jeden Konflikt-Hunk) entscheiden'}>Lösen</button>
      <button type="button" onclick={() => { offen = false; daten = null; }}>Abbrechen</button>
    </div>
  {/if}
  {#if fehler}<p class="kn-kl-fehler" title={fehler}>{fehler}</p>{/if}
</div>

<style>
  .kn-kl {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-size: 12.5px;
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
  .kn-kl-oeffnen {
    align-self: flex-start;
  }
  .kn-kl-datei {
    border: 1px solid var(--t-rahmen);
    border-radius: 8px;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: var(--t-karte, var(--t-bg));
  }
  .kn-kl-kopf {
    display: flex;
    gap: 8px;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .kn-kl-kopf code {
    font-weight: 600;
    color: var(--t-text);
  }
  .kn-kl-hinweis {
    font-size: 11.5px;
    color: var(--t-gelb, var(--t-rot));
    border: 1px solid currentColor;
    border-radius: 4px;
    padding: 0 5px;
  }
  .kn-kl-seiten {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    color: var(--t-text2);
  }
  .kn-kl-seite {
    display: flex;
    gap: 6px;
    align-items: baseline;
  }
  .kn-kl-seite-name {
    min-width: 42px;
    color: var(--t-dim);
  }
  .kn-kl-zeit {
    color: var(--t-dim);
    margin-left: auto;
  }
  .kn-kl-wahl {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }
  .kn-kl-an {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-kl-zahl {
    margin-left: 5px;
    padding: 0 5px;
    border-radius: 8px;
    background: var(--t-rot);
    color: #fff;
    font-size: 11px;
  }
  .kn-kl-vorschau {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .kn-kl-gegen,
  .kn-kl-konfliktfrei {
    margin: 0;
    color: var(--t-dim);
  }
  .kn-kl-konfliktfrei {
    color: var(--t-gruen);
  }
  .kn-kl-l-weg {
    color: var(--t-rot);
  }
  .kn-kl-diff,
  .kn-kl-merge {
    margin: 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11.5px;
    line-height: 1.45;
    border: 1px solid var(--t-linie);
    border-radius: 6px;
    overflow: auto;
    max-height: 320px;
    white-space: normal;
  }
  .kn-kl-z {
    white-space: pre;
    padding: 0 6px 0 18px;
    position: relative;
    min-height: 1.45em;
  }
  .kn-kl-z::before {
    position: absolute;
    left: 5px;
    color: var(--t-dim);
  }
  .kn-kl-z-hinzu {
    background: color-mix(in srgb, var(--t-gruen) 16%, transparent);
  }
  .kn-kl-z-hinzu::before {
    content: '+';
    color: var(--t-gruen);
  }
  .kn-kl-z-weg {
    background: color-mix(in srgb, var(--t-rot) 16%, transparent);
  }
  .kn-kl-z-weg::before {
    content: '−';
    color: var(--t-rot);
  }
  .kn-kl-z-ausgelassen {
    color: var(--t-dim);
    font-style: italic;
  }
  .kn-kl-hunk {
    border-left: 3px solid var(--t-rot);
    background: color-mix(in srgb, var(--t-rot) 6%, transparent);
    margin: 2px 0;
  }
  .kn-kl-hunk-kopf {
    display: flex;
    gap: 6px;
    align-items: center;
    padding: 3px 6px;
    color: var(--t-rot);
    font-family: system-ui, sans-serif;
    font-weight: 600;
  }
  .kn-kl-hunk-kopf button {
    padding: 1px 8px;
    font-size: 11.5px;
    font-weight: 400;
  }
  .kn-kl-hunk-meine .kn-kl-z::before {
    content: 'm';
  }
  .kn-kl-hunk-extern .kn-kl-z::before {
    content: 'e';
  }
  .kn-kl-hunk-meine {
    background: color-mix(in srgb, var(--t-blau) 10%, transparent);
  }
  .kn-kl-hunk-extern {
    background: color-mix(in srgb, var(--t-lila, var(--t-blau)) 14%, transparent);
  }
  .kn-kl-ab {
    opacity: 0.4;
    text-decoration: line-through;
  }
  .kn-kl-knoepfe {
    display: flex;
    gap: 8px;
  }
  .kn-kl-loesen:not(:disabled) {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-kl-leer {
    margin: 0;
    color: var(--t-dim);
  }
  .kn-kl-meldung {
    margin: 0;
    color: var(--t-gruen);
  }
  .kn-kl-fehler {
    margin: 0;
    color: var(--t-rot);
  }
</style>

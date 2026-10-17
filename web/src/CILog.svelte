<script>
  // Die Log-Ansicht eines Jobs (ci-runs, #122): Zeilen mit Nummer, der
  // Zeitstempel erst per Schalter, Fehlerzeilen rot, ein Suchfeld und der
  // Sprung an den Beginn eines Schritts. Zeilen, Zeitstempel, Fehlermarke und
  // die Grenzen der Schritte kommen fertig aus Go (appctx.CILogAufbereiten);
  // jeder Text wird nur als Text gesetzt, nie als HTML.
  import { tick } from 'svelte';

  let { log = null, laedt = false, fehler = '', schritt = 0, sprung = 0 } = $props();

  let zeiten = $state(false);
  let suche = $state('');
  let kasten = $state(null);

  let zielZeile = $derived((log?.schritte || []).find((s) => s.nummer === schritt)?.zeile || 0);
  let treffer = $derived.by(() => {
    const t = suche.trim().toLowerCase();
    if (!t || !log) return new Set();
    return new Set((log.zeilen || []).filter((z) => z.text.toLowerCase().includes(t)).map((z) => z.nr));
  });
  let ersterTreffer = $derived(treffer.size ? Math.min(...treffer) : 0);

  async function hin(nr) {
    await tick();
    if (!kasten || !nr) return;
    const el = kasten.querySelector(`[data-nr="${nr}"]`);
    if (el) kasten.scrollTop = el.offsetTop;
  }

  // Beim Laden, beim Wechsel des Schritts und bei jedem Klick (sprung) an den
  // Beginn des Schritts; ohne Grenze an den Anfang.
  $effect(() => {
    const nr = zielZeile;
    void sprung;
    if (!log) return;
    if (nr) hin(nr);
    else if (kasten) kasten.scrollTop = 0;
  });

  $effect(() => {
    if (ersterTreffer) hin(ersterTreffer);
  });
</script>

<div class="kn-log">
  <div class="kn-log-leiste">
    <span class="kn-log-name">{log?.name || ''}</span>
    <label class="kn-log-schalter">
      <input type="checkbox" class="kn-log-zeiten" bind:checked={zeiten} /> Zeitstempel
    </label>
    <input type="search" class="kn-log-suche" placeholder="Im Log suchen" bind:value={suche} aria-label="Im Log suchen" />
    {#if suche.trim()}<span class="kn-log-trefferzahl">{treffer.size} Treffer</span>{/if}
    {#if log?.url}<a class="kn-log-github" href={log.url} target="_blank" rel="noopener noreferrer">auf GitHub öffnen ↗</a>{/if}
  </div>
  {#if fehler}
    <p class="kn-log-hinweis kn-log-rot" title={fehler}>{fehler}</p>
  {:else if laedt && !log}
    <p class="kn-log-hinweis">Lade Log …</p>
  {/if}
  {#if log}
    {#if log.grund}<p class="kn-log-hinweis kn-log-rot" title={log.grund}>Log nicht verfügbar · {log.grund}</p>{/if}
    {#if log.gekuerzt}<p class="kn-log-hinweis kn-log-gekuerzt">Das Log war länger als 2 MB — es steht nur sein Ende da.</p>{/if}
    {#if !log.grenzen && log.zeilen?.length}<p class="kn-log-hinweis kn-log-ganz">Keine Schritt-Marken im Log — es steht ganz da.</p>{/if}
    <div class="kn-log-zeilen" bind:this={kasten}>
      {#each log.zeilen || [] as z (z.nr)}
        <div class="kn-log-zeile" class:kn-log-fehler={z.fehler} class:kn-log-ziel={z.nr === zielZeile}
             class:kn-log-treffer={treffer.has(z.nr)} data-nr={z.nr}
             data-ziel={z.nr === zielZeile ? 'ja' : undefined} data-treffer={treffer.has(z.nr) ? 'ja' : undefined}>
          <span class="kn-log-nr">{z.nr}</span>
          {#if zeiten}<span class="kn-log-zeit">{z.zeit || ''}</span>{/if}
          <span class="kn-log-text">{z.text}</span>
        </div>
      {:else}
        <p class="kn-log-hinweis">Das Log ist leer.</p>
      {/each}
    </div>
  {/if}
</div>

<style>
  .kn-log {
    display: flex;
    flex-direction: column;
    min-width: 0;
    gap: 6px;
  }
  .kn-log-leiste {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    font-size: 12.5px;
    color: var(--t-dim);
  }
  .kn-log-name {
    font-weight: 600;
    color: var(--t-text);
    flex: 1 1 auto;
  }
  .kn-log-schalter {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
  }
  .kn-log-suche {
    font: inherit;
    padding: 3px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
    width: 160px;
  }
  .kn-log-github {
    color: var(--t-blau);
    text-decoration: none;
  }
  .kn-log-github:hover {
    text-decoration: underline;
  }
  .kn-log-hinweis {
    margin: 0;
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-log-rot {
    color: var(--t-rot);
  }
  .kn-log-zeilen {
    position: relative;
    max-height: 460px;
    overflow: auto;
    background: #0d1117;
    color: #c9d1d9;
    border-radius: 8px;
    padding: 6px 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;
    font-size: 12px;
    line-height: 18px;
  }
  .kn-log-zeile {
    display: flex;
    gap: 10px;
    padding: 0 10px 0 0;
    white-space: pre;
  }
  .kn-log-zeile:hover {
    background: rgba(255, 255, 255, 0.04);
  }
  .kn-log-nr {
    flex: 0 0 44px;
    text-align: right;
    color: #6e7681;
    user-select: none;
  }
  .kn-log-zeit {
    flex: 0 0 auto;
    color: #8b949e;
  }
  .kn-log-text {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;
    color: #c9d1d9;
  }
  .kn-log-fehler {
    background: rgba(248, 81, 73, 0.12);
  }
  .kn-log-fehler .kn-log-text {
    color: #ff7b72;
  }
  .kn-log-ziel {
    box-shadow: inset 3px 0 0 #58a6ff;
    background: rgba(56, 139, 253, 0.12);
  }
  .kn-log-treffer {
    background: rgba(187, 128, 9, 0.25);
  }
</style>

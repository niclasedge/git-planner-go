<script>
  // Bearbeitung eines Issues in der Detailspalte (issue-verwaltung, #26):
  // Zustand, Titel und Text, Labels, Zustaendige, Blocker, Kommentare. Jede
  // Aktion geht ueber schreiben(aktion, felder); danach laedt die Spalte das
  // Issue neu und App die Arbeitsliste.
  import { markdown } from './markdown.js';
  import { datum, tagIn } from './status.js';

  let { inhalt, meta, schreiben } = $props();

  let bearbeiten = $state(false);
  let titel = $state('');
  let text = $state('');
  let kommentar = $state('');
  let blocker = $state('');
  let arbeitet = $state(false);
  let fehler = $state('');

  let labels = $derived((inhalt.labels || []).map((l) => l.name));
  let zustaendig = $derived(inhalt.zustaendig || []);
  let offen = $derived(inhalt.zustand !== 'closed');
  let alleLabels = $derived.by(() => {
    const bekannt = new Map((meta?.labels || []).map((l) => [l.name, l]));
    for (const l of inhalt.labels || []) if (!bekannt.has(l.name)) bekannt.set(l.name, l);
    return [...bekannt.values()];
  });
  let alleLeute = $derived([...new Set([...(meta?.zustaendige || []), ...zustaendig])]);
  // Milestone (pr-merge-optionen, #82): die offenen des Repos, dazu der
  // aktuelle, falls er schon geschlossen ist.
  let alleMeilensteine = $derived.by(() => {
    const liste = [...(meta?.meilensteine || [])];
    if (inhalt.meilenstein && !liste.some((m) => m.nummer === inhalt.meilenstein.nummer)) liste.push(inhalt.meilenstein);
    return liste;
  });

  async function tun(aktion, felder) {
    arbeitet = true;
    fehler = '';
    try {
      await schreiben(aktion, felder);
      return true;
    } catch (e) {
      fehler = e.message;
      return false;
    } finally {
      arbeitet = false;
    }
  }

  function starteBearbeiten() {
    titel = inhalt.titel || '';
    text = inhalt.text || '';
    bearbeiten = true;
  }

  async function speichern() {
    if (await tun('aendern', { titel: titel.trim(), text })) bearbeiten = false;
  }

  function label(name) {
    const neu = labels.includes(name) ? labels.filter((l) => l !== name) : [...labels, name];
    tun('aendern', { labels: neu });
  }

  function person(login) {
    const neu = zustaendig.includes(login) ? zustaendig.filter((z) => z !== login) : [...zustaendig, login];
    tun('aendern', { zustaendig: neu });
  }

  async function kommentieren(ev) {
    ev.preventDefault();
    if (!kommentar.trim()) return;
    if (await tun('kommentieren', { text: kommentar })) kommentar = '';
  }

  // Faelligkeit (faelligkeit, #39): der Dienst ersetzt nur die Zeile
  // "Fällig: …" am Textende, der Rest des Textes bleibt.
  function faelligSetzen(wert) {
    if (wert === (inhalt.faellig || '')) return;
    tun('aendern', { faellig: wert });
  }

  function meilensteinSetzen(wert) {
    const n = parseInt(wert, 10) || 0;
    if (n === (inhalt.meilenstein?.nummer || 0)) return;
    tun('aendern', { meilenstein: n });
  }

  async function blockerSetzen(ev) {
    ev.preventDefault();
    const n = parseInt(String(blocker).replace('#', ''), 10);
    if (!n) return;
    if (await tun('blocker', { blocker: n })) blocker = '';
  }
</script>

<div class="kn-ib">
  <div class="kn-ib-aktionen">
    <button type="button" class="kn-ib-zustand" disabled={arbeitet}
            onclick={() => tun('aendern', { zustand: offen ? 'closed' : 'open' })}>
      {offen ? 'Schließen' : 'Wieder öffnen'}
    </button>
    {#if !bearbeiten}
      <button type="button" disabled={arbeitet} onclick={starteBearbeiten}>Bearbeiten</button>
    {/if}
    {#if !offen}<span class="kn-ib-zu">geschlossen</span>{/if}
  </div>

  <div class="kn-ib-faellig" data-faellig={inhalt.faellig || ''}>
    <svg width="13" height="13" viewBox="0 0 12 12" aria-hidden="true"><rect x="1" y="2" width="10" height="9" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.1" /><path d="M1 4.8h10M4 1v2M8 1v2" stroke="currentColor" stroke-width="1.1" /></svg>
    <input type="date" name="faellig" value={inhalt.faellig || ''} disabled={arbeitet} aria-label="Fällig am"
           onchange={(ev) => faelligSetzen(ev.currentTarget.value)} />
    <button type="button" class="kn-faellig-heute" disabled={arbeitet} onclick={() => faelligSetzen(tagIn(0))}>Heute</button>
    <button type="button" class="kn-faellig-morgen" disabled={arbeitet} onclick={() => faelligSetzen(tagIn(1))}>Morgen</button>
    {#if inhalt.faellig}
      <button type="button" class="kn-faellig-entfernen" disabled={arbeitet} aria-label="Datum entfernen" title="Datum entfernen" onclick={() => faelligSetzen('')}>×</button>
    {/if}
  </div>

  {#if bearbeiten}
    <div class="kn-ib-edit">
      <input type="text" bind:value={titel} aria-label="Titel" />
      <textarea rows="10" bind:value={text} aria-label="Text (Markdown)"></textarea>
      <div class="kn-ib-reihe">
        <button type="button" onclick={() => (bearbeiten = false)}>Abbrechen</button>
        <button type="button" class="kn-ib-primaer" disabled={arbeitet || !titel.trim()} onclick={speichern}>Speichern</button>
      </div>
    </div>
  {:else}
    <div class="kn-d-md">{@html markdown(inhalt.text) || '<p class="kn-ib-leer">Kein Text.</p>'}</div>
  {/if}

  <h3>Labels</h3>
  <div class="kn-label-wahl">
    {#each alleLabels as l (l.name)}
      <button type="button" data-label={l.name} aria-pressed={labels.includes(l.name)} disabled={arbeitet}
              style="--l: #{l.farbe || '8a8f98'}" onclick={() => label(l.name)}>{l.name}</button>
    {:else}
      <span class="kn-ib-leer">Das Repo hat keine Labels.</span>
    {/each}
  </div>

  {#if alleLeute.length}
    <h3>Zuständig</h3>
    <div class="kn-label-wahl kn-ib-leute">
      {#each alleLeute as login (login)}
        <button type="button" aria-pressed={zustaendig.includes(login)} disabled={arbeitet} onclick={() => person(login)}>{login}</button>
      {/each}
    </div>
  {/if}

  <h3>Milestone</h3>
  <div class="kn-ib-meilenstein">
    <select name="meilenstein" aria-label="Milestone" disabled={arbeitet} value={String(inhalt.meilenstein?.nummer || 0)}
            onchange={(ev) => meilensteinSetzen(ev.currentTarget.value)}>
      <option value="0">kein Milestone</option>
      {#each alleMeilensteine as m (m.nummer)}<option value={String(m.nummer)}>{m.titel}</option>{/each}
    </select>
  </div>

  <h3>Blockiert durch</h3>
  <div class="kn-ib-blocker">
    {#each inhalt.blockiert_durch || [] as b (b)}
      <span class="kn-ib-chip">#{b}
        <button type="button" aria-label={'Blocker #' + b + ' lösen'} disabled={arbeitet} onclick={() => tun('blocker', { blocker: b, entfernen: true })}>×</button>
      </span>
    {/each}
    <form class="kn-ib-inline" onsubmit={blockerSetzen}>
      <input type="text" placeholder="#Nummer" bind:value={blocker} aria-label="Blocker hinzufügen" />
      <button type="submit" disabled={arbeitet || !String(blocker).trim()}>Hinzufügen</button>
    </form>
  </div>

  <h3>Kommentare</h3>
  {#each inhalt.kommentare || [] as k, i (i)}
    <div class="kn-kommentar">
      <div class="kn-k-kopfzeile"><b>{k.autor}</b> · {datum(k.zeit)}</div>
      <div class="kn-d-md">{@html markdown(k.text)}</div>
    </div>
  {:else}
    <p class="kn-ib-leer">Noch keine Kommentare.</p>
  {/each}
  <form class="kn-kommentieren" onsubmit={kommentieren}>
    <textarea rows="3" placeholder="Kommentar (Markdown)" bind:value={kommentar} aria-label="Kommentar"></textarea>
    <button type="submit" disabled={arbeitet || !kommentar.trim()}>Kommentieren</button>
  </form>
  {#if fehler}<p class="kn-ib-fehler" title={fehler}>{fehler}</p>{/if}
</div>

<style>
  .kn-ib-aktionen,
  .kn-ib-reihe {
    display: flex;
    gap: 8px;
    align-items: center;
    margin: 12px 0;
  }
  button {
    font: inherit;
    font-size: 12.5px;
    padding: 4px 11px;
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
  .kn-ib-zustand,
  .kn-ib-primaer {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-ib-faellig {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 0 12px;
    color: var(--t-dim);
  }
  .kn-ib-faellig input {
    padding: 3px 6px;
    font-size: 12.5px;
    color-scheme: light dark;
  }
  .kn-ib-faellig[data-faellig=''] input {
    color: var(--t-dim);
  }
  .kn-faellig-entfernen {
    padding: 2px 8px;
  }
  .kn-ib-zu {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-ib-edit {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  input,
  textarea {
    font: inherit;
    font-size: 13.5px;
    padding: 6px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
  }
  h3 {
    margin: 20px 0 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--t-blau);
  }
  .kn-label-wahl {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .kn-label-wahl button {
    font-size: 11.5px;
    padding: 1px 9px;
    border-radius: 10px;
    border-color: color-mix(in srgb, var(--l, var(--t-rahmen)) 60%, transparent);
    color: var(--t-text2);
  }
  .kn-label-wahl button[aria-pressed='true'] {
    background: color-mix(in srgb, var(--l, var(--t-blau)) 22%, transparent);
    color: var(--t-text);
  }
  .kn-ib-leute button[aria-pressed='true'] {
    background: var(--t-auswahl);
  }
  .kn-ib-meilenstein select {
    font: inherit;
    font-size: 12.5px;
    padding: 3px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
  }
  .kn-ib-blocker {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .kn-ib-chip {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    font-size: 12px;
    padding: 1px 2px 1px 8px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--t-rot) 12%, transparent);
    color: var(--t-rot);
  }
  .kn-ib-chip button {
    border: 0;
    background: transparent;
    color: inherit;
    padding: 0 5px;
  }
  .kn-ib-inline {
    display: flex;
    gap: 6px;
  }
  .kn-ib-inline input {
    width: 90px;
    padding: 3px 6px;
  }
  .kn-kommentar {
    padding: 8px 0;
    border-top: 1px solid var(--t-linie);
  }
  .kn-k-kopfzeile {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-kommentieren {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
    margin-top: 8px;
  }
  .kn-kommentieren textarea {
    width: 100%;
    box-sizing: border-box;
  }
  .kn-ib-leer {
    color: var(--t-dim);
    font-size: 13px;
  }
  .kn-ib-fehler {
    color: var(--t-rot);
    font-size: 12px;
  }
  .kn-label-wahl button[aria-pressed='true']::before {
    content: '✓ ';
    font-weight: 600;
  }
</style>

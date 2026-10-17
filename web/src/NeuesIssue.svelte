<script>
  // "+ Neues Issue" wie das Plus in Things: ein Klick oeffnet ein kleines
  // Formular in der Liste. Labels kommen aus /api/repo-meta; das Datum geht
  // als faellig mit und landet als "Fällig: …" am Textende (faelligkeit, #39).
  let { meta, anlegen } = $props();

  let offen = $state(false);
  let titel = $state('');
  let text = $state('');
  let faellig = $state('');
  let labels = $state([]);
  let fehler = $state('');
  let arbeitet = $state(false);

  function umschalten(name) {
    labels = labels.includes(name) ? labels.filter((l) => l !== name) : [...labels, name];
  }

  async function absenden(ev) {
    ev.preventDefault();
    if (!titel.trim()) return;
    arbeitet = true;
    fehler = '';
    try {
      await anlegen({ titel: titel.trim(), text, labels, zustaendig: [], faellig });
      titel = '';
      text = '';
      faellig = '';
      labels = [];
      offen = false;
    } catch (e) {
      fehler = e.message;
    } finally {
      arbeitet = false;
    }
  }
</script>

{#if !offen}
  <button type="button" class="kn-neues-issue-knopf" onclick={() => (offen = true)}>
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M6 1.5v9M1.5 6h9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
    Neues Issue
  </button>
{:else}
  <form class="kn-neues-issue" onsubmit={absenden}>
    <!-- svelte-ignore a11y_autofocus -->
    <input name="titel" type="text" placeholder="Titel" bind:value={titel} autofocus aria-label="Titel" />
    <textarea name="text" rows="3" placeholder="Beschreibung (Markdown)" bind:value={text} aria-label="Beschreibung"></textarea>
    {#if meta?.labels?.length}
      <div class="kn-ni-labels">
        {#each meta.labels as l (l.name)}
          <button type="button" class="kn-ni-label" aria-pressed={labels.includes(l.name)} style="--l: #{l.farbe || '8a8f98'}" onclick={() => umschalten(l.name)}>{l.name}</button>
        {/each}
      </div>
    {/if}
    <div class="kn-ni-fuss">
      <label class="kn-ni-datum" title="Fällig am">
        <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><rect x="1" y="2" width="10" height="9" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.1" /><path d="M1 4.8h10M4 1v2M8 1v2" stroke="currentColor" stroke-width="1.1" /></svg>
        <input name="faellig" type="date" bind:value={faellig} aria-label="Fällig am" />
      </label>
      {#if fehler}<span class="kn-ni-fehler" title={fehler}>{fehler}</span>{/if}
      <button type="button" onclick={() => (offen = false)}>Abbrechen</button>
      <button type="submit" class="kn-ni-primaer" disabled={arbeitet || !titel.trim()}>Anlegen</button>
    </div>
  </form>
{/if}

<style>
  .kn-neues-issue-knopf {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 4px 0 0;
    padding: 7px 8px;
    border: 0;
    background: transparent;
    color: var(--t-blau);
    font: inherit;
    font-size: 14px;
    border-radius: 8px;
    cursor: pointer;
  }
  .kn-neues-issue-knopf:hover {
    background: var(--t-hover);
  }
  .kn-neues-issue {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 10px -6px;
    padding: 12px 14px;
    background: var(--t-karte);
    border-radius: 8px;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06), 0 6px 22px rgba(0, 0, 0, 0.09);
  }
  input,
  textarea {
    font: inherit;
    font-size: 14px;
    border: 0;
    background: transparent;
    color: var(--t-text);
    outline: none;
    resize: vertical;
  }
  input {
    font-size: 15px;
  }
  textarea {
    color: var(--t-text2);
  }
  .kn-ni-labels {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .kn-ni-label {
    font: inherit;
    font-size: 11px;
    padding: 1px 8px;
    border-radius: 10px;
    border: 1px solid color-mix(in srgb, var(--l) 60%, transparent);
    background: transparent;
    color: var(--t-text2);
    cursor: pointer;
  }
  .kn-ni-label[aria-pressed='true'] {
    background: color-mix(in srgb, var(--l) 22%, transparent);
    color: var(--t-text);
  }
  .kn-ni-fuss {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 8px;
  }
  .kn-ni-fuss button {
    font: inherit;
    font-size: 13px;
    padding: 4px 12px;
    border-radius: 6px;
    border: 1px solid var(--t-rahmen);
    background: var(--t-flaeche);
    color: var(--t-text);
    cursor: pointer;
  }
  .kn-ni-fuss .kn-ni-primaer {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
  .kn-ni-fuss button:disabled {
    opacity: 0.5;
  }
  .kn-ni-datum {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: auto;
    color: var(--t-dim);
  }
  .kn-ni-datum input {
    font-size: 13px;
    color: var(--t-text2);
    color-scheme: light dark;
  }
  .kn-ni-fehler {
    flex: 1;
    font-size: 12px;
    color: var(--t-rot);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-ni-label[aria-pressed='true']::before {
    content: '✓ ';
    font-weight: 600;
  }
</style>

<script>
  // Branch und PR aus einem Issue anlegen (pr-anlegen, #79). Welche Aktion
  // ansteht und womit das Formular vorbelegt ist, rechnet Go
  // (appctx.AnlegenVorschlag); hier wird nur gezeigt und gesendet. Nach dem
  // Anlegen laedt die Liste neu, und mit ihr kommt der naechste Vorschlag.
  import { branchSchreiben, prSchreiben } from './api.js';

  let { vorschlag, repo, nummer, geaendert = () => {} } = $props();

  let offen = $state(false);
  let name = $state('');
  let titel = $state('');
  let text = $state('');
  let draft = $state(false);
  let fehler = $state('');
  let arbeitet = $state(false);

  // Ein neuer Vorschlag (etwa nach dem Branch der PR) schliesst das Formular
  // und belegt es neu.
  $effect(() => {
    const v = vorschlag;
    offen = false;
    fehler = '';
    name = v?.branch || '';
    titel = v?.titel || '';
    text = v?.text || '';
    draft = false;
  });

  let istPR = $derived(vorschlag?.aktion === 'pr');

  async function absenden(ev) {
    ev.preventDefault();
    arbeitet = true;
    fehler = '';
    try {
      if (istPR) {
        await prSchreiben('anlegen', { repo, head: vorschlag.branch, titel: titel.trim(), text, draft });
      } else {
        await branchSchreiben('anlegen', { repo, nummer, name: name.trim() });
      }
      offen = false;
      geaendert();
    } catch (e) {
      fehler = e.message;
    } finally {
      arbeitet = false;
    }
  }
</script>

{#if vorschlag}
  <div class="kn-an" data-aktion={vorschlag.aktion}>
    {#if !offen}
      <button type="button" class="kn-an-knopf" data-aktion={vorschlag.aktion} onclick={() => (offen = true)}>
        <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M6 1.5v9M1.5 6h9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
        {istPR ? 'PR anlegen' : 'Branch anlegen'}
      </button>
    {:else}
      <form class="kn-an-form" onsubmit={absenden}>
        {#if istPR}
          <div class="kn-an-head"><code>{vorschlag.branch}</code> → <code>{vorschlag.base}</code></div>
          <label>Titel <input name="titel" type="text" bind:value={titel} /></label>
          <label>Text <textarea name="text" rows="4" bind:value={text}></textarea></label>
          <label class="kn-an-draft"><input name="draft" type="checkbox" bind:checked={draft} /> Draft</label>
        {:else}
          <label>Branch <input name="branch" type="text" bind:value={name} spellcheck="false" /></label>
          <div class="kn-an-base">von <code>{vorschlag.base}</code></div>
        {/if}
        {#if fehler}<p class="kn-an-fehler" title={fehler}>{fehler}</p>{/if}
        <div class="kn-an-fuss">
          <button type="button" onclick={() => (offen = false)}>Abbrechen</button>
          <button type="submit" class="kn-an-senden" disabled={arbeitet || (istPR ? !titel.trim() : !name.trim())}>
            {istPR ? 'PR anlegen' : 'Anlegen'}
          </button>
        </div>
      </form>
    {/if}
  </div>
{/if}

<style>
  .kn-an {
    margin: 10px 0 12px;
  }
  .kn-an-knopf {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--t-blau);
  }
  .kn-an-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px;
    background: var(--t-karte);
    border-radius: 8px;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06), 0 6px 22px rgba(0, 0, 0, 0.09);
    font-size: 13px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 3px;
    color: var(--t-dim);
    font-size: 12px;
  }
  .kn-an-draft {
    flex-direction: row;
    align-items: center;
    gap: 6px;
    color: var(--t-text2);
    font-size: 13px;
  }
  input[type='text'],
  textarea {
    font: inherit;
    font-size: 13.5px;
    padding: 5px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 6px;
    background: var(--t-flaeche);
    color: var(--t-text);
    resize: vertical;
  }
  .kn-an-head,
  .kn-an-base {
    color: var(--t-text2);
    font-size: 12.5px;
  }
  code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 12px;
  }
  .kn-an-fehler {
    margin: 0;
    color: var(--t-rot);
    font-size: 12.5px;
  }
  .kn-an-fuss {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  button {
    font: inherit;
    font-size: 13px;
    padding: 4px 12px;
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
  .kn-an-senden {
    background: var(--t-blau);
    border-color: var(--t-blau);
    color: #fff;
  }
</style>

<script>
  // Der naechste Schritt eines Changes: Satz und, wenn es einen gibt, der
  // Befehl mit Kopierknopf. Abgeleitet wird er in Go (internal/appctx,
  // naechster-schritt #52); hier wird nur gezeichnet und kopiert.
  let { schritt } = $props();

  let kopiert = $state(false);
  let fehler = $state('');

  async function kopieren() {
    fehler = '';
    const befehl = schritt?.befehl || '';
    if (!befehl) return;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(befehl);
      } else {
        // Ohne sicheren Kontext (Netz-Adresse per http) fehlt die
        // Clipboard-API; dann ueber ein unsichtbares Textfeld.
        const feld = document.createElement('textarea');
        feld.value = befehl;
        feld.setAttribute('readonly', '');
        feld.style.position = 'fixed';
        feld.style.opacity = '0';
        document.body.appendChild(feld);
        feld.select();
        const ok = document.execCommand('copy');
        feld.remove();
        if (!ok) throw new Error('Kopieren nicht möglich');
      }
      kopiert = true;
      setTimeout(() => (kopiert = false), 1800);
    } catch (e) {
      fehler = e?.message || String(e);
    }
  }
</script>

{#if schritt}
  <div class="kn-schritt" data-lage={schritt.lage}>
    <span class="kn-schritt-punkt" aria-hidden="true"></span>
    <div class="kn-schritt-inhalt">
      <span class="kn-schritt-text">{schritt.text}</span>
      {#if schritt.befehl}
        <span class="kn-schritt-zeile">
          <code class="kn-schritt-befehl">{schritt.befehl}</code>
          <button type="button" class="kn-schritt-kopieren" class:kn-schritt-kopiert={kopiert}
                  title={fehler || 'Befehl in die Zwischenablage kopieren'} onclick={kopieren}>
            {kopiert ? 'kopiert' : 'Kopieren'}
          </button>
        </span>
      {/if}
    </div>
  </div>
{/if}

<style>
  .kn-schritt {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 7px 10px;
    margin: 0 0 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 7px;
    background: var(--t-flaeche);
    font-size: 12.5px;
    color: var(--t-text);
  }
  .kn-schritt-punkt {
    flex: none;
    width: 8px;
    height: 8px;
    margin-top: 5px;
    border-radius: 50%;
    background: var(--t-dim);
  }
  .kn-schritt[data-lage='tun'] .kn-schritt-punkt {
    background: var(--t-blau);
  }
  .kn-schritt[data-lage='warten'] .kn-schritt-punkt {
    background: var(--t-gelb, #f5c400);
  }
  .kn-schritt-inhalt {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .kn-schritt-zeile {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .kn-schritt-befehl {
    font-size: 12px;
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--t-bg);
    border: 1px solid var(--t-linie);
    white-space: pre-wrap;
    word-break: break-all;
  }
  .kn-schritt-kopieren {
    font: inherit;
    font-size: 11.5px;
    padding: 1px 8px;
    border: 1px solid var(--t-rahmen);
    border-radius: 5px;
    background: var(--t-bg);
    color: var(--t-blau);
    cursor: pointer;
  }
  .kn-schritt-kopieren:hover {
    background: var(--t-hover);
  }
  .kn-schritt-kopiert {
    color: var(--t-gruen);
    border-color: var(--t-gruen);
  }
</style>

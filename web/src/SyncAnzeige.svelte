<script>
  // Dezente Sync-Anzeige in der Kopfzeile (sync-status, #38): wann zuletzt
  // mit GitHub abgeglichen wurde, und ein Punkt fuer den Zustand — grau
  // aktuell, blau laedt, rot Fehler. Ein Klick holt frisch.
  let { zeit = '', laeuft = false, fehler = '', aktualisieren } = $props();

  let jetzt = $state(Date.now());
  $effect(() => {
    const t = setInterval(() => (jetzt = Date.now()), 20000);
    return () => clearInterval(t);
  });

  let alter = $derived.by(() => {
    if (!zeit) return '';
    const s = Math.max(0, Math.round((jetzt - new Date(zeit).getTime()) / 1000));
    if (s < 45) return 'gerade eben';
    if (s < 3600) return 'vor ' + Math.round(s / 60) + ' min';
    if (s < 86400) return 'vor ' + Math.round(s / 3600) + ' h';
    return new Date(zeit).toLocaleDateString('de-DE');
  });
  let zustand = $derived(laeuft ? 'laeuft' : fehler ? 'fehler' : 'ok');
  let titel = $derived(
    (zeit ? 'Letzter Sync: ' + new Date(zeit).toLocaleString('de-DE') : 'Noch kein Sync') +
      (fehler ? '\nFehler: ' + fehler : '') +
      '\nKlick: jetzt synchronisieren',
  );
</script>

<button type="button" class="kn-sync-anzeige" title={titel} onclick={aktualisieren}>
  <span class="kn-sync-punkt" data-sync={zustand}></span>
  <span class="kn-sync-text">{laeuft ? 'Sync …' : alter ? 'Sync ' + alter : 'Sync'}</span>
</button>

<style>
  .kn-sync-anzeige {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: 0;
    background: transparent;
    color: var(--t-dim);
    font: inherit;
    font-size: 11px;
    padding: 3px 6px;
    border-radius: 6px;
    cursor: pointer;
  }
  .kn-sync-anzeige:hover {
    background: var(--t-hover);
    color: var(--t-text);
  }
  .kn-sync-punkt {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--t-grau);
  }
  .kn-sync-punkt[data-sync='laeuft'] {
    background: var(--t-blau);
    animation: kn-puls 1s ease-in-out infinite;
  }
  .kn-sync-punkt[data-sync='fehler'] {
    background: var(--t-rot);
  }
  @keyframes kn-puls {
    50% {
      opacity: 0.3;
    }
  }
</style>

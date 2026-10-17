<script>
  // Die Symbole der Ansichten wie in Things: gelber Stern fuer Heute, roter
  // Kalender fuer Demnaechst, beige Kiste fuer Irgendwann (faelligkeit, #39), gruener Haken fuer das
  // Logbuch (heute-logbuch, #42).
  // Die Farben kommen aus den Themen-Variablen (things-farben-fortschritt, #41).
  let { art, groesse = 16 } = $props();
</script>

<svg class="kn-as" width={groesse} height={groesse} viewBox="0 0 16 16" aria-hidden="true">
  {#if art === 'heute'}
    <path d="M8 1.3l1.95 4.1 4.45.55-3.28 3.08.85 4.42L8 11.3l-3.97 2.15.85-4.42L1.6 5.95l4.45-.55z" fill="var(--t-gelb, #f5c400)" stroke="var(--t-gelb, #f5c400)" stroke-width=".6" stroke-linejoin="round" />
  {:else if art === 'demnaechst'}
    <rect x="1.5" y="2.5" width="13" height="12" rx="2.2" fill="var(--t-rot, #f0454f)" />
    <rect x="2.8" y="6" width="10.4" height="7.3" rx="1" fill="var(--t-bg, #fff)" />
    <path d="M5 1.2v2.6M11 1.2v2.6" stroke="var(--t-rot, #f0454f)" stroke-width="1.5" stroke-linecap="round" />
  {:else if art === 'logbuch'}
    <rect x="1.5" y="1.5" width="13" height="13" rx="3" fill="var(--t-gruen, #3dab59)" />
    <path d="M4.6 8.2 7 10.5l4.5-5" fill="none" stroke="var(--t-bg, #fff)" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
  {:else}
    <path d="M1.8 5.2h12.4v8.1a1.2 1.2 0 0 1-1.2 1.2H3a1.2 1.2 0 0 1-1.2-1.2z" fill="var(--t-beige, #cdb27a)" opacity=".75" />
    <rect x="1" y="2.2" width="14" height="3.4" rx="1" fill="var(--t-beige, #cdb27a)" />
    <path d="M6 8h4" stroke="var(--t-bg, #fff)" stroke-width="1.3" stroke-linecap="round" />
  {/if}
</svg>

<style>
  .kn-as {
    flex: none;
  }
</style>

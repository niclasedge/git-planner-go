<script>
  // Fortschrittskreis wie bei den Projekten in Things: ein Ring, der sich
  // von oben im Uhrzeigersinn als Tortenstueck fuellt. anteil kommt fertig
  // gerechnet (0..1) und steht als data-anteil am Kreis.
  let { anteil = 0, groesse = 14, farbe = 'var(--t-blau)' } = $props();
  const r = 5;
  let winkel = $derived(Math.max(0, Math.min(1, anteil)) * 2 * Math.PI);
  let pfad = $derived.by(() => {
    if (anteil <= 0) return '';
    if (anteil >= 1) return '';
    const x = 7 + r * Math.sin(winkel);
    const y = 7 - r * Math.cos(winkel);
    const gross = winkel > Math.PI ? 1 : 0;
    return `M7 7 L7 ${7 - r} A${r} ${r} 0 ${gross} 1 ${x.toFixed(3)} ${y.toFixed(3)} Z`;
  });
</script>

<svg class="kn-kreis" data-anteil={anteil} width={groesse} height={groesse} viewBox="0 0 14 14" aria-hidden="true">
  <circle cx="7" cy="7" r="6.2" fill="none" stroke={farbe} stroke-width="1.3" />
  {#if anteil >= 1}
    <circle cx="7" cy="7" r={r} fill={farbe} />
  {:else if pfad}
    <path d={pfad} fill={farbe} />
  {/if}
</svg>

<style>
  .kn-kreis {
    flex: none;
    display: block;
  }
</style>

<script>
  // Das Datum an einer Zeile (faelligkeit, #39): Kalender und Text, rot wenn
  // ueberfaellig, gelb wenn heute.
  import { faelligText } from './status.js';

  let { faellig, termin = '', tage = 0 } = $props();
  let f = $derived(faelligText(faellig, termin, tage));
</script>

{#if faellig}
  <span class="kn-faellig" data-termin={termin} style="color: {f.farbe}" title={'Fällig: ' + (f.titel || faellig)}>
    <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1" y="2" width="10" height="9" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.1" />
      <path d="M1 4.8h10M4 1v2M8 1v2" stroke="currentColor" stroke-width="1.1" />
    </svg>
    {f.text}
  </span>
{/if}

<style>
  .kn-faellig {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    white-space: nowrap;
  }
</style>

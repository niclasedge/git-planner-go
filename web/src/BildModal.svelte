<script>
  // Bildansicht im selben Fenster (beleg-bilder, #105), eine Komponente fuer
  // alle Stellen: Galerie "Belegt", "Geaenderte Bilder", die Zeile eines
  // Eintrags und die Detailspalte. Sie bekommt die Liste fertig (Bild-URL,
  // Titel, Stufe, Zeit, Link) und zeichnet nur: eingepasst (contain), Zoom
  // ueber Knoepfe, Mausrad und Doppelklick, Verschieben mit gedrueckter Maus,
  // Blaettern mit den Pfeiltasten. Schliessen per Klick daneben, Esc, Knopf.
  let { bilder = [], start = 0, schliessen } = $props();

  // Rand der Buehne in Pixeln; ein Klick dorthin liegt immer neben dem Bild.
  const RAND = 24;
  const SCHRITT = 1.25;

  let i = $state(0);
  $effect.pre(() => {
    i = Math.min(Math.max(0, start), Math.max(0, bilder.length - 1));
  });
  let b = $derived(bilder[i] || null);

  let bw = $state(0);
  let bh = $state(0);
  let nw = $state(0);
  let nh = $state(0);
  // massstab null = eingepasst; sonst Pixel des Bildes je Pixel am Schirm.
  let massstab = $state(null);
  let dx = $state(0);
  let dy = $state(0);
  let img = $state(null);

  let einpassung = $derived(nw && nh && bw > 2 * RAND && bh > 2 * RAND ? Math.min((bw - 2 * RAND) / nw, (bh - 2 * RAND) / nh) : 1);
  let s = $derived(massstab ?? einpassung);
  let stil = $derived(
    nw && nh
      ? `width:${nw * s}px;height:${nh * s}px;transform:translate(calc(-50% + ${dx}px), calc(-50% + ${dy}px))`
      : 'max-width:calc(100% - 48px);max-height:calc(100% - 48px);transform:translate(-50%, -50%)'
  );

  function zuruecksetzen() {
    massstab = null;
    dx = 0;
    dy = 0;
  }
  function wechseln(n) {
    if (n < 0 || n >= bilder.length || n === i) return;
    i = n;
    nw = 0;
    nh = 0;
    zuruecksetzen();
  }
  function geladen() {
    if (!img) return;
    nw = img.naturalWidth;
    nh = img.naturalHeight;
  }
  $effect(() => {
    // Ein Bild aus dem Speicher kann schon fertig sein, bevor load feuert.
    if (img && b && img.complete && img.naturalWidth) geladen();
  });

  function zoomen(neu, px, py) {
    neu = Math.min(Math.max(neu, 0.05), 40);
    if (px !== undefined && bw) {
      // Um den Zeiger herum: der Punkt unter dem Zeiger bleibt stehen.
      const r = img?.parentElement?.getBoundingClientRect();
      if (r) {
        const ox = px - (r.left + r.width / 2);
        const oy = py - (r.top + r.height / 2);
        const f = neu / s;
        dx = ox - f * (ox - dx);
        dy = oy - f * (oy - dy);
      }
    }
    massstab = neu;
  }
  function rein() {
    zoomen(s * SCHRITT);
  }
  function raus() {
    zoomen(s / SCHRITT);
  }
  function hundert() {
    massstab = 1;
    dx = 0;
    dy = 0;
  }
  function rad(ev) {
    ev.preventDefault();
    zoomen(ev.deltaY < 0 ? s * 1.15 : s / 1.15, ev.clientX, ev.clientY);
  }
  function doppelklick(ev) {
    ev.preventDefault();
    if (massstab === null) hundert();
    else zuruecksetzen();
  }

  let ziehen = $state(null);
  let gezogen = false;
  function runter(ev) {
    if (massstab === null || ev.button !== 0) return;
    ev.preventDefault();
    ziehen = { x: ev.clientX, y: ev.clientY, dx, dy };
    gezogen = false;
    try {
      ev.currentTarget.setPointerCapture(ev.pointerId);
    } catch {
      // Ein synthetischer Zeiger laesst sich nicht fangen; dann eben ohne.
    }
  }
  function bewegen(ev) {
    if (!ziehen) return;
    const x = ev.clientX - ziehen.x;
    const y = ev.clientY - ziehen.y;
    if (Math.abs(x) + Math.abs(y) > 3) gezogen = true;
    dx = ziehen.dx + x;
    dy = ziehen.dy + y;
  }
  function hoch() {
    ziehen = null;
  }

  function rand(ev) {
    // Nur ein Klick auf den Grund schliesst, nicht auf Bild oder Leiste —
    // und nicht das Loslassen nach dem Verschieben.
    if (gezogen) {
      gezogen = false;
      return;
    }
    if (ev.target === ev.currentTarget) schliessen?.();
  }

  function taste(ev) {
    if (ev.key === 'Escape') {
      ev.preventDefault();
      ev.stopPropagation();
      schliessen?.();
    } else if (ev.key === 'ArrowRight') {
      ev.preventDefault();
      wechseln(i + 1);
    } else if (ev.key === 'ArrowLeft') {
      ev.preventDefault();
      wechseln(i - 1);
    } else if (ev.key === '+' || ev.key === '=') {
      rein();
    } else if (ev.key === '-') {
      raus();
    }
  }

  const zeit = (iso) => String(iso || '').slice(0, 16).replace('T', ' ');
  let meta = $derived(b ? [b.stufe, zeit(b.zeit), b.notiz].filter(Boolean).join(' · ') : '');
  let prozent = $derived(Math.round(s * 100));
</script>

<svelte:window onkeydown={taste} />

{#if b}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="kn-modal" role="dialog" aria-modal="true" aria-label={b.titel || 'Bild'} tabindex="-1" onclick={rand}>
    <div class="kn-modal-leiste">
      <div class="kn-modal-text">
        <span class="kn-modal-titel" title={b.titel}>{b.titel}</span>
        <span class="kn-modal-meta">{meta}</span>
      </div>
      <div class="kn-modal-knoepfe">
        {#if bilder.length > 1}<span class="kn-modal-zahl">{i + 1} / {bilder.length}</span>{/if}
        <button type="button" data-zoom="raus" title="Verkleinern" aria-label="Verkleinern" onclick={raus}>−</button>
        <span class="kn-modal-prozent">{prozent} %</span>
        <button type="button" data-zoom="rein" title="Vergrößern" aria-label="Vergrößern" onclick={rein}>+</button>
        <button type="button" data-zoom="einpassen" class:kn-modal-an={massstab === null} onclick={zuruecksetzen}>Einpassen</button>
        <button type="button" data-zoom="100" class:kn-modal-an={massstab === 1} onclick={hundert}>100 %</button>
        <a class="kn-modal-original" href={b.link || b.bild} target="_blank" rel="noopener">Original öffnen</a>
        <button type="button" class="kn-modal-zu" title="Schließen (Esc)" aria-label="Schließen" onclick={() => schliessen?.()}>×</button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="kn-modal-buehne" class:kn-modal-zoom={massstab !== null} class:kn-modal-zieht={!!ziehen}
         bind:clientWidth={bw} bind:clientHeight={bh}
         onclick={rand} onwheel={rad} onpointermove={bewegen} onpointerup={hoch} onpointercancel={hoch}>
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <img class="kn-modal-bild" bind:this={img} src={b.bild} alt={b.titel || 'Bild'} style={stil} draggable="false"
           onload={geladen} ondblclick={doppelklick} onpointerdown={runter} />
    </div>
    {#if bilder.length > 1}
      <button type="button" class="kn-modal-pfeil kn-modal-vor" aria-label="Voriges Bild" disabled={i === 0} onclick={() => wechseln(i - 1)}>‹</button>
      <button type="button" class="kn-modal-pfeil kn-modal-nach" aria-label="Nächstes Bild" disabled={i === bilder.length - 1} onclick={() => wechseln(i + 1)}>›</button>
    {/if}
  </div>
{/if}

<style>
  .kn-modal {
    position: fixed;
    inset: 0;
    z-index: 1000;
    display: flex;
    flex-direction: column;
    background: rgba(12, 14, 18, 0.86);
    color: #eef0f3;
    outline: none;
  }
  .kn-modal-leiste {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.45);
    font-size: 13px;
  }
  .kn-modal-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .kn-modal-titel {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-modal-meta {
    font-size: 12px;
    color: #b8bec8;
  }
  .kn-modal-knoepfe {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }
  .kn-modal-knoepfe button {
    min-width: 30px;
    height: 28px;
    padding: 0 9px;
    border: 1px solid rgba(255, 255, 255, 0.25);
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.08);
    color: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .kn-modal-knoepfe button:hover {
    background: rgba(255, 255, 255, 0.18);
  }
  .kn-modal-knoepfe .kn-modal-an {
    border-color: #7fb2ff;
    color: #cfe0ff;
  }
  .kn-modal-prozent,
  .kn-modal-zahl {
    min-width: 44px;
    text-align: center;
    color: #b8bec8;
    font-variant-numeric: tabular-nums;
  }
  .kn-modal-original {
    margin: 0 6px;
    color: #9cc4ff;
    text-decoration: none;
  }
  .kn-modal-original:hover {
    text-decoration: underline;
  }
  .kn-modal-knoepfe .kn-modal-zu {
    font-size: 18px;
    line-height: 1;
  }
  .kn-modal-buehne {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    padding: 24px;
    cursor: zoom-out;
    touch-action: none;
  }
  .kn-modal-bild {
    position: absolute;
    left: 50%;
    top: 50%;
    max-width: none;
    user-select: none;
    cursor: zoom-in;
    box-shadow: 0 6px 30px rgba(0, 0, 0, 0.5);
    background: #fff;
  }
  .kn-modal-zoom .kn-modal-bild {
    cursor: grab;
  }
  .kn-modal-zieht .kn-modal-bild {
    cursor: grabbing;
  }
  .kn-modal-pfeil {
    position: absolute;
    top: 50%;
    width: 40px;
    height: 64px;
    margin-top: -32px;
    border: 0;
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.35);
    color: #fff;
    font-size: 30px;
    cursor: pointer;
  }
  .kn-modal-pfeil:disabled {
    opacity: 0.25;
    cursor: default;
  }
  .kn-modal-vor {
    left: 10px;
  }
  .kn-modal-nach {
    right: 10px;
  }
</style>

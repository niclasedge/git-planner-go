<script>
  // Eine Zeile der Arbeitsliste. Ein Klick auf den Titel klappt sie wie eine
  // Aufgabe in Things zur Karte auf: darin die Aufgaben des Changes als
  // Subtasks und die Belegbilder. Der Details-Knopf oeffnet den vollen Inhalt
  // rechts in der dritten Spalte.
  import { status as statusText, datum } from './status.js';
  import { zeile } from './markdown.js';
  import PRAktionen from './PRAktionen.svelte';
  import Faellig from './Faellig.svelte';
  import Schritt from './Schritt.svelte';
  import Lebenslauf from './Lebenslauf.svelte';
  import BildModal from './BildModal.svelte';

  let { e, id, offen = false, umschalten, details, detailAktiv = false, umhaken = null, prAktion = null, repo = '' } = $props();

  let arbeitet = $state(false);
  async function haken() {
    if (!umhaken || arbeitet) return;
    arbeitet = true;
    try {
      await umhaken(e);
    } finally {
      arbeitet = false;
    }
  }

  let s = $derived(statusText(e.status));
  let c = $derived(e.change);
  let erledigt = $derived(e.status === 'erledigt');
  let bilder = $derived((c?.belege || []).filter((b) => b.bild));
  // Bildansicht im selben Fenster (beleg-bilder, #105): Index des offenen Bildes.
  let bildOffen = $state(-1);
  let modalBilder = $derived(bilder.map((b) => ({ bild: b.bild, titel: (c?.kennung || '') + ' · ' + b.pfad, stufe: b.stufe, zeit: b.zeit, link: b.link })));
  let pr = $derived(e.art === 'pr');
  // Konfliktzustand, gerechnet in Go (konflikte-anzeigen, #62).
  const zustandText = { sauber: 'sauber', veraltet: 'veraltet', konflikt: 'Konflikt', unbekannt: 'wird geprüft' };
  const zustandFarbe = { sauber: '--t-gruen', veraltet: '--t-blau', konflikt: '--t-rot' };
  let titel = $derived(e.art === 'change' ? (c?.kennung || e.titel) : e.titel);
  // Einruecktiefe unter dem Blocker; gerechnet in Go (liste-abhaengigkeiten, #48).
  let tiefe = $derived(Number(e.tiefe) > 0 ? Number(e.tiefe) : 0);
  // Verknuepfte Branches und das Flag aktiv; beides kommt fertig aus Go
  // (issue-branches, #50).
  let branches = $derived(e.branches || []);
  // Letzte Aenderung aus Issue, PR oder Branch; Quelle und Text kommen
  // fertig aus Go (naechste-geaendert, #118).
  let g = $derived(e.zuletzt_geaendert);
</script>

<div class="kn-e" class:kn-e-offen={offen} class:kn-e-erledigt={erledigt} class:kn-e-gewaehlt={detailAktiv}
     class:kn-e-eingerueckt={tiefe > 0} style="--tiefe: {tiefe}"
     data-eintrag={id} data-status={e.status} data-tiefe={tiefe} data-unter={e.unter || undefined}>
  <div class="kn-e-zeile">
    {#if umhaken && e.art === 'issue'}
      <button type="button" class="kn-e-check kn-e-klickbar" class:kn-e-arbeitet={arbeitet} role="checkbox"
              aria-checked={erledigt ? 'true' : 'false'} title={erledigt ? 'Wieder öffnen' : 'Schließen'} onclick={haken}>
        {#if erledigt}
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 5.2 4.2 7.4 8.2 2.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        {/if}
      </button>
    {:else}
      <span class="kn-e-check" role="checkbox" tabindex="-1" aria-checked={erledigt ? 'true' : 'false'} aria-disabled="true">
        {#if erledigt}
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 5.2 4.2 7.4 8.2 2.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        {/if}
      </span>
    {/if}
    <button type="button" class="kn-e-titel" aria-expanded={offen} onclick={umschalten}>
      <span class="kn-e-text">{titel}</span>
      {#if e.hat_text}
        <svg class="kn-e-symbol" width="11" height="12" viewBox="0 0 11 12" aria-label="hat Text"><path d="M1.5 1h5.2L9.5 3.8V11h-8z M6.5 1v3h3" fill="none" stroke="currentColor" stroke-width="1" /></svg>
      {/if}
      {#if c && c.gesamt}
        <svg class="kn-e-symbol" width="12" height="12" viewBox="0 0 12 12" aria-label="hat Aufgaben"><path d="M1 3h2M1 6h2M1 9h2M5 3h6M5 6h6M5 9h6" stroke="currentColor" stroke-width="1" /></svg>
      {/if}
      {#if c}
        <span class="kn-e-beleg" class:kn-e-beleg-da={bilder.length > 0} data-beleg={bilder.length ? 'ja' : 'nein'}
              title={bilder.length ? bilder.length + ' Bildbeleg(e)' : 'kein Bildbeleg'}>
          <svg width="13" height="12" viewBox="0 0 13 12" aria-hidden="true">
            <rect x="1" y="1.5" width="11" height="9" rx="1.5" fill="none" stroke="currentColor" stroke-width="1" />
            <circle cx="4.3" cy="4.4" r="1" fill="currentColor" />
            <path d="M1.5 9.5 5 6.5l2 1.6L9 6l2.5 2.6" fill="none" stroke="currentColor" stroke-width="1" />
            {#if !bilder.length}<path d="M1 11.5 12 .5" stroke="currentColor" stroke-width="1.1" />{/if}
          </svg>
        </span>
      {/if}
    </button>
    <span class="kn-e-rechts">
      {#if e.lebenslauf}<Lebenslauf lauf={e.lebenslauf} />{/if}
      {#if e.faellig && !erledigt}<Faellig faellig={e.faellig} termin={e.termin} tage={e.tage} />{/if}
      {#each e.labels || [] as l (l.name)}
        <span class="kn-e-label" style="--l: #{l.farbe || '8a8f98'}">{l.name}</span>
      {/each}
      {#if pr}
        {#if e.draft}<span class="kn-e-status" style="color: var(--t-dim); border-color: var(--t-rahmen)">Entwurf</span>{/if}
        {#if e.zustand}<span class="kn-e-status kn-e-zustand" data-zustand={e.zustand} style="color: var({zustandFarbe[e.zustand] || '--t-dim'}); border-color: var({zustandFarbe[e.zustand] || '--t-rahmen'})">{zustandText[e.zustand] || e.zustand}</span>
        {:else if e.mergebar === true}<span class="kn-e-status" style="color: var(--t-gruen); border-color: var(--t-gruen)">mergebar</span>
        {:else if e.mergebar === false}<span class="kn-e-status" style="color: var(--t-rot); border-color: var(--t-rot)">Konflikt</span>
        {:else if e.head}<span class="kn-e-status" style="color: var(--t-dim); border-color: var(--t-rahmen)">wird geprüft</span>{/if}
        {#if e.auto_merge}<span class="kn-e-status kn-e-automerge" data-auto-merge={e.auto_merge.methode} title={'Auto-Merge (' + e.auto_merge.methode + ')'} style="color: var(--t-gruen); border-color: var(--t-gruen)">Auto-Merge</span>{/if}
        {#if e.checks && e.checks.marke}<span class="kn-e-checks kn-e-checks-{e.checks.gesamt}" data-checks={e.checks.gesamt} title={e.checks.zusammenfassung?.text}>{e.checks.marke}&nbsp;{e.checks.zahl}</span>{/if}
        {#if e.reviews && e.reviews.marke}<span class="kn-e-reviews kn-e-reviews-{e.reviews.entscheidung}" data-reviews={e.reviews.entscheidung} title={e.reviews.titel}>{e.reviews.marke}</span>{/if}
        {#if e.reviews && e.reviews.offene_threads}<span class="kn-e-threads" title={e.reviews.titel}>{e.reviews.offene_threads}</span>{/if}
      {/if}
      {#if branches.length && !erledigt}
        <span class="kn-e-branches" class:kn-e-branches-aktiv={e.aktiv} data-branches={branches.length}
              data-aktiv={e.aktiv ? 'ja' : 'nein'}
              title={branches.map((b) => b.name).join(', ') + (e.aktiv ? ' · aktiv' : '')}>
          <svg width="10" height="12" viewBox="0 0 10 12" aria-hidden="true">
            <circle cx="2.5" cy="2.2" r="1.3" fill="none" stroke="currentColor" stroke-width="1" />
            <circle cx="2.5" cy="9.8" r="1.3" fill="none" stroke="currentColor" stroke-width="1" />
            <circle cx="7.5" cy="3.8" r="1.3" fill="none" stroke="currentColor" stroke-width="1" />
            <path d="M2.5 3.5v5M7.5 5.1c0 2.2-5 1.6-5 3.4" fill="none" stroke="currentColor" stroke-width="1" />
          </svg>
          {branches.length}{#if e.aktiv}&nbsp;aktiv{/if}
        </span>
      {/if}
      {#if e.art !== 'pr' && e.status && !erledigt}
        <span class="kn-e-status" style="color: {s.farbe}; border-color: {s.farbe}">
          {s.text}{#if e.blockiert_durch?.length}&nbsp;durch {e.blockiert_durch.map((n) => '#' + n).join(', ')}{/if}{#if e.grund}<span class="kn-e-grund">&nbsp;· {e.grund}</span>{/if}
        </span>
      {/if}
      {#if erledigt && e.geschlossen_text}<span class="kn-e-datum" title={e.geschlossen_voll || undefined}>{e.geschlossen_text}{#if e.gemergt_mit}&nbsp;· gemergt mit <a class="kn-e-gemergt" href={e.gemergt_mit.url} target="_blank" rel="noopener noreferrer" title={e.gemergt_mit.titel}>#{e.gemergt_mit.nummer}</a>{/if}</span>{/if}
      {#if g && g.text}<span class="kn-e-datum kn-e-geaendert" data-quelle={g.quelle} title={g.voll || undefined}>geändert {g.text}{#if g.quelle === 'pr' && g.pr}&nbsp;· PR #{g.pr}{:else if g.quelle === 'branch'}&nbsp;· Branch{/if}</span>{/if}
      {#if e.nummer}<span class="kn-e-nr">#{e.nummer}</span>{/if}
      <button type="button" class="kn-e-details" title="Details" aria-label="Details" onclick={details}>
        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><rect x="1" y="1.5" width="12" height="11" rx="2" fill="none" stroke="currentColor" stroke-width="1.1" /><path d="M8.5 1.5v11" stroke="currentColor" stroke-width="1.1" /></svg>
      </button>
    </span>
  </div>
  {#if pr && e.head && !offen}
    <div class="kn-e-meta">{[e.head, e.autor ? 'von ' + e.autor : ''].filter(Boolean).join(' · ')}</div>
  {/if}
  {#if c && !offen}
    <div class="kn-e-meta">
      {[c.kennung, c.gesamt ? c.erledigt + '/' + c.gesamt : '', c.ref && c.ref.startsWith('change/') ? c.ref : ''].filter(Boolean).join(' · ')}
    </div>
  {/if}

  {#if offen}
    <div class="kn-e-karte">
      {#if c}
        <div class="kn-e-kopf">
          <span>Change <b>{c.kennung}</b></span>
          {#if c.ref}<span class="kn-e-ref">{c.ref}</span>{/if}
          {#each c.gates || [] as g (g)}<span class="kn-e-gate">{g}</span>{/each}
        </div>
        <Schritt schritt={c.schritt} />
        {#if c.aufgaben?.length}
          <ul class="kn-e-subtasks">
            {#each c.aufgaben as a, i (i)}
              <li>
                <div class="kn-e-sub" class:kn-e-sub-erledigt={a.erledigt} data-subtask role="checkbox" tabindex="-1"
                     aria-checked={a.erledigt ? 'true' : 'false'} aria-disabled="true">
                  <span class="kn-e-kreis" aria-hidden="true">
                    {#if a.erledigt}<svg width="8" height="8" viewBox="0 0 10 10"><path d="M2 5.2 4.2 7.4 8.2 2.8" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" /></svg>{/if}
                  </span>
                  <span class="kn-e-subtext">{@html zeile(a.text)}</span>
                </div>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="kn-e-leer">Der Change hat noch keine Aufgaben.</p>
        {/if}
        {#if bilder.length}
          <div class="kn-e-belege">
            {#each bilder as b, n (b.bild)}
              <button type="button" data-bild-oeffnen onclick={() => (bildOffen = n)} title={b.stufe + ' · ' + b.pfad}>
                <img src={b.bild} alt={'Beleg ' + b.stufe} loading="lazy" />
                <span>{b.stufe}</span>
              </button>
            {/each}
          </div>
          {#if bildOffen >= 0}
            <BildModal bilder={modalBilder} start={bildOffen} schliessen={() => (bildOffen = -1)} />
          {/if}
        {/if}
      {:else if pr}
        {#if prAktion}<PRAktionen {e} {repo} aktion={(name, felder) => prAktion(e, name, felder)} />{/if}
      {:else if e.art === 'issue'}
        <p class="kn-e-leer">Zu diesem Issue gibt es noch keinen Change.</p>
      {/if}
      <div class="kn-e-fuss">
        {#if e.autor}<span>von {e.autor}</span>{/if}
        {#if e.zustaendig?.length}<span>{'· zuständig ' + e.zustaendig.join(', ')}</span>{/if}
        {#if g}
          {#if g.issue_text}<span title={g.issue_voll || undefined}>{'· Issue geändert ' + g.issue_text}</span>{/if}
          {#if g.pr && g.pr_text}<span title={g.pr_voll || undefined}>{'· PR #' + g.pr + ' geändert ' + g.pr_text}</span>{/if}
        {:else if e.aktualisiert}<span>{'· geändert ' + datum(e.aktualisiert)}</span>{/if}
        {#if e.url}<a href={e.url} target="_blank" rel="noopener">Auf GitHub ↗</a>{/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .kn-e {
    --einzug: calc(var(--tiefe, 0) * 22px);
    border-radius: 8px;
    transition: background 0.12s, box-shadow 0.15s, margin 0.15s;
    margin-left: var(--einzug);
  }
  /* Fuehrungslinie: vom Blocker herunter und quer zur Checkbox. */
  .kn-e-eingerueckt {
    position: relative;
  }
  .kn-e-eingerueckt::before {
    content: '';
    position: absolute;
    left: -13px;
    top: -2px;
    height: 19px;
    width: 12px;
    border-left: 1.5px solid var(--t-rahmen);
    border-bottom: 1.5px solid var(--t-rahmen);
    border-bottom-left-radius: 6px;
    pointer-events: none;
  }
  .kn-e-zeile {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 10px;
    min-height: 34px;
    padding: 0 8px;
  }
  .kn-e:not(.kn-e-offen):hover {
    background: var(--t-hover);
  }
  .kn-e-gewaehlt:not(.kn-e-offen) {
    background: var(--t-auswahl);
  }
  .kn-e-offen {
    background: var(--t-karte);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06), 0 6px 22px rgba(0, 0, 0, 0.09);
    margin: 10px -6px 14px calc(var(--einzug) - 6px);
    padding: 4px 6px 8px;
  }
  .kn-e-check {
    flex: none;
    width: 14px;
    height: 14px;
    border: 1.3px solid var(--t-rahmen);
    border-radius: 4px;
    display: grid;
    place-items: center;
    color: #fff;
    background: var(--t-flaeche);
  }
  .kn-e-klickbar {
    padding: 0;
    cursor: pointer;
  }
  .kn-e-klickbar:hover {
    border-color: var(--t-blau);
  }
  .kn-e-arbeitet {
    opacity: 0.5;
  }
  .kn-e-erledigt .kn-e-check {
    background: var(--t-blau);
    border-color: var(--t-blau);
  }
  /* Der Titel behaelt 160 Pixel; passen die Chips nicht daneben, brechen sie
     in eine eigene Zeile um, statt ihn zu ueberdecken. */
  .kn-e-titel {
    flex: 1 1 160px;
    min-width: 160px;
    display: flex;
    align-items: center;
    gap: 6px;
    border: 0;
    background: transparent;
    padding: 7px 0;
    color: var(--t-text);
    font: inherit;
    font-size: 15px;
    text-align: left;
    cursor: pointer;
  }
  .kn-e-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-e-offen .kn-e-text {
    white-space: normal;
  }
  .kn-e-erledigt .kn-e-text {
    color: var(--t-dim);
  }
  .kn-e-symbol {
    flex: none;
    color: var(--t-dim);
  }
  .kn-e-beleg {
    display: inline-grid;
    place-items: center;
    color: var(--t-rahmen);
  }
  .kn-e-beleg-da {
    color: var(--t-gruen);
  }
  .kn-e-rechts {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    margin-left: auto;
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    align-items: center;
    gap: 4px 6px;
    padding: 4px 0;
  }
  .kn-e-label {
    font-size: 11px;
    padding: 1px 7px;
    border-radius: 10px;
    border: 1px solid color-mix(in srgb, var(--l) 70%, transparent);
    color: color-mix(in srgb, var(--l) 75%, var(--t-text));
    background: color-mix(in srgb, var(--l) 10%, transparent);
    white-space: nowrap;
  }
  .kn-e-status {
    font-size: 11px;
    padding: 1px 7px;
    border: 1px solid;
    border-radius: 10px;
    white-space: nowrap;
  }
  .kn-e-checks {
    font-size: 11px;
    font-weight: 600;
    padding: 1px 7px;
    border: 1px solid;
    border-radius: 10px;
    white-space: nowrap;
  }
  .kn-e-checks-gruen {
    color: var(--t-gruen);
  }
  .kn-e-checks-rot {
    color: var(--t-rot);
  }
  .kn-e-checks-laeuft {
    color: #b8860b;
  }
  /* Reviews je PR (pr-reviews, #81). */
  .kn-e-reviews,
  .kn-e-threads {
    font-size: 11px;
    font-weight: 600;
    padding: 1px 7px;
    border: 1px solid;
    border-radius: 10px;
    white-space: nowrap;
  }
  .kn-e-reviews-genehmigt {
    color: var(--t-gruen);
  }
  .kn-e-reviews-aenderungen {
    color: var(--t-rot);
  }
  .kn-e-threads {
    color: var(--t-text2);
    border-color: var(--t-rahmen);
  }
  .kn-e-threads::before {
    content: 'Threads ';
    font-weight: 400;
  }
  .kn-e-branches {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 11px;
    padding: 1px 7px;
    border: 1px solid var(--t-rahmen);
    border-radius: 10px;
    color: var(--t-dim);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .kn-e-branches-aktiv {
    color: var(--t-gruen);
    border-color: var(--t-gruen);
    background: color-mix(in srgb, var(--t-gruen) 10%, transparent);
    font-weight: 600;
  }
  .kn-e-nr,
  .kn-e-datum {
    font-size: 12px;
    color: var(--t-dim);
    font-variant-numeric: tabular-nums;
  }
  .kn-e-gemergt {
    color: inherit;
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .kn-e-gemergt:hover {
    color: var(--t-blau);
  }
  .kn-e-details {
    border: 0;
    background: transparent;
    color: var(--t-dim);
    padding: 4px;
    border-radius: 5px;
    cursor: pointer;
    display: grid;
    place-items: center;
    opacity: 0.55;
  }
  .kn-e:hover .kn-e-details,
  .kn-e-gewaehlt .kn-e-details {
    opacity: 1;
  }
  .kn-e-details:hover {
    background: var(--t-hover);
    color: var(--t-blau);
  }
  .kn-e-meta {
    margin: -6px 0 0 32px;
    padding-bottom: 6px;
    font-size: 11.5px;
    color: var(--t-dim);
  }
  .kn-e-karte {
    padding: 2px 10px 4px 32px;
  }
  .kn-e-kopf {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    color: var(--t-dim);
    margin-bottom: 6px;
  }
  .kn-e-ref,
  .kn-e-gate {
    font-size: 11px;
    padding: 0 6px;
    border-radius: 4px;
    background: var(--t-hover);
  }
  .kn-e-subtasks {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .kn-e-sub {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
    font-size: 14px;
  }
  .kn-e-subtasks li {
    border-top: 1px solid var(--t-linie);
  }
  .kn-e-subtasks li:last-child {
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-e-kreis {
    flex: none;
    width: 12px;
    height: 12px;
    border: 1.6px solid var(--t-blau);
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: #fff;
  }
  .kn-e-sub-erledigt .kn-e-kreis {
    background: var(--t-blau);
  }
  .kn-e-sub-erledigt {
    color: var(--t-dim);
  }
  .kn-e-subtext :global(code) {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 12.5px;
    background: var(--t-hover);
    padding: 0 4px;
    border-radius: 4px;
  }
  @media (max-width: 600px) {
    .kn-e-zeile {
      flex-wrap: wrap;
      row-gap: 0;
    }
    .kn-e-titel {
      flex-basis: calc(100% - 30px);
    }
    .kn-e-text {
      white-space: normal;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      -webkit-box-orient: vertical;
    }
    .kn-e-rechts {
      width: 100%;
      padding: 0 0 6px 24px;
      flex-wrap: wrap;
    }
    .kn-e-label {
      display: none;
    }
    .kn-e-details {
      margin-left: auto;
    }
    .kn-e-meta {
      margin-top: -4px;
    }
    .kn-e-karte {
      padding-left: 12px;
    }
  }
  .kn-e-belege {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 12px;
  }
  .kn-e-belege button {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--t-dim);
    text-align: left;
    cursor: zoom-in;
  }
  .kn-e-belege img {
    width: 150px;
    height: 94px;
    object-fit: cover;
    object-position: top left;
    border-radius: 6px;
    border: 1px solid var(--t-linie);
    background: var(--t-flaeche);
  }
  .kn-e-leer {
    font-size: 13px;
    color: var(--t-dim);
    margin: 4px 0;
  }
  .kn-e-fuss {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 12px;
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-e-fuss a {
    margin-left: auto;
    color: var(--t-blau);
    text-decoration: none;
  }
</style>

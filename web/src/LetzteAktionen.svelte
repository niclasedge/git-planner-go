<script>
  // "Letzte Aktionen" als Zeitleiste (letzte-aktionen, #126;
  // aktionen-zeitleiste, #128): Pushes samt Commits, Merges, Branches, CI-Runs,
  // geschlossene PRs, Bilder, HTML-Dateien, Belege aus allen Branches und die
  // geschlossenen Issues, mit Tageskopf und Uhrzeit. Auswahl, Folge,
  // Zusammenfassung, Tag, Zeit- und Tooltip-Texte kommen fertig aus Go
  // (internal/appctx/letzteaktionen.go); hier wird nur gezeichnet.
  import BildModal from './BildModal.svelte';

  // umhaken oeffnet ein geschlossenes Issue wieder, wie das Kaestchen eines
  // erledigten Eintrags (issue-verwaltung, #26).
  let { aktionen = [], oeffnen = null, umhaken = null } = $props();
  let arbeitet = $state(0);
  async function wiederOeffnen(a) {
    if (!umhaken || arbeitet) return;
    arbeitet = a.nummer;
    try {
      await umhaken({ art: 'issue', nummer: a.nummer, status: 'erledigt' });
    } finally {
      arbeitet = 0;
    }
  }

  // Ein Zeichen je Art; der CI-Run zeigt stattdessen seinen Punkt.
  const SYMBOL = { merge: '⤵', push: '↑', force_push: '⇑', branch_neu: '⑂', branch_weg: '✕', pr: '⇄', bild: '▣', html: '◧', beleg: '◆', issue: '✓' };
  const MIT_COMMIT = { merge: true, push: true, force_push: true };
  // Arten, deren Branch als Chip mit "auf" steht; die Branch-Zeilen nennen ihn ohne.
  const MIT_AUF = { merge: true, push: true, force_push: true, ci: true, pr: true, bild: true, html: true, beleg: true };
  const BRANCH_ZEILE = { branch_neu: true, branch_weg: true };

  // Zuerst zehn Zeilen; der Knopf zeigt alle aus den letzten 24 Stunden als
  // Zeitstrahl mit Stunden- und Pausenmarken (aktionen-24h, #132).
  const ZUERST = 10;
  let alle = $state(false);
  let sichtbar = $derived(alle ? aktionen : aktionen.slice(0, ZUERST));

  // Die Bilder des Stroms fuer die Bildansicht, in der Folge des Stroms.
  let bilder = $derived(
    aktionen
      .filter((a) => a.bild && (a.art === 'bild' || a.art === 'beleg'))
      .map((a) => ({ bild: a.bild, titel: a.art === 'beleg' ? a.kennung + ' · ' + a.name : a.pfad || a.name,
        stufe: a.stufe || '', zeit: a.zeit, notiz: a.betreff || '', link: a.link || '' }))
  );
  let ansicht = $state(null);
  function bildOeffnen(a) {
    const start = bilder.findIndex((b) => b.bild === a.bild);
    if (start >= 0) ansicht = { start };
  }
</script>

<div class="kn-la" data-letzte-aktionen>
  {#each sichtbar as a, i (a.art + '|' + a.zeit + '|' + i)}
    {#if a.tag && (i === 0 || sichtbar[i - 1].tag !== a.tag)}
      <div class="kn-la-tag" data-tag>{a.tag}</div>
    {/if}
    {#if alle && a.pause}
      <div class="kn-la-marke kn-la-pause" data-pause>{a.pause}</div>
    {/if}
    {#if alle && a.stunde && (i === 0 || sichtbar[i - 1].stunde !== a.stunde || sichtbar[i - 1].tag !== a.tag)}
      <div class="kn-la-marke kn-la-stunde" data-stunde>{a.stunde}</div>
    {/if}
    <div class="kn-la-eintrag">
      <div class="kn-la-zeile" data-aktion={a.art} data-juengste={a.juengste ? '' : undefined} title={a.titel}
           data-eintrag={a.art === 'issue' ? String(a.nummer) : undefined}>
        <span class="kn-la-knoten" data-punkt-art={a.art} data-punkt={a.art === 'ci' ? a.punkt : undefined} aria-hidden="true"></span>
        <span class="kn-la-symbol" aria-hidden="true">
          {#if a.art === 'ci'}<span class="kn-la-punkt" data-punkt={a.punkt}>●</span>{:else}{SYMBOL[a.art] || '•'}{/if}
        </span>
        <span class="kn-la-text">
          <span class="kn-la-zeit">{a.uhr || a.text}</span>
          <span class="kn-la-trenner">·</span>
          <span class="kn-la-wort">{a.wort}</span>
          {#if a.art === 'ci'}
            <a class="kn-la-ci" href="#kn-ci">{a.workflow}</a>
          {:else if a.art === 'pr'}
            <a class="kn-la-prnr" href={a.url} target="_blank" rel="noopener noreferrer">#{a.nummer}</a>
            <span class="kn-la-name">{a.pr_titel}</span>
          {:else if BRANCH_ZEILE[a.art]}
            <span class="kn-la-branch" data-branch-chip>{a.branch}</span>
            {#if a.kurz}<span class="kn-la-trenner">·</span> <code class="kn-la-sha">{a.kurz}</code>{/if}
            {#if a.autor}<span class="kn-la-trenner">·</span> <span class="kn-la-autor">{a.autor}</span>{/if}
          {:else if a.art === 'bild'}
            {#if a.bild}<button type="button" class="kn-la-knopf" data-bild-oeffnen onclick={() => bildOeffnen(a)}>{a.name}</button>{:else}<span class="kn-la-name">{a.name}</span>{/if}
          {:else if a.art === 'html'}
            <a class="kn-la-html" href={a.adresse} target="_blank" rel="noopener">{a.name}</a>
          {:else if a.art === 'beleg'}
            <span class="kn-la-kennung">{a.kennung}</span>
            <span class="kn-la-trenner">·</span>
            {#if a.bild}<button type="button" class="kn-la-knopf" data-bild-oeffnen onclick={() => bildOeffnen(a)}>{a.name}</button>{:else if a.link}<a href={a.link} target="_blank" rel="noopener">{a.name}</a>{:else}<span class="kn-la-name">{a.name}</span>{/if}
          {:else if a.art === 'issue'}
            <button type="button" class="kn-e-check kn-la-check" class:kn-la-arbeitet={arbeitet === a.nummer} role="checkbox"
                    aria-checked="true" disabled={!umhaken} title="Wieder öffnen" onclick={() => wiederOeffnen(a)}>
              <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 5.2 4.2 7.4 8.2 2.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
            </button>
            {#if oeffnen}<button type="button" class="kn-la-knopf" data-issue-oeffnen onclick={() => oeffnen(a)}>#{a.nummer} {a.issue_titel}</button>{:else}<span class="kn-la-name">#{a.nummer} {a.issue_titel}</span>{/if}
            {#if a.gemergt_mit}
              <span class="kn-la-trenner">·</span>
              <span class="kn-la-gemergt">gemergt mit <a href={a.gemergt_mit.url} target="_blank" rel="noopener noreferrer" title={a.gemergt_mit.titel}>#{a.gemergt_mit.nummer}</a></span>
            {/if}
          {/if}
          {#if a.branch && MIT_AUF[a.art]}
            <span class="kn-la-auf">auf</span>
            <span class="kn-la-branch" class:kn-la-standard={a.standard} data-branch-chip>{a.branch}</span>
          {/if}
          {#if MIT_COMMIT[a.art]}
            {#if a.betreff_kurz}<span class="kn-la-trenner">·</span> <span class="kn-la-betreff">„{a.betreff_kurz}“</span>{/if}
            <span class="kn-la-trenner">·</span>
            <code class="kn-la-sha">{a.kurz}</code>
            {#if a.autor}<span class="kn-la-trenner">·</span> <span class="kn-la-autor">{a.autor}</span>{/if}
            {#if a.pr}<span class="kn-la-pr">→ PR <a href={a.pr.url} target="_blank" rel="noopener noreferrer" title={a.pr.titel || undefined}>#{a.pr.nummer}</a></span>{/if}
          {/if}
        </span>
      </div>
      {#if a.commits?.length}
        <div class="kn-la-commits">
          {#each a.commits as c (c.sha)}
            <div class="kn-la-commit" data-commit title={c.sha}><code>{c.kurz}</code> {c.betreff + (c.autor ? ' · ' + c.autor : '')}</div>
          {/each}
          {#if a.commit_zahl > a.commits.length}
            <div class="kn-la-commit kn-la-commits-mehr" data-commits-mehr>+ {a.commit_zahl - a.commits.length} {a.commit_zahl - a.commits.length === 1 ? 'weiterer Commit' : 'weitere Commits'}</div>
          {/if}
        </div>
      {/if}
    </div>
  {/each}
  {#if !alle && aktionen.length > ZUERST}
    <button type="button" class="kn-la-mehr" data-mehr onclick={() => (alle = true)}>{aktionen.length - ZUERST} weitere aus 24 Std.</button>
  {:else if alle}
    <button type="button" class="kn-la-mehr" data-weniger onclick={() => (alle = false)}>weniger</button>
  {/if}
</div>

{#if ansicht && bilder.length}
  <BildModal {bilder} start={ansicht.start} schliessen={() => (ansicht = null)} />
{/if}

<style>
  .kn-la {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 6px 0 4px;
    padding-left: 20px;
  }
  /* Die Linie der Zeitleiste, an ihr ein Knoten je Zeile. */
  .kn-la::before {
    content: '';
    position: absolute;
    left: 7px;
    top: 4px;
    bottom: 4px;
    width: 2px;
    border-radius: 1px;
    background: var(--t-linie);
  }
  .kn-la-tag {
    position: relative;
    margin: 10px 0 2px -20px;
    padding-left: 20px;
    font-size: 11.5px;
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--t-text2);
    background: var(--t-bg, transparent);
  }
  .kn-la-tag:first-child {
    margin-top: 0;
  }
  .kn-la-eintrag {
    position: relative;
  }
  /* Marken des Zeitstrahls: die Stunde als Strich mit Uhrzeit auf der Linie,
     die Pause gestrichelt. */
  .kn-la-marke {
    position: relative;
    margin: 6px 0 2px;
    padding-left: 10px;
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: var(--t-dim);
  }
  .kn-la-marke::before {
    content: '';
    position: absolute;
    left: -16px;
    top: 50%;
    width: 14px;
    border-top: 2px solid var(--t-linie);
  }
  .kn-la-stunde {
    font-weight: 600;
    color: var(--t-text2);
  }
  .kn-la-pause {
    font-style: italic;
  }
  .kn-la-pause::before {
    border-top-style: dashed;
  }
  .kn-la-knoten {
    position: absolute;
    left: -18px;
    top: 10px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--t-grau);
    box-shadow: 0 0 0 2px var(--t-bg, #fff);
  }
  .kn-la-knoten[data-punkt-art='merge'],
  .kn-la-knoten[data-punkt-art='pr'] {
    background: var(--t-lila);
  }
  .kn-la-knoten[data-punkt-art='push'] {
    background: var(--t-blau);
  }
  .kn-la-knoten[data-punkt-art='force_push'],
  .kn-la-knoten[data-punkt='rot'] {
    background: var(--t-rot);
  }
  .kn-la-knoten[data-punkt-art='branch_neu'],
  .kn-la-knoten[data-punkt-art='issue'],
  .kn-la-knoten[data-punkt='gruen'] {
    background: var(--t-gruen);
  }
  .kn-la-knoten[data-punkt='laeuft'] {
    background: var(--t-gelb, #f5c400);
  }
  .kn-la-knoten[data-punkt-art='bild'],
  .kn-la-knoten[data-punkt-art='html'],
  .kn-la-knoten[data-punkt-art='beleg'] {
    background: var(--t-orange, #e8912d);
  }
  .kn-la-commits {
    margin: 0 0 4px 34px;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .kn-la-commit {
    font-size: 12px;
    color: var(--t-text2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kn-la-commit code {
    font-size: 11.5px;
    color: var(--t-dim);
  }
  .kn-la-commits-mehr {
    color: var(--t-dim);
  }
  .kn-la-mehr {
    align-self: flex-start;
    margin: 4px 0 0 10px;
    border: 0;
    background: transparent;
    padding: 2px 0;
    font: inherit;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--t-blau);
    cursor: pointer;
  }
  .kn-la-mehr:hover {
    text-decoration: underline;
  }
  .kn-la-zeile {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px;
    border-radius: 8px;
    border: 1px solid transparent;
    font-size: 13px;
    color: var(--t-text);
    white-space: nowrap;
    position: relative;
    min-width: 0;
  }
  .kn-la-zeile[data-juengste] {
    background: color-mix(in srgb, var(--t-blau) 11%, transparent);
    border-color: color-mix(in srgb, var(--t-blau) 28%, transparent);
  }
  .kn-la-symbol {
    flex: 0 0 16px;
    text-align: center;
    color: var(--t-dim);
    font-size: 13px;
  }
  .kn-la-zeile[data-juengste] .kn-la-symbol {
    color: var(--t-blau);
  }
  .kn-la-punkt {
    font-size: 11px;
    color: var(--t-grau);
  }
  .kn-la-punkt[data-punkt='gruen'] {
    color: var(--t-gruen);
  }
  .kn-la-punkt[data-punkt='rot'] {
    color: var(--t-rot);
  }
  .kn-la-punkt[data-punkt='laeuft'] {
    color: var(--t-gelb, #f5c400);
  }
  .kn-la-text {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
  }
  .kn-la-zeit {
    color: var(--t-text2);
    font-variant-numeric: tabular-nums;
  }
  .kn-la-wort {
    font-weight: 600;
  }
  .kn-la-trenner,
  .kn-la-auf {
    color: var(--t-dim);
  }
  .kn-la-branch {
    padding: 1px 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--t-lila) 14%, transparent);
    color: var(--t-lila);
    font-size: 12px;
    font-weight: 500;
  }
  .kn-la-branch.kn-la-standard {
    background: var(--t-hover);
    color: var(--t-text2);
  }
  .kn-la-betreff {
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .kn-la-sha {
    font-size: 12px;
    color: var(--t-text2);
  }
  .kn-la-autor,
  .kn-la-kennung,
  .kn-la-gemergt,
  .kn-la-pr {
    color: var(--t-text2);
  }
  .kn-la a,
  .kn-la-knopf {
    color: var(--t-blau);
    text-decoration: none;
    font-weight: 600;
  }
  .kn-la-knopf {
    border: 0;
    background: transparent;
    padding: 0;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .kn-la a:hover,
  .kn-la-knopf:hover {
    text-decoration: underline;
  }
  .kn-la-check {
    flex: none;
    width: 14px;
    height: 14px;
    padding: 0;
    border: 1.3px solid var(--t-blau);
    border-radius: 4px;
    display: grid;
    place-items: center;
    color: #fff;
    background: var(--t-blau);
    cursor: pointer;
  }
  .kn-la-check:disabled {
    cursor: default;
  }
  .kn-la-arbeitet {
    opacity: 0.5;
  }
</style>

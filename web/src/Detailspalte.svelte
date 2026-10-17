<script>
  // Die dritte Spalte: der volle Inhalt eines Eintrags. Ein Issue zeigt seinen
  // Text, ein Change seine Dateien (proposal, tasks, design, Delta-Specs) und
  // seine Belegbilder. Nach Art aufgebaut, damit eine weitere Art (etwa ein
  // Bead) nur einen weiteren Zweig braucht.
  import { untrack } from 'svelte';
  import { holen, mitQuery, issueSchreiben, prSchreiben } from './api.js';
  import KonfliktLoesen from './KonfliktLoesen.svelte';
  import PRChecks from './PRChecks.svelte';
  import PRReviews from './PRReviews.svelte';
  import IssueBearbeiten from './IssueBearbeiten.svelte';
  import Zeitleiste from './Zeitleiste.svelte';
  import Schritt from './Schritt.svelte';
  import DateienAbgleich from './DateienAbgleich.svelte';
  import AnlegenAktion from './AnlegenAktion.svelte';
  import Lebenslauf from './Lebenslauf.svelte';
  import BildModal from './BildModal.svelte';
  import { markdown } from './markdown.js';

  let { repo, ziel, schliessen, wechseln, meta = null, geaendert = () => {} } = $props();

  let stand = $state(0);

  let inhalt = $state(null);
  let fehler = $state('');

  $effect(() => {
    const z = ziel;
    const r = repo;
    stand;
    untrack(() => {
      if (!inhalt || inhalt.art !== z?.ansicht || (z?.ansicht === 'issue' && inhalt.nummer !== z?.nummer)) inhalt = null;
    });
    fehler = '';
    if (!z || !r) return;
    const url = z.ansicht === 'change'
      ? mitQuery('/api/detail', { repo: r, art: 'change', kennung: z.kennung })
      : mitQuery('/api/detail', { repo: r, art: 'issue', nummer: z.nummer });
    let aktuell = true;
    holen(url).then(
      (d) => { if (aktuell) inhalt = d; },
      (e) => { if (aktuell) fehler = e.message; },
    );
    return () => { aktuell = false; };
  });

  async function schreiben(aktion, felder) {
    await issueSchreiben(aktion, { repo, nummer: ziel.nummer, ...felder });
    stand++;
    geaendert();
  }

  // Konflikt je Datei loesen (konflikte-loesen, #75); danach laedt die
  // Liste und mit ihr der PR-Zustand neu.
  async function konfliktLoesen(nummer, felder) {
    const antwort = await prSchreiben('konflikt-loesen', { repo, nummer, ...felder });
    geaendert();
    return antwort;
  }

  // Fehlgeschlagene Pruefung neu starten (pr-checks, #80); danach laedt die
  // Liste und mit ihr der Stand der Pruefungen neu.
  async function checksNeu(nummer, id) {
    const antwort = await prSchreiben('checks-neu', { repo, nummer, check_run_id: id });
    geaendert();
    return antwort;
  }

  // Reviewer, Antworten, Threads (pr-reviews, #81); danach laedt die Liste
  // und mit ihr ziel.reviews neu (App.detailAuffrischen).
  async function reviewsSchreiben(nummer, aktion, felder) {
    const antwort = await prSchreiben(aktion, { repo, nummer, ...felder });
    geaendert();
    return antwort;
  }

  // Nachsorge nach dem Merge (pr-lebenslauf, #83): nur vorhandene Routen;
  // danach laedt die Liste und mit ihr der Lebenslauf neu.
  async function nachsorgen(p) {
    if (p.art === 'issue_schliessen') await issueSchreiben('aendern', { repo, nummer: p.nummer, zustand: 'closed' });
    else if (p.art === 'branch_loeschen') await prSchreiben('branch-loeschen', { repo, nummer: p.nummer });
    else return;
    geaendert();
  }

  let abschnitte = $derived.by(() => {
    if (!inhalt) return [];
    if (inhalt.art === 'change') {
      const out = [];
      if (inhalt.proposal) out.push({ titel: 'Vorschlag', text: inhalt.proposal });
      if (inhalt.tasks) out.push({ titel: 'Aufgaben', text: inhalt.tasks });
      if (inhalt.design) out.push({ titel: 'Design', text: inhalt.design });
      for (const s of inhalt.specs || []) out.push({ titel: 'Spec · ' + s.capability, text: s.text });
      return out;
    }
    return inhalt.text ? [{ titel: '', text: inhalt.text }] : [];
  });
  let bilder = $derived((inhalt?.belege || []).filter((b) => b.bild));
  // Bildansicht im selben Fenster (beleg-bilder, #105): Index des offenen Bildes.
  let bildOffen = $state(-1);
  let modalBilder = $derived(bilder.map((b) => ({ bild: b.bild, titel: (inhalt?.kennung || ziel?.kennung || '') + ' · ' + b.pfad, stufe: b.stufe, zeit: b.zeit, link: b.link })));
  // Die verknuepften Branches kommen mit dem Eintrag aus /api/arbeit, fertig
  // zugeordnet und sortiert (issue-branches, #50).
  let branches = $derived(ziel?.branches || []);
  // Konfliktzustand des PRs, gerechnet in Go (konflikte-anzeigen, #62).
  const zustandText = { sauber: 'sauber', veraltet: 'veraltet', konflikt: 'Konflikt', unbekannt: 'wird geprüft' };
  function zeitpunkt(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d)) return '';
    return d.toLocaleString('de-DE', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  }
</script>

<aside class="kn-detail" aria-label="Details">
  <div class="kn-d-kopf">
    <div>
      <div class="kn-d-art">{[ziel?.ansicht === 'change' ? 'Change ' + ziel.kennung : ziel?.pr ? 'PR' : 'Issue', ziel?.nummer ? '#' + ziel.nummer : '', inhalt?.ref || ''].filter(Boolean).join(' · ')}</div>
      <h2>{inhalt?.titel || ziel?.titel || ''}</h2>
    </div>
    <button type="button" class="kn-d-zu" aria-label="Schließen" onclick={schliessen}>
      <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
    </button>
  </div>
  {#if ziel?.nummer && ziel?.kennung}
    <div class="kn-d-reiter" role="tablist">
      <button type="button" role="tab" aria-selected={ziel.ansicht === 'change'} onclick={() => wechseln('change')}>Change</button>
      <button type="button" role="tab" aria-selected={ziel.ansicht === 'issue'} onclick={() => wechseln('issue')}>Issue</button>
    </div>
  {/if}
  <div class="kn-detail-inhalt">
    {#if ziel?.lebenslauf}
      <h3>Lebenslauf</h3>
      <Lebenslauf lauf={ziel.lebenslauf} gross={true} {nachsorgen} />
    {/if}
    <Schritt schritt={ziel?.schritt} />
    {#if ziel?.nummer && ziel?.anlegen}
      <AnlegenAktion vorschlag={ziel.anlegen} {repo} nummer={ziel.nummer} {geaendert} />
    {/if}
    {#if ziel?.checks}
      <h3>Prüfungen</h3>
      <PRChecks checks={ziel.checks} neuStarten={(id) => checksNeu(ziel.nummer, id)} />
    {/if}
    {#if ziel?.reviews}
      <h3>Reviews</h3>
      <PRReviews reviews={ziel.reviews} zustaendige={meta?.zustaendige || []} schreiben={(aktion, felder) => reviewsSchreiben(ziel.nummer, aktion, felder)} />
    {/if}
    {#if branches.length}
      <h3>Branches</h3>
      <ul class="kn-d-branches">
        {#each branches as b (b.name)}
          <li data-branch={b.name} data-aktiv={b.aktiv ? 'ja' : 'nein'} class:kn-d-branch-aktiv={b.aktiv}>
            <div class="kn-d-branch-kopf">
              <code>{b.name}</code>
              {#if b.aktiv}<span class="kn-d-aktiv">aktiv</span>{/if}
              <span class="kn-d-vz" title="voraus / zurück gegen den Default-Branch">↑{b.voraus} ↓{b.zurueck}</span>
            </div>
            <div class="kn-d-branch-commit">
              {#if b.kurz_sha}<code>{b.kurz_sha}</code>{/if}
              {#if b.betreff}<span>{b.betreff}</span>{/if}
              {#if b.zeit}<span class="kn-d-zeit">{zeitpunkt(b.zeit)}</span>{/if}
            </div>
            {#if b.pr}
              <div class="kn-d-branch-pr">
                <a class="kn-d-link" href={b.pr.url} target="_blank" rel="noopener">PR #{b.pr.nummer}{b.pr.draft ? ' (Entwurf)' : ''} ↗</a>
                {#if b.pr.zustand}<span class="kn-d-zustand kn-d-zustand-{b.pr.zustand}" data-zustand={b.pr.zustand}>{zustandText[b.pr.zustand] || b.pr.zustand}</span>{/if}
              </div>
              {#if b.pr.zustand === 'konflikt' && (b.pr.konflikt_dateien || []).length}
                <ul class="kn-d-dateien">
                  {#each b.pr.konflikt_dateien as d (d)}<li><code>{d}</code></li>{/each}
                </ul>
              {/if}
              {#if b.pr.zustand === 'konflikt'}
                <KonfliktLoesen {repo} nummer={b.pr.nummer} loesen={(felder) => konfliktLoesen(b.pr.nummer, felder)} />
              {/if}
              {#if b.pr.checks}<PRChecks checks={b.pr.checks} neuStarten={(id) => checksNeu(b.pr.nummer, id)} />{/if}
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
    {#if fehler}
      <p class="kn-d-fehler" title={fehler}>{fehler}</p>
    {:else if !inhalt}
      <p class="kn-d-leer">Lade …</p>
    {:else if inhalt.grund}
      <p class="kn-d-fehler" title={inhalt.grund}>{inhalt.grund}</p>
    {:else if inhalt.art === 'issue'}
      <IssueBearbeiten {inhalt} {meta} {schreiben} />
      {#if inhalt.url}<p><a class="kn-d-link" href={inhalt.url} target="_blank" rel="noopener">Auf GitHub öffnen ↗</a></p>{/if}
    {:else}
      <Zeitleiste {repo} kennung={inhalt.kennung} />
      {#if inhalt.abgleich}<DateienAbgleich abgleich={inhalt.abgleich} />{/if}
      {#each abschnitte as a, i (i)}
        {#if a.titel}<h3>{a.titel}</h3>{/if}
        <div class="kn-d-md">{@html markdown(a.text)}</div>
      {:else}
        <p class="kn-d-leer">Kein Text.</p>
      {/each}
      {#if bilder.length}
        <h3>Belege</h3>
        {#each bilder as b, n (b.bild)}
          <figure>
            <button type="button" class="kn-d-bild" data-bild-oeffnen onclick={() => (bildOffen = n)} title={b.stufe + ' · ' + b.pfad}><img src={b.bild} alt={'Beleg ' + b.stufe} loading="lazy" /></button>
            <figcaption>{b.stufe} · {b.pfad}</figcaption>
          </figure>
        {/each}
        {#if bildOffen >= 0}
          <BildModal bilder={modalBilder} start={bildOffen} schliessen={() => (bildOffen = -1)} />
        {/if}
      {/if}
      {#if inhalt.url}<p><a class="kn-d-link" href={inhalt.url} target="_blank" rel="noopener">Auf GitHub öffnen ↗</a></p>{/if}
    {/if}
  </div>
</aside>

<style>
  .kn-d-branch-pr {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .kn-d-zustand {
    font-size: 11px;
    padding: 0 6px;
    border-radius: 4px;
    border: 1px solid var(--t-rahmen);
    color: var(--t-dim);
  }
  .kn-d-zustand-sauber {
    color: var(--t-gruen);
    border-color: var(--t-gruen);
  }
  .kn-d-zustand-veraltet {
    color: var(--t-blau);
    border-color: var(--t-blau);
  }
  .kn-d-zustand-konflikt {
    color: var(--t-rot);
    border-color: var(--t-rot);
  }
  .kn-d-dateien {
    margin: 2px 0 0;
    padding-left: 18px;
    font-size: 12px;
    color: var(--t-text2);
  }
  .kn-d-branches {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
  }
  .kn-d-branches > li {
    border-top: 1px solid var(--t-linie);
    padding: 6px 0;
    font-size: 12.5px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .kn-d-branches > li:last-child {
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-d-branch-kopf,
  .kn-d-branch-commit {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .kn-d-branch-commit {
    color: var(--t-dim);
  }
  .kn-d-branches code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 12px;
  }
  .kn-d-aktiv {
    font-size: 11px;
    padding: 0 6px;
    border-radius: 10px;
    color: var(--t-gruen);
    border: 1px solid var(--t-gruen);
  }
  .kn-d-vz {
    margin-left: auto;
    color: var(--t-dim);
    font-variant-numeric: tabular-nums;
  }
  .kn-detail {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--t-flaeche);
    border-left: 1px solid var(--t-linie);
    min-width: 0;
  }
  .kn-d-kopf {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
    padding: 22px 22px 12px;
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-d-reiter {
    display: flex;
    gap: 4px;
    padding: 8px 22px 0;
  }
  .kn-d-reiter button {
    border: 0;
    background: transparent;
    font: inherit;
    font-size: 12.5px;
    color: var(--t-dim);
    padding: 3px 10px;
    border-radius: 10px;
    cursor: pointer;
  }
  .kn-d-reiter button[aria-selected='true'] {
    background: var(--t-auswahl);
    color: var(--t-text);
  }
  .kn-d-art {
    font-size: 12px;
    color: var(--t-dim);
  }
  h2 {
    margin: 4px 0 0;
    font-size: 19px;
    font-weight: 700;
    color: var(--t-text);
  }
  .kn-d-zu {
    border: 0;
    background: transparent;
    color: var(--t-dim);
    border-radius: 6px;
    padding: 6px;
    cursor: pointer;
  }
  .kn-d-zu:hover {
    background: var(--t-hover);
  }
  .kn-detail-inhalt {
    overflow-y: auto;
    padding: 6px 22px 30px;
    font-size: 14px;
    line-height: 1.55;
    color: var(--t-text);
  }
  h3 {
    margin: 22px 0 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--t-blau);
  }
  :global(.kn-d-md h1),
  :global(.kn-d-md h2),
  :global(.kn-d-md h3) {
    font-size: 15px;
    margin: 14px 0 4px;
    color: var(--t-text);
  }
  :global(.kn-d-md code) {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 12.5px;
    background: var(--t-hover);
    padding: 1px 4px;
    border-radius: 4px;
  }
  :global(.kn-d-md pre) {
    background: var(--t-hover);
    padding: 10px;
    border-radius: 6px;
    overflow-x: auto;
  }
  :global(.kn-d-md pre code) {
    background: none;
    padding: 0;
  }
  :global(.kn-d-md ul) {
    padding-left: 20px;
  }
  :global(.kn-d-md a),
  .kn-d-link {
    color: var(--t-blau);
  }
  :global(.kn-d-md img) {
    max-width: 100%;
  }
  :global(.kn-d-md table) {
    border-collapse: collapse;
    font-size: 13px;
  }
  :global(.kn-d-md td),
  :global(.kn-d-md th) {
    border: 1px solid var(--t-linie);
    padding: 3px 6px;
  }
  .kn-d-bild {
    display: block;
    width: 100%;
    padding: 0;
    border: 0;
    background: none;
    cursor: zoom-in;
  }
  figure {
    margin: 10px 0;
  }
  figure img {
    max-width: 100%;
    border-radius: 6px;
    border: 1px solid var(--t-linie);
  }
  figcaption {
    font-size: 11px;
    color: var(--t-dim);
  }
  .kn-d-leer {
    color: var(--t-dim);
  }
  .kn-d-fehler {
    color: var(--t-rot);
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
  }
</style>

<script>
  // Die Mitte: ein Repo wie ein Projekt in Things. Oben die Karte als eine
  // Zeile und "Letzte Aktionen" als Zeitstrom aus Pushes, Merges, CI, Erzeugtem
  // und geschlossenen Issues (letzte-aktionen, #126), darunter die offene Arbeit in der Folge, in der
  // Go sie liefert (blocked_by, laufende zuerst), dann Changes ohne Issue und
  // offene Pull Requests. Die Seite ordnet nichts um. Unter "Als Nächstes"
  // sind die Abhaengigen eines Blockers eingeklappt; die Zeile darunter zaehlt
  // sie (aktionen-zeitleiste, #128), gezaehlt wird in Go.
  import Kreis from './Kreis.svelte';
  import Eintrag from './Eintrag.svelte';
  import NeuesIssue from './NeuesIssue.svelte';
  import Karte from './Karte.svelte';
  import ArchivListe from './ArchivListe.svelte';
  import BelegGalerie from './BelegGalerie.svelte';
  import GeschlossenePRs from './GeschlossenePRs.svelte';
  import BranchAufraeumen from './BranchAufraeumen.svelte';
  import CIRuns from './CIRuns.svelte';
  import LetzteAktionen from './LetzteAktionen.svelte';

  let { repo, arbeit, laedt = false, fehler = '', offen, umschalten, details, detailId = '', einstellungen, umhaken = null, meta = null, anlegen = null, prAktion = null } = $props();

  let anteil = $derived(arbeit?.fortschritt?.gesamt ? arbeit.fortschritt.erledigt / arbeit.fortschritt.gesamt : 0);
  let hinweise = $derived((arbeit?.quellen || []).filter((q) => q.grund));
  let prs = $derived(
    (arbeit?.prs || []).map((p) => ({ ...p, art: 'pr', status: '', blockiert_durch: [], hat_text: false,
      // Der Lebenslauf je PR kommt fertig aus Go (pr-lebenslauf, #83).
      lebenslauf: arbeit?.pr_laeufe?.[p.nummer] || null })),
  );

  // Die umgeschalteten Blocker. Jeder beginnt eingeklappt, ein erledigter
  // Blocker aufgeklappt (abhaengige.aufgeklappt, reihe-erledigte-blocker,
  // #130); auch nach dem Wechsel des Repos. Verborgen ist, wessen vorfahren
  // einen eingeklappten nennt.
  let umgeschaltet = $state([]);
  let letztesRepo = '';
  $effect(() => {
    const nwo = repo?.nwo || '';
    if (nwo !== letztesRepo) {
      letztesRepo = nwo;
      umgeschaltet = [];
    }
  });
  let vorgabe = $derived(new Set((arbeit?.reihe || []).filter((e) => e.abhaengige?.aufgeklappt).map((e) => e.nummer)));
  function istOffen(nr) {
    return vorgabe.has(nr) !== umgeschaltet.includes(nr);
  }
  let reihe = $derived((arbeit?.reihe || []).filter((e) => !(e.vorfahren || []).some((v) => !istOffen(v))));
  function abhaengigeUmschalten(nr) {
    umgeschaltet = umgeschaltet.includes(nr) ? umgeschaltet.filter((x) => x !== nr) : [...umgeschaltet, nr];
  }

  function idVon(e) {
    if (e.art === 'pr') return 'pr-' + e.nummer;
    if (e.art === 'change') return e.change?.kennung || e.titel;
    return String(e.nummer);
  }
</script>

<header class="kn-a-kopf">
  <div class="kn-a-titel">
    <Kreis {anteil} groesse={24} />
    <h1>{repo?.name || repo?.nwo}</h1>
    <button type="button" class="kn-einstellungen-knopf" title="Settings und Details des Repos" aria-label="Settings" onclick={einstellungen}>
      <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true"><circle cx="4" cy="9" r="1.4" /><circle cx="9" cy="9" r="1.4" /><circle cx="14" cy="9" r="1.4" /></svg>
    </button>
  </div>
  {#if repo?.beschreibung}<p class="kn-a-notiz">{repo.beschreibung}</p>{/if}
  {#if arbeit?.fortschritt?.gesamt}
    <p class="kn-a-stand">{arbeit.fortschritt.erledigt} von {arbeit.fortschritt.gesamt} Aufgaben in offenen Changes erledigt</p>
  {/if}
</header>

{#if fehler}
  <p class="kn-a-fehler" title={fehler}>{fehler}</p>
{:else if laedt && !arbeit}
  <p class="kn-a-laedt">Lade Issues, Changes und Belege …</p>
{/if}

{#each hinweise as h (h.quelle)}
  <p class="kn-a-hinweis" title={h.grund}><b>{h.quelle}</b> nicht verfügbar · {h.grund}</p>
{/each}

{#if arbeit}
  {#snippet eintrag(e)}
    <Eintrag {e} id={idVon(e)} offen={offen.includes(idVon(e))}
             umschalten={() => umschalten(idVon(e))} details={() => details(e, idVon(e))}
             detailAktiv={detailId === idVon(e)} {umhaken} {prAktion} repo={repo?.nwo || ''} />
  {/snippet}
  {#snippet abschnitt(titel, eintraege, leer, plus = false)}
    <section class="kn-a-abschnitt">
      <h2>{titel}</h2>
      {#each eintraege as e (idVon(e))}
        {@render eintrag(e)}
      {:else}
        {#if leer}<p class="kn-a-leer">{leer}</p>{/if}
      {/each}
      {#if plus && anlegen}<NeuesIssue {meta} {anlegen} />{/if}
    </section>
  {/snippet}

  <Karte repo={repo.nwo} kopf={arbeit.karte_kopf} oeffnen={(z) => details({ art: 'issue', nummer: z.nummer, titel: z.titel }, String(z.nummer))} />

  {#if arbeit.letzte_aktionen}
    <!-- Der Zeitstrom kommt fertig aus Go; letzter_commit wird nicht mehr gezeichnet. -->
    {#if arbeit.letzte_aktionen.length}
      <section class="kn-a-abschnitt">
        <h2>Letzte Aktionen</h2>
        <LetzteAktionen aktionen={arbeit.letzte_aktionen} {umhaken}
                        oeffnen={(a) => details({ art: 'issue', nummer: a.nummer, titel: a.issue_titel }, String(a.nummer))} />
      </section>
    {/if}
  {:else if arbeit.zuletzt?.length}
    <!-- Eine aeltere Antwort ohne letzte_aktionen: die Eintraege aus zuletzt. -->
    {@render abschnitt('Letzte Aktionen', arbeit.zuletzt, '')}
  {/if}
  {#if arbeit.archiv?.length}
    <ArchivListe archiv={arbeit.archiv} />
  {/if}
  <section class="kn-a-abschnitt">
    <h2>Als Nächstes</h2>
    {#each reihe as e (idVon(e))}
      {@render eintrag(e)}
      {#if e.abhaengige}
        {#if e.abhaengige.kinder > 0}
          <button type="button" class="kn-a-abh kn-a-abh-knopf" data-abhaengige={String(e.nummer)} style="--tiefe: {(Number(e.tiefe) || 0) + 1}"
                  aria-expanded={istOffen(e.nummer) ? 'true' : 'false'} onclick={() => abhaengigeUmschalten(e.nummer)}>
            <span class="kn-a-abh-pfeil" aria-hidden="true">{istOffen(e.nummer) ? '▾' : '▸'}</span> {e.abhaengige.text}
          </button>
        {:else}
          <span class="kn-a-abh" data-abhaengige={String(e.nummer)} style="--tiefe: {(Number(e.tiefe) || 0) + 1}">{e.abhaengige.text}</span>
        {/if}
      {/if}
    {:else}
      <p class="kn-a-leer">Keine offenen Issues.</p>
    {/each}
    {#if anlegen}<NeuesIssue {meta} {anlegen} />{/if}
  </section>
  {#if arbeit.ohne_issue?.length}
    {@render abschnitt('Changes ohne Issue', arbeit.ohne_issue, '')}
  {/if}
  {#if prs.length}
    {@render abschnitt('Pull Requests', prs, '')}
  {/if}
  <GeschlossenePRs repo={repo.nwo} {prAktion} />
  <CIRuns repo={repo.nwo} />

  <BelegGalerie repo={repo.nwo} />
  <BranchAufraeumen repo={repo.nwo} />
{/if}

<style>
  .kn-a-kopf {
    margin-bottom: 18px;
  }
  .kn-a-titel {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  h1 {
    margin: 0;
    font-size: 26px;
    font-weight: 700;
    letter-spacing: -0.01em;
    color: var(--t-text);
  }
  .kn-einstellungen-knopf {
    border: 0;
    background: transparent;
    color: var(--t-dim);
    fill: currentColor;
    border-radius: 6px;
    padding: 3px 4px;
    cursor: pointer;
    display: grid;
    place-items: center;
  }
  .kn-einstellungen-knopf:hover {
    background: var(--t-hover);
    color: var(--t-text);
  }
  .kn-a-notiz {
    margin: 10px 0 0 36px;
    font-size: 15px;
    line-height: 1.45;
    color: var(--t-text2);
  }
  .kn-a-stand {
    margin: 6px 0 0 36px;
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-a-abschnitt {
    margin-top: 28px;
  }
  h2 {
    margin: 0 0 4px;
    padding: 0 8px 6px;
    font-size: 15px;
    font-weight: 600;
    color: var(--t-blau);
    border-bottom: 1px solid var(--t-linie);
  }
  .kn-a-laedt {
    color: var(--t-dim);
    font-size: 14px;
    padding: 6px 8px;
  }
  /* Die Zeile unter einem Blocker, eingerueckt wie seine Kinder. */
  .kn-a-abh {
    display: block;
    margin: 0 0 2px calc(var(--tiefe, 1) * 22px + 30px);
    padding: 1px 6px;
    font-size: 12px;
    color: var(--t-dim);
    text-align: left;
  }
  .kn-a-abh-knopf {
    border: 0;
    background: transparent;
    font: inherit;
    font-size: 12px;
    border-radius: 6px;
    cursor: pointer;
  }
  .kn-a-abh-knopf:hover {
    background: var(--t-hover);
    color: var(--t-text2);
  }
  .kn-a-abh-pfeil {
    display: inline-block;
    width: 1em;
    color: var(--t-text2);
  }
  .kn-a-leer {
    color: var(--t-dim);
    font-size: 14px;
    padding: 6px 8px;
    margin: 0;
  }
  .kn-a-fehler,
  .kn-a-hinweis {
    margin: 6px 0;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 12px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kn-a-fehler {
    color: var(--t-rot);
    background: color-mix(in srgb, var(--t-rot) 8%, transparent);
  }
  .kn-a-hinweis {
    color: var(--t-dim);
    background: var(--t-hover);
  }
</style>

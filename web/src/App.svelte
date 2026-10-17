<script>
  // Die Things-Oberflaeche (repo-leiste, #25): links die Repos in Kategorien,
  // in der Mitte die Arbeitsliste des gewaehlten Repos, rechts auf Wunsch die
  // Detailspalte. Oben in der Leiste die Ansichten Heute und Irgendwann
  // (faelligkeit, #39) und das Logbuch (heute-logbuch, #42) ueber alle Repos. Die Seite rechnet nicht:
  // Gruppierung, Reihenfolge, Status und Zahlen kommen fertig aus Go
  // (/api/repos, /api/arbeit, /api/detail, /api/uebersicht).
  //
  // Der Bundle-Marker steht als Attribut am Wurzelknoten: die Routen- und
  // E2E-Tests der Bauschiene (gkp-a1k) suchen genau dieses Literal im Bundle.
  import { onMount } from 'svelte';
  import RepoLeiste from './RepoLeiste.svelte';
  import Arbeitsliste from './Arbeitsliste.svelte';
  import Detailspalte from './Detailspalte.svelte';
  import Einstellungen from './Einstellungen.svelte';
  import SyncAnzeige from './SyncAnzeige.svelte';
  import Uebersicht from './Uebersicht.svelte';
  import Logbuch from './Logbuch.svelte';
  import { holen, senden, mitQuery, issueSchreiben, prSchreiben } from './api.js';
  import { gewaehltesRepo, repoMerken } from './klappzustand.js';
  import { wahlLesen, naechste, anwenden, merken, systemFolgen, NAMEN } from './thema.js';

  const marker = 'gkp-a1k Svelte-Bauschiene';

  let leiste = $state(null);
  let leisteFehler = $state('');
  let gewaehlt = $state('');
  let arbeit = $state(null);
  let arbeitFehler = $state('');
  let laedt = $state(false);
  let offen = $state([]);
  let detail = $state(null);
  let settingsOffen = $state(false);
  let settingsFehler = $state('');
  let leisteSichtbar = $state(false);
  let themaWahl = $state('auto');
  let repoStand = $state(null);
  let meta = $state(null);
  let metaFuer = '';
  // Die Ansicht in der Mitte: '' ist das gewaehlte Repo, sonst heute,
  // irgendwann oder logbuch.
  let ansicht = $state('');
  let uebersicht = $state(null);
  let uebersichtFehler = $state('');
  let uebersichtAbdruck = '';
  // Fenster von Heute und Logbuch in Tagen; Mehr laden vergroessert sie
  // (heute-logbuch, #42).
  let uebersichtTage = 30;
  let logbuch = $state(null);
  let logbuchFehler = $state('');
  let logbuchTage = 14;
  // Laufende Anfragen dieser Seite (sync-status, #38) und der zuletzt
  // gezeichnete Inhalt: eine Antwort ohne Aenderung zeichnet nicht neu.
  let unterwegs = $state(0);
  let arbeitAbdruck = '';
  let leisteAbdruck = '';

  function abdruck(v) {
    return JSON.stringify({ ...v, sync: null, stand: null });
  }

  async function mitZaehler(f) {
    unterwegs++;
    try {
      return await f();
    } finally {
      unterwegs--;
    }
  }

  // Der juengste Sync der sichtbaren Quellen, fuer die Kopfzeile.
  let syncZeit = $derived([leiste?.sync?.zeit, arbeit?.sync?.zeit, uebersicht?.sync?.zeit, ansicht === 'logbuch' ? logbuch?.sync?.zeit : ''].filter(Boolean).sort().pop() || '');
  let syncLaeuft = $derived(unterwegs > 0 || !!leiste?.sync?.laeuft || !!arbeit?.sync?.laeuft || !!uebersicht?.sync?.laeuft);
  let syncFehler = $derived(leiste?.sync?.fehler || arbeit?.sync?.fehler || arbeitFehler || '');

  let repo = $derived.by(() => {
    for (const k of leiste?.kategorien || []) {
      for (const r of k.repos || []) if (r.nwo === gewaehlt) return r;
    }
    return null;
  });
  let kategorieDesRepos = $derived.by(() => {
    for (const k of leiste?.kategorien || []) {
      if (!k.ohne && (k.repos || []).some((r) => r.nwo === gewaehlt)) return k.name;
    }
    return '';
  });

  async function leisteLaden(frisch) {
    try {
      const l = await mitZaehler(() => holen(frisch ? '/api/repos?frisch=1' : '/api/repos'));
      const a = abdruck(l);
      // Nur neu zeichnen, wenn sich etwas geaendert hat; der Sync-Stand wird
      // immer uebernommen.
      if (a !== leisteAbdruck || !leiste) {
        leiste = l;
        leisteAbdruck = a;
      } else {
        leiste.sync = l.sync;
      }
      leisteFehler = '';
      if (l.sync?.laeuft) nachfragen(() => leisteLaden(false));
    } catch (e) {
      leisteFehler = 'Leiste nicht geladen: ' + e.message;
    }
  }

  // Eine Antwort mit laeuft: true heisst, der Dienst erneuert gerade im
  // Hintergrund — einmal kurz danach nachfragen.
  const nachfragenOffen = new Set();
  function nachfragen(f) {
    if (nachfragenOffen.has(f.toString())) return;
    nachfragenOffen.add(f.toString());
    setTimeout(() => {
      nachfragenOffen.delete(f.toString());
      f();
    }, 2500);
  }

  // Still aktualisieren: Auswahl, Klappzustand und Detail bleiben.
  async function arbeitAktualisieren(frisch) {
    const nwo = gewaehlt;
    if (!nwo) return;
    try {
      const a = await mitZaehler(() => holen(mitQuery('/api/arbeit', { repo: nwo, frisch: frisch ? 1 : '' })));
      if (gewaehlt !== nwo) return;
      const neu = abdruck(a);
      if (neu !== arbeitAbdruck || !arbeit) {
        arbeit = a;
        arbeitAbdruck = neu;
      } else {
        arbeit.sync = a.sync;
      }
      arbeitFehler = '';
      if (a.sync?.laeuft) nachfragen(() => arbeitAktualisieren(false));
    } catch (e) {
      if (gewaehlt === nwo) arbeitFehler = e.message;
    }
  }

  // Heute, Demnaechst, Irgendwann: die Zaehler in der Leiste brauchen die
  // Uebersicht immer, nicht nur, wenn eine der Ansichten offen ist.
  async function uebersichtLaden(frisch) {
    try {
      const u = await mitZaehler(() =>
        holen(mitQuery('/api/uebersicht', { tage: uebersichtTage !== 30 ? uebersichtTage : '', frisch: frisch ? 1 : '' })),
      );
      const a = abdruck(u);
      if (a !== uebersichtAbdruck || !uebersicht) {
        uebersicht = u;
        uebersichtAbdruck = a;
      } else {
        uebersicht.sync = u.sync;
      }
      uebersichtFehler = '';
      if (u.sync?.laeuft) nachfragen(() => uebersichtLaden(false));
    } catch (e) {
      uebersichtFehler = e.message;
    }
  }

  async function logbuchLaden(frisch) {
    try {
      const l = await mitZaehler(() =>
        holen(mitQuery('/api/logbuch', { tage: logbuchTage !== 14 ? logbuchTage : '', frisch: frisch ? 1 : '' })),
      );
      logbuch = l;
      logbuchFehler = '';
      if (l.sync?.laeuft) nachfragen(() => logbuchLaden(false));
    } catch (e) {
      logbuchFehler = e.message;
    }
  }

  async function uebersichtMehr() {
    uebersichtTage += 30;
    await uebersichtLaden(false);
  }

  async function logbuchMehr() {
    logbuchTage += 14;
    await logbuchLaden(false);
  }

  function ansichtWaehlen(art) {
    ansicht = art;
    detail = null;
    leisteSichtbar = false;
    if (art === 'logbuch') logbuchLaden(false);
    else uebersichtLaden(false);
  }

  function jetztSynchronisieren() {
    leisteLaden(true);
    arbeitAktualisieren(true);
    uebersichtLaden(true);
    standLaden(true);
    if (ansicht === 'logbuch') logbuchLaden(true);
  }

  async function metaLaden(nwo) {
    metaFuer = nwo;
    meta = null;
    try {
      const m = await holen(mitQuery('/api/repo-meta', { repo: nwo }));
      if (metaFuer === nwo) meta = m;
    } catch {
      /* ohne Labels bleibt nur die Auswahl leer */
    }
  }

  // Schliessen oder wieder oeffnen per Checkbox (issue-verwaltung, #26).
  async function umhaken(e) {
    const nwo = gewaehlt;
    try {
      await issueSchreiben('aendern', { repo: nwo, nummer: e.nummer, zustand: e.status === 'erledigt' ? 'open' : 'closed' });
    } catch (err) {
      arbeitFehler = err.message;
      return;
    }
    uebersichtLaden(false);
    standLaden(false);
    await waehlen(nwo, true);
  }

  // Schliessen aus Heute, Demnaechst oder Irgendwann: das Repo steht an der
  // Zeile, nicht in der Auswahl.
  async function uHaken(e) {
    try {
      await issueSchreiben('aendern', { repo: e.repo, nummer: e.nummer, zustand: 'closed' });
    } catch (err) {
      uebersichtFehler = err.message;
      return;
    }
    if (e.repo === gewaehlt) arbeit = null;
    standLaden(false);
    await uebersichtLaden(true);
  }

  function uDetails(e, id) {
    if (detail && detail.id === id) {
      detail = null;
      return;
    }
    if (metaFuer !== e.repo) metaLaden(e.repo);
    detail = { id, titel: e.titel, nummer: e.nummer, kennung: '', ansicht: 'issue', repo: e.repo };
  }

  // Nach jedem Schreiben in der Detailspalte: Liste oder Ansicht und die
  // Zaehler neu.
  function detailGeaendert() {
    if (ansicht === 'logbuch') logbuchLaden(true);
    uebersichtLaden(!!ansicht);
    standLaden(false);
    if (!ansicht && gewaehlt) waehlen(gewaehlt, true);
  }

  // Freigeben, Mergen, Branch loeschen (pr-aktionen, #32). Die Antwort geht
  // an die Karte zurueck (etwa "Branch geloescht"), danach laedt die Liste.
  async function prAktion(e, name, felder) {
    const nwo = gewaehlt;
    const antwort = await prSchreiben(name, { repo: nwo, nummer: e.nummer, ...felder });
    await waehlen(nwo, true);
    return antwort;
  }

  async function issueAnlegen(felder) {
    const nwo = gewaehlt;
    await issueSchreiben('anlegen', { repo: nwo, ...felder });
    uebersichtLaden(false);
    standLaden(false);
    await waehlen(nwo, true);
  }

  async function waehlen(nwo, frisch) {
    if (!frisch || nwo !== gewaehlt || metaFuer !== nwo) metaLaden(nwo);
    if (ansicht) {
      ansicht = '';
      detail = null;
    }
    gewaehlt = nwo;
    repoMerken(nwo);
    leisteSichtbar = false;
    if (!frisch) {
      arbeit = null;
      offen = [];
      detail = null;
    }
    arbeitFehler = '';
    laedt = true;
    try {
      const a = await mitZaehler(() => holen(mitQuery('/api/arbeit', { repo: nwo, frisch: frisch ? 1 : '' })));
      if (gewaehlt === nwo) {
        arbeit = a;
        arbeitAbdruck = abdruck(a);
        if (frisch) detailAuffrischen(a, nwo);
        if (a.sync?.laeuft) nachfragen(() => arbeitAktualisieren(false));
      }
    } catch (e) {
      if (gewaehlt === nwo) arbeitFehler = e.message;
    } finally {
      if (gewaehlt === nwo) laedt = false;
    }
  }

  async function einklappen(name, zu) {
    try {
      await senden('/action/task/bereich/einklappen', { bereich: name, eingeklappt: zu });
      await leisteLaden();
    } catch (e) {
      // Ohne Schreibrecht (Netz-Adresse) klappt die Kategorie trotzdem, nur
      // fuer diese Seite.
      for (const k of leiste?.kategorien || []) if (k.name === name) k.eingeklappt = zu;
      leisteFehler = e.message;
    }
  }

  async function zuordnen(kategorie) {
    settingsFehler = '';
    try {
      await senden('/action/task/bereich', { projekt: gewaehlt, bereich: kategorie });
      await leisteLaden();
    } catch (e) {
      settingsFehler = e.message;
    }
  }

  async function anlegen(name) {
    settingsFehler = '';
    try {
      await senden('/action/task/bereich/anlegen', { bereich: name });
    } catch (e) {
      settingsFehler = e.message;
      return;
    }
    await zuordnen(name);
  }

  function umschalten(id) {
    offen = offen.includes(id) ? offen.filter((x) => x !== id) : [...offen, id];
  }

  function details(e, id) {
    if (detail && detail.id === id) {
      detail = null;
      return;
    }
    detail = {
      id,
      titel: e.art === 'change' ? e.change?.kennung : e.titel,
      nummer: e.art === 'change' ? 0 : e.nummer,
      kennung: e.change?.kennung || '',
      ansicht: e.change ? 'change' : 'issue',
      repo: gewaehlt,
      branches: e.branches || [],
      schritt: e.change?.schritt || null,
      anlegen: e.anlegen || null,
      // CI-Pruefungen eines PRs (pr-checks, #80).
      checks: e.art === 'pr' ? e.checks || null : null,
      // Reviews eines PRs (pr-reviews, #81).
      pr: e.art === 'pr',
      reviews: e.art === 'pr' ? e.reviews || null : null,
      // Stufenleiste und Nachsorge (pr-lebenslauf, #83).
      lebenslauf: e.lebenslauf || null,
    };
  }

  // Nach dem Neuladen zeigt die Detailspalte den neuen Stand des Issues:
  // Branches, Schritt und Vorschlag (pr-anlegen, #79).
  function detailAuffrischen(a, nwo) {
    if (!detail || !detail.nummer || detail.repo !== nwo) return;
    if (detail.pr) {
      const p = (a.prs || []).find((x) => x.nummer === detail.nummer);
      if (p) {
        detail.checks = p.checks || null;
        detail.reviews = p.reviews || null;
      }
      detail.lebenslauf = a.pr_laeufe?.[detail.nummer] || null;
      return;
    }
    const e = [...(a.reihe || []), ...(a.zuletzt || [])].find((x) => x.art === 'issue' && x.nummer === detail.nummer);
    if (!e) return;
    detail.branches = e.branches || [];
    detail.schritt = e.change?.schritt || null;
    detail.anlegen = e.anlegen || null;
    detail.lebenslauf = e.lebenslauf || null;
  }

  // Hell, Dunkel, Automatisch reihum (things-farben-fortschritt, #41).
  function thema() {
    themaWahl = naechste(themaWahl);
    merken(themaWahl);
    anwenden(themaWahl);
  }

  // Kreis und Zahlen je Repo in der Leiste; der Dienst zaehlt, die Seite
  // zeichnet nur.
  async function standLaden(frisch) {
    try {
      const s = await mitZaehler(() => holen(frisch ? '/api/repo-stand?frisch=1' : '/api/repo-stand'));
      repoStand = s;
      if (s.sync?.laeuft) nachfragen(() => standLaden(false));
    } catch {
      /* ohne Stand bleiben Kreis und Zahlen leer */
    }
  }
  let standJe = $derived(Object.fromEntries((repoStand?.repos || []).map((r) => [r.repo, r])));

  // Bei sichtbarem Tab jede Minute bedingt aktualisieren; der Dienst fragt
  // GitHub nur, wenn sein Stand abgelaufen ist, und dann mit ETag.
  $effect(() => {
    const t = setInterval(() => {
      if (document.visibilityState !== 'visible') return;
      leisteLaden(false);
      arbeitAktualisieren(false);
      uebersichtLaden(false);
      standLaden(false);
    }, 60000);
    const sichtbar = () => {
      if (document.visibilityState === 'visible') {
        leisteLaden(false);
        arbeitAktualisieren(false);
        uebersichtLaden(false);
        standLaden(false);
      }
    };
    document.addEventListener('visibilitychange', sichtbar);
    const abmelden = systemFolgen(() => themaWahl);
    return () => {
      abmelden();
      clearInterval(t);
      document.removeEventListener('visibilitychange', sichtbar);
    };
  });

  onMount(async () => {
    themaWahl = wahlLesen();
    anwenden(themaWahl);
    await leisteLaden();
    uebersichtLaden(false);
    standLaden(false);
    const gemerkt = gewaehltesRepo();
    if (gemerkt && leiste && (leiste.kategorien || []).some((k) => (k.repos || []).some((r) => r.nwo === gemerkt))) {
      waehlen(gemerkt);
    }
  });
</script>

<div class="kn-app" class:kn-mit-detail={!!detail} data-schiene={marker}>
  <nav class="kn-leiste" class:kn-sichtbar={leisteSichtbar} aria-label="Repos">
    <div class="kn-leiste-kopf">
      <span class="kn-marke">knowledge</span>
      <SyncAnzeige zeit={syncZeit} laeuft={syncLaeuft} fehler={syncFehler} aktualisieren={jetztSynchronisieren} />
    </div>
    <div class="kn-leiste-inhalt">
      {#if !leiste && !leisteFehler}
        <p class="kn-leer">Lade …</p>
      {:else}
        <RepoLeiste {leiste} gewaehlt={ansicht ? '' : gewaehlt} {waehlen} {einklappen} fehler={leisteFehler}
                    {uebersicht} {ansicht} {ansichtWaehlen} {standJe} />
      {/if}
    </div>
    <div class="kn-leiste-fuss">
      <a href="/ueberblick" class="kn-fuss-link">Überblick</a>
      <button type="button" class="kn-klein kn-thema" data-thema={themaWahl} title={'Thema: ' + NAMEN[themaWahl] + ' — klicken zum Wechseln'}
              aria-label={'Thema: ' + NAMEN[themaWahl]} onclick={thema}>
        {#if themaWahl === 'light'}
          <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="3" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M7 .8v1.6M7 11.6v1.6M.8 7h1.6M11.6 7h1.6M2.6 2.6l1.1 1.1M10.3 10.3l1.1 1.1M2.6 11.4l1.1-1.1M10.3 3.7l1.1-1.1" stroke="currentColor" stroke-width="1.2" /></svg>
        {:else if themaWahl === 'dark'}
          <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M11.8 8.6A5 5 0 0 1 5.4 2.2a5 5 0 1 0 6.4 6.4z" fill="none" stroke="currentColor" stroke-width="1.3" /></svg>
        {:else}
          <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="5.4" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M7 1.6a5.4 5.4 0 0 1 0 10.8z" fill="currentColor" /></svg>
        {/if}
        <span class="kn-thema-name">{NAMEN[themaWahl]}</span>
      </button>
    </div>
  </nav>

  <main class="kn-mitte">
    <button type="button" class="kn-menue" aria-label="Repos zeigen" onclick={() => (leisteSichtbar = !leisteSichtbar)}>
      <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 4h12M2 8h12M2 12h12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
    </button>
    <div class="kn-mitte-inhalt">
      {#if ansicht === 'logbuch'}
        <Logbuch {logbuch} laedt={!logbuch} fehler={logbuchFehler} details={uDetails} detailId={detail?.id || ''} mehr={logbuchMehr} />
      {:else if ansicht}
        <Uebersicht art={ansicht} {uebersicht} laedt={!uebersicht} fehler={uebersichtFehler}
                    details={uDetails} detailId={detail?.id || ''} umhaken={uHaken} mehr={uebersichtMehr} />
      {:else if !gewaehlt || !repo}
        <div class="kn-willkommen">
          <h1>Issues</h1>
          <p>Links ein Repo wählen — oder Heute, Irgendwann, Logbuch über alle Repos.</p>
        </div>
      {:else}
        <Arbeitsliste {repo} {arbeit} {laedt} fehler={arbeitFehler} {offen} {umschalten} {details} {umhaken} {meta} anlegen={issueAnlegen} {prAktion}
                      detailId={detail?.id || ''} einstellungen={() => { settingsFehler = ''; settingsOffen = true; }} />
      {/if}
    </div>
  </main>

  {#if detail?.repo}
    <div class="kn-rechts">
      <Detailspalte repo={detail.repo} ziel={detail} schliessen={() => (detail = null)}
                    wechseln={(a) => (detail = { ...detail, ansicht: a })}
                    {meta} geaendert={detailGeaendert} />
    </div>
  {/if}

  {#if settingsOffen && repo}
    <Einstellungen {repo} kategorien={leiste?.kategorien || []} kategorie={kategorieDesRepos}
                   {zuordnen} {anlegen} fehler={settingsFehler} schliessen={() => (settingsOffen = false)}
                   synchronisieren={jetztSynchronisieren} />
  {/if}
</div>

<style>
  /* Farben von Things 3 (things-farben-fortschritt, #41), hell und dunkel an
     einer Stelle. */
  .kn-app {
    --t-bg: #ffffff;
    --t-leiste: #f5f5f7;
    --t-text: #1d1d1f;
    --t-text2: #3c3c43;
    --t-dim: #8a8a8e;
    --t-hover: rgba(0, 0, 0, 0.045);
    --t-auswahl: #e1e1e6;
    --t-linie: #e6e6ea;
    --t-rahmen: #c4c4c9;
    --t-flaeche: #ffffff;
    --t-karte: #ffffff;
    --t-blau: #1c7cf4;
    --t-rot: #f0454f;
    --t-gruen: #3dab59;
    --t-lila: #8e4ec6;
    --t-grau: #b3b3b8;
    --t-gelb: #f5c400;
    --t-beige: #cdb27a;

    position: fixed;
    inset: 0;
    display: grid;
    grid-template-columns: 260px minmax(0, 1fr);
    background: var(--t-bg);
    color: var(--t-text);
    font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Segoe UI', Inter, Roboto, 'Helvetica Neue', Arial, sans-serif;
    font-size: 14px;
    -webkit-font-smoothing: antialiased;
  }
  .kn-app.kn-mit-detail {
    grid-template-columns: 260px minmax(0, 1fr) minmax(340px, 36vw);
  }
  :global(html[data-theme='dark']) .kn-app {
    --t-bg: #1f1f21;
    --t-leiste: #2a2a2c;
    --t-text: #e8e8ea;
    --t-text2: #bdbdc2;
    --t-dim: #8b8b90;
    --t-hover: rgba(255, 255, 255, 0.055);
    --t-auswahl: #3a3a3e;
    --t-linie: #333336;
    --t-rahmen: #535357;
    --t-flaeche: #1f1f21;
    --t-karte: #2c2c2f;
    --t-blau: #4a90f7;
    --t-rot: #f25a5f;
    --t-gruen: #52c26b;
    --t-lila: #a877dc;
    --t-grau: #6c6c71;
    --t-gelb: #f7c843;
    --t-beige: #c9ae76;
    color-scheme: dark;
  }
  .kn-leiste {
    background: var(--t-leiste);
    border-right: 1px solid var(--t-linie);
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .kn-leiste-kopf,
  .kn-leiste-fuss {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 14px 6px 20px;
  }
  .kn-leiste-fuss {
    padding: 8px 14px 12px 20px;
    border-top: 1px solid var(--t-linie);
  }
  .kn-marke {
    font-weight: 600;
    font-size: 13px;
    color: var(--t-dim);
    letter-spacing: 0.02em;
  }
  .kn-leiste-inhalt {
    flex: 1;
    overflow-y: auto;
    padding: 0 10px;
  }
  .kn-fuss-link {
    font-size: 13px;
    color: var(--t-dim);
    text-decoration: none;
  }
  .kn-fuss-link:hover {
    color: var(--t-text);
  }
  .kn-klein {
    border: 0;
    background: transparent;
    color: var(--t-dim);
    padding: 5px;
    border-radius: 6px;
    cursor: pointer;
    display: grid;
    place-items: center;
  }
  .kn-thema {
    display: flex;
    gap: 6px;
    font: inherit;
    font-size: 12px;
  }
  .kn-klein:hover {
    background: var(--t-hover);
    color: var(--t-text);
  }
  .kn-mitte {
    background: var(--t-bg);
    overflow-y: auto;
    min-width: 0;
    position: relative;
    /* Bezug fuer die Breite der Karte (repo-karte-folge, #99): sie nutzt die
       ganze Spalte, nicht nur die Textbreite. */
    container-type: inline-size;
  }
  .kn-mitte-inhalt {
    /* Waechst mit dem Fenster bis 1160 Pixel (liste-abhaengigkeiten, #48). */
    max-width: 1160px;
    margin: 0 auto;
    padding: 44px 48px 80px;
  }
  .kn-menue {
    display: none;
  }
  .kn-rechts {
    min-width: 0;
    min-height: 0;
  }
  .kn-willkommen h1 {
    font-size: 26px;
    margin: 0 0 8px;
  }
  .kn-willkommen p,
  .kn-leer {
    color: var(--t-dim);
  }
  @media (max-width: 760px) {
    .kn-app,
    .kn-app.kn-mit-detail {
      grid-template-columns: minmax(0, 1fr);
    }
    .kn-leiste {
      position: fixed;
      inset: 0 auto 0 0;
      width: min(290px, 86vw);
      z-index: 10;
      transform: translateX(-100%);
      transition: transform 0.2s;
      box-shadow: 0 0 40px rgba(0, 0, 0, 0.2);
    }
    .kn-leiste.kn-sichtbar {
      transform: none;
    }
    .kn-menue {
      display: grid;
      place-items: center;
      position: sticky;
      top: 10px;
      margin: 10px 0 0 10px;
      border: 0;
      background: var(--t-leiste);
      color: var(--t-text);
      border-radius: 8px;
      padding: 7px;
      z-index: 5;
    }
    .kn-mitte-inhalt {
      padding: 10px 18px 60px;
    }
    .kn-rechts {
      position: fixed;
      inset: 0;
      z-index: 15;
    }
  }
</style>

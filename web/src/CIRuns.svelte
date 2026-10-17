<script>
  // Der Abschnitt CI eines Repos (ci-runs, #122): die letzten 20
  // Workflow-Runs mit Punkt, Workflow, Branch, Event, Betreff, SHA, Zeit,
  // Dauer und einer Punktreihe je Schritt. Ein Run klappt auf in Jobs und
  // Schritte links und das Log rechts; gewaehlt ist, was Go vorgibt
  // (wahl_job, wahl_schritt). Punkte, Folge und Texte kommen fertig aus Go
  // (appctx.CIAnsichtBauen). Der Stand wird beim Oeffnen des Repos geholt,
  // der Dienst erneuert ihn dabei im Hintergrund.
  import { holen, mitQuery, senden } from './api.js';
  import CILog from './CILog.svelte';

  let { repo = '' } = $props();

  let offen = $state(false);
  let ansicht = $state(null);
  let fehler = $state('');
  let offenerRun = $state(0);
  let job = $state(0);
  let schritt = $state(0);
  let sprung = $state(0);
  let log = $state(null);
  let logLaedt = $state(false);
  let logFehler = $state('');
  let arbeitet = $state(0);
  let neuFehler = $state('');
  let nachgefragt = '';

  $effect(() => {
    const r = repo;
    ansicht = null;
    fehler = '';
    offenerRun = 0;
    log = null;
    nachgefragt = '';
    if (r) laden(r, false);
  });

  async function laden(r, frisch) {
    try {
      const a = await holen(mitQuery('/api/ci', { repo: r, frisch: frisch ? 1 : '' }));
      if (repo !== r) return;
      ansicht = a;
      fehler = '';
      // Erneuert der Dienst gerade im Hintergrund, einmal nachfragen.
      if (a?.sync?.laeuft && nachgefragt !== r) {
        nachgefragt = r;
        setTimeout(() => {
          if (repo === r) laden(r, false);
        }, 2500);
      }
    } catch (e) {
      if (repo === r) fehler = e.message;
    }
  }

  async function logLaden(id) {
    const r = repo;
    job = id;
    log = null;
    logFehler = '';
    if (!id) return;
    logLaedt = true;
    try {
      const l = await holen(mitQuery('/api/ci-log', { repo: r, job: id }));
      if (repo === r && job === id) log = l;
    } catch (e) {
      if (repo === r && job === id) logFehler = e.message;
    } finally {
      logLaedt = false;
    }
  }

  function runUmschalten(run) {
    if (offenerRun === run.id) {
      offenerRun = 0;
      return;
    }
    offenerRun = run.id;
    schritt = run.wahl_schritt || 0;
    sprung++;
    logLaden(run.wahl_job || 0);
  }

  function jobWaehlen(j) {
    schritt = 0;
    sprung++;
    if (j.id !== job || !log) logLaden(j.id);
  }

  function schrittWaehlen(j, s) {
    schritt = s.nummer;
    sprung++;
    if (j.id !== job) logLaden(j.id);
  }

  async function neuStarten(run) {
    if (arbeitet) return;
    arbeitet = run.id;
    neuFehler = '';
    try {
      await senden('/action/ci/neu-starten', { repo, run_id: run.id });
      await laden(repo, true);
    } catch (e) {
      neuFehler = 'Run ' + run.nummer + ': ' + e.message;
    } finally {
      arbeitet = 0;
    }
  }
</script>

<section class="kn-ci" id="kn-ci" data-punkt={ansicht?.punkt || ''}>
  <h2>
    <button type="button" class="kn-ci-kopf" aria-expanded={offen} onclick={() => (offen = !offen)}>
      <span class="kn-ci-pfeil" aria-hidden="true">{offen ? '▾' : '▸'}</span> CI
      {#if ansicht?.punkt}<span class="kn-ci-punkt" data-punkt={ansicht.punkt} aria-hidden="true"></span>{/if}
      {#if ansicht?.gesamt}
        <span class="kn-ci-zahl">{ansicht.kopf}</span>
      {:else if ansicht?.grund}
        <span class="kn-ci-grund">{ansicht.grund}</span>
      {/if}
    </button>
  </h2>
  {#if fehler}<p class="kn-ci-hinweis kn-ci-rot" title={fehler}>{fehler}</p>{/if}
  {#if ansicht?.sync?.fehler}
    <p class="kn-ci-hinweis" title={ansicht.sync.fehler}><span class="kn-ci-q">ci</span> nicht verfügbar · {ansicht.sync.fehler}</p>
  {/if}
  {#if offen}
    {#if !ansicht && !fehler}
      <p class="kn-ci-hinweis">Lade Workflow-Runs …</p>
    {:else if ansicht}
      {#each ansicht.runs || [] as run (run.id)}
        <div class="kn-ci-run" data-run={run.id} data-punkt={run.punkt}>
          <div class="kn-ci-zeile">
            <button type="button" class="kn-ci-run-zeile" aria-expanded={offenerRun === run.id} onclick={() => runUmschalten(run)}>
              <span class="kn-ci-punkt" data-punkt={run.punkt} title={run.ergebnis || run.status}></span>
              <span class="kn-ci-mitte">
                <span class="kn-ci-oben">
                  <span class="kn-ci-wf">{run.workflow}</span>
                  <span class="kn-ci-nr">#{run.nummer}</span>
                  <span class="kn-ci-branch">{run.branch}</span>
                  <span class="kn-ci-event">{run.event}</span>
                </span>
                <span class="kn-ci-unten">
                  <span class="kn-ci-betreff" title={run.betreff_voll}>{run.betreff}</span>
                  <span class="kn-ci-sha">{run.sha}</span>
                  <span class="kn-ci-zeit" title={run.zeit_voll}>{run.zeit}</span>
                  {#if run.dauer}<span class="kn-ci-dauer">{run.dauer}</span>{/if}
                </span>
              </span>
              <span class="kn-ci-reihe">
                {#each run.punkte || [] as p, i (p.job_id + ':' + p.schritt + ':' + i)}
                  <span class="kn-ci-p" data-punkt={p.punkt} title={p.titel}></span>
                {/each}
              </span>
            </button>
            {#if run.neu_startbar}
              <button type="button" class="kn-ci-neu" disabled={arbeitet !== 0} onclick={() => neuStarten(run)}>
                {arbeitet === run.id ? 'startet …' : 'Fehlgeschlagene Jobs neu starten'}
              </button>
            {/if}
          </div>
          {#if offenerRun === run.id}
            <div class="kn-ci-offen" data-run={run.id}>
              <div class="kn-ci-jobs">
                {#each run.jobs || [] as j (j.id)}
                  <div class="kn-ci-job" data-job={j.id} data-punkt={j.punkt} data-gewaehlt={j.id === job ? 'ja' : undefined}>
                    <button type="button" class="kn-ci-job-name" onclick={() => jobWaehlen(j)}>
                      <span class="kn-ci-punkt" data-punkt={j.punkt}></span>
                      <span class="kn-ci-jn">{j.name}</span>
                      {#if j.dauer}<span class="kn-ci-dauer">{j.dauer}</span>{/if}
                    </button>
                    <ul>
                      {#each j.schritte || [] as s (s.nummer)}
                        <li>
                          <button type="button" class="kn-ci-schritt" data-schritt={s.nummer} data-punkt={s.punkt}
                                  data-gewaehlt={j.id === job && s.nummer === schritt ? 'ja' : undefined}
                                  onclick={() => schrittWaehlen(j, s)}>
                            <span class="kn-ci-p" data-punkt={s.punkt}></span>
                            <span class="kn-ci-sn">{s.name}</span>
                            {#if s.dauer}<span class="kn-ci-dauer">{s.dauer}</span>{/if}
                          </button>
                        </li>
                      {/each}
                    </ul>
                  </div>
                {:else}
                  <p class="kn-ci-hinweis">Keine Jobs.</p>
                {/each}
              </div>
              <div class="kn-ci-log">
                <CILog {log} laedt={logLaedt} fehler={logFehler} {schritt} {sprung} />
              </div>
            </div>
          {/if}
        </div>
      {:else}
        <p class="kn-ci-hinweis">{ansicht.grund || 'Keine Workflow-Runs.'}</p>
      {/each}
      {#if neuFehler}<p class="kn-ci-hinweis kn-ci-rot" title={neuFehler}>{neuFehler}</p>{/if}
    {/if}
  {/if}
</section>

<style>
  .kn-ci {
    margin-top: 26px;
  }
  h2 {
    margin: 0 0 8px;
    font-size: 16px;
    font-weight: 700;
  }
  .kn-ci-kopf {
    font: inherit;
    border: 0;
    background: transparent;
    color: var(--t-blau);
    padding: 0;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .kn-ci-pfeil {
    display: inline-block;
    width: 1em;
    color: var(--t-dim);
  }
  .kn-ci-zahl,
  .kn-ci-grund {
    font-size: 12.5px;
    font-weight: 500;
    color: var(--t-dim);
  }
  .kn-ci-punkt,
  .kn-ci-p {
    display: inline-block;
    flex: 0 0 auto;
    border-radius: 50%;
    background: var(--t-grau);
  }
  .kn-ci-punkt {
    width: 10px;
    height: 10px;
  }
  .kn-ci-p {
    width: 8px;
    height: 8px;
  }
  [data-punkt='gruen'].kn-ci-punkt,
  [data-punkt='gruen'].kn-ci-p {
    background: var(--t-gruen);
  }
  [data-punkt='rot'].kn-ci-punkt,
  [data-punkt='rot'].kn-ci-p {
    background: var(--t-rot);
  }
  [data-punkt='grau'].kn-ci-punkt,
  [data-punkt='grau'].kn-ci-p {
    background: var(--t-grau);
    opacity: 0.6;
  }
  [data-punkt='laeuft'].kn-ci-punkt,
  [data-punkt='laeuft'].kn-ci-p {
    background: transparent;
    border: 2px solid var(--t-gelb);
    box-sizing: border-box;
    animation: kn-ci-puls 1.4s ease-in-out infinite;
  }
  @keyframes kn-ci-puls {
    50% {
      opacity: 0.35;
    }
  }
  .kn-ci-run {
    border-bottom: 1px solid var(--t-rahmen);
  }
  .kn-ci-zeile {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .kn-ci-run-zeile {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 4px;
    border: 0;
    background: transparent;
    font: inherit;
    text-align: left;
    color: var(--t-text);
    cursor: pointer;
    border-radius: 6px;
  }
  .kn-ci-run-zeile:hover {
    background: var(--t-hover);
  }
  .kn-ci-mitte {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .kn-ci-oben,
  .kn-ci-unten {
    display: flex;
    gap: 8px;
    align-items: baseline;
    min-width: 0;
    white-space: nowrap;
  }
  .kn-ci-wf {
    font-weight: 600;
    font-size: 13.5px;
  }
  .kn-ci-nr,
  .kn-ci-event,
  .kn-ci-zeit,
  .kn-ci-dauer {
    font-size: 12px;
    color: var(--t-dim);
  }
  .kn-ci-branch {
    font-size: 11.5px;
    padding: 0 6px;
    border: 1px solid var(--t-rahmen);
    border-radius: 9px;
    color: var(--t-text2);
  }
  .kn-ci-betreff {
    font-size: 12.5px;
    color: var(--t-text2);
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .kn-ci-sha {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 11.5px;
    color: var(--t-dim);
  }
  .kn-ci-reihe {
    flex: 0 1 auto;
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 3px;
    max-width: 40%;
  }
  .kn-ci-neu {
    flex: 0 0 auto;
    font: inherit;
    font-size: 12px;
    padding: 3px 9px;
    border-radius: 6px;
    border: 1px solid var(--t-rot);
    background: var(--t-flaeche);
    color: var(--t-rot);
    cursor: pointer;
  }
  .kn-ci-neu:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .kn-ci-offen {
    display: grid;
    grid-template-columns: minmax(220px, 300px) minmax(0, 1fr);
    gap: 14px;
    padding: 6px 0 14px 22px;
  }
  .kn-ci-jobs {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .kn-ci-job ul {
    list-style: none;
    margin: 2px 0 0;
    padding: 0 0 0 14px;
  }
  .kn-ci-job-name,
  .kn-ci-schritt {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 7px;
    border: 0;
    background: transparent;
    font: inherit;
    text-align: left;
    color: var(--t-text);
    padding: 3px 6px;
    border-radius: 5px;
    cursor: pointer;
  }
  .kn-ci-job-name {
    font-weight: 600;
    font-size: 13px;
  }
  .kn-ci-job[data-gewaehlt='ja'] > .kn-ci-job-name {
    background: var(--t-auswahl, var(--t-hover));
  }
  .kn-ci-schritt {
    font-size: 12.5px;
  }
  .kn-ci-schritt:hover,
  .kn-ci-job-name:hover {
    background: var(--t-hover);
  }
  .kn-ci-schritt[data-gewaehlt='ja'] {
    background: var(--t-auswahl, var(--t-hover));
    font-weight: 600;
  }
  .kn-ci-jn,
  .kn-ci-sn {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kn-ci-log {
    min-width: 0;
  }
  .kn-ci-hinweis {
    margin: 4px 0;
    font-size: 12.5px;
    color: var(--t-dim);
  }
  .kn-ci-rot {
    color: var(--t-rot);
  }
  .kn-ci-q {
    font-weight: 600;
  }
</style>

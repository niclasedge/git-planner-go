// Zugriff auf die Routen des Dienstes (repo-leiste, #25). Ein Fehler kommt
// als Error mit dem grund der Antwort zurueck, damit die Seite ihn nennen kann.

async function lesen(antwort) {
  let rumpf = null;
  try {
    rumpf = await antwort.json();
  } catch {
    rumpf = null;
  }
  if (!antwort.ok) {
    const grund = (rumpf && (rumpf.grund || rumpf.fehler)) || 'HTTP ' + antwort.status;
    const e = new Error(grund);
    e.status = antwort.status;
    throw e;
  }
  return rumpf;
}

export async function holen(url) {
  return lesen(await fetch(url, { headers: { Accept: 'application/json' } }));
}

export async function senden(url, rumpf) {
  const antwort = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(rumpf),
  });
  if (antwort.status === 403) {
    // Das Netz-Gate: schreibende Routen gibt es ueber 127.0.0.1 und fuer die
    // mit --write-clients freigegebenen IPs (#46); alles andere ist 403.
    const e = new Error('Speichern über diese Adresse ist nicht freigegeben (--write-clients).');
    e.status = 403;
    throw e;
  }
  return lesen(antwort);
}

export function mitQuery(pfad, werte) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(werte)) {
    if (v !== undefined && v !== null && v !== '') q.set(k, String(v));
  }
  return pfad + '?' + q.toString();
}

// Schreibaktionen auf Issues (issue-verwaltung, #26).
export function issueSchreiben(aktion, rumpf) {
  return senden('/action/issue/' + aktion, rumpf);
}

// Aktionen auf Pull Requests (pr-aktionen, #32).
export function prSchreiben(aktion, rumpf) {
  return senden('/action/pr/' + aktion, rumpf);
}

// Branch anlegen (pr-anlegen, #79).
export function branchSchreiben(aktion, rumpf) {
  return senden('/action/branch/' + aktion, rumpf);
}

// Das gewaehlte Repo in localStorage (repo-leiste, #25), damit es ein
// Neuladen uebersteht. Der Einklappzustand der Kategorien steht dagegen in
// areas.json (POST /action/task/bereich/einklappen): er soll auch in einem
// zweiten Browser gelten.

const SCHLUESSEL = 'kn-things-repo';

export function gewaehltesRepo() {
  try {
    const r = localStorage.getItem(SCHLUESSEL);
    return typeof r === 'string' ? r : '';
  } catch {
    return '';
  }
}

export function repoMerken(nwo) {
  try {
    localStorage.setItem(SCHLUESSEL, nwo);
  } catch {
    // Voller oder gesperrter Speicher: die Seite vergisst nur die Auswahl.
  }
}

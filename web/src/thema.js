// Farbthemen wie in Things 3 (things-farben-fortschritt, #41): Hell, Dunkel
// und Automatisch. Die Wahl steht in localStorage (kn-theme), das wirksame
// Thema als data-theme an <html> — dort setzt es die Huelle schon vor dem
// Bundle (internal/render/things.go), hier nur noch bei einem Wechsel.

export const FOLGE = ['auto', 'light', 'dark'];
export const NAMEN = { auto: 'Automatisch', light: 'Hell', dark: 'Dunkel' };

const system = () => (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');

export function wahlLesen() {
  try {
    const t = localStorage.getItem('kn-theme');
    if (FOLGE.includes(t)) return t;
  } catch {
    /* ohne Speicher gilt Automatisch */
  }
  return 'auto';
}

export function naechste(wahl) {
  return FOLGE[(FOLGE.indexOf(wahl) + 1) % FOLGE.length];
}

export function anwenden(wahl) {
  document.documentElement.dataset.theme = wahl === 'auto' ? system() : wahl;
}

export function merken(wahl) {
  try {
    localStorage.setItem('kn-theme', wahl);
  } catch {
    /* ohne Speicher gilt die Wahl bis zum Neuladen */
  }
}

// Folgt dem System, solange die Wahl Automatisch ist; liefert das Abmelden.
export function systemFolgen(wahlJetzt) {
  const mq = matchMedia('(prefers-color-scheme: dark)');
  const f = () => {
    if (wahlJetzt() === 'auto') anwenden('auto');
  };
  mq.addEventListener('change', f);
  return () => mq.removeEventListener('change', f);
}

// Anzeige-Texte der Status aus /api/arbeit. Nur Beschriftung und Farbe; der
// Status selbst kommt fertig aus Go (internal/appctx/repoarbeit.go).
export const STATUS = {
  erledigt: { text: 'Erledigt', farbe: 'var(--t-dim)' },
  im_pr: { text: 'Im PR', farbe: 'var(--t-lila)' },
  fertig: { text: 'Fertig', farbe: 'var(--t-gruen)' },
  in_arbeit: { text: 'In Arbeit', farbe: 'var(--t-blau)' },
  bereit: { text: 'Bereit', farbe: 'var(--t-gruen)' },
  blockiert: { text: 'Blockiert', farbe: 'var(--t-rot)' },
  ohne_change: { text: 'Kein Change', farbe: 'var(--t-dim)' },
  // Rollen (karte-rollen, #102); den Grund liefert Go in `grund`.
  wartet_auf_mensch: { text: 'Wartet auf Mensch', farbe: 'var(--t-gelb)' },
  hygiene: { text: 'Aufräumen', farbe: 'var(--t-rot)' },
};

export function status(s) {
  return STATUS[s] || { text: s || '', farbe: 'var(--t-dim)' };
}

export function datum(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return '';
  return d.toLocaleDateString('de-DE', { day: 'numeric', month: 'short' });
}

// Beschriftung eines Faelligkeitsdatums (faelligkeit, #39). Termin und Abstand
// in Tagen kommen fertig aus Go; hier steht nur der Text dazu.
const WOCHENTAG = ['So', 'Mo', 'Di', 'Mi', 'Do', 'Fr', 'Sa'];

export function faelligText(faellig, termin, tage) {
  if (!faellig) return { text: '', farbe: 'var(--t-dim)' };
  const d = new Date(faellig + 'T00:00:00');
  const kurz = d.toLocaleDateString('de-DE', { day: 'numeric', month: 'short' });
  if (termin === 'ueberfaellig') {
    return { text: tage === -1 ? 'gestern' : 'seit ' + -tage + ' Tagen', farbe: 'var(--t-rot)', titel: kurz };
  }
  if (termin === 'heute') return { text: 'Heute', farbe: 'var(--t-gelb)', titel: kurz };
  if (tage === 1) return { text: 'Morgen', farbe: 'var(--t-dim)', titel: kurz };
  if (tage > 1 && tage < 7) return { text: WOCHENTAG[d.getDay()] + ', ' + kurz, farbe: 'var(--t-dim)', titel: kurz };
  return { text: kurz, farbe: 'var(--t-dim)', titel: faellig };
}

// Ueberschrift eines Tages in Demnaechst.
export function tagUeberschrift(faellig, tage) {
  const d = new Date(faellig + 'T00:00:00');
  if (tage === 1) return 'Morgen';
  return d.toLocaleDateString('de-DE', { weekday: 'long', day: 'numeric', month: 'long' });
}

// Das Datum von heute plus delta Tagen als JJJJ-MM-TT in Ortszeit — fuer die
// Knoepfe Heute und Morgen, die ein Datum schreiben.
export function tagIn(delta) {
  const t = new Date();
  t.setDate(t.getDate() + delta);
  return t.getFullYear() + '-' + String(t.getMonth() + 1).padStart(2, '0') + '-' + String(t.getDate()).padStart(2, '0');
}

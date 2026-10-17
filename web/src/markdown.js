// Markdown fuer die Detailspalte. Issue-Texte kommen von GitHub und koennen
// beliebiges HTML tragen; DOMPurify entfernt alles Ausfuehrbare, bevor es ins
// DOM geht. Links oeffnen in einem neuen Tab.
import { marked } from 'marked';
import DOMPurify from 'dompurify';

marked.setOptions({ gfm: true, breaks: false });

DOMPurify.addHook('afterSanitizeAttributes', (knoten) => {
  if (knoten.tagName === 'A' && knoten.getAttribute('href')) {
    knoten.setAttribute('target', '_blank');
    knoten.setAttribute('rel', 'noopener noreferrer');
  }
});

export function markdown(text) {
  if (!text) return '';
  return DOMPurify.sanitize(marked.parse(String(text)), { USE_PROFILES: { html: true } });
}

// Eine Zeile (Subtask, Titel): nur Inline-Markdown wie `code` und **fett**.
export function zeile(text) {
  if (!text) return '';
  return DOMPurify.sanitize(marked.parseInline(String(text)), { USE_PROFILES: { html: true } });
}

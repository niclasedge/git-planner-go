# Facts

Kurze, dauerhaft nützliche Erkenntnisse zu diesem Repo.

## Betrieb

- **`-env` ist CWD-relativ.** Default ist `.env` im Arbeitsverzeichnis. Wird das
  Binary aus einem anderen Verzeichnis gestartet (z. B. einem Build-Ordner),
  fehlen die Tokens *lautlos* bis auf eine WARN-Zeile
  (`token "work" skipped: environment variable GITHUB_PAT_WORK is empty`), und
  die App läuft mit einem Token weiter. Beim Start immer `-env <repo>/.env`
  mitgeben und die Startzeile prüfen: `tokens=2`.
- Fine-grained PATs können `/notifications` grundsätzlich nicht lesen (nur
  Classic-PAT). Die WARN beim Start ist erwartet, kein Fehler.

## Schreibpfad (Issue-Edit)

- Schreibende Requests sind unkonditional, gehen nicht durch den ETag-Cache und
  kosten Rate-Limit. Eine Write-Antwort darf nie in den Read-Cache — der Cache
  heilt sich danach selbst (Body geändert → ETag geändert → 200).
- Bei `403` nennt der Response-Header `X-Accepted-GitHub-Permissions` die
  fehlende Berechtigung des fine-grained PAT. Für Issue-Edits:
  `Issues: Read and write`.
- Das Due-Date lebt als kanonische Zeile `target date: YYYY-MM-DD` am Anfang des
  Bodys (`SetDue`/`ParseDue`/`StripDue`). Inline-Formen in Prosa
  (`… bis due: 2026-08-01 fertig`) werden bewusst nicht angefasst.

## CSS

- Spezifität schlägt Reihenfolge: `.ed-newlb .in { flex: 0 1 200px }` (zwei
  Klassen) gewinnt gegen ein späteres `.ed-color { flex: 0 0 32px }` (eine
  Klasse). Der Color-Swatch trägt deshalb *kein* `.in`.

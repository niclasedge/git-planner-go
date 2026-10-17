<script>
  // Die oberste Zeile von "Letzte Aktionen" (karte-einzeilig, #124): der
  // juengste Push oder Merge ueber alle Branches. Welcher es ist, Branch,
  // Zeit, Tooltip und PR kommen fertig aus Go (internal/appctx/letztercommit.go);
  // hier wird nur gezeichnet.
  let { l } = $props();
</script>

<div class="kn-letzter-commit" data-letzter-commit title={l.titel}>
  <span class="kn-lc-punkt" aria-hidden="true">●</span>
  <span class="kn-lc-wort">Letzter Commit</span>
  <span class="kn-lc-zeit">{l.text}</span>
  <span class="kn-lc-trenner">·</span>
  <span class="kn-lc-branch" class:kn-lc-standard={l.standard} data-branch-chip>{l.branch}</span>
  {#if l.betreff_kurz}<span class="kn-lc-trenner">·</span> <span class="kn-lc-betreff">„{l.betreff_kurz}“</span>{/if}
  <span class="kn-lc-trenner">·</span>
  <code class="kn-lc-sha">{l.kurz}</code>
  {#if l.autor}<span class="kn-lc-trenner">·</span> <span class="kn-lc-autor">{l.autor}</span>{/if}
  {#if l.pr}<span class="kn-lc-pr">→ PR <a href={l.pr.url} target="_blank" rel="noopener noreferrer">#{l.pr.nummer}</a></span>{/if}
</div>

<style>
  .kn-letzter-commit {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 6px 0 4px;
    padding: 7px 10px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--t-blau) 11%, transparent);
    border: 1px solid color-mix(in srgb, var(--t-blau) 28%, transparent);
    font-size: 13px;
    color: var(--t-text);
    white-space: nowrap;
    overflow: hidden;
  }
  .kn-lc-punkt {
    color: var(--t-blau);
    font-size: 11px;
  }
  .kn-lc-wort {
    font-weight: 600;
  }
  .kn-lc-trenner {
    color: var(--t-dim);
  }
  .kn-lc-branch {
    padding: 1px 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--t-lila) 14%, transparent);
    color: var(--t-lila);
    font-size: 12px;
    font-weight: 500;
  }
  .kn-lc-branch.kn-lc-standard {
    background: var(--t-hover);
    color: var(--t-text2);
  }
  .kn-lc-betreff {
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .kn-lc-sha {
    font-size: 12px;
    color: var(--t-text2);
  }
  .kn-lc-autor {
    color: var(--t-text2);
  }
  .kn-lc-pr {
    margin-left: 2px;
    color: var(--t-text2);
  }
  .kn-lc-pr a {
    color: var(--t-blau);
    text-decoration: none;
    font-weight: 600;
  }
  .kn-lc-pr a:hover {
    text-decoration: underline;
  }
</style>

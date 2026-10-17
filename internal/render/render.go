// Package render produces the dashboard HTML and the /read + git-result pages —
// Go-native port of the Deno render.ts (browser.js-compatible DOM).
package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/md"
	"github.com/niclasedge/git-planner-go/internal/shared"
)

// Assets carries the embedded static asset contents for inline embedding.
type Assets struct {
	DashCSS, BrowserCSS, BrowserJS string
}

// faviconLink steht in JEDEM <head> dieser App und zeigt auf die eingebettete
// static/favicon.svg (Route /assets/*, internal/server). Nur SVG, kein .ico:
// die Datei traegt ihre eigene prefers-color-scheme-Regel und passt sich damit
// dem Browser-Theme an, was eine Bitmap nicht kann. Ohne rel="icon" fragt der
// Browser /favicon.ico ab, was hier 404 gibt — sichtbar als leeres Tab-Icon.
const faviconLink = `<link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">`

// htmlEscaper matches @std/html/entities escape: & < > " ' (single pass, & first).
var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#39;",
)

func escape(s string) string { return htmlEscaper.Replace(s) }

// SafeScriptJson JSON-serializes obj for a <script> context. Go's encoding/json
// already escapes <, >, & (→ </>/&) AND U+2028/U+2029, which is
// exactly what the Deno safeScriptJson did by hand.
func SafeScriptJson(obj any) string {
	b, err := json.Marshal(obj)
	if err != nil {
		return "null"
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// resultPage — outcome page for git actions
// ---------------------------------------------------------------------------

const resultCSS = `body{margin:0;background:#0a0b0d;color:#d7dee5;font-family:"JetBrains Mono",ui-monospace,Menlo,monospace;font-size:13px;line-height:1.55;}.wrap{max-width:900px;margin:0 auto;padding:34px 24px;}h1{font-size:16px;font-weight:700;margin:0 0 4px;}.cmd{color:#6c7682;font-size:12px;margin:0 0 14px;}pre.out{background:#0b0d10;border:1px solid #1c2229;border-radius:9px;padding:14px;font-size:12px;white-space:pre-wrap;word-break:break-word;color:#aab3bd;}a{color:oklch(0.83 0.11 215);} .ok{color:oklch(0.83 0.15 152);} .err{color:oklch(0.68 0.19 26);}@media (prefers-color-scheme: light){body{background:#eff1f5;color:#4c4f69;}pre.out{background:#e6e9ef;border-color:#ccd0da;color:#5c5f77;}.cmd{color:#8c8fa1;} a{color:#209fb5;} .ok{color:#40a02b;} .err{color:#d20f39;}}`

// ResultPage renders the outcome page for a git action.
func ResultPage(action string, result map[string]any) string {
	ok := truthy(result["ok"])
	badge := `<span class="err">Fehler</span>`
	if ok {
		badge = `<span class="ok">OK</span>`
	}
	return "<!DOCTYPE html>\n<html lang='de'><head><meta charset='utf-8'>" +
		"<meta name='viewport' content='width=device-width, initial-scale=1'>" +
		"<title>git " + escape(action) + "</title>" + faviconLink +
		"<style>" + resultCSS + "</style></head>" +
		"<body><div class='wrap'>" +
		"<h1>git " + escape(action) + " " + badge + "</h1>" +
		"<p class='cmd'><code>" + escape(str(result["cmd"])) + "</code></p>" +
		"<pre class='out'>" + escape(str(result["output"])) + "</pre>" +
		"<p><a href='/'>← zurück zum Dashboard</a></p>" +
		"</div></body></html>"
}

// ---------------------------------------------------------------------------
// readDoc — whitelisted file reader for GET /read
// ---------------------------------------------------------------------------

const readMaxChars = 60000

var (
	nameAlnumColon = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	nameAlnum      = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func under(child, parent string) bool {
	c := child
	if !strings.HasPrefix(child, "/") {
		c = parent + "/" + child
	}
	norm := filepath.Clean(c)
	if norm == parent {
		return true
	}
	p := parent
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(norm, p)
}

// ReadDoc reads a whitelisted file by kind, guarding path traversal.
func ReadDoc(cwd, claudeHome, kind, name, path string) map[string]any {
	skillsRoot := filepath.Join(claudeHome, "skills")
	var fpath string

	switch kind {
	case "claude-global":
		fpath = filepath.Join(claudeHome, "CLAUDE.md")
	case "claude-project":
		fpath = filepath.Join(collectors.RepoRoot(cwd), "CLAUDE.md")
	case "skill":
		if !nameAlnumColon.MatchString(name) {
			return errDoc("ungültiger Skill-Name")
		}
		fpath = filepath.Join(skillsRoot, name, "SKILL.md")
		if !under(fpath, skillsRoot) {
			return errDoc("Pfad ausserhalb skills/")
		}
	case "skillfile":
		if !nameAlnumColon.MatchString(name) {
			return errDoc("ungültiger Skill-Name")
		}
		skillRoot := filepath.Join(skillsRoot, name)
		fpath = filepath.Join(skillRoot, path)
		if !under(fpath, skillRoot) {
			return errDoc("Pfad ausserhalb des Skills")
		}
	case "agent":
		if !nameAlnum.MatchString(name) {
			return errDoc("ungültiger Agent-Name")
		}
		fpath = filepath.Join(claudeHome, "agents", name+".md")
		if !under(fpath, filepath.Join(claudeHome, "agents")) {
			return errDoc("Pfad ausserhalb agents/")
		}
	case "homefile":
		if path == "" {
			return errDoc("kein Pfad")
		}
		if strings.HasPrefix(path, "/") {
			fpath = path
		} else {
			fpath = filepath.Join(claudeHome, path)
		}
		if !under(fpath, claudeHome) {
			return errDoc("Pfad ausserhalb ~/.claude")
		}
	case "project-agent":
		if !nameAlnum.MatchString(name) {
			return errDoc("ungültiger Agent-Name")
		}
		projAgentsRoot := filepath.Join(collectors.RepoRoot(cwd), ".claude", "agents")
		fpath = filepath.Join(projAgentsRoot, name+".md")
		if !under(fpath, projAgentsRoot) {
			return errDoc("Pfad ausserhalb .claude/agents")
		}
	case "memory":
		if !nameAlnum.MatchString(name) {
			return errDoc("ungültiger Memory-Name")
		}
		memRoot := filepath.Join(collectors.RepoRoot(cwd), "knowledge", "memory")
		fpath = filepath.Join(memRoot, name)
		if !under(fpath, memRoot) {
			return errDoc("Pfad ausserhalb knowledge/memory")
		}
	case "lektion":
		if !nameAlnum.MatchString(name) {
			return errDoc("ungültiger Lektion-Name")
		}
		knRoot := filepath.Join(collectors.RepoRoot(cwd), "knowledge")
		fpath = filepath.Join(knRoot, name)
		if !under(fpath, knRoot) {
			return errDoc("Pfad ausserhalb knowledge/")
		}
	case "knowfile":
		knRoot := filepath.Join(collectors.RepoRoot(cwd), "knowledge")
		rel := path
		if rel == "" {
			rel = name
		}
		rel = strings.TrimPrefix(rel, "knowledge/")
		fpath = filepath.Join(knRoot, rel)
		if !under(fpath, knRoot) {
			return errDoc("Pfad ausserhalb knowledge/")
		}
	default:
		return errDoc("unbekannte Art: " + kind)
	}

	text, ok := shared.ReadText(fpath)
	if !ok {
		return errDoc("nicht gefunden: " + lastSeg(fpath))
	}
	var size int64
	var mtime any = nil
	if st, err := os.Stat(fpath); err == nil {
		size = st.Size()
		mtime = md.FmtMtime(float64(st.ModTime().Unix()))
	}
	truncated := utf8.RuneCountInString(text) > readMaxChars
	content := text
	if truncated {
		content = string([]rune(text)[:readMaxChars])
	}
	return map[string]any{
		"ok":        true,
		"kind":      kind,
		"name":      name,
		"path":      path,
		"tokens":    md.EstTokens(text),
		"size":      size,
		"mtime":     mtime,
		"truncated": truncated,
		"content":   content,
	}
}

func errDoc(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }

func lastSeg(p string) string {
	parts := strings.Split(p, "/")
	return parts[len(parts)-1]
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case float64:
		return x
	default:
		return 0
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// encodeURIComponent mirrors JS encodeURIComponent (keeps A-Za-z0-9 -_.!~*'()).
func encodeURIComponent(s string) string {
	const keep = "-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

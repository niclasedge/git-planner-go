package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMdEscMdInline_NoAttributeBreakoutViaQuotes is the JS-side half of
// demo-axk.4 review Welle 2, finding 1 (stored XSS via notes -> mdToHtml
// -> mdInline): mdEsc must escape " and ' so a markdown link's URL, built
// into an href="..." attribute by mdInline, can never break out of that
// attribute. It extracts the CURRENT mdEsc/mdInline source verbatim out of
// the embedded browser.js (between two stable markers) instead of
// duplicating it by hand — a hand-copied fixture would silently drift from
// the real function the next time either changes, and would keep "passing"
// after a regression. This test always runs the shipped code.
//
// Skips (does not fail) if node is unavailable, matching the fail-open
// convention the beads/dot Go tests already use for missing external tools.
func TestMdEscMdInline_NoAttributeBreakoutViaQuotes(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node nicht installiert")
	}

	const startMarker = "function mdEsc(s) {"
	const endMarker = "function stripFrontmatter(src) {"
	start := strings.Index(BrowserJS, startMarker)
	end := strings.Index(BrowserJS, endMarker)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("mdEsc/mdInline markers not found in browser.js — update this test's markers alongside any refactor of that section")
	}
	src := BrowserJS[start:end]

	script := src + "\n" + jsAssertions
	dir := t.TempDir()
	path := filepath.Join(dir, "mdesc_xss_check.js")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, path).CombinedOutput()
	if err != nil {
		t.Fatalf("node assertion script failed: %v\n%s", err, out)
	}
}

// jsAssertions runs against the extracted mdEsc/mdInline exactly as the
// review's live repro did: a markdown link whose URL contains a literal "
// (and, for good measure, a ') must never let mdInline emit those raw quote
// characters into the output — every " in the result must come from OUR OWN
// <a ...> template (href/target/rel — 6 quote characters total), never from
// attacker-controlled input smuggled through unescaped.
const jsAssertions = `
function assert(cond, msg) { if (!cond) { throw new Error("assertion failed: " + msg); } }

// The review's exact repro (demo-axk.4 Welle 2 finding 1).
var raw = '[click](http://evil.com"onmouseover="alert(1)")';
var out = mdInline(mdEsc(raw));
var quoteCount = (out.match(/"/g) || []).length;
assert(quoteCount === 6, "expected exactly 6 double-quote chars (href/target/rel), got " + quoteCount + " in: " + out);
assert(out.indexOf('onmouseover="alert') === -1, "onmouseover must never appear as a live HTML attribute: " + out);

// Single-quote variant: mdEsc must escape ' too (classic attribute hardening).
var rawSq = "[click](http://evil.com'onmouseover='alert(1)')";
var outSq = mdInline(mdEsc(rawSq));
assert(outSq.indexOf("'") === -1, "expected mdEsc to escape single quotes, got: " + outSq);

// Non-adversarial input must still render a normal, intact link.
var benign = mdInline(mdEsc('[docs](https://example.com/a?b=c)'));
assert(benign === '<a href="https://example.com/a?b=c" target="_blank" rel="noopener">docs</a>', "benign link must render unchanged, got: " + benign);

// mdEsc alone: all five XSS-relevant characters escaped, nothing else touched.
var escInput = "&<>" + String.fromCharCode(34) + String.fromCharCode(39);
var escOut = mdEsc(escInput);
assert(escOut === "&amp;&lt;&gt;&quot;&#39;", "mdEsc must escape all five special characters, got: " + escOut);
`

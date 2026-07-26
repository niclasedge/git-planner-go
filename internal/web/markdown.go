package web

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// md renders issue bodies for the detail pane. Raw HTML stays escaped — that is
// goldmark's default and it is the right one here: the bodies are written by
// whoever can open an issue, and this page has no business running their markup.
// GFM is on for the two things issue bodies are actually full of: task lists and
// tables. A goldmark.Markdown is safe for concurrent use, so one instance is
// enough.
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

func renderMarkdown(s string) template.HTML {
	if s == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		// A body that will not parse is still worth reading. Show it as text.
		return template.HTML("<p>" + template.HTMLEscapeString(s) + "</p>")
	}
	return template.HTML(buf.String())
}

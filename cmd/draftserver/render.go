package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

// LaTeX block patterns — must be extracted BEFORE markdown parsing
// because gomarkdown treats \[ as an escaped '[' and strips the backslash.
var (
	reDisplayMath = regexp.MustCompile(`(?s)\\\[(.+?)\\\]`)
	reInlineMath  = regexp.MustCompile(`\\\((.+?)\\\)`)
)

// renderMarkdown converts a Markdown string to safe HTML with:
//   - LaTeX math blocks protected and restored around the markdown pass
//   - Each <table> wrapped in a horizontal scroll container
//   - KaTeX auto-render picks up the restored \[...\] / \(...\) in the browser
func renderMarkdown(md string) string {
	if md == "" {
		return ""
	}

	// Step 1: extract LaTeX blocks into a side-map so markdown can't touch them.
	maths := map[string]string{}
	counter := 0

	md = reDisplayMath.ReplaceAllStringFunc(md, func(m string) string {
		key := fmt.Sprintf("ZLATEX_DISPLAY_%d_ENDZ", counter)
		maths[key] = m
		counter++
		return key
	})
	md = reInlineMath.ReplaceAllStringFunc(md, func(m string) string {
		key := fmt.Sprintf("ZLATEX_INLINE_%d_ENDZ", counter)
		maths[key] = m
		counter++
		return key
	})

	// Step 2: render markdown.
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock | parser.Tables
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(md))

	opts := html.RendererOptions{Flags: html.CommonFlags | html.HrefTargetBlank}
	renderer := html.NewRenderer(opts)
	raw := string(markdown.Render(doc, renderer))

	// Step 3: restore LaTeX delimiters.
	for key, math := range maths {
		raw = strings.ReplaceAll(raw, key, math)
	}

	// Step 4: wrap tables in scroll containers.
	raw = strings.ReplaceAll(raw, "<table>", `<div class="table-scroll"><table>`)
	raw = strings.ReplaceAll(raw, "</table>", `</table></div>`)

	return raw
}

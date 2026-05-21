package pdfgen

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/emandor/gostudentubl/internal/notify"
	"github.com/gomarkdown/markdown"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

type Options struct {
	OutputDir    string
	LogoPath     string
	ChromiumPath string
	StudentName  string
	StudentNIM   string
}

type Result struct {
	HTMLPath string
	PDFPath  string
}

func Generate(ctx context.Context, ev notify.NotificationEvent, review notify.DraftReview, opts Options) (Result, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = "submissions"
	}
	if opts.ChromiumPath == "" {
		opts.ChromiumPath = "chromium"
	}
	if opts.StudentName == "" {
		opts.StudentName = "Aris Kurniawan"
	}
	if opts.StudentNIM == "" {
		opts.StudentNIM = "2311510438"
	}
	dir := filepath.Join(opts.OutputDir, fmt.Sprintf("event_%d", ev.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	htmlPath := filepath.Join(dir, "answer.html")
	pdfPath := filepath.Join(dir, "answer.pdf")
	body := buildHTML(ev, review, opts)
	if err := os.WriteFile(htmlPath, []byte(body), 0o644); err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, opts.ChromiumPath, "--headless", "--no-sandbox", "--disable-gpu", "--print-to-pdf="+pdfPath, "file://"+htmlPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{HTMLPath: htmlPath, PDFPath: pdfPath}, fmt.Errorf("chromium pdf failed: %w: %s", err, string(out))
	}
	return Result{HTMLPath: htmlPath, PDFPath: pdfPath}, nil
}

func buildHTML(ev notify.NotificationEvent, review notify.DraftReview, opts Options) string {
	logo := ""
	if opts.LogoPath != "" {
		if b, err := os.ReadFile(opts.LogoPath); err == nil {
			logo = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
		}
	}
	answer := strings.TrimSpace(review.ReviewedText)
	if answer == "" {
		answer = ev.DraftText
	}
	answer = normalizeSubmissionText(answer)
	return fmt.Sprintf(`<!doctype html>
<html lang="id">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@300;400;500;600;700&display=swap" rel="stylesheet">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css" crossorigin="anonymous">
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js" crossorigin="anonymous"></script>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/contrib/auto-render.min.js" crossorigin="anonymous"></script>
<style>%s</style>
</head>
<body>
<main class="page">
<header>
  <div class="logo-wrap">%s</div>
  <div>
    <p class="doc-kicker">Jawaban Tugas</p>
    <h1>%s</h1>
    <p class="item-name">%s</p>
    <p><b>Nama:</b> %s</p>
    <p><b>NIM:</b> %s</p>
  </div>
</header>
<hr>
<section class="answer">%s</section>
</main>
%s
</body>
</html>`, css, logoHTML(logo), escapeText(strings.ToUpper(ev.ItemTitle)), escapeText(ev.ItemName), escapeText(opts.StudentName), escapeText(opts.StudentNIM), renderAnswerMarkdown(answer), katexInit)
}

func normalizeSubmissionText(s string) string {
	out := strings.TrimSpace(s)
	replacements := []struct {
		old string
		new string
	}{
		{"**Draf solusi referensi:**", "**Jawaban:**"},
		{"**Draft solusi referensi:**", "**Jawaban:**"},
		{"Draf solusi referensi:", "Jawaban:"},
		{"Draft solusi referensi:", "Jawaban:"},
		{"Berikut adalah draf solusi referensi", "Berikut adalah jawaban"},
		{"Berikut draf solusi", "Berikut jawaban"},
		{"draf jawaban", "jawaban"},
		{"draft jawaban", "jawaban"},
	}
	for _, r := range replacements {
		out = strings.ReplaceAll(out, r.old, r.new)
	}
	out = removeInternalNoteBlocks(out)
	return strings.TrimSpace(out)
}

func removeInternalNoteBlocks(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	skipParagraph := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		low := strings.ToLower(trimmed)
		startsInternalNote := strings.HasPrefix(low, "**catatan penting:**") || strings.HasPrefix(low, "catatan penting:")
		if startsInternalNote && (strings.Contains(low, "dosen memberi") || strings.Contains(low, "ganti angkanya") || strings.Contains(low, "angka input")) {
			skipParagraph = true
			continue
		}
		if skipParagraph {
			if trimmed == "" {
				skipParagraph = false
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func logoHTML(src string) string {
	if src == "" {
		return `<div class="logo-fallback">UBL</div>`
	}
	return `<img src="` + src + `" alt="UBL logo">`
}

var (
	reDisplayMath = regexp.MustCompile(`(?s)\\\[(.+?)\\\]`)
	reInlineMath  = regexp.MustCompile(`\\\((.+?)\\\)`)
)

func renderAnswerMarkdown(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
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

	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock | parser.Tables | parser.FencedCode | parser.Strikethrough
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(md))
	opts := mdhtml.RendererOptions{Flags: mdhtml.CommonFlags | mdhtml.HrefTargetBlank | mdhtml.SkipHTML}
	renderer := mdhtml.NewRenderer(opts)
	raw := string(markdown.Render(doc, renderer))

	for key, math := range maths {
		raw = strings.ReplaceAll(raw, key, math)
	}
	raw = strings.ReplaceAll(raw, "<table>", `<div class="table-scroll"><table>`)
	raw = strings.ReplaceAll(raw, "</table>", `</table></div>`)
	return raw
}

func escapeText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	return s
}

const katexInit = `<script>
document.addEventListener("DOMContentLoaded",function(){
  if(window.renderMathInElement){
    renderMathInElement(document.body,{
      delimiters:[
        {left:"\\[",right:"\\]",display:true},
        {left:"\\(",right:"\\)",display:false}
      ],
      throwOnError:false
    });
  }
});
</script>`

const css = `@page{size:A4;margin:20mm 22mm}
*{box-sizing:border-box}
:root{--text:#111;--muted:#555;--border:#d0d0d0;--soft:#f4f4f4;--blue:#0f62fe;--sans:'IBM Plex Sans',Arial,Helvetica,sans-serif;--mono:'IBM Plex Mono','Courier New',monospace}
html{font-size:15px}body{font-family:var(--sans);color:var(--text);background:#fff;margin:0}.page{max-width:780px;margin:0 auto}
header{display:grid;grid-template-columns:96px 1fr;gap:24px;align-items:start}.logo-wrap img{width:88px;height:auto}.logo-fallback{width:86px;height:86px;border:2px solid #111;display:grid;place-items:center;font-weight:700}.doc-kicker{font-size:.78rem;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);margin:0 0 6px}.item-name{font-weight:500;color:#333;margin-bottom:10px}
h1{font-size:1.58rem;letter-spacing:.01em;line-height:1.18;margin:0 0 10px;text-transform:uppercase;font-weight:600}p{font-size:.95rem;line-height:1.62;margin:7px 0}hr{border:0;border-top:1.4px solid #111;margin:22px 0 24px}
.answer{font-size:.95rem;line-height:1.62}.answer h1,.answer h2,.answer h3{break-after:avoid;font-weight:600;line-height:1.25}.answer h1{font-size:1.45rem;margin:24px 0 10px}.answer h2{font-size:1.2rem;margin:22px 0 9px;border-bottom:1px solid var(--border);padding-bottom:4px}.answer h3{font-size:1.05rem;margin:18px 0 8px}.answer strong{font-weight:700}.answer em{font-style:italic}.answer hr{border-top:1px solid var(--border);margin:18px 0}.answer ul,.answer ol{margin:8px 0 12px 24px;padding:0}.answer li{margin:5px 0;line-height:1.58}.answer li>p{margin:4px 0}.answer blockquote{border-left:4px solid var(--blue);background:#f8faff;margin:14px 0;padding:8px 12px;color:#333}.answer code{font-family:var(--mono);font-size:.9em;background:var(--soft);padding:.08rem .25rem;border-radius:2px}.answer pre{background:#f7f7f7;border:1px solid var(--border);padding:12px;overflow-x:auto;white-space:pre-wrap;break-inside:avoid}.answer pre code{background:transparent;padding:0}.table-scroll{overflow-x:auto;margin:12px 0;break-inside:avoid}.answer table{width:100%;border-collapse:collapse;font-size:.86rem}.answer th,.answer td{border:1px solid var(--border);padding:6px 8px;text-align:left;vertical-align:top}.answer th{background:#f2f2f2;font-weight:600}.katex-display{margin:12px 0;overflow-x:auto;overflow-y:hidden}.katex{font-size:1.02em}`

package pdfgen

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/emandor/gostudentubl/internal/notify"
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
	return fmt.Sprintf(`<!doctype html><html lang="id"><head><meta charset="utf-8"><style>%s</style></head><body><main class="page"><header><div class="logo-wrap">%s</div><div><h1>%s</h1><p>%s</p><p><b>Nama:</b> %s</p><p><b>NIM:</b> %s</p></div></header><hr><section class="answer">%s</section></main></body></html>`, css, logoHTML(logo), html.EscapeString(strings.ToUpper(ev.ItemTitle)), html.EscapeString(ev.ItemName), html.EscapeString(opts.StudentName), html.EscapeString(opts.StudentNIM), markdownish(answer))
}

func logoHTML(src string) string {
	if src == "" {
		return `<div class="logo-fallback">UBL</div>`
	}
	return `<img src="` + src + `" alt="UBL logo">`
}

var headingRE = regexp.MustCompile(`^#{1,6}\s+`)

func markdownish(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	inList := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			if inList {
				b.WriteString("</ol>")
				inList = false
			}
			continue
		}
		if headingRE.MatchString(line) {
			if inList {
				b.WriteString("</ol>")
				inList = false
			}
			b.WriteString("<h2>" + html.EscapeString(headingRE.ReplaceAllString(line, "")) + "</h2>")
			continue
		}
		if regexp.MustCompile(`^\d+[\.)]\s+`).MatchString(line) {
			if !inList {
				b.WriteString("<ol>")
				inList = true
			}
			b.WriteString("<li>" + html.EscapeString(regexp.MustCompile(`^\d+[\.)]\s+`).ReplaceAllString(line, "")) + "</li>")
			continue
		}
		if inList {
			b.WriteString("</ol>")
			inList = false
		}
		b.WriteString("<p>" + html.EscapeString(line) + "</p>")
	}
	if inList {
		b.WriteString("</ol>")
	}
	return b.String()
}

const css = `@page{size:A4;margin:22mm 24mm}body{font-family:Arial,Helvetica,sans-serif;color:#111;background:#fff}.page{max-width:760px;margin:0 auto}header{display:grid;grid-template-columns:96px 1fr;gap:24px;align-items:start}.logo-wrap img{width:90px;height:auto}.logo-fallback{width:86px;height:86px;border:2px solid #111;display:grid;place-items:center;font-weight:700}h1{font-size:24px;letter-spacing:.02em;margin:2px 0 10px;text-transform:uppercase}p{font-size:14px;line-height:1.55;margin:4px 0}hr{border:0;border-top:1.5px solid #111;margin:22px 0 26px}.answer{font-size:14px}.answer h2{font-size:16px;margin:18px 0 8px}.answer ol{margin:0 0 12px 22px;padding:0}.answer li{margin:7px 0;line-height:1.55}`

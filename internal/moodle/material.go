package moodle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type CourseResource struct {
	Title string
	URL   string
	Type  string
	ID    string
}

type CoursePageSnapshot struct {
	Title     string
	Markdown  string
	Resources []CourseResource
}

func (c *Client) GetCoursePageSnapshot(ctx context.Context, cr Course) (CoursePageSnapshot, error) {
	u := strings.TrimSpace(cr.CourseLink)
	if u == "" {
		u = fmt.Sprintf("https://elearning.budiluhur.ac.id/course/view.php?id=%d", cr.CourseID)
	}
	doc, _, err := c.get(ctx, u)
	if err != nil {
		return CoursePageSnapshot{}, err
	}
	return parseCoursePageSnapshot(doc, cr, u), nil
}

func (c *Client) GetCourseResources(ctx context.Context, cr Course) ([]CourseResource, error) {
	base := c.Base.CoursesURL
	if base == "" && cr.CourseLink != "" {
		base = cr.CourseLink
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	parsed.Path = "/mod/resource/index.php"
	q := parsed.Query()
	q.Set("id", fmt.Sprintf("%d", cr.CourseID))
	parsed.RawQuery = q.Encode()
	doc, _, err := c.get(ctx, parsed.String())
	if err != nil {
		return nil, err
	}
	return parseResourceIndex(doc), nil
}

func (c *Client) DownloadBytes(ctx context.Context, u string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", c.UA)
	res, err := c.HC.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return nil, res.Header.Get("Content-Type"), fmt.Errorf("download status %d", res.StatusCode)
	}
	reader := res.Body
	if maxBytes > 0 {
		reader = io.NopCloser(io.LimitReader(res.Body, maxBytes+1))
	}
	buf, err := io.ReadAll(reader)
	if err != nil {
		return nil, res.Header.Get("Content-Type"), err
	}
	if maxBytes > 0 && int64(len(buf)) > maxBytes {
		return nil, res.Header.Get("Content-Type"), fmt.Errorf("download exceeds max bytes")
	}
	return buf, res.Header.Get("Content-Type"), nil
}

func parseCoursePageSnapshot(doc *goquery.Document, cr Course, sourceURL string) CoursePageSnapshot {
	title := strings.TrimSpace(doc.Find("h1, .page-header-headings h1").First().Text())
	if title == "" {
		title = cr.CourseName
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\nSource: %s\n\n", title, sourceURL)
	doc.Find(".course-content .section, li.section, .activity").Each(func(i int, s *goquery.Selection) {
		heading := cleanText(s.Find("h3, h4, .sectionname, .instancename").First().Text())
		text := cleanText(s.Text())
		if text == "" {
			return
		}
		if heading != "" {
			fmt.Fprintf(&sb, "\n## %s\n\n", heading)
		}
		fmt.Fprintf(&sb, "%s\n", text)
	})
	if strings.TrimSpace(sb.String()) == "" {
		sb.WriteString(cleanText(doc.Text()))
	}
	return CoursePageSnapshot{Title: title, Markdown: sb.String(), Resources: parseResourceLinks(doc)}
}

func parseResourceIndex(doc *goquery.Document) []CourseResource {
	var out []CourseResource
	doc.Find(".generaltable tbody tr, table tbody tr").Each(func(i int, s *goquery.Selection) {
		a := s.Find("a").First()
		title := cleanText(a.Text())
		href, _ := a.Attr("href")
		if title == "" || href == "" {
			return
		}
		out = append(out, CourseResource{Title: title, URL: href, Type: "resource", ID: firstMatch(href, `id=(\d+)`)})
	})
	return out
}

func parseResourceLinks(doc *goquery.Document) []CourseResource {
	seen := map[string]bool{}
	var out []CourseResource
	doc.Find("a[href]").Each(func(i int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !strings.Contains(href, "/mod/resource/") && !strings.Contains(href, "/pluginfile.php/") && !strings.Contains(href, "/mod/folder/") && !strings.Contains(href, "/mod/page/") {
			return
		}
		if seen[href] {
			return
		}
		seen[href] = true
		title := cleanText(a.Text())
		if title == "" {
			title = href
		}
		out = append(out, CourseResource{Title: title, URL: href, Type: "link", ID: firstMatch(href, `id=(\d+)`)})
	})
	return out
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(s), " ")
}

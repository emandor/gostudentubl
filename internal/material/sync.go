package material

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
)

type Options struct {
	CacheDir  string
	MaxFileMB int
	Limit     int
}

type Store interface {
	UpsertCourseMaterial(context.Context, notify.CourseMaterial) error
}

type Result struct {
	Courses   int
	Materials int
}

func SyncCourses(ctx context.Context, m *moodle.Client, store Store, courses []moodle.Course, opts Options) (Result, error) {
	if opts.CacheDir == "" {
		opts.CacheDir = "materials"
	}
	if opts.MaxFileMB <= 0 {
		opts.MaxFileMB = 50
	}
	limit := opts.Limit
	if limit <= 0 || limit > len(courses) {
		limit = len(courses)
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return Result{}, err
	}
	var res Result
	for i := 0; i < limit; i++ {
		cr := courses[i]
		res.Courses++
		snap, err := m.GetCoursePageSnapshot(ctx, cr)
		if err == nil {
			mat := materialRecord(cr, "course_page", fmt.Sprintf("%d", cr.CourseID), snap.Title, cr.CourseLink, snap.Markdown, "", "")
			if err := writeMaterial(ctx, store, opts.CacheDir, mat); err != nil {
				return res, err
			}
			res.Materials++
			for _, rr := range snap.Resources {
				if upsertResource(ctx, m, store, cr, rr, opts) == nil {
					res.Materials++
				}
			}
		}
		resources, err := m.GetCourseResources(ctx, cr)
		if err == nil {
			for _, rr := range resources {
				if upsertResource(ctx, m, store, cr, rr, opts) == nil {
					res.Materials++
				}
			}
		}
	}
	return res, nil
}

func upsertResource(ctx context.Context, m *moodle.Client, store Store, cr moodle.Course, rr moodle.CourseResource, opts Options) error {
	maxBytes := int64(opts.MaxFileMB) * 1024 * 1024
	body, ct, err := m.DownloadBytes(ctx, rr.URL, maxBytes)
	markdown := fmt.Sprintf("# %s\n\nSource: %s\n\n", rr.Title, rr.URL)
	status := "ready"
	errText := ""
	cachePath := ""
	if err != nil {
		status = "error"
		errText = err.Error()
		markdown += "Download failed.\n"
	} else {
		cachePath, _ = writeCacheFile(opts.CacheDir, cr.CourseID, rr.Title, body)
		if isTextLike(ct, rr.URL) {
			markdown += string(body)
		} else {
			markdown += fmt.Sprintf("Binary/resource file cached at `%s`. Content-Type: %s. Parse with external extractor when needed.\n", cachePath, ct)
		}
	}
	mat := materialRecord(cr, rr.Type, rr.ID, rr.Title, rr.URL, markdown, cachePath, errText)
	mat.Status = status
	return writeMaterial(ctx, store, opts.CacheDir, mat)
}

func writeMaterial(ctx context.Context, store Store, cacheDir string, mat notify.CourseMaterial) error {
	mdPath := filepath.Join(cacheDir, fmt.Sprintf("course_%d", mat.CourseID), safeName(mat.Title)+".md")
	if err := os.MkdirAll(filepath.Dir(mdPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(mat.Markdown), 0o644); err != nil {
		return err
	}
	mat.MarkdownPath = mdPath
	h := sha256.Sum256([]byte(mat.Markdown))
	mat.ContentHash = hex.EncodeToString(h[:])
	now := time.Now().UTC().Format(time.RFC3339)
	mat.FetchedAt = now
	mat.UpdatedAt = now
	return store.UpsertCourseMaterial(ctx, mat)
}

func materialRecord(cr moodle.Course, st, sid, title, url, markdown, cachePath, errText string) notify.CourseMaterial {
	if title == "" {
		title = cr.CourseName
	}
	return notify.CourseMaterial{CourseID: cr.CourseID, CourseName: cr.CourseName, SourceType: st, SourceID: sid, Title: title, SourceURL: url, CachePath: cachePath, Markdown: markdown, Status: "ready", Error: errText}
}

func writeCacheFile(cacheDir string, courseID int, title string, body []byte) (string, error) {
	dir := filepath.Join(cacheDir, fmt.Sprintf("course_%d", courseID), "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, safeName(title))
	if filepath.Ext(p) == "" {
		p += ".bin"
	}
	return p, os.WriteFile(p, body, 0o644)
}

func isTextLike(contentType, url string) bool {
	ct := strings.ToLower(contentType)
	u := strings.ToLower(url)
	return strings.Contains(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.HasSuffix(u, ".txt") || strings.HasSuffix(u, ".csv") || strings.HasSuffix(u, ".md")
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeName(s string) string {
	s = strings.Trim(unsafeName.ReplaceAllString(s, "_"), "_")
	if s == "" {
		return "material"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

package runner

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emandor/gostudentubl/internal/llm"
	"github.com/emandor/gostudentubl/internal/material"
	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
	"github.com/emandor/gostudentubl/internal/pdfgen"
)

func (r *Runner) syncCourseMaterials(ctx context.Context, courses []moodle.Course) error {
	res, err := material.SyncCourses(ctx, r.M, r.NotificationStore, courses, material.Options{CacheDir: r.MaterialCacheDir, MaxFileMB: r.MaterialMaxFileMB, Limit: r.MaterialSyncLimit})
	if err != nil {
		return err
	}
	r.Log.Info().Int("courses", res.Courses).Int("materials", res.Materials).Msg("course materials synced")
	return nil
}

func (r *Runner) processDraftReviews(ctx context.Context) error {
	if r.LLMClient == nil || r.NotificationStore == nil {
		return nil
	}
	items, err := r.NotificationStore.ListDraftsNeedingReview(ctx, r.DraftLimit)
	if err != nil {
		return err
	}
	for _, item := range items {
		materials, _ := r.NotificationStore.ListCourseMaterials(ctx, item.CourseID, 8)
		corpus := buildCorpus(materials, 10000)
		prompt := fmt.Sprintf(`Review and improve this coursework draft using the course materials.
Return in this exact structure:
SCORE: <0-100>
NOTES:
- concise findings
FINAL:
<final answer in Indonesian, ready to submit>

Course: %s
Item: %s
Due: %s

Assignment/quiz content:
%s

Course materials:
%s

Draft to review:
%s`, item.CourseName, item.ItemName, item.DueDate, item.RawContent, corpus, item.DraftText)
		resp, err := r.LLMClient.GetSuggestion(ctx, llm.SuggestionRequest{EventType: item.EventType, CourseName: item.CourseName, ItemName: item.ItemName, ItemTitle: "Draft review", Content: prompt, MaxTokens: 3500})
		if err != nil {
			r.Log.Warn().Err(err).Int64("event_id", item.ID).Msg("draft review failed")
			continue
		}
		score, notes, final := parseReview(resp.Suggestion)
		if strings.TrimSpace(final) == "" {
			final = resp.Suggestion
		}
		if err := r.NotificationStore.UpsertDraftReview(ctx, notify.DraftReview{EventID: item.ID, Status: "ready", Score: score, Notes: notes, ReviewedText: final, Model: resp.Model, TokensUsed: resp.TokensUsed}); err != nil {
			return err
		}
		r.Log.Info().Int64("event_id", item.ID).Int("score", score).Msg("draft reviewed")
	}
	return nil
}

func (r *Runner) processPDFArtifacts(ctx context.Context) error {
	items, err := r.NotificationStore.ListReviewedNeedingPDF(ctx, r.DraftLimit)
	if err != nil {
		return err
	}
	for _, item := range items {
		review, err := r.NotificationStore.GetDraftReview(ctx, item.ID)
		if err != nil {
			continue
		}
		result, err := pdfgen.Generate(ctx, item, review, pdfgen.Options{OutputDir: r.PDFCacheDir, LogoPath: "assets/pdf/logo.png", ChromiumPath: r.ChromiumPath, StudentName: r.PDFStudentName, StudentNIM: r.PDFStudentNIM})
		if err != nil {
			_ = r.NotificationStore.UpsertSubmissionArtifact(ctx, notify.SubmissionArtifact{EventID: item.ID, Status: "error", HTMLPath: result.HTMLPath, PDFPath: result.PDFPath, Error: err.Error()})
			r.Log.Warn().Err(err).Int64("event_id", item.ID).Msg("pdf generation failed")
			continue
		}
		if err := r.NotificationStore.UpsertSubmissionArtifact(ctx, notify.SubmissionArtifact{EventID: item.ID, Status: "ready", HTMLPath: result.HTMLPath, PDFPath: result.PDFPath}); err != nil {
			return err
		}
		r.Log.Info().Int64("event_id", item.ID).Str("pdf", result.PDFPath).Msg("submission pdf ready")
	}
	return nil
}

func (r *Runner) processApprovedSubmissions(ctx context.Context, now time.Time) error {
	if r.NotificationStore == nil {
		return nil
	}
	window := time.Duration(r.SubmissionDeadlineWindowHours) * time.Hour
	if window <= 0 {
		window = 2 * time.Hour
	}
	items, err := r.NotificationStore.ListApprovedDueForSubmission(ctx, now, window)
	if err != nil {
		return err
	}
	for _, item := range items {
		artifact, err := r.NotificationStore.GetSubmissionArtifact(ctx, item.ID)
		if err != nil {
			continue
		}
		status := "skipped"
		msg := "SUBMISSION_ENABLED=false; approved artifact is ready but real Moodle upload is disabled"
		if r.SubmissionEnabled {
			status = "pending_real_submit"
			msg = "approved artifact reached deadline window; Moodle upload adapter intentionally not enabled until form capture is implemented"
		}
		_ = r.NotificationStore.RecordSubmissionAttempt(ctx, item.ID, artifact.ID, status, msg)
		r.Log.Info().Int64("event_id", item.ID).Str("status", status).Str("pdf", artifact.PDFPath).Msg("submission gate evaluated")
	}
	return nil
}

func buildCorpus(materials []notify.CourseMaterial, max int) string {
	var b strings.Builder
	for _, m := range materials {
		if b.Len() >= max {
			break
		}
		fmt.Fprintf(&b, "\n## %s\n%s\n", m.Title, truncate(m.Markdown, max-b.Len()))
	}
	if strings.TrimSpace(b.String()) == "" {
		return "(no course materials indexed yet)"
	}
	return b.String()
}

var scoreRE = regexp.MustCompile(`(?i)SCORE:\s*(\d{1,3})`)

func parseReview(s string) (int, string, string) {
	score := 0
	if m := scoreRE.FindStringSubmatch(s); len(m) == 2 {
		score, _ = strconv.Atoi(m[1])
		if score > 100 {
			score = 100
		}
	}
	upper := strings.ToUpper(s)
	notes := ""
	final := ""
	ni := strings.Index(upper, "NOTES:")
	fi := strings.Index(upper, "FINAL:")
	if ni >= 0 && fi > ni {
		notes = strings.TrimSpace(s[ni+len("NOTES:") : fi])
	}
	if fi >= 0 {
		final = strings.TrimSpace(s[fi+len("FINAL:"):])
	}
	return score, notes, final
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...(truncated)"
}

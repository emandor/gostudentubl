package notify

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type CourseMaterial struct {
	ID           int64
	CourseID     int
	CourseName   string
	SourceType   string
	SourceID     string
	Title        string
	SourceURL    string
	CachePath    string
	MarkdownPath string
	ContentHash  string
	Markdown     string
	Status       string
	Error        string
	FetchedAt    string
	UpdatedAt    string
}

type DraftReview struct {
	ID           int64
	EventID      int64
	Status       string
	Score        int
	Notes        string
	ReviewedText string
	Model        string
	TokensUsed   int
	UpdatedAt    string
}

type SubmissionArtifact struct {
	ID          int64
	EventID     int64
	Status      string
	PDFPath     string
	HTMLPath    string
	Approved    bool
	ApprovedAt  string
	GeneratedAt string
	Error       string
}

func (s *NotificationStore) initCourseworkTables(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS course_materials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  course_id INTEGER NOT NULL,
  course_name TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT '',
  source_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  cache_path TEXT NOT NULL DEFAULT '',
  markdown_path TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  markdown TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  UNIQUE(course_id, source_url)
);
CREATE INDEX IF NOT EXISTS idx_course_materials_course ON course_materials(course_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS draft_reviews (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id INTEGER NOT NULL UNIQUE REFERENCES notification_events(id),
  status TEXT NOT NULL DEFAULT '',
  score INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  reviewed_text TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  tokens_used INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_draft_reviews_status ON draft_reviews(status);

CREATE TABLE IF NOT EXISTS submission_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id INTEGER NOT NULL UNIQUE REFERENCES notification_events(id),
  status TEXT NOT NULL DEFAULT '',
  pdf_path TEXT NOT NULL DEFAULT '',
  html_path TEXT NOT NULL DEFAULT '',
  approved INTEGER NOT NULL DEFAULT 0 CHECK(approved IN (0,1)),
  approved_at TEXT NOT NULL DEFAULT '',
  generated_at TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_submission_artifacts_status ON submission_artifacts(status, approved);

CREATE TABLE IF NOT EXISTS submission_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id INTEGER NOT NULL REFERENCES notification_events(id),
  artifact_id INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  attempted_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_submission_attempts_event ON submission_attempts(event_id, attempted_at DESC);
`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("migrate coursework tables: %w", err)
	}
	return nil
}

func (s *NotificationStore) UpsertCourseMaterial(ctx context.Context, m CourseMaterial) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if m.FetchedAt == "" {
		m.FetchedAt = now
	}
	if m.UpdatedAt == "" {
		m.UpdatedAt = now
	}
	if strings.TrimSpace(m.Status) == "" {
		m.Status = "ready"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO course_materials (course_id, course_name, source_type, source_id, title, source_url, cache_path, markdown_path, content_hash, markdown, status, error, fetched_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(course_id, source_url) DO UPDATE SET
  course_name=excluded.course_name, source_type=excluded.source_type, source_id=excluded.source_id,
  title=excluded.title, cache_path=excluded.cache_path, markdown_path=excluded.markdown_path,
  content_hash=excluded.content_hash, markdown=excluded.markdown, status=excluded.status,
  error=excluded.error, fetched_at=excluded.fetched_at, updated_at=excluded.updated_at;
`, m.CourseID, m.CourseName, m.SourceType, m.SourceID, m.Title, m.SourceURL, m.CachePath, m.MarkdownPath, m.ContentHash, m.Markdown, m.Status, m.Error, m.FetchedAt, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert course material: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListCourseMaterials(ctx context.Context, courseID int, limit int) ([]CourseMaterial, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, course_id, course_name, source_type, source_id, title, source_url, cache_path, markdown_path, content_hash, markdown, status, error, fetched_at, updated_at FROM course_materials WHERE course_id=? AND status='ready' ORDER BY updated_at DESC LIMIT ?`, courseID, limit)
	if err != nil {
		return nil, fmt.Errorf("query course materials: %w", err)
	}
	defer rows.Close()
	var out []CourseMaterial
	for rows.Next() {
		var m CourseMaterial
		if err := rows.Scan(&m.ID, &m.CourseID, &m.CourseName, &m.SourceType, &m.SourceID, &m.Title, &m.SourceURL, &m.CachePath, &m.MarkdownPath, &m.ContentHash, &m.Markdown, &m.Status, &m.Error, &m.FetchedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *NotificationStore) ListDraftsNeedingReview(ctx context.Context, limit int) ([]NotificationEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name, ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed, ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model, COALESCE(ne.draft_updated_at,''), COALESCE(idt.raw_content,'')
FROM notification_events ne
LEFT JOIN item_details idt ON idt.event_id=ne.id
LEFT JOIN draft_reviews dr ON dr.event_id=ne.id
WHERE ne.draft_status IN ('ready','open') AND ne.draft_text<>'' AND COALESCE(dr.status,'') NOT IN ('ready','reviewed')
ORDER BY ne.id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query drafts needing review: %w", err)
	}
	defer rows.Close()
	var out []NotificationEvent
	for rows.Next() {
		var ev NotificationEvent
		if err := rows.Scan(&ev.ID, &ev.EventType, &ev.CourseID, &ev.CourseName, &ev.ItemTitle, &ev.ItemName, &ev.ItemLink, &ev.ItemID, &ev.DueDate, &ev.SubmissionStatus, &ev.Grade, &ev.DueDateParsedRFC3339, &ev.DraftStatus, &ev.DraftText, &ev.DraftProvider, &ev.DraftModel, &ev.DraftUpdatedAt, &ev.RawContent); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *NotificationStore) UpsertDraftReview(ctx context.Context, r DraftReview) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if r.UpdatedAt == "" {
		r.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO draft_reviews(event_id,status,score,notes,reviewed_text,model,tokens_used,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(event_id) DO UPDATE SET status=excluded.status, score=excluded.score, notes=excluded.notes, reviewed_text=excluded.reviewed_text, model=excluded.model, tokens_used=excluded.tokens_used, updated_at=excluded.updated_at`, r.EventID, r.Status, r.Score, r.Notes, r.ReviewedText, r.Model, r.TokensUsed, r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert draft review: %w", err)
	}
	return nil
}

func (s *NotificationStore) GetDraftReview(ctx context.Context, eventID int64) (DraftReview, error) {
	var r DraftReview
	err := s.db.QueryRowContext(ctx, `SELECT id,event_id,status,score,notes,reviewed_text,model,tokens_used,updated_at FROM draft_reviews WHERE event_id=?`, eventID).Scan(&r.ID, &r.EventID, &r.Status, &r.Score, &r.Notes, &r.ReviewedText, &r.Model, &r.TokensUsed, &r.UpdatedAt)
	if err != nil {
		return DraftReview{}, err
	}
	return r, nil
}

func (s *NotificationStore) ListReviewedNeedingPDF(ctx context.Context, limit int) ([]NotificationEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name, ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed, ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model, COALESCE(ne.draft_updated_at,''), COALESCE(idt.raw_content,'')
FROM notification_events ne
JOIN draft_reviews dr ON dr.event_id=ne.id AND dr.status IN ('ready','reviewed') AND dr.reviewed_text<>''
LEFT JOIN submission_artifacts sa ON sa.event_id=ne.id
LEFT JOIN item_details idt ON idt.event_id=ne.id
WHERE COALESCE(sa.status,'') NOT IN ('ready')
ORDER BY ne.id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query reviewed needing pdf: %w", err)
	}
	defer rows.Close()
	var out []NotificationEvent
	for rows.Next() {
		var ev NotificationEvent
		if err := rows.Scan(&ev.ID, &ev.EventType, &ev.CourseID, &ev.CourseName, &ev.ItemTitle, &ev.ItemName, &ev.ItemLink, &ev.ItemID, &ev.DueDate, &ev.SubmissionStatus, &ev.Grade, &ev.DueDateParsedRFC3339, &ev.DraftStatus, &ev.DraftText, &ev.DraftProvider, &ev.DraftModel, &ev.DraftUpdatedAt, &ev.RawContent); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *NotificationStore) UpsertSubmissionArtifact(ctx context.Context, a SubmissionArtifact) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if a.GeneratedAt == "" && a.Status == "ready" {
		a.GeneratedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO submission_artifacts(event_id,status,pdf_path,html_path,approved,approved_at,generated_at,error) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(event_id) DO UPDATE SET status=excluded.status, pdf_path=excluded.pdf_path, html_path=excluded.html_path, generated_at=excluded.generated_at, error=excluded.error`, a.EventID, a.Status, a.PDFPath, a.HTMLPath, boolToInt(a.Approved), a.ApprovedAt, a.GeneratedAt, a.Error)
	if err != nil {
		return fmt.Errorf("upsert submission artifact: %w", err)
	}
	return nil
}

func (s *NotificationStore) GetSubmissionArtifact(ctx context.Context, eventID int64) (SubmissionArtifact, error) {
	var a SubmissionArtifact
	var approved int
	err := s.db.QueryRowContext(ctx, `SELECT id,event_id,status,pdf_path,html_path,approved,approved_at,generated_at,error FROM submission_artifacts WHERE event_id=?`, eventID).Scan(&a.ID, &a.EventID, &a.Status, &a.PDFPath, &a.HTMLPath, &approved, &a.ApprovedAt, &a.GeneratedAt, &a.Error)
	if err != nil {
		return SubmissionArtifact{}, err
	}
	a.Approved = approved == 1
	return a, nil
}

func (s *NotificationStore) ApproveSubmissionArtifact(ctx context.Context, eventID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE submission_artifacts SET approved=1, approved_at=? WHERE event_id=? AND status='ready'`, now, eventID)
	if err != nil {
		return fmt.Errorf("approve submission artifact: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListApprovedDueForSubmission(ctx context.Context, now time.Time, window time.Duration) ([]NotificationEvent, error) {
	from := now.UTC().Format(time.RFC3339)
	to := now.UTC().Add(window).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name, ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed, ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model, COALESCE(ne.draft_updated_at,''), COALESCE(idt.raw_content,'')
FROM notification_events ne
JOIN submission_artifacts sa ON sa.event_id=ne.id AND sa.status='ready' AND sa.approved=1
LEFT JOIN item_details idt ON idt.event_id=ne.id
WHERE ne.event_type='assignment' AND ne.due_date_parsed>? AND ne.due_date_parsed<=? AND (ne.submission_status='' OR lower(ne.submission_status) NOT LIKE '%submitted for grading%')
ORDER BY ne.due_date_parsed ASC`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query approved due submissions: %w", err)
	}
	defer rows.Close()
	var out []NotificationEvent
	for rows.Next() {
		var ev NotificationEvent
		if err := rows.Scan(&ev.ID, &ev.EventType, &ev.CourseID, &ev.CourseName, &ev.ItemTitle, &ev.ItemName, &ev.ItemLink, &ev.ItemID, &ev.DueDate, &ev.SubmissionStatus, &ev.Grade, &ev.DueDateParsedRFC3339, &ev.DraftStatus, &ev.DraftText, &ev.DraftProvider, &ev.DraftModel, &ev.DraftUpdatedAt, &ev.RawContent); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *NotificationStore) RecordSubmissionAttempt(ctx context.Context, eventID, artifactID int64, status, message string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO submission_attempts(event_id,artifact_id,status,message,attempted_at) VALUES(?,?,?,?,?)`, eventID, artifactID, status, message, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record submission attempt: %w", err)
	}
	return nil
}

func isNoRows(err error) bool { return err == sql.ErrNoRows }

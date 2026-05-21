package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	NotificationTypeAssignment = "assignment"
	NotificationTypeQuiz       = "quiz"
)

type NotificationEvent struct {
	ID                   int64 // populated when read from DB
	EventType            string
	CourseID             int
	CourseName           string
	ItemTitle            string
	ItemName             string
	ItemLink             string
	ItemID               string
	DueDate              string
	SubmissionStatus     string
	Grade                string
	DueDateParsedRFC3339 string
	RawContent           string // populated by ListNeedingDraft via JOIN
	DraftStatus          string
	DraftText            string
	DraftProvider        string
	DraftModel           string
	DraftUpdatedAt       string
}

type PendingNotification struct {
	ID               int64
	EventType        string
	CourseID         int
	CourseName       string
	ItemTitle        string
	ItemName         string
	ItemLink         string
	ItemID           string
	AttemptCount     int
	LastError        string
	DueDate          string
	SubmissionStatus string
	Grade            string
	DueDateParsed    string
	Reminder24hSent  bool
	Reminder12hSent  bool
	DetailFetched    bool
	SuggestionStatus string
	SuggestionText   string
	RawContent       string
}

type ItemDetailRecord struct {
	EventID          int64
	EventType        string
	ItemID           string
	SubmissionStatus string
	GradingStatus    string
	DueDate          string
	DueDateParsed    string
	AttemptsAllowed  string
	AttemptState     string
	AttemptGrade     string
	CanAttempt       bool
	NoMoreAttempts   bool
	FileSubmissions  []string
	RawContent       string
	FetchedAt        time.Time
}

type RunRecord struct {
	ID                  int64
	JobName             string
	Owner               string
	StartedAt           time.Time
	FinishedAt          time.Time
	Status              string
	TotalCourses        int
	MatchedCourses      int
	AttendanceFound     int
	AttendanceSubmitted int
	AssignmentsFound    int
	QuizzesFound        int
	NotificationsSent   int
	RemindersSent       int
	DraftsReady         int
	Error               string
}

type OperationalSummary struct {
	Since                time.Time
	Until                time.Time
	RunTotal             int
	RunSuccess           int
	RunFailed            int
	AttendanceSubmitted  int
	AssignmentsFound     int
	QuizzesFound         int
	NotificationsSent    int
	RemindersSent        int
	DraftsReady          int
	LastError            string
	PendingNotifications int
	ReadyDrafts          int
	OpenDrafts           int
}

type NotificationStore struct {
	db *sql.DB
}

func NewNotificationStore(dbPath string) (*NotificationStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = "notifications.db"
	}
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &NotificationStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *NotificationStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *NotificationStore) init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		return fmt.Errorf("sqlite pragma journal_mode: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout=5000;`); err != nil {
		return fmt.Errorf("sqlite pragma busy_timeout: %w", err)
	}

	const createEventsTable = `
CREATE TABLE IF NOT EXISTS notification_events (
id INTEGER PRIMARY KEY AUTOINCREMENT,
event_type TEXT NOT NULL,
course_id INTEGER NOT NULL,
course_name TEXT NOT NULL,
item_title TEXT NOT NULL,
item_name TEXT NOT NULL,
item_link TEXT NOT NULL,
item_id TEXT NOT NULL,
fingerprint TEXT NOT NULL UNIQUE,
notified INTEGER NOT NULL DEFAULT 0 CHECK(notified IN (0, 1)),
attempt_count INTEGER NOT NULL DEFAULT 0,
last_error TEXT NOT NULL DEFAULT '',
first_seen_at TEXT NOT NULL,
notified_at TEXT,
updated_at TEXT NOT NULL,
due_date TEXT NOT NULL DEFAULT '',
submission_status TEXT NOT NULL DEFAULT '',
grade TEXT NOT NULL DEFAULT '',
due_date_parsed TEXT NOT NULL DEFAULT '',
reminder_24h_sent INTEGER NOT NULL DEFAULT 0 CHECK(reminder_24h_sent IN (0, 1)),
reminder_12h_sent INTEGER NOT NULL DEFAULT 0 CHECK(reminder_12h_sent IN (0, 1)),
detail_fetched INTEGER NOT NULL DEFAULT 0 CHECK(detail_fetched IN (0, 1)),
suggestion_status TEXT NOT NULL DEFAULT '',
suggestion_text TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_notification_events_notified_id ON notification_events(notified, id);
CREATE INDEX IF NOT EXISTS idx_notification_events_due_date ON notification_events(due_date_parsed);
CREATE INDEX IF NOT EXISTS idx_notification_events_detail_pending ON notification_events(detail_fetched, id);
CREATE INDEX IF NOT EXISTS idx_notification_events_suggestion_pending ON notification_events(detail_fetched, suggestion_status, id);
`
	if _, err := s.db.ExecContext(ctx, createEventsTable); err != nil {
		return fmt.Errorf("migrate notification_events: %w", err)
	}

	if err := s.ensureNotificationEventColumns(ctx); err != nil {
		return err
	}

	const createItemDetailsTable = `
CREATE TABLE IF NOT EXISTS item_details (
id INTEGER PRIMARY KEY AUTOINCREMENT,
event_id INTEGER NOT NULL REFERENCES notification_events(id),
event_type TEXT NOT NULL,
item_id TEXT NOT NULL,
submission_status TEXT NOT NULL DEFAULT '',
grading_status TEXT NOT NULL DEFAULT '',
due_date TEXT NOT NULL DEFAULT '',
attempts_allowed TEXT NOT NULL DEFAULT '',
attempt_state TEXT NOT NULL DEFAULT '',
attempt_grade TEXT NOT NULL DEFAULT '',
can_attempt INTEGER NOT NULL DEFAULT 0 CHECK(can_attempt IN (0, 1)),
no_more_attempts INTEGER NOT NULL DEFAULT 0 CHECK(no_more_attempts IN (0, 1)),
file_submissions TEXT NOT NULL DEFAULT '',
raw_content TEXT NOT NULL DEFAULT '',
fetched_at TEXT NOT NULL,
UNIQUE(event_type, item_id)
);
`
	if _, err := s.db.ExecContext(ctx, createItemDetailsTable); err != nil {
		return fmt.Errorf("migrate item_details: %w", err)
	}

	const createDraftResultsTable = `
CREATE TABLE IF NOT EXISTS draft_results (
id         INTEGER PRIMARY KEY AUTOINCREMENT,
event_id   INTEGER NOT NULL,
provider   TEXT    NOT NULL,
model      TEXT    NOT NULL DEFAULT '',
draft_text TEXT    NOT NULL DEFAULT '',
tokens_used INTEGER NOT NULL DEFAULT 0,
error      TEXT    NOT NULL DEFAULT '',
attempt    INTEGER NOT NULL DEFAULT 1,
created_at TEXT    NOT NULL,
UNIQUE(event_id, provider)
);
CREATE INDEX IF NOT EXISTS idx_draft_results_event ON draft_results(event_id);
`
	if _, err := s.db.ExecContext(ctx, createDraftResultsTable); err != nil {
		return fmt.Errorf("migrate draft_results: %w", err)
	}

	const createRunLocksTable = `
CREATE TABLE IF NOT EXISTS run_locks (
job_name     TEXT PRIMARY KEY,
owner        TEXT NOT NULL,
locked_until TEXT NOT NULL,
updated_at   TEXT NOT NULL
);
`
	if _, err := s.db.ExecContext(ctx, createRunLocksTable); err != nil {
		return fmt.Errorf("migrate run_locks: %w", err)
	}

	const createAutomationRunsTable = `
CREATE TABLE IF NOT EXISTS automation_runs (
id INTEGER PRIMARY KEY AUTOINCREMENT,
job_name TEXT NOT NULL,
owner TEXT NOT NULL DEFAULT '',
started_at TEXT NOT NULL,
finished_at TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL,
total_courses INTEGER NOT NULL DEFAULT 0,
matched_courses INTEGER NOT NULL DEFAULT 0,
attendance_found INTEGER NOT NULL DEFAULT 0,
attendance_submitted INTEGER NOT NULL DEFAULT 0,
assignments_found INTEGER NOT NULL DEFAULT 0,
quizzes_found INTEGER NOT NULL DEFAULT 0,
notifications_sent INTEGER NOT NULL DEFAULT 0,
reminders_sent INTEGER NOT NULL DEFAULT 0,
drafts_ready INTEGER NOT NULL DEFAULT 0,
error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_automation_runs_started_at ON automation_runs(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_automation_runs_status ON automation_runs(status);
`
	if _, err := s.db.ExecContext(ctx, createAutomationRunsTable); err != nil {
		return fmt.Errorf("migrate automation_runs: %w", err)
	}
	if err := s.initCourseworkTables(ctx); err != nil {
		return err
	}
	return nil
}

func (s *NotificationStore) ensureNotificationEventColumns(ctx context.Context) error {
	toEnsure := []struct {
		column string
		ddl    string
	}{
		{column: "due_date", ddl: "due_date TEXT NOT NULL DEFAULT ''"},
		{column: "submission_status", ddl: "submission_status TEXT NOT NULL DEFAULT ''"},
		{column: "grade", ddl: "grade TEXT NOT NULL DEFAULT ''"},
		{column: "due_date_parsed", ddl: "due_date_parsed TEXT NOT NULL DEFAULT ''"},
		{column: "reminder_24h_sent", ddl: "reminder_24h_sent INTEGER NOT NULL DEFAULT 0 CHECK(reminder_24h_sent IN (0, 1))"},
		{column: "reminder_12h_sent", ddl: "reminder_12h_sent INTEGER NOT NULL DEFAULT 0 CHECK(reminder_12h_sent IN (0, 1))"},
		{column: "detail_fetched", ddl: "detail_fetched INTEGER NOT NULL DEFAULT 0 CHECK(detail_fetched IN (0, 1))"},
		{column: "suggestion_status", ddl: "suggestion_status TEXT NOT NULL DEFAULT ''"},
		{column: "suggestion_text", ddl: "suggestion_text TEXT NOT NULL DEFAULT ''"},
		{column: "draft_status", ddl: "draft_status TEXT NOT NULL DEFAULT ''"},
		{column: "draft_text", ddl: "draft_text TEXT NOT NULL DEFAULT ''"},
		{column: "draft_provider", ddl: "draft_provider TEXT NOT NULL DEFAULT ''"},
		{column: "draft_model", ddl: "draft_model TEXT NOT NULL DEFAULT ''"},
		{column: "draft_updated_at", ddl: "draft_updated_at TEXT"},
	}
	for _, c := range toEnsure {
		if err := s.ensureColumn(ctx, "notification_events", c.column, c.ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationStore) ensureColumn(ctx context.Context, table, column, ddl string) error {
	exists, err := s.columnExists(ctx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", table, ddl)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *NotificationStore) columnExists(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return false, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table_info(%s): %w", table, err)
	}
	return false, nil
}

func (s *NotificationStore) UpsertEvents(ctx context.Context, events []NotificationEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO notification_events (
event_type, course_id, course_name, item_title, item_name, item_link, item_id,
fingerprint, notified, attempt_count, last_error, first_seen_at, updated_at,
due_date, submission_status, grade, due_date_parsed
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, '', ?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint) DO UPDATE SET
course_name = excluded.course_name,
item_title = excluded.item_title,
item_name = excluded.item_name,
item_link = excluded.item_link,
due_date = excluded.due_date,
submission_status = excluded.submission_status,
grade = excluded.grade,
due_date_parsed = excluded.due_date_parsed,
updated_at = excluded.updated_at;
`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ev := range events {
		if err := validateEvent(ev); err != nil {
			return err
		}
		fp := fingerprintForEvent(ev)
		if _, err := stmt.ExecContext(
			ctx,
			ev.EventType,
			ev.CourseID,
			ev.CourseName,
			ev.ItemTitle,
			ev.ItemName,
			ev.ItemLink,
			ev.ItemID,
			fp,
			now,
			now,
			ev.DueDate,
			ev.SubmissionStatus,
			ev.Grade,
			ev.DueDateParsedRFC3339,
		); err != nil {
			return fmt.Errorf("upsert event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert tx: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListPending(ctx context.Context, limit int) ([]PendingNotification, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_type, course_id, course_name, item_title, item_name, item_link, item_id,
attempt_count, last_error, due_date, submission_status, grade, due_date_parsed,
reminder_24h_sent, reminder_12h_sent, detail_fetched, suggestion_status, suggestion_text
FROM notification_events
WHERE notified = 0
ORDER BY id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer rows.Close()

	items := make([]PendingNotification, 0, limit)
	for rows.Next() {
		var item PendingNotification
		var reminder24 int
		var reminder12 int
		var detailFetched int
		if err := rows.Scan(
			&item.ID,
			&item.EventType,
			&item.CourseID,
			&item.CourseName,
			&item.ItemTitle,
			&item.ItemName,
			&item.ItemLink,
			&item.ItemID,
			&item.AttemptCount,
			&item.LastError,
			&item.DueDate,
			&item.SubmissionStatus,
			&item.Grade,
			&item.DueDateParsed,
			&reminder24,
			&reminder12,
			&detailFetched,
			&item.SuggestionStatus,
			&item.SuggestionText,
		); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		item.Reminder24hSent = reminder24 == 1
		item.Reminder12hSent = reminder12 == 1
		item.DetailFetched = detailFetched == 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending: %w", err)
	}
	return items, nil
}

func (s *NotificationStore) ListNeedingDetail(ctx context.Context, limit int) ([]PendingNotification, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_type, course_id, course_name, item_title, item_name, item_link, item_id,
attempt_count, last_error, due_date, submission_status, grade, due_date_parsed,
reminder_24h_sent, reminder_12h_sent, detail_fetched, suggestion_status, suggestion_text
FROM notification_events
WHERE detail_fetched = 0
ORDER BY id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query needing detail: %w", err)
	}
	defer rows.Close()

	items := make([]PendingNotification, 0, limit)
	for rows.Next() {
		var item PendingNotification
		var reminder24 int
		var reminder12 int
		var detailFetched int
		if err := rows.Scan(
			&item.ID,
			&item.EventType,
			&item.CourseID,
			&item.CourseName,
			&item.ItemTitle,
			&item.ItemName,
			&item.ItemLink,
			&item.ItemID,
			&item.AttemptCount,
			&item.LastError,
			&item.DueDate,
			&item.SubmissionStatus,
			&item.Grade,
			&item.DueDateParsed,
			&reminder24,
			&reminder12,
			&detailFetched,
			&item.SuggestionStatus,
			&item.SuggestionText,
		); err != nil {
			return nil, fmt.Errorf("scan needing detail: %w", err)
		}
		item.Reminder24hSent = reminder24 == 1
		item.Reminder12hSent = reminder12 == 1
		item.DetailFetched = detailFetched == 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate needing detail: %w", err)
	}
	return items, nil
}

func (s *NotificationStore) UpdateDetailFetched(ctx context.Context, id int64, submissionStatus, dueDate, dueDateParsed string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET detail_fetched = 1,
submission_status = CASE WHEN ? <> '' THEN ? ELSE submission_status END,
due_date = CASE WHEN ? <> '' THEN ? ELSE due_date END,
due_date_parsed = CASE WHEN ? <> '' THEN ? ELSE due_date_parsed END,
updated_at = ?
WHERE id = ?;
`, submissionStatus, submissionStatus, dueDate, dueDate, dueDateParsed, dueDateParsed, now, id); err != nil {
		return fmt.Errorf("update detail fetched: %w", err)
	}
	return nil
}

func (s *NotificationStore) UpsertItemDetail(ctx context.Context, detail ItemDetailRecord) error {
	if detail.EventType != NotificationTypeAssignment && detail.EventType != NotificationTypeQuiz {
		return fmt.Errorf("unsupported detail event type: %q", detail.EventType)
	}
	if strings.TrimSpace(detail.ItemID) == "" {
		return errors.New("detail item id is required")
	}
	if detail.EventID <= 0 {
		return errors.New("detail event id must be > 0")
	}
	filesJSON := "[]"
	if len(detail.FileSubmissions) > 0 {
		buf, err := json.Marshal(detail.FileSubmissions)
		if err != nil {
			return fmt.Errorf("marshal file submissions: %w", err)
		}
		filesJSON = string(buf)
	}
	fetchedAt := detail.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	fetchedAtRFC := fetchedAt.UTC().Format(time.RFC3339)

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO item_details (
event_id, event_type, item_id, submission_status, grading_status, due_date,
attempts_allowed, attempt_state, attempt_grade, can_attempt, no_more_attempts,
file_submissions, raw_content, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_type, item_id) DO UPDATE SET
event_id = excluded.event_id,
submission_status = excluded.submission_status,
grading_status = excluded.grading_status,
due_date = excluded.due_date,
attempts_allowed = excluded.attempts_allowed,
attempt_state = excluded.attempt_state,
attempt_grade = excluded.attempt_grade,
can_attempt = excluded.can_attempt,
no_more_attempts = excluded.no_more_attempts,
file_submissions = excluded.file_submissions,
raw_content = excluded.raw_content,
fetched_at = excluded.fetched_at;
`,
		detail.EventID,
		detail.EventType,
		detail.ItemID,
		detail.SubmissionStatus,
		detail.GradingStatus,
		detail.DueDate,
		detail.AttemptsAllowed,
		detail.AttemptState,
		detail.AttemptGrade,
		boolToInt(detail.CanAttempt),
		boolToInt(detail.NoMoreAttempts),
		filesJSON,
		detail.RawContent,
		fetchedAtRFC,
	); err != nil {
		return fmt.Errorf("upsert item detail: %w", err)
	}

	if err := s.UpdateDetailFetched(ctx, detail.EventID, detail.SubmissionStatus, detail.DueDate, detail.DueDateParsed); err != nil {
		return err
	}
	return nil
}

func (s *NotificationStore) ListApproachingDeadlines(ctx context.Context, now time.Time, window time.Duration) ([]PendingNotification, error) {
	if window <= 0 {
		return nil, nil
	}
	from := now.UTC().Format(time.RFC3339)
	to := now.UTC().Add(window).Format(time.RFC3339)

	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_type, course_id, course_name, item_title, item_name, item_link, item_id,
attempt_count, last_error, due_date, submission_status, grade, due_date_parsed,
reminder_24h_sent, reminder_12h_sent, detail_fetched, suggestion_status, suggestion_text
FROM notification_events
WHERE due_date_parsed <> ''
AND due_date_parsed > ?
AND due_date_parsed <= ?
AND (submission_status = '' OR lower(submission_status) NOT LIKE '%submitted for grading%')
ORDER BY due_date_parsed ASC;
`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query approaching deadlines: %w", err)
	}
	defer rows.Close()

	out := make([]PendingNotification, 0)
	for rows.Next() {
		var item PendingNotification
		var reminder24 int
		var reminder12 int
		var detailFetched int
		if err := rows.Scan(
			&item.ID,
			&item.EventType,
			&item.CourseID,
			&item.CourseName,
			&item.ItemTitle,
			&item.ItemName,
			&item.ItemLink,
			&item.ItemID,
			&item.AttemptCount,
			&item.LastError,
			&item.DueDate,
			&item.SubmissionStatus,
			&item.Grade,
			&item.DueDateParsed,
			&reminder24,
			&reminder12,
			&detailFetched,
			&item.SuggestionStatus,
			&item.SuggestionText,
		); err != nil {
			return nil, fmt.Errorf("scan approaching deadlines: %w", err)
		}
		item.Reminder24hSent = reminder24 == 1
		item.Reminder12hSent = reminder12 == 1
		item.DetailFetched = detailFetched == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approaching deadlines: %w", err)
	}
	return out, nil
}

func (s *NotificationStore) MarkReminder24hSent(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET reminder_24h_sent = 1, updated_at = ?
WHERE id = ?;
`, now, id); err != nil {
		return fmt.Errorf("mark 24h reminder sent: %w", err)
	}
	return nil
}

func (s *NotificationStore) MarkReminder12hSent(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET reminder_12h_sent = 1, updated_at = ?
WHERE id = ?;
`, now, id); err != nil {
		return fmt.Errorf("mark 12h reminder sent: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListNeedingSuggestion(ctx context.Context, limit int) ([]PendingNotification, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name, ne.item_link, ne.item_id,
ne.attempt_count, ne.last_error, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed,
ne.reminder_24h_sent, ne.reminder_12h_sent, ne.detail_fetched, ne.suggestion_status, ne.suggestion_text,
COALESCE(idt.raw_content, '')
FROM notification_events ne
LEFT JOIN item_details idt ON idt.event_type = ne.event_type AND idt.item_id = ne.item_id
WHERE ne.detail_fetched = 1
AND (ne.suggestion_status = '' OR ne.suggestion_status = 'pending')
AND (ne.submission_status = '' OR lower(ne.submission_status) NOT LIKE '%submitted for grading%')
AND COALESCE(idt.raw_content, '') <> ''
ORDER BY ne.id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query needing suggestion: %w", err)
	}
	defer rows.Close()

	out := make([]PendingNotification, 0, limit)
	for rows.Next() {
		var item PendingNotification
		var reminder24 int
		var reminder12 int
		var detailFetched int
		if err := rows.Scan(
			&item.ID,
			&item.EventType,
			&item.CourseID,
			&item.CourseName,
			&item.ItemTitle,
			&item.ItemName,
			&item.ItemLink,
			&item.ItemID,
			&item.AttemptCount,
			&item.LastError,
			&item.DueDate,
			&item.SubmissionStatus,
			&item.Grade,
			&item.DueDateParsed,
			&reminder24,
			&reminder12,
			&detailFetched,
			&item.SuggestionStatus,
			&item.SuggestionText,
			&item.RawContent,
		); err != nil {
			return nil, fmt.Errorf("scan needing suggestion: %w", err)
		}
		item.Reminder24hSent = reminder24 == 1
		item.Reminder12hSent = reminder12 == 1
		item.DetailFetched = detailFetched == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate needing suggestion: %w", err)
	}
	return out, nil
}

func (s *NotificationStore) UpdateSuggestion(ctx context.Context, id int64, status, text string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET suggestion_status = ?, suggestion_text = ?, updated_at = ?
WHERE id = ?;
`, status, text, now, id); err != nil {
		return fmt.Errorf("update suggestion: %w", err)
	}
	return nil
}

func (s *NotificationStore) MarkNotified(ctx context.Context, id int64, at time.Time) error {
	notifiedAt := at.UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET notified = 1, notified_at = ?, last_error = '', updated_at = ?
WHERE id = ?;
`, notifiedAt, notifiedAt, id); err != nil {
		return fmt.Errorf("mark notified: %w", err)
	}
	return nil
}

func (s *NotificationStore) MarkFailed(ctx context.Context, id int64, sendErr error) error {
	msg := "unknown error"
	if sendErr != nil {
		msg = sendErr.Error()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET attempt_count = attempt_count + 1, last_error = ?, updated_at = ?
WHERE id = ?;
`, msg, now, id); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

func (s *NotificationStore) PruneNotifiedBefore(ctx context.Context, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
DELETE FROM notification_events
WHERE notified = 1 AND notified_at IS NOT NULL AND notified_at < ?;
`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune notified: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune rows affected: %w", err)
	}
	return rows, nil
}

// ListNeedingDraft returns events that have raw_content, are not yet submitted,
// and have draft_status in (”, 'queued').
func (s *NotificationStore) ListNeedingDraft(ctx context.Context, limit int) ([]NotificationEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name,
ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed,
ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
COALESCE(ne.draft_updated_at, ''),
COALESCE(idt.raw_content, '')
FROM notification_events ne
LEFT JOIN item_details idt ON idt.event_type = ne.event_type AND idt.item_id = ne.item_id
WHERE ne.detail_fetched = 1
AND (ne.draft_status = '' OR ne.draft_status = 'queued')
AND (ne.submission_status = '' OR lower(ne.submission_status) NOT LIKE '%submitted for grading%')
AND COALESCE(idt.raw_content, '') <> ''
ORDER BY ne.id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query needing draft: %w", err)
	}
	defer rows.Close()

	out := make([]NotificationEvent, 0, limit)
	for rows.Next() {
		var ev NotificationEvent
		if err := rows.Scan(
			&ev.ID,
			&ev.EventType,
			&ev.CourseID,
			&ev.CourseName,
			&ev.ItemTitle,
			&ev.ItemName,
			&ev.ItemLink,
			&ev.ItemID,
			&ev.DueDate,
			&ev.SubmissionStatus,
			&ev.Grade,
			&ev.DueDateParsedRFC3339,
			&ev.DraftStatus,
			&ev.DraftText,
			&ev.DraftProvider,
			&ev.DraftModel,
			&ev.DraftUpdatedAt,
			&ev.RawContent,
		); err != nil {
			return nil, fmt.Errorf("scan needing draft: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate needing draft: %w", err)
	}
	return out, nil
}

// UpdateDraft sets the draft columns on a notification event.
func (s *NotificationStore) UpdateDraft(ctx context.Context, id int64, status, text, provider, model string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET draft_status = ?, draft_text = ?, draft_provider = ?, draft_model = ?,
draft_updated_at = ?, updated_at = ?
WHERE id = ?;
`, status, text, provider, model, now, now, id); err != nil {
		return fmt.Errorf("update draft: %w", err)
	}
	return nil
}

// MarkDraftOpen promotes a ready draft to 'open' (publicly visible).
func (s *NotificationStore) MarkDraftOpen(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
UPDATE notification_events
SET draft_status = 'open', draft_updated_at = ?, updated_at = ?
WHERE id = ? AND draft_status = 'ready';
`, now, now, id); err != nil {
		return fmt.Errorf("mark draft open: %w", err)
	}
	return nil
}

// ListReadyDrafts returns all events with draft_status in ('ready', 'open').
func (s *NotificationStore) ListReadyDrafts(ctx context.Context) ([]NotificationEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name,
ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed,
ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
COALESCE(ne.draft_updated_at, '')
FROM notification_events ne
WHERE ne.draft_status IN ('ready', 'open')
ORDER BY ne.id DESC;
`)
	if err != nil {
		return nil, fmt.Errorf("query ready drafts: %w", err)
	}
	defer rows.Close()
	return scanDraftEvents(rows)
}

// ListOpenDrafts returns only publicly visible drafts (draft_status = 'open').
func (s *NotificationStore) ListOpenDrafts(ctx context.Context) ([]NotificationEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name,
ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed,
ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
COALESCE(ne.draft_updated_at, '')
FROM notification_events ne
WHERE ne.draft_status = 'open'
ORDER BY ne.id DESC;
`)
	if err != nil {
		return nil, fmt.Errorf("query open drafts: %w", err)
	}
	defer rows.Close()
	return scanDraftEvents(rows)
}

func scanDraftEvents(rows *sql.Rows) ([]NotificationEvent, error) {
	out := make([]NotificationEvent, 0)
	for rows.Next() {
		var ev NotificationEvent
		if err := rows.Scan(
			&ev.ID,
			&ev.EventType,
			&ev.CourseID,
			&ev.CourseName,
			&ev.ItemTitle,
			&ev.ItemName,
			&ev.ItemLink,
			&ev.ItemID,
			&ev.DueDate,
			&ev.SubmissionStatus,
			&ev.Grade,
			&ev.DueDateParsedRFC3339,
			&ev.DraftStatus,
			&ev.DraftText,
			&ev.DraftProvider,
			&ev.DraftModel,
			&ev.DraftUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan draft event: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft events: %w", err)
	}
	return out, nil
}

// GetDraftByID returns a single draft event by its primary key.
func (s *NotificationStore) GetDraftByID(ctx context.Context, id int64) (NotificationEvent, error) {
	var ev NotificationEvent
	err := s.db.QueryRowContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name,
ne.item_link, ne.item_id, ne.due_date, ne.submission_status, ne.grade, ne.due_date_parsed,
ne.draft_status, ne.draft_text, ne.draft_provider, ne.draft_model,
COALESCE(ne.draft_updated_at, '')
FROM notification_events ne
WHERE ne.id = ?;
`, id).Scan(
		&ev.ID,
		&ev.EventType,
		&ev.CourseID,
		&ev.CourseName,
		&ev.ItemTitle,
		&ev.ItemName,
		&ev.ItemLink,
		&ev.ItemID,
		&ev.DueDate,
		&ev.SubmissionStatus,
		&ev.Grade,
		&ev.DueDateParsedRFC3339,
		&ev.DraftStatus,
		&ev.DraftText,
		&ev.DraftProvider,
		&ev.DraftModel,
		&ev.DraftUpdatedAt,
	)
	if err != nil {
		return NotificationEvent{}, fmt.Errorf("get draft by id: %w", err)
	}
	return ev, nil
}

func validateEvent(ev NotificationEvent) error {
	if ev.EventType != NotificationTypeAssignment && ev.EventType != NotificationTypeQuiz {
		return fmt.Errorf("unsupported event type: %q", ev.EventType)
	}
	if ev.CourseID <= 0 {
		return errors.New("course id must be > 0")
	}
	if strings.TrimSpace(ev.CourseName) == "" {
		return errors.New("course name is required")
	}
	if strings.TrimSpace(ev.ItemName) == "" {
		return errors.New("item name is required")
	}
	if strings.TrimSpace(ev.ItemLink) == "" {
		return errors.New("item link is required")
	}
	if strings.TrimSpace(ev.ItemID) == "" {
		return errors.New("item id is required")
	}
	return nil
}

func fingerprintForEvent(ev NotificationEvent) string {
	return fmt.Sprintf("%s:%d:%s", ev.EventType, ev.CourseID, strings.TrimSpace(ev.ItemID))
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ── draft_results ─────────────────────────────────────────────────────────────

// DraftResult holds one provider's draft attempt for a notification event.
type DraftResult struct {
	ID         int64
	EventID    int64
	Provider   string
	Model      string
	DraftText  string
	TokensUsed int
	Error      string
	Attempt    int
	CreatedAt  string
}

// UpsertDraftResult inserts or replaces a per-provider draft result.
func (s *NotificationStore) UpsertDraftResult(ctx context.Context, eventID int64, provider, model, draftText string, tokensUsed int, errStr string, attempt int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO draft_results (event_id, provider, model, draft_text, tokens_used, error, attempt, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_id, provider) DO UPDATE SET
  model       = excluded.model,
  draft_text  = excluded.draft_text,
  tokens_used = excluded.tokens_used,
  error       = excluded.error,
  attempt     = excluded.attempt,
  created_at  = excluded.created_at;
`, eventID, provider, model, draftText, tokensUsed, errStr, attempt, now)
	if err != nil {
		return fmt.Errorf("upsert draft result: %w", err)
	}
	return nil
}

// ListDraftResults returns all per-provider draft results for a given event.
func (s *NotificationStore) ListDraftResults(ctx context.Context, eventID int64) ([]DraftResult, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_id, provider, model, draft_text, tokens_used, error, attempt, created_at
FROM draft_results WHERE event_id = ? ORDER BY provider ASC`, eventID)
	if err != nil {
		return nil, fmt.Errorf("query draft results: %w", err)
	}
	defer rows.Close()
	var out []DraftResult
	for rows.Next() {
		var r DraftResult
		if err := rows.Scan(&r.ID, &r.EventID, &r.Provider, &r.Model, &r.DraftText, &r.TokensUsed, &r.Error, &r.Attempt, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan draft result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *NotificationStore) AcquireRunLock(ctx context.Context, jobName, owner string, ttl time.Duration, now time.Time) (bool, error) {
	if strings.TrimSpace(jobName) == "" {
		return false, errors.New("job name is required")
	}
	if strings.TrimSpace(owner) == "" {
		owner = "unknown"
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	now = now.UTC()
	nowStr := now.Format(time.RFC3339)
	untilStr := now.Add(ttl).Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO run_locks (job_name, owner, locked_until, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(job_name) DO UPDATE SET
owner = excluded.owner,
locked_until = excluded.locked_until,
updated_at = excluded.updated_at
WHERE run_locks.locked_until <= ?;
`, jobName, owner, untilStr, nowStr, nowStr)
	if err != nil {
		return false, fmt.Errorf("acquire run lock: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("acquire run lock rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *NotificationStore) ReleaseRunLock(ctx context.Context, jobName, owner string) error {
	if strings.TrimSpace(jobName) == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM run_locks
WHERE job_name = ? AND owner = ?;
`, jobName, owner); err != nil {
		return fmt.Errorf("release run lock: %w", err)
	}
	return nil
}

func (s *NotificationStore) StartRun(ctx context.Context, rec RunRecord) (int64, error) {
	started := rec.StartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	status := strings.TrimSpace(rec.Status)
	if status == "" {
		status = "running"
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO automation_runs (job_name, owner, started_at, status)
VALUES (?, ?, ?, ?);
`, rec.JobName, rec.Owner, started.UTC().Format(time.RFC3339), status)
	if err != nil {
		return 0, fmt.Errorf("start run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("start run id: %w", err)
	}
	return id, nil
}

func (s *NotificationStore) FinishRun(ctx context.Context, id int64, rec RunRecord) error {
	if id <= 0 {
		return nil
	}
	finished := rec.FinishedAt
	if finished.IsZero() {
		finished = time.Now().UTC()
	}
	status := strings.TrimSpace(rec.Status)
	if status == "" {
		status = "success"
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE automation_runs
SET finished_at = ?, status = ?, total_courses = ?, matched_courses = ?,
attendance_found = ?, attendance_submitted = ?, assignments_found = ?, quizzes_found = ?,
notifications_sent = ?, reminders_sent = ?, drafts_ready = ?, error = ?
WHERE id = ?;
`, finished.UTC().Format(time.RFC3339), status, rec.TotalCourses, rec.MatchedCourses,
		rec.AttendanceFound, rec.AttendanceSubmitted, rec.AssignmentsFound, rec.QuizzesFound,
		rec.NotificationsSent, rec.RemindersSent, rec.DraftsReady, rec.Error, id); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

func (s *NotificationStore) DailyOperationalSummary(ctx context.Context, since, until time.Time) (OperationalSummary, error) {
	summary := OperationalSummary{Since: since, Until: until}
	sinceStr := since.UTC().Format(time.RFC3339)
	untilStr := until.UTC().Format(time.RFC3339)

	row := s.db.QueryRowContext(ctx, `
SELECT
COUNT(*),
COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
COALESCE(SUM(attendance_submitted), 0),
COALESCE(SUM(assignments_found), 0),
COALESCE(SUM(quizzes_found), 0),
COALESCE(SUM(notifications_sent), 0),
COALESCE(SUM(reminders_sent), 0),
COALESCE(SUM(drafts_ready), 0)
FROM automation_runs
WHERE started_at >= ? AND started_at < ?;
`, sinceStr, untilStr)
	if err := row.Scan(
		&summary.RunTotal,
		&summary.RunSuccess,
		&summary.RunFailed,
		&summary.AttendanceSubmitted,
		&summary.AssignmentsFound,
		&summary.QuizzesFound,
		&summary.NotificationsSent,
		&summary.RemindersSent,
		&summary.DraftsReady,
	); err != nil {
		return summary, fmt.Errorf("daily run summary: %w", err)
	}

	var lastErr string
	err := s.db.QueryRowContext(ctx, `
SELECT error
FROM automation_runs
WHERE started_at >= ? AND started_at < ? AND error <> ''
ORDER BY id DESC
LIMIT 1;
`, sinceStr, untilStr).Scan(&lastErr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return summary, fmt.Errorf("daily last error: %w", err)
	}
	summary.LastError = lastErr

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_events WHERE notified = 0;`).Scan(&summary.PendingNotifications); err != nil {
		return summary, fmt.Errorf("pending notifications count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_events WHERE draft_status = 'ready';`).Scan(&summary.ReadyDrafts); err != nil {
		return summary, fmt.Errorf("ready drafts count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_events WHERE draft_status = 'open';`).Scan(&summary.OpenDrafts); err != nil {
		return summary, fmt.Errorf("open drafts count: %w", err)
	}
	return summary, nil
}

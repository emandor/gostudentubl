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
<<<<<<< Updated upstream
	EventType        string
	CourseID         int
	CourseName       string
	ItemTitle        string
	ItemName         string
	ItemLink         string
	ItemID           string
	DueDate          string // raw text
	SubmissionStatus string
	Grade            string
	DueDateParsed    string // RFC3339 or empty
=======
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
>>>>>>> Stashed changes
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
<<<<<<< Updated upstream
	Reminder24hSent  int
	Reminder12hSent  int
	DetailFetched    int
	SuggestionStatus string
	SuggestionText   string
=======
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
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
	// Schema migration: add new columns if they don't exist
	newCols := []struct{ name, def string }{
		{"due_date", "TEXT NOT NULL DEFAULT ''"},
		{"submission_status", "TEXT NOT NULL DEFAULT ''"},
		{"grade", "TEXT NOT NULL DEFAULT ''"},
		{"due_date_parsed", "TEXT"},
		{"reminder_24h_sent", "INTEGER NOT NULL DEFAULT 0"},
		{"reminder_12h_sent", "INTEGER NOT NULL DEFAULT 0"},
		{"detail_fetched", "INTEGER NOT NULL DEFAULT 0"},
		{"suggestion_status", "TEXT NOT NULL DEFAULT ''"},
		{"suggestion_text", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, col := range newCols {
		if !s.columnExists(ctx, "notification_events", col.name) {
			alter := fmt.Sprintf("ALTER TABLE notification_events ADD COLUMN %s %s;", col.name, col.def)
			if _, err := s.db.ExecContext(ctx, alter); err != nil {
				return fmt.Errorf("migrate add column %s: %w", col.name, err)
			}
		}
	}

	// Create item_details table
	const detailsTable = `
CREATE TABLE IF NOT EXISTS item_details (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id INTEGER NOT NULL,
=======
	if err := s.ensureNotificationEventColumns(ctx); err != nil {
		return err
	}

	const createItemDetailsTable = `
CREATE TABLE IF NOT EXISTS item_details (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id INTEGER NOT NULL REFERENCES notification_events(id),
>>>>>>> Stashed changes
	event_type TEXT NOT NULL,
	item_id TEXT NOT NULL,
	submission_status TEXT NOT NULL DEFAULT '',
	grading_status TEXT NOT NULL DEFAULT '',
	due_date TEXT NOT NULL DEFAULT '',
	attempts_allowed TEXT NOT NULL DEFAULT '',
	attempt_state TEXT NOT NULL DEFAULT '',
	attempt_grade TEXT NOT NULL DEFAULT '',
<<<<<<< Updated upstream
	can_attempt INTEGER NOT NULL DEFAULT 0,
	no_more_attempts INTEGER NOT NULL DEFAULT 0,
=======
	can_attempt INTEGER NOT NULL DEFAULT 0 CHECK(can_attempt IN (0, 1)),
	no_more_attempts INTEGER NOT NULL DEFAULT 0 CHECK(no_more_attempts IN (0, 1)),
>>>>>>> Stashed changes
	file_submissions TEXT NOT NULL DEFAULT '',
	raw_content TEXT NOT NULL DEFAULT '',
	fetched_at TEXT NOT NULL,
	UNIQUE(event_type, item_id)
);
`
<<<<<<< Updated upstream
	if _, err := s.db.ExecContext(ctx, detailsTable); err != nil {
		return fmt.Errorf("migrate item_details: %w", err)
	}

	return nil
}

func (s *NotificationStore) columnExists(ctx context.Context, table, column string) bool {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
=======
	if _, err := s.db.ExecContext(ctx, createItemDetailsTable); err != nil {
		return fmt.Errorf("migrate item_details: %w", err)
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
>>>>>>> Stashed changes
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
<<<<<<< Updated upstream
	due_date = CASE WHEN excluded.due_date != '' THEN excluded.due_date ELSE due_date END,
	submission_status = CASE WHEN excluded.submission_status != '' THEN excluded.submission_status ELSE submission_status END,
	grade = CASE WHEN excluded.grade != '' THEN excluded.grade ELSE grade END,
	due_date_parsed = CASE WHEN excluded.due_date_parsed IS NOT NULL AND excluded.due_date_parsed != '' THEN excluded.due_date_parsed ELSE due_date_parsed END,
=======
	due_date = excluded.due_date,
	submission_status = excluded.submission_status,
	grade = excluded.grade,
	due_date_parsed = excluded.due_date_parsed,
>>>>>>> Stashed changes
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
		var dueDateParsed sql.NullString
		if ev.DueDateParsed != "" {
			dueDateParsed = sql.NullString{String: ev.DueDateParsed, Valid: true}
		}
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
<<<<<<< Updated upstream
			dueDateParsed,
=======
			ev.DueDateParsedRFC3339,
>>>>>>> Stashed changes
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
<<<<<<< Updated upstream
       attempt_count, last_error, due_date, submission_status, grade
=======
	attempt_count, last_error, due_date, submission_status, grade, due_date_parsed,
	reminder_24h_sent, reminder_12h_sent, detail_fetched, suggestion_status, suggestion_text
>>>>>>> Stashed changes
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
<<<<<<< Updated upstream
=======
			&item.DueDateParsed,
			&reminder24,
			&reminder12,
			&detailFetched,
			&item.SuggestionStatus,
			&item.SuggestionText,
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
// ListApproachingDeadlines returns unsubmitted items with a parsed due date within the given window.
func (s *NotificationStore) ListApproachingDeadlines(ctx context.Context, now time.Time, window time.Duration) ([]PendingNotification, error) {
	deadline := now.Add(window).UTC().Format(time.RFC3339)
	nowStr := now.UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_type, course_id, course_name, item_title, item_name, item_link, item_id,
       attempt_count, last_error, due_date, submission_status, grade, COALESCE(due_date_parsed,''),
       reminder_24h_sent, reminder_12h_sent
FROM notification_events
WHERE due_date_parsed IS NOT NULL
  AND due_date_parsed != ''
  AND due_date_parsed > ?
  AND due_date_parsed <= ?
  AND submission_status NOT IN ('Submitted for grading')
ORDER BY due_date_parsed ASC;
`, nowStr, deadline)
	if err != nil {
		return nil, fmt.Errorf("query approaching deadlines: %w", err)
	}
	defer rows.Close()

	var out []PendingNotification
	for rows.Next() {
		var item PendingNotification
		if err := rows.Scan(
			&item.ID, &item.EventType, &item.CourseID, &item.CourseName,
			&item.ItemTitle, &item.ItemName, &item.ItemLink, &item.ItemID,
			&item.AttemptCount, &item.LastError, &item.DueDate, &item.SubmissionStatus,
			&item.Grade, &item.DueDateParsed, &item.Reminder24hSent, &item.Reminder12hSent,
		); err != nil {
			return nil, fmt.Errorf("scan approaching deadline: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *NotificationStore) MarkReminder24hSent(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE notification_events SET reminder_24h_sent = 1, updated_at = ? WHERE id = ?;`, now, id)
	return err
}

func (s *NotificationStore) MarkReminder12hSent(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE notification_events SET reminder_12h_sent = 1, updated_at = ? WHERE id = ?;`, now, id)
	return err
}

// ListUnfetchedDetails returns items that haven't had detail pages fetched yet.
func (s *NotificationStore) ListUnfetchedDetails(ctx context.Context, limit int) ([]PendingNotification, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_type, course_id, course_name, item_title, item_name, item_link, item_id,
       attempt_count, last_error, due_date, submission_status, grade
FROM notification_events
WHERE detail_fetched = 0
ORDER BY id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query unfetched details: %w", err)
	}
	defer rows.Close()

	var out []PendingNotification
	for rows.Next() {
		var item PendingNotification
		if err := rows.Scan(
			&item.ID, &item.EventType, &item.CourseID, &item.CourseName,
			&item.ItemTitle, &item.ItemName, &item.ItemLink, &item.ItemID,
			&item.AttemptCount, &item.LastError, &item.DueDate, &item.SubmissionStatus, &item.Grade,
		); err != nil {
			return nil, fmt.Errorf("scan unfetched detail: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *NotificationStore) UpdateDetailFetched(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE notification_events SET detail_fetched = 1, updated_at = ? WHERE id = ?;`, now, id)
	return err
}

func (s *NotificationStore) UpsertItemDetail(ctx context.Context, eventID int64, eventType, itemID, submissionStatus, gradingStatus, dueDate, attemptsAllowed, attemptState, attemptGrade string, canAttempt, noMoreAttempts bool, fileSubmissions, rawContent string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	canAttemptInt := 0
	if canAttempt {
		canAttemptInt = 1
	}
	noMoreInt := 0
	if noMoreAttempts {
		noMoreInt = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO item_details (
	event_id, event_type, item_id, submission_status, grading_status, due_date,
	attempts_allowed, attempt_state, attempt_grade, can_attempt, no_more_attempts,
	file_submissions, raw_content, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_type, item_id) DO UPDATE SET
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
`, eventID, eventType, itemID, submissionStatus, gradingStatus, dueDate,
		attemptsAllowed, attemptState, attemptGrade, canAttemptInt, noMoreInt,
		fileSubmissions, rawContent, now)
	return err
}

// ListNeedingSuggestion returns items that have detail fetched but no suggestion yet.
func (s *NotificationStore) ListNeedingSuggestion(ctx context.Context, limit int) ([]PendingNotification, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT ne.id, ne.event_type, ne.course_id, ne.course_name, ne.item_title, ne.item_name, ne.item_link, ne.item_id,
       ne.attempt_count, ne.last_error, ne.due_date, ne.submission_status, ne.grade,
       COALESCE(ne.suggestion_status,''), COALESCE(ne.suggestion_text,''),
       COALESCE(id2.raw_content, '')
FROM notification_events ne
LEFT JOIN item_details id2 ON ne.event_type = id2.event_type AND ne.item_id = id2.item_id
WHERE ne.detail_fetched = 1
  AND (ne.suggestion_status = '' OR ne.suggestion_status = 'pending')
  AND ne.submission_status NOT IN ('Submitted for grading')
ORDER BY ne.id ASC
LIMIT ?;
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query needing suggestion: %w", err)
	}
	defer rows.Close()

	var out []PendingNotification
	for rows.Next() {
		var item PendingNotification
		var rawContent string
		if err := rows.Scan(
			&item.ID, &item.EventType, &item.CourseID, &item.CourseName,
			&item.ItemTitle, &item.ItemName, &item.ItemLink, &item.ItemID,
			&item.AttemptCount, &item.LastError, &item.DueDate, &item.SubmissionStatus,
			&item.Grade, &item.SuggestionStatus, &item.SuggestionText, &rawContent,
		); err != nil {
			return nil, fmt.Errorf("scan needing suggestion: %w", err)
		}
		// Store raw content in LastError field for convenience (caller uses it)
		if rawContent != "" {
			item.LastError = rawContent
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *NotificationStore) UpdateSuggestion(ctx context.Context, id int64, status, text string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE notification_events SET suggestion_status = ?, suggestion_text = ?, updated_at = ? WHERE id = ?;
`, status, text, now, id)
	return err
=======
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
>>>>>>> Stashed changes
}
